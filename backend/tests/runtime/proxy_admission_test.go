package runtimetest

import (
	"context"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRuntimeAdmissionExhaustionDoesNotIncrementRetries(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "admission-terminal-public-" + suffix
	firstTargetID := "admission-terminal-first-" + suffix
	secondTargetID := "admission-terminal-second-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "admission-terminal-strategy-"+suffix, "fill-first")
	publicConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	firstConfigID := harness.seedModel(t, profileID, "openai", firstTargetID, "native", &strategyID)
	secondConfigID := harness.seedModel(t, profileID, "openai", secondTargetID, "native", &strategyID)
	harness.seedProxyTargetAtPosition(t, publicConfigID, firstConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicConfigID, secondConfigID, 1)
	firstEndpointID := harness.seedEndpoint(t, profileID, "admission-terminal-first-endpoint-"+suffix, harness.upstream.baseURL("/admission/terminal/first"), "admission-terminal-first-key")
	secondEndpointID := harness.seedEndpoint(t, profileID, "admission-terminal-second-endpoint-"+suffix, harness.upstream.baseURL("/admission/terminal/second"), "admission-terminal-second-key")
	firstConnectionID := harness.seedConnection(t, profileID, firstConfigID, firstEndpointID, "admission-terminal-first-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondConfigID, secondEndpointID, "admission-terminal-second-connection-"+suffix, nil, nil, 0)
	maxInFlightNonStream := 1
	harness.updateConnectionAdmissionLimits(t, firstConnectionID, nil, &maxInFlightNonStream, nil)
	harness.seedRuntimeState(t, runtimeStateSeed{ProfileID: profileID, ConnectionID: firstConnectionID, InFlightNonStream: 1, CircuitState: "closed"})
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	harness.upstream.clear()
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(publicModelID, "admission terminal failover"), nil)
	assertStatus(t, response, http.StatusOK)
	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 1 || requests[0].Path != "/admission/terminal/second/v1/chat/completions" {
		t.Fatalf("expected admission exhaustion to fail over to second terminal target, got %+v", requests)
	}
	if got := requestModelID(t, requests[0].Body); got != secondTargetID {
		t.Fatalf("expected fallback request body model %q, got %q", secondTargetID, got)
	}
	state, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, firstConnectionID)
	if !ok || state.CycleRetryAttempts != 0 || state.CumulativeRetryAttempts != 0 || state.NextRetryAt != nil || state.BanMode != "off" {
		t.Fatalf("expected admission rejection to avoid retry-budget failure accounting, got ok=%v state=%+v", ok, state)
	}
}

