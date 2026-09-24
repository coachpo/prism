package runtimetest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// A request-scoped rejection (the strategy's reroute_status_codes, 400 by
// default) moves the same request to the next candidate without touching the
// rejecting target's health, for every operation shape.

var textOnlyRejection = map[string]any{"error": map[string]any{"message": "image input is not supported", "type": "invalid_request_error"}}

func TestRuntimeRequestScopedRerouteServesEveryOperationShape(t *testing.T) {
	for _, test := range mixedOrderMatrixCases() {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			rejecting := newScriptedUpstream(t, http.StatusBadRequest, textOnlyRejection)
			serving := newRouteMatrixUpstream(t, test.responseContentType, []byte(test.responseBody))
			route := seedSelectorRoute(t, harness, selectorRouteSeed{
				profileID:    profileID,
				apiFamily:    test.apiFamily,
				prefix:       "reroute-matrix",
				strategyType: "fill-first",
				endpoints: []selectorEndpointSeed{
					{label: "text-only", baseURL: rejecting.baseURL("/reroute/text-only"), priority: 0},
					{label: "vision", baseURL: serving.baseURL("/reroute/vision"), priority: 1},
				},
			})

			for request := 1; request <= 2; request++ {
				response := harness.requestJSON(t, http.MethodPost, test.requestPath(route.publicModelID), test.requestBody(route.publicModelID, route.targetModelID), nil)
				assertStatus(t, response, http.StatusOK)
				if body := readResponseBody(t, response); !strings.Contains(body, test.responseContains) {
					t.Fatalf("request %d: expected the next candidate's response containing %q, got %q", request, test.responseContains, body)
				}
				// The rejecting target keeps its place: every ordinary request still tries it first.
				if got, want := len(rejecting.requestsSnapshot()), request; got != want {
					t.Fatalf("request %d: rejecting target received %d requests, want %d", request, got, want)
				}
				assertTargetHealthUntouched(t, harness, profileID, route.connectionIDs[0])
			}
			if got := len(serving.requestsSnapshot()); got != 2 {
				t.Fatalf("expected the next candidate to serve both requests, got %d", got)
			}
			waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 4, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)
			assertLatestRerouteEvidence(t, harness, profileID, []string{"initial:400", "reroute:200"}, "reroute")
		})
	}
}

func TestRuntimeRequestScopedRerouteReturnsLastUpstreamResponse(t *testing.T) {
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	unreachableURL := closed.URL
	closed.Close()
	cases := []struct {
		name              string
		secondUnreachable bool
		wantBody          string
		wantAttempts      []string
	}{
		{name: "every target rejects", wantBody: "second target rejects", wantAttempts: []string{"initial:400", "reroute:400"}},
		{name: "next target is unreachable", secondUnreachable: true, wantBody: "image input is not supported", wantAttempts: []string{"initial:400", "reroute:0"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			harness := newRuntimeHarness(t)
			profileID := harness.activeProfileID(t)
			first := newScriptedUpstream(t, http.StatusBadRequest, textOnlyRejection)
			second := newScriptedUpstream(t, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "second target rejects", "type": "invalid_request_error"}})
			secondURL := second.baseURL("/reroute/second")
			if test.secondUnreachable {
				secondURL = unreachableURL + "/reroute/unreachable"
			}
			route := seedSelectorRoute(t, harness, selectorRouteSeed{
				profileID:    profileID,
				prefix:       "reroute-last",
				strategyType: "fill-first",
				endpoints: []selectorEndpointSeed{
					{label: "first", baseURL: first.baseURL("/reroute/first"), priority: 0},
					{label: "second", baseURL: secondURL, priority: 1},
				},
			})

			response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "reroute last response"), nil)
			assertStatus(t, response, http.StatusBadRequest)
			if body := readResponseBody(t, response); !strings.Contains(body, test.wantBody) {
				t.Fatalf("expected the last upstream response containing %q, got %q", test.wantBody, body)
			}
			if got := len(first.requestsSnapshot()); got != 1 {
				t.Fatalf("expected one attempt per candidate, first target received %d", got)
			}
			assertTargetHealthUntouched(t, harness, profileID, route.connectionIDs[0])
			waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
			assertLatestRerouteEvidence(t, harness, profileID, test.wantAttempts, "")
		})
	}
}

