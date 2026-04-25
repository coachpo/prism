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
		CircuitState:       "closed",
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
		ConnectionID:        connectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &priorFailureKind,
		LastCooldownSeconds: 60,
		MaxCooldownStrikes:  1,
		BanMode:             "off",
		OpenUntilAt:         &probeEligibleAt,
		ProbeAvailableAt:    &probeEligibleAt,
		CircuitState:        "open",
		LastLiveFailureKind: &priorFailureKind,
		LastLiveFailureAt:   &probeEligibleAt,
	}, probeEligibleAt, probeEligibleAt)

	response, snapshot := harness.captureJSONRequest(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "phase-2 local circuit recovery"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	snapshot.assertExcludesCategory(t, runtimeSQLCategoryRuntimeStateTables)
	state, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, connectionID)
	if !ok || state.CircuitState != "closed" || state.ConsecutiveFailures != 0 {
		t.Fatalf("expected successful probe to recover local circuit state, got %+v ok=%t", state, ok)
	}
}

func BenchmarkRuntimeLocalAdmissionContention(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-phase2-admission-public-" + randomSuffix(),
		TargetModelID:   "benchmark-phase2-admission-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/benchmark/phase2/admission"),
		EndpointAPIKey:  "benchmark-phase2-admission-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-2 local admission contention")
	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil || statusCode != http.StatusOK {
		b.Fatalf("warm phase-2 local admission benchmark request failed: status=%d err=%v", statusCode, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := runRuntimeBenchmarkStorm(harness.client, harness.url+"/v1/chat/completions", rawBody, runtimePhase0AdmissionContentionConcurrency); err != nil {
			b.Fatalf("run phase-2 local admission contention benchmark: %v", err)
		}
	}
}

func BenchmarkRuntimeLocalRoundRobinContention(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	suffix := randomSuffix()
	strategyID := harness.seedLegacyStrategy(b, profileID, "benchmark-phase2-round-robin-"+suffix, "round-robin")
	publicModelID := "benchmark-phase2-round-robin-public-" + suffix
	targetModelID := "benchmark-phase2-round-robin-target-" + suffix
	targetModelConfigID := harness.seedModel(b, profileID, "gemini", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(b, profileID, "gemini", publicModelID, "proxy", nil)
	harness.seedProxyTarget(b, publicModelConfigID, targetModelConfigID)
	firstEndpointID := harness.seedEndpoint(b, profileID, "benchmark-phase2-round-robin-first-"+suffix, harness.upstream.baseURL("/benchmark/phase2/round-robin/first"), "benchmark-phase2-round-robin-first-key", 0)
	secondEndpointID := harness.seedEndpoint(b, profileID, "benchmark-phase2-round-robin-second-"+suffix, harness.upstream.baseURL("/benchmark/phase2/round-robin/second"), "benchmark-phase2-round-robin-second-key", 1)
	harness.seedConnection(b, profileID, targetModelConfigID, firstEndpointID, "benchmark-phase2-round-robin-connection-a-"+suffix, nil, nil, 0)
	harness.seedConnection(b, profileID, targetModelConfigID, secondEndpointID, "benchmark-phase2-round-robin-connection-b-"+suffix, nil, nil, 1)
	requestURL := fmt.Sprintf("%s/v1beta/models/%s:generateContent", harness.url, publicModelID)
	rawBody := runtimePhase0GeminiBenchmarkRequestBody(b, "phase-2 local round-robin contention")
	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, requestURL, rawBody)
	if err != nil || statusCode != http.StatusOK {
		b.Fatalf("warm phase-2 local round-robin benchmark request failed: status=%d err=%v", statusCode, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, requestURL, rawBody)
		if err != nil || statusCode != http.StatusOK {
			b.Fatalf("run phase-2 local round-robin benchmark request failed: status=%d err=%v", statusCode, err)
		}
	}
}

func BenchmarkRuntimeCircuitFailureRecovery(b *testing.B) {
	harness := newRuntimePhase0HarnessWithOptions(b, runtimePhase0HarnessOptions{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	suffix := randomSuffix()
	primaryUpstream := newRuntimeBenchmarkUpstream(b, http.StatusServiceUnavailable, []byte(`{"error":"phase2 primary unavailable"}`))
	secondaryUpstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	autoRecovery := `{"mode":"enabled","status_codes":[503],"cooldown":{"base_seconds":60,"failure_threshold":1,"backoff_multiplier":2.0,"max_cooldown_seconds":900},"ban":{"mode":"off","max_open_strikes_before_ban":0,"ban_duration_seconds":0}}`
	strategyID := harness.seedLegacyStrategyWithAutoRecovery(b, profileID, "benchmark-phase2-circuit-"+suffix, "fill-first", autoRecovery)
	publicModelID := "benchmark-phase2-circuit-public-" + suffix
	targetModelID := "benchmark-phase2-circuit-target-" + suffix
	targetModelConfigID := harness.seedModel(b, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(b, profileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(b, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(b, profileID, "benchmark-phase2-circuit-primary-"+suffix, primaryUpstream.baseURL("/benchmark/phase2/circuit/primary"), "benchmark-phase2-circuit-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(b, profileID, "benchmark-phase2-circuit-secondary-"+suffix, secondaryUpstream.baseURL("/benchmark/phase2/circuit/secondary"), "benchmark-phase2-circuit-secondary-key", 1)
	harness.seedConnection(b, profileID, targetModelConfigID, primaryEndpointID, "benchmark-phase2-circuit-primary-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(b, profileID, targetModelConfigID, secondaryEndpointID, "benchmark-phase2-circuit-secondary-connection-"+suffix, nil, nil, 1)
	rawBody := runtimeBenchmarkRequestBody(b, publicModelID, "phase-2 circuit failure recovery")
	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil || statusCode != http.StatusOK {
		b.Fatalf("warm phase-2 circuit benchmark request failed: status=%d err=%v", statusCode, err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
		if err != nil || statusCode != http.StatusOK {
			b.Fatalf("run phase-2 circuit benchmark request failed: status=%d err=%v", statusCode, err)
		}
	}
}
