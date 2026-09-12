package runtimetest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimeIngressResponseIDFindsPersistedRequest(t *testing.T) {
	tests := []struct {
		name            string
		status          int
		stream          bool
		planningFailure bool
	}{
		{name: "success", status: http.StatusOK},
		{name: "provider_failure", status: http.StatusNotFound},
		{name: "stream_success", status: http.StatusOK, stream: true},
		{name: "stream_request_provider_failure", status: http.StatusNotFound, stream: true},
		{name: "planning_failure", status: http.StatusServiceUnavailable, planningFailure: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			modelID := "ingress-correlation-" + randomSuffix()
			if test.planningFailure {
				harness.seedModel(t, profileID, "openai", modelID, "native", nil)
				harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
			} else {
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set(runtimeapi.IngressRequestIDHeader, "provider-forged")
					if test.stream && test.status == http.StatusOK {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = io.WriteString(w, "data: {\"id\":\"chatcmpl-test\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(test.status)
					if test.status != http.StatusOK {
						_, _ = io.WriteString(w, `{"error":{"message":"Test model unavailable","type":"not_found"}}`)
						return
					}
					_, _ = io.WriteString(w, `{"id":"chatcmpl-test","choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
				}))
				t.Cleanup(upstream.Close)
				harness.seedProxyRoute(t, runtimeRouteSeed{
					ProfileID: profileID, APIFamily: "openai", PublicModelID: modelID,
					TargetModelID: modelID + "-target", EndpointBaseURL: upstream.URL, EndpointAPIKey: "test-only",
				})
			}
			response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
				"model": modelID, "stream": test.stream,
				"messages": []map[string]any{{"role": "user", "content": "test correlation"}},
			}, map[string]string{runtimeapi.IngressRequestIDHeader: "caller-forged", "X-Request-ID": "caller-trace"})
			assertStatus(t, response, test.status)
			_ = readResponseBody(t, response)
			waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
			assertRuntimeResponseIngressLookup(t, harness, profileID, response, 1)
		})
	}
}

func assertRuntimeResponseIngressLookup(t *testing.T, harness *runtimeHarness, profileID int, response *http.Response, wantRows int) {
	t.Helper()
	ingressID := response.Header.Get(runtimeapi.IngressRequestIDHeader)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(ingressID) {
		t.Fatalf("accepted operation response ID must be a server UUIDv4, got %q", ingressID)
	}
	var requestCount, usageCount int
	if err := harness.conn.QueryRow(context.Background(), `
		SELECT (SELECT count(*) FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2),
		       (SELECT count(*) FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2)`,
		profileID, ingressID).Scan(&requestCount, &usageCount); err != nil {
		t.Fatalf("load retained request/usage identity: %v", err)
	}
	if requestCount != wantRows || usageCount != 1 {
		t.Fatalf("response ID %q must identify %d request rows and one usage event, got %d/%d", ingressID, wantRows, requestCount, usageCount)
	}
	lookup := harness.requestJSON(t, http.MethodGet, "/api/stats/requests?ingress_request_id="+url.QueryEscape(ingressID)+"&limit=5&offset=0", nil, nil)
	assertStatus(t, lookup, http.StatusOK)
	var page struct {
		Items []struct {
			IngressRequestID string `json:"ingress_request_id"`
		} `json:"items"`
	}
	decodeJSONResponse(t, lookup, &page)
	if len(page.Items) != wantRows {
		t.Fatalf("exact response-ID lookup must return %d retained rows, got %+v", wantRows, page)
	}
	for _, item := range page.Items {
		if item.IngressRequestID != ingressID {
			t.Fatalf("exact response-ID lookup returned a different request: %+v", page)
		}
	}
}
