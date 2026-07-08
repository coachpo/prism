package runtime_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func TestRuntimeAdmissionUsesLocalStateOnly(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "phase2-admission-public-" + suffix
	targetModelID := "phase2-admission-target-" + suffix
	strategyID := harness.seedAdaptiveStrategy(t, profileID, "phase2-admission-"+suffix)
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	rejectedEndpointID := harness.seedEndpoint(t, profileID, "phase2-admission-rejected-"+suffix, harness.upstream.baseURL("/phase2/admission/rejected"), "phase2-admission-rejected-key", 0)
	eligibleEndpointID := harness.seedEndpoint(t, profileID, "phase2-admission-eligible-"+suffix, harness.upstream.baseURL("/phase2/admission/eligible"), "phase2-admission-eligible-key", 1)
	rejectedConnectionID := harness.seedConnection(t, profileID, targetModelConfigID, rejectedEndpointID, "phase2-admission-rejected-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, profileID, targetModelConfigID, eligibleEndpointID, "phase2-admission-eligible-connection-"+suffix, nil, nil, 1)
	qpsLimit := 1
	harness.updateConnectionAdmissionLimits(t, rejectedConnectionID, &qpsLimit, nil, nil)
	windowStartedAt := time.Now().UTC()
	harness.runtimeService.RuntimeState().SeedConnectionState(profileID, targetModelConfigID, rejectedConnectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:       rejectedConnectionID,
		BanMode:            "off",
		WindowStartedAt:    &windowStartedAt,
		WindowRequestCount: 1,
	}, windowStartedAt, windowStartedAt)

	response, snapshot := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-2 local admission"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	snapshot.assertExcludesCategory(t, runtimeSQLCategoryRuntimeStateTables)
	if upstreamRequest := harness.upstream.lastRequest(t); upstreamRequest.Path != "/phase2/admission/eligible/v1/chat/completions" {
		t.Fatalf("expected runtime to skip the qps-exhausted connection via local state, got %s", upstreamRequest.Path)
	}
}

func TestRuntimeRoundRobinUsesLocalCursorOnly(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.seedLegacyStrategy(t, profileID, "phase2-round-robin-"+suffix, "round-robin")
	publicModelID := "phase2-round-robin-public-" + suffix
	targetModelID := "phase2-round-robin-target-" + suffix
	targetModelConfigID := harness.seedModel(t, profileID, "gemini", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "gemini", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	firstEndpointID := harness.seedEndpoint(t, profileID, "phase2-round-robin-first-"+suffix, harness.upstream.baseURL("/phase2/round-robin/first"), "phase2-round-robin-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "phase2-round-robin-second-"+suffix, harness.upstream.baseURL("/phase2/round-robin/second"), "phase2-round-robin-second-key", 1)
	_ = harness.seedConnection(t, profileID, targetModelConfigID, firstEndpointID, "phase2-round-robin-first-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, profileID, targetModelConfigID, secondEndpointID, "phase2-round-robin-second-connection-"+suffix, nil, nil, 1)
	requestBody := runtimePhase0GeminiRequest("phase-2 local round robin")

	warmResponse := harness.requestJSON(t, http.MethodPost, fmt.Sprintf("/v1beta/models/%s:generateContent", publicModelID), requestBody, nil)
	assertStatus(t, warmResponse, http.StatusOK)
	response, snapshot := harness.captureJSONRequest(t, http.MethodPost, fmt.Sprintf("/v1beta/models/%s:generateContent", publicModelID), requestBody, nil)
	assertStatus(t, response, http.StatusOK)
	snapshot.assertExcludesCategory(t, runtimeSQLCategoryRoundRobinState)
	if upstreamRequest := harness.upstream.lastRequest(t); upstreamRequest.Path != "/phase2/round-robin/second/v1beta/models/"+targetModelID+":generateContent" {
		t.Fatalf("expected second launch to use the locally advanced round-robin cursor, got %s", upstreamRequest.Path)
	}
	if cursor := harness.runtimeService.RuntimeState().PeekRoundRobinCursor(profileID, targetModelConfigID, 2); cursor != 0 {
		t.Fatalf("expected local round-robin cursor to wrap after two launches, got %d", cursor)
	}
}

func TestRuntimeCircuitRecoveryUsesLocalStateOnly(t *testing.T) {
	harness := newRuntimePhase0Harness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "phase2-circuit-public-" + suffix
	targetModelID := "phase2-circuit-target-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "phase2-circuit-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, profileID, "phase2-circuit-endpoint-"+suffix, harness.upstream.baseURL("/phase2/circuit/recovery"), "phase2-circuit-key", 0)
	connectionID := harness.seedConnection(t, profileID, targetModelConfigID, endpointID, "phase2-circuit-connection-"+suffix, nil, nil, 0)
	probeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	harness.runtimeService.RuntimeState().SeedConnectionState(profileID, targetModelConfigID, connectionID, loadbalancedomain.RuntimeConnectionState{
		ConnectionID:            connectionID,
		CycleRetryAttempts:      1,
		CumulativeRetryAttempts: 1,
		LastFailureKind:         &priorFailureKind,
		LastRetryDelayMS:        60_000,
		BanMode:                 "off",
		NextRetryAt:             &probeEligibleAt,
	}, probeEligibleAt, probeEligibleAt)

	response, snapshot := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-2 local circuit recovery"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	snapshot.assertExcludesCategory(t, runtimeSQLCategoryRuntimeStateTables)
	state, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	if !ok || state.CycleRetryAttempts != 0 || state.CumulativeRetryAttempts != 0 || state.NextRetryAt != nil {
		t.Fatalf("expected success to recover local retry state, got %+v ok=%t", state, ok)
	}
}