func TestRuntimeAdmissionSkipsQPSExhaustedConnectionBeforeLaunch(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-loadbalance-admission-qps-"+suffix)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		prefix:     "loadbalance-admission-qps",
		suffix:     suffix,
		strategyID: strategyID,
		endpoints: []selectorEndpointSeed{
			{label: "rejected", baseURL: harness.upstream.baseURL("/loadbalance/admission/qps-rejected"), priority: 0},
			{label: "eligible", baseURL: harness.upstream.baseURL("/loadbalance/admission/qps-eligible"), priority: 1},
		},
	})
	qpsLimit := 1
	harness.updateConnectionAdmissionLimits(t, route.connectionIDs[0], &qpsLimit, nil, nil)
	windowStartedAt := time.Now().UTC()
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:          activeProfileID,
		ConnectionID:       route.connectionIDs[0],
		WindowStartedAt:    &windowStartedAt,
		WindowRequestCount: 1,
		CircuitState:       "closed",
	})

	harness.upstream.clear()
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "qps admission skip"), nil)
	assertStatus(t, response, http.StatusOK)
	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected exactly one upstream request after admission skip fallback, got %d", len(requests))
	}
	if requests[0].Path != "/loadbalance/admission/qps-eligible/v1/chat/completions" {
		t.Fatalf("expected runtime to skip QPS-exhausted connection and use eligible path, got %s", requests[0].Path)
	}
	if requestModelID(t, requests[0].Body) != route.targetModelID {
		t.Fatalf("expected fallback upstream body model %q, got %q", route.targetModelID, requestModelID(t, requests[0].Body))
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, activeProfileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeAdmissionSkipsQPSExhaustedAnthropicConnectionBeforeLaunch(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-anthropic-admission-qps-"+suffix)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		apiFamily:  "anthropic",
		prefix:     "anthropic-admission-qps",
		suffix:     suffix,
		strategyID: strategyID,
		endpoints: []selectorEndpointSeed{
			{label: "rejected", baseURL: harness.upstream.baseURL("/anthropic/admission/qps-rejected"), priority: 0},
			{label: "eligible", baseURL: harness.upstream.baseURL("/anthropic/admission/qps-eligible"), priority: 1},
		},
	})
	qpsLimit := 1
	harness.updateConnectionAdmissionLimits(t, route.connectionIDs[0], &qpsLimit, nil, nil)
	windowStartedAt := time.Now().UTC()
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:          activeProfileID,
		ConnectionID:       route.connectionIDs[0],
		WindowStartedAt:    &windowStartedAt,
		WindowRequestCount: 1,
		CircuitState:       "closed",
	})

	harness.upstream.clear()
	response := harness.requestJSON(t, http.MethodPost, "/v1/messages", anthropicMessagesBody(route.publicModelID, "qps admission skip", 16), nil)
	assertStatus(t, response, http.StatusOK)
	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 1 || requests[0].Path != "/anthropic/admission/qps-eligible/v1/messages" {
		t.Fatalf("expected Anthropic QPS overflow to use eligible upstream, got %+v", requests)
	}
	if requestModelID(t, requests[0].Body) != route.targetModelID {
		t.Fatalf("expected fallback upstream body model %q, got %q", route.targetModelID, requestModelID(t, requests[0].Body))
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, activeProfileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeAdmissionRejectsAllConnectionsBeforeLaunch(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-loadbalance-admission-all-rejected-"+suffix)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		prefix:     "loadbalance-admission-all-rejected",
		suffix:     suffix,
		strategyID: strategyID,
		endpoints:  []selectorEndpointSeed{{label: "only", baseURL: harness.upstream.baseURL("/loadbalance/admission/all-rejected"), priority: 0}},
	})
	maxInFlightNonStream := 1
	harness.updateConnectionAdmissionLimits(t, route.connectionIDs[0], nil, &maxInFlightNonStream, nil)
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:         activeProfileID,
		ConnectionID:      route.connectionIDs[0],
		InFlightNonStream: 1,
		CircuitState:      "closed",
	})

	harness.upstream.clear()
	callerRequestID := "admissioncaller"
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "all admission rejected"), map[string]string{"X-Request-ID": callerRequestID})
	assertStatus(t, response, http.StatusServiceUnavailable)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	detail, _ := payload["detail"].(string)
	if !strings.Contains(detail, "admission limit 'max_in_flight_non_stream'") {
		t.Fatalf("expected deterministic admission rejection detail, got %+v", payload)
	}
	if got, _ := payload["error"].(string); got != "admission_exhausted" {
		t.Fatalf("expected typed admission_exhausted error, got %+v", payload)
	}
	if got, _ := payload["route_reason"].(string); got != "concurrency_overflow" {
		t.Fatalf("expected concurrency_overflow route reason, got %+v", payload)
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected no upstream attempts when all connections are admission-rejected, got %d", got)
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, activeProfileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	var retainedCallerRequestID *string
	var rowKind string
	var errorCode *string
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT caller_request_id, row_kind, error_code
		   FROM request_logs
		  WHERE profile_id = $1`,
		activeProfileID,
	).Scan(&retainedCallerRequestID, &rowKind, &errorCode); err != nil {
		t.Fatalf("load admission telemetry correlation: %v", err)
	}
	if retainedCallerRequestID == nil || *retainedCallerRequestID != callerRequestID {
		t.Fatalf("expected admission telemetry caller_request_id %q, got %+v", callerRequestID, retainedCallerRequestID)
	}
	if rowKind != "admission" || errorCode == nil || *errorCode != "admission_exhausted" {
		t.Fatalf("expected typed admission telemetry, got row_kind=%q error_code=%+v", rowKind, errorCode)
	}
}