// TestRuntimeImageRequestReroutesPastTextOnlyTarget reproduces the operator
// case behind rerouting: a text-only first target rejects only image requests,
// a vision target serves them, and the next text request still lands on the
// text-only target first.
func TestRuntimeImageRequestReroutesPastTextOnlyTarget(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	textOnly := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body), "image_url") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"image input is not supported","type":"invalid_request_error"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"chatcmpl-text-only"}`))
	}))
	defer textOnly.Close()
	vision := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-vision"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    profileID,
		prefix:       "reroute-image",
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "text-only", baseURL: textOnly.URL + "/reroute/text-only", priority: 0},
			{label: "vision", baseURL: vision.baseURL("/reroute/vision"), priority: 1},
		},
	})

	imageContent := []map[string]any{{"type": "text", "text": "describe this"}, {"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,iVBORw0KGgo="}}}
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.publicModelID, "messages": []map[string]any{{"role": "user", "content": imageContent}}}, nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-vision")
	assertTargetHealthUntouched(t, harness, profileID, route.connectionIDs[0])
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRerouteEvidence(t, harness, profileID, []string{"initial:400", "reroute:200"}, "reroute")

	response = harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "plain text"), nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-text-only")
	if got := len(vision.requestsSnapshot()); got != 1 {
		t.Fatalf("expected the text request to stay on the text-only target, vision received %d requests", got)
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 3, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)
	assertLatestRerouteEvidence(t, harness, profileID, []string{"initial:200"}, "initial")
}

func assertTargetHealthUntouched(t *testing.T, harness *runtimeHarness, profileID int, connectionID int) {
	t.Helper()
	state, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	if ok && (state.NextRetryAt != nil || state.CycleRetryAttempts != 0 || state.CumulativeRetryAttempts != 0 || state.BannedUntilAt != nil) {
		t.Fatalf("expected a request-scoped rejection to leave target %d healthy, got %+v", connectionID, state)
	}
}

// assertLatestRerouteEvidence checks the latest ingress's upstream rows as
// "trigger:status" (0 for no response) and, when wantFinalTrigger is set, the
// usage event's final attempt trigger.
func assertLatestRerouteEvidence(t *testing.T, harness *runtimeHarness, profileID int, want []string, wantFinalTrigger string) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)
	rows, err := harness.conn.Query(context.Background(), `SELECT attempt_trigger || ':' || COALESCE(upstream_status_code, 0), success_flag FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 AND row_kind = 'upstream' ORDER BY attempt_number`, profileID, ingressRequestID)
	if err != nil {
		t.Fatalf("query reroute attempt rows: %v", err)
	}
	defer rows.Close()
	got := []string{}
	for rows.Next() {
		var attempt string
		var success bool
		if err := rows.Scan(&attempt, &success); err != nil {
			t.Fatalf("scan reroute attempt row: %v", err)
		}
		if success && !strings.HasSuffix(attempt, ":200") {
			t.Fatalf("expected a rejected attempt never to count as success, got %s", attempt)
		}
		got = append(got, attempt)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("attempt chain = %v, want %v", got, want)
	}
	t.Logf("ingress %s attempt chain %v", ingressRequestID, got)
	if wantFinalTrigger == "" {
		return
	}
	var finalTrigger string
	if err := harness.conn.QueryRow(context.Background(), `SELECT final_attempt_trigger FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2`, profileID, ingressRequestID).Scan(&finalTrigger); err != nil || finalTrigger != wantFinalTrigger {
		t.Fatalf("final attempt trigger = %q (err %v), want %q", finalTrigger, err, wantFinalTrigger)
	}
}
