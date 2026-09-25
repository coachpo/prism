package runtimetest

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

const streamStartChatErrorStream = "event: error\ndata: {\"error\":{\"message\":\"unknown variant `max`\",\"type\":\"client_error\",\"code\":\"client_error\"}}\n\nevent: done\ndata: [DONE]\n\n"

type streamStartAttemptRow struct {
	AttemptResult  string
	SuccessFlag    bool
	StreamOutcome  string
	FailureStage   string
	ErrorCode      string
	AttemptTrigger string
}

// A provider error first event is an in-band rejection of this request: it
// reroutes to the next candidate and leaves the rejecting target healthy.
func TestRuntimeStreamStartErrorReroutesToNextCandidate(t *testing.T) {
	chatBody := func(model string) any {
		return map[string]any{"model": model, "stream": true, "messages": []map[string]any{{"role": "user", "content": "hi"}}}
	}
	tests := []struct {
		name, apiFamily, path, errorStream, okStream, wantCode string
		body                                                   func(model string) any
	}{
		{"OpenAIChat", "openai", "/v1/chat/completions", streamStartChatErrorStream, openaiStreamUsageChatChunks, "client_error", chatBody},
		{"OpenAIResponses", "openai", "/v1/responses",
			"event: error\ndata: {\"type\":\"error\",\"code\":\"invalid_request_error\",\"message\":\"unsupported effort\"}\n\n",
			"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_ok\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n",
			"invalid_request_error", func(model string) any { return map[string]any{"model": model, "stream": true, "input": "hi"} }},
		{"AnthropicMessages", "anthropic", "/v1/messages",
			"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n",
			"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_ok\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
			"overloaded_error", func(model string) any {
				return map[string]any{"model": model, "stream": true, "max_tokens": 32, "messages": []map[string]any{{"role": "user", "content": "hi"}}}
			}},
		{"GeminiStream", "gemini", "/v1beta/models/%s:streamGenerateContent?alt=sse",
			"data: {\"error\":{\"code\":400,\"message\":\"unsupported thinking level\",\"status\":\"INVALID_ARGUMENT\"}}\n\n",
			"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]}}],\"usageMetadata\":{\"promptTokenCount\":1,\"candidatesTokenCount\":1,\"totalTokenCount\":2}}\n\n",
			"INVALID_ARGUMENT", func(string) any { return runtimePhase0GeminiRequest("hi") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			primary := newRouteMatrixUpstream(t, "text/event-stream", []byte(test.errorStream))
			secondary := newRouteMatrixUpstream(t, "text/event-stream", []byte(test.okStream))
			route := seedSelectorRoute(t, harness, selectorRouteSeed{profileID: profileID, apiFamily: test.apiFamily, prefix: "stream-start-error", strategyType: "fill-first", endpoints: []selectorEndpointSeed{
				{label: "primary", baseURL: primary.baseURL(""), priority: 0},
				{label: "secondary", baseURL: secondary.baseURL(""), priority: 1},
			}})
			path := test.path
			if strings.Contains(path, "%s") {
				path = fmt.Sprintf(path, route.publicModelID)
			}

			response := harness.requestJSON(t, http.MethodPost, path, test.body(route.publicModelID), nil)
			assertStatus(t, response, http.StatusOK)
			if got := readResponseBody(t, response); got != strings.TrimSpace(test.okStream) {
				t.Fatalf("expected the next candidate's stream, got %q", got)
			}
			if len(primary.requestsSnapshot()) != 1 || len(secondary.requestsSnapshot()) != 1 {
				t.Fatalf("expected one request per candidate, got primary=%d secondary=%d", len(primary.requestsSnapshot()), len(secondary.requestsSnapshot()))
			}
			assertTargetHealthUntouched(t, harness, profileID, route.connectionIDs[0])
			want := []streamStartAttemptRow{
				{AttemptResult: "stream_error", StreamOutcome: "provider_incomplete", FailureStage: "stream", ErrorCode: test.wantCode, AttemptTrigger: "initial"},
				{AttemptResult: "completed", SuccessFlag: true, StreamOutcome: "completed", AttemptTrigger: "reroute"},
			}
			if got := loadStreamStartAttemptRows(t, harness, profileID, len(want)); !reflect.DeepEqual(got, want) {
				t.Fatalf("unexpected attempt rows:\n got %+v\nwant %+v", got, want)
			}
		})
	}
}

func TestRuntimeStreamStartInspectionLeavesSelectedStreamsUnchanged(t *testing.T) {
	tests := []struct {
		name          string
		primaryStream string
		withSecondary bool
	}{
		{"healthy first candidate is replayed", openaiStreamUsageChatChunks, true},
		{"error on the last candidate passes through", streamStartChatErrorStream, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			primary := newRouteMatrixUpstream(t, "text/event-stream", []byte(test.primaryStream))
			secondary := newRouteMatrixUpstream(t, "text/event-stream", []byte(openaiStreamNoUsageChatChunks))
			endpoints := []selectorEndpointSeed{{label: "primary", baseURL: primary.baseURL(""), priority: 0}}
			if test.withSecondary {
				endpoints = append(endpoints, selectorEndpointSeed{label: "secondary", baseURL: secondary.baseURL(""), priority: 1})
			}
			route := seedSelectorRoute(t, harness, selectorRouteSeed{profileID: profileID, prefix: "stream-start-keep", strategyType: "fill-first", endpoints: endpoints})

			response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.publicModelID, "stream": true, "messages": []map[string]any{{"role": "user", "content": "hi"}}}, nil)
			assertStatus(t, response, http.StatusOK)
			if got := readResponseBody(t, response); got != strings.TrimSpace(test.primaryStream) {
				t.Fatalf("expected the selected stream byte-for-byte, got %q", got)
			}
			if len(secondary.requestsSnapshot()) != 0 {
				t.Fatalf("expected no request to the secondary candidate, got %d", len(secondary.requestsSnapshot()))
			}
		})
	}
}

func loadStreamStartAttemptRows(t *testing.T, harness *runtimeHarness, profileID int, wantRows int) []streamStartAttemptRow {
	t.Helper()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: wantRows, UsageEvents: 1}, 5*time.Second)
	rows, err := harness.conn.Query(
		context.Background(),
		`SELECT attempt_result, success_flag, stream_outcome, COALESCE(failure_stage, ''), COALESCE(error_code, ''), attempt_trigger
		 FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number`,
		profileID,
		loadLatestRuntimeIngressRequestID(t, harness.conn, profileID),
	)
	if err != nil {
		t.Fatalf("query attempt rows: %v", err)
	}
	defer rows.Close()
	var attempts []streamStartAttemptRow
	for rows.Next() {
		var row streamStartAttemptRow
		if err := rows.Scan(&row.AttemptResult, &row.SuccessFlag, &row.StreamOutcome, &row.FailureStage, &row.ErrorCode, &row.AttemptTrigger); err != nil {
			t.Fatalf("scan attempt row: %v", err)
		}
		attempts = append(attempts, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate attempt rows: %v", err)
	}
	return attempts
}
