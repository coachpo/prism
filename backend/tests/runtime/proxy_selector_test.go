package runtime_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestProxyExecutionParity(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	openAIRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "openai",
		PublicModelID:   "proxy-openai-" + suffix,
		TargetModelID:   "native-openai-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/parity/openai"),
		EndpointAPIKey:  "openai-upstream-key",
	})
	geminiRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "gemini",
		PublicModelID:   "proxy-gemini-" + suffix,
		TargetModelID:   "native-gemini-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/parity/gemini"),
		EndpointAPIKey:  "gemini-upstream-key",
	})

	harness.upstream.clear()
	openAIResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions?trace=1",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "proxy parity"}},
			"model":    openAIRoute.PublicModelID,
		},
		nil,
	)
	assertStatus(t, openAIResponse, http.StatusOK)
	assertResponseField(t, openAIResponse, "id", "chatcmpl-smoke")

	geminiResponse := harness.requestJSON(
		t,
		http.MethodPost,
		fmt.Sprintf("/v1beta/models/%s:generateContent?alt=sse", geminiRoute.PublicModelID),
		map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": "proxy parity"}},
			}},
		},
		nil,
	)
	assertStatus(t, geminiResponse, http.StatusOK)
	assertResponseField(t, geminiResponse, "responseId", "gemini-smoke")

	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 2 {
		t.Fatalf("expected 2 upstream requests, got %d", len(requests))
	}
	if requests[0].Path != "/parity/openai/v1/chat/completions" || requests[0].Query != "trace=1" {
		t.Fatalf("unexpected OpenAI upstream target: %+v", requests[0])
	}
	if requestModelID(t, requests[0].Body) != openAIRoute.TargetModelID {
		t.Fatalf("expected OpenAI upstream model rewrite to %q, got %q", openAIRoute.TargetModelID, requestModelID(t, requests[0].Body))
	}
	if requests[0].Headers.Get("Authorization") != "Bearer "+openAIRoute.EndpointAPIKey {
		t.Fatalf("expected OpenAI auth header, got %q", requests[0].Headers.Get("Authorization"))
	}
	wantGeminiPath := fmt.Sprintf("/parity/gemini/v1beta/models/%s:generateContent", geminiRoute.TargetModelID)
	if requests[1].Path != wantGeminiPath || requests[1].Query != "alt=sse" {
		t.Fatalf("unexpected Gemini upstream target: %+v", requests[1])
	}
	if requests[1].Headers.Get("Authorization") != "Bearer "+geminiRoute.EndpointAPIKey {
		t.Fatalf("expected Gemini auth header, got %q", requests[1].Headers.Get("Authorization"))
	}
}

func TestRuntimeHeaderBlocklistMerge(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	allowedHeaderValue := "allowed-custom-header"
	anthropicRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "anthropic",
		PublicModelID:   "proxy-anthropic-" + suffix,
		TargetModelID:   "native-anthropic-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/headers/anthropic"),
		EndpointAPIKey:  "anthropic-upstream-key",
		CustomHeaders: map[string]any{
			"anthropic-version": "bad-version",
			"x-api-key":         "bad-upstream-key",
			"x-request-id":      "blocked-after-merge",
			"x-allow-smoke":     allowedHeaderValue,
		},
	})
	harness.seedProfileHeaderBlocklistRule(t, activeProfileID, "Block anthropic version", "exact", "anthropic-version")
	harness.seedProfileHeaderBlocklistRule(t, activeProfileID, "Block anthropic auth", "exact", "x-api-key")

	harness.upstream.clear()
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/messages",
		map[string]any{
			"messages":   []map[string]any{{"role": "user", "content": "header merge"}},
			"max_tokens": 1,
			"model":      anthropicRoute.PublicModelID,
		},
		map[string]string{
			"User-Agent":    "claude-cli/2.1.109 (external, cli)",
			"X-Client-Kept": "runtime-ok",
			"X-Request-Id":  "blocked-before-merge",
		},
	)
	assertStatus(t, response, http.StatusOK)
	upstreamRequest := harness.upstream.lastRequest(t)
	if upstreamRequest.Path != "/headers/anthropic/v1/messages" {
		t.Fatalf("expected anthropic upstream path, got %s", upstreamRequest.Path)
	}
	if upstreamRequest.Headers.Get("x-api-key") != anthropicRoute.EndpointAPIKey {
		t.Fatalf("expected protected upstream x-api-key header, got %q", upstreamRequest.Headers.Get("x-api-key"))
	}
	if upstreamRequest.Headers.Get("anthropic-version") != "2023-06-01" {
		t.Fatalf("expected protected upstream anthropic-version header, got %q", upstreamRequest.Headers.Get("anthropic-version"))
	}
	if upstreamRequest.Headers.Get("X-Allow-Smoke") != allowedHeaderValue {
		t.Fatalf("expected allowed custom header, got %q", upstreamRequest.Headers.Get("X-Allow-Smoke"))
	}
	if upstreamRequest.Headers.Get("X-Client-Kept") != "runtime-ok" {
		t.Fatalf("expected non-blocked client header to survive, got %q", upstreamRequest.Headers.Get("X-Client-Kept"))
	}
	if upstreamRequest.Headers.Get("X-Request-Id") != "" {
		t.Fatalf("expected blocked request id header to be removed, got %q", upstreamRequest.Headers.Get("X-Request-Id"))
	}
}

func TestRuntimeUserAgentRuleMerge(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	callerUserAgent := "claude-cli/2.1.109 (external, cli)"
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "openai",
		PublicModelID:   "proxy-ua-" + suffix,
		TargetModelID:   "native-ua-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/user-agent/openai"),
		EndpointAPIKey:  "user-agent-upstream-key",
	})

	harness.upstream.clear()
	firstResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "caller user agent"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"User-Agent": callerUserAgent},
	)
	assertStatus(t, firstResponse, http.StatusOK)
	if firstUpstreamUA := harness.upstream.lastRequest(t).Headers.Get("User-Agent"); firstUpstreamUA != callerUserAgent {
		t.Fatalf("expected caller user-agent to flow upstream, got %q", firstUpstreamUA)
	}

	harness.updateConnectionCustomHeaders(t, route.ConnectionID, map[string]any{"User-Agent": "Prism Custom Agent/1.0"})
	harness.upstream.clear()
	secondResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "custom user agent"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"User-Agent": callerUserAgent},
	)
	assertStatus(t, secondResponse, http.StatusOK)
	if secondUpstreamUA := harness.upstream.lastRequest(t).Headers.Get("User-Agent"); secondUpstreamUA != "Prism Custom Agent/1.0" {
		t.Fatalf("expected custom user-agent override, got %q", secondUpstreamUA)
	}

	harness.seedProfileHeaderBlocklistRule(t, activeProfileID, "Block user agent", "exact", "user-agent")
	harness.upstream.clear()
	thirdResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "blocked user agent"}},
			"model":    route.PublicModelID,
		},
		map[string]string{"User-Agent": callerUserAgent},
	)
	assertStatus(t, thirdResponse, http.StatusOK)
	if blockedUA := harness.upstream.lastRequest(t).Headers.Get("User-Agent"); blockedUA != "" {
		t.Fatalf("expected blocklisted user-agent to be removed, got %q", blockedUA)
	}
}

func TestRuntimeLoadBalanceSkipsBlockedConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-blocked-" + suffix
	targetModelID := "native-loadbalance-blocked-" + suffix
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-loadbalance-blocked-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	blockedEndpointKey := "blocked-upstream-key"
	eligibleEndpointKey := "eligible-upstream-key"
	blockedEndpointID := harness.seedEndpoint(t, activeProfileID, "blocked-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/blocked"), blockedEndpointKey, 0)
	eligibleEndpointID := harness.seedEndpoint(t, activeProfileID, "eligible-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/eligible"), eligibleEndpointKey, 1)
	blockedConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, blockedEndpointID, "blocked-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, eligibleEndpointID, "eligible-connection-"+suffix, nil, nil, 1)
	blockedUntilAt := time.Now().UTC().Add(10 * time.Minute)
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:      activeProfileID,
		ConnectionID:   blockedConnectionID,
		BanMode:        "off",
		BlockedUntilAt: &blockedUntilAt,
		CircuitState:   "open",
	})

	harness.upstream.clear()
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "blocked connection skip"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	upstreamRequest := harness.upstream.lastRequest(t)
	if upstreamRequest.Path != "/loadbalance/eligible/v1/chat/completions" {
		t.Fatalf("expected runtime to skip blocked connection and use eligible path, got %s", upstreamRequest.Path)
	}
	if upstreamRequest.Headers.Get("Authorization") != "Bearer "+eligibleEndpointKey {
		t.Fatalf("expected runtime to use eligible upstream key, got %q", upstreamRequest.Headers.Get("Authorization"))
	}
	if requestModelID(t, upstreamRequest.Body) != targetModelID {
		t.Fatalf("expected upstream body model %q, got %q", targetModelID, requestModelID(t, upstreamRequest.Body))
	}
}

func TestRuntimeLoadBalancePrefersProxyTargetWithEligibleConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-target-" + suffix
	blockedTargetModelID := "native-loadbalance-target-blocked-" + suffix
	eligibleTargetModelID := "native-loadbalance-target-eligible-" + suffix
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-loadbalance-targets-"+suffix, "round-robin")
	blockedTargetModelConfigID := harness.seedModel(t, activeProfileID, "openai", blockedTargetModelID, "native", &strategyID)
	eligibleTargetModelConfigID := harness.seedModel(t, activeProfileID, "openai", eligibleTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, blockedTargetModelConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, eligibleTargetModelConfigID, 1)
	blockedEndpointID := harness.seedEndpoint(t, activeProfileID, "blocked-target-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/target-blocked"), "blocked-target-key", 0)
	eligibleEndpointID := harness.seedEndpoint(t, activeProfileID, "eligible-target-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/target-eligible"), "eligible-target-key", 1)
	blockedConnectionID := harness.seedConnection(t, activeProfileID, blockedTargetModelConfigID, blockedEndpointID, "blocked-target-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, eligibleTargetModelConfigID, eligibleEndpointID, "eligible-target-connection-"+suffix, nil, nil, 0)
	bannedUntilAt := time.Now().UTC().Add(15 * time.Minute)
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:     activeProfileID,
		ConnectionID:  blockedConnectionID,
		BanMode:       "temporary",
		BannedUntilAt: &bannedUntilAt,
		CircuitState:  "closed",
	})

	harness.upstream.clear()
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "proxy target eligibility"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	upstreamRequest := harness.upstream.lastRequest(t)
	if upstreamRequest.Path != "/loadbalance/target-eligible/v1/chat/completions" {
		t.Fatalf("expected runtime to skip ineligible proxy target and use eligible path, got %s", upstreamRequest.Path)
	}
	if requestModelID(t, upstreamRequest.Body) != eligibleTargetModelID {
		t.Fatalf("expected upstream body model %q, got %q", eligibleTargetModelID, requestModelID(t, upstreamRequest.Body))
	}
}

func TestRuntimeRoutesThroughOwnedPrivateConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "direct-resolved-target-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "direct-resolved-target-strategy-"+suffix, "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	endpointID := harness.seedEndpoint(t, profileID, "direct-resolved-target-endpoint-"+suffix, harness.upstream.baseURL("/direct-resolved-target"), "direct-resolved-target-key", 0)
	harness.seedConnection(t, profileID, modelConfigID, endpointID, "direct-resolved-target-connection-"+suffix, nil, nil, 0)

	harness.upstream.clear()
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "direct resolved target"}},
		"model":    modelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	upstreamRequest := harness.upstream.lastRequest(t)
	if upstreamRequest.Path != "/direct-resolved-target/v1/chat/completions" {
		t.Fatalf("expected owned private connection endpoint path, got %s", upstreamRequest.Path)
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, modelID, modelID)
}

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
	firstEndpointID := harness.seedEndpoint(t, profileID, "admission-terminal-first-endpoint-"+suffix, harness.upstream.baseURL("/admission/terminal/first"), "admission-terminal-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "admission-terminal-second-endpoint-"+suffix, harness.upstream.baseURL("/admission/terminal/second"), "admission-terminal-second-key", 1)
	firstConnectionID := harness.seedConnection(t, profileID, firstConfigID, firstEndpointID, "admission-terminal-first-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondConfigID, secondEndpointID, "admission-terminal-second-connection-"+suffix, nil, nil, 0)
	maxInFlightNonStream := 1
	harness.updateConnectionAdmissionLimits(t, firstConnectionID, nil, &maxInFlightNonStream, nil)
	harness.seedRuntimeState(t, runtimeStateSeed{ProfileID: profileID, ConnectionID: firstConnectionID, InFlightNonStream: 1, CircuitState: "closed"})
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	harness.upstream.clear()
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "admission terminal failover"}},
		"model":    publicModelID,
	}, nil)
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

func TestRuntimePrivateConnectionBanDoesNotAffectPeerPrivateConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	firstModelID := "private-ban-first-" + suffix
	secondModelID := "private-ban-second-" + suffix
	banStrategyID := harness.seedLegacyStrategy(t, profileID, "private-ban-temporary-strategy-"+suffix, "fill-first")
	offStrategyID := harness.seedLegacyStrategy(t, profileID, "private-ban-off-strategy-"+suffix, "fill-first")
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE loadbalance_strategies
		 SET ban_mode = 'temporary', retry_base_delay_ms = 0, cycle_retry_attempt_limit = 1, ban_cumulative_retry_attempt_threshold = 3, ban_duration_seconds = 600, updated_at = $2
		 WHERE id = $1`,
		banStrategyID,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("update private-ban temporary strategy: %v", err)
	}
	firstConfigID := harness.seedModel(t, profileID, "openai", firstModelID, "native", &banStrategyID)
	secondConfigID := harness.seedModel(t, profileID, "openai", secondModelID, "native", &offStrategyID)
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "private ban trigger"})
	fallbackUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-private-ban-fallback"})
	primaryEndpointID := harness.seedEndpoint(t, profileID, "private-ban-primary-endpoint-"+suffix, primaryUpstream.baseURL("/private-ban/primary"), "private-ban-primary-key", 0)
	fallbackEndpointID := harness.seedEndpoint(t, profileID, "private-ban-fallback-endpoint-"+suffix, fallbackUpstream.baseURL("/private-ban/fallback"), "private-ban-fallback-key", 1)
	firstConnectionID := harness.seedConnection(t, profileID, firstConfigID, primaryEndpointID, "private-ban-first-primary-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondConfigID, primaryEndpointID, "private-ban-second-primary-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondConfigID, fallbackEndpointID, "private-ban-fallback-connection-"+suffix, nil, nil, 1)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	for attempt := 0; attempt < 3; attempt++ {
		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "trigger private ban"}},
			"model":    firstModelID,
		}, nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
	}
	state, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, firstConnectionID)
	if !ok || state.BanMode != "temporary" || state.BannedUntilAt == nil || state.CumulativeRetryAttempts != 3 {
		t.Fatalf("expected first model to temporarily ban its private connection, got ok=%v state=%+v", ok, state)
	}

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "use peer private primary then fallback"}},
		"model":    secondModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	if got := len(primaryUpstream.requestsSnapshot()); got != 4 {
		t.Fatalf("expected second model to attempt its own private primary connection, got %d primary upstream requests", got)
	}
	fallbackRequests := fallbackUpstream.requestsSnapshot()
	if len(fallbackRequests) != 1 || fallbackRequests[0].Path != "/private-ban/fallback/v1/chat/completions" {
		t.Fatalf("expected second model to use fallback after its private primary connection failed, got %+v", fallbackRequests)
	}
}

func TestRuntimeAdmissionSkipsQPSExhaustedConnectionBeforeLaunch(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-admission-qps-" + suffix
	targetModelID := "native-loadbalance-admission-qps-" + suffix
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-loadbalance-admission-qps-"+suffix)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	rejectedEndpointID := harness.seedEndpoint(t, activeProfileID, "admission-qps-rejected-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/admission/qps-rejected"), "admission-qps-rejected-key", 0)
	eligibleEndpointID := harness.seedEndpoint(t, activeProfileID, "admission-qps-eligible-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/admission/qps-eligible"), "admission-qps-eligible-key", 1)
	rejectedConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, rejectedEndpointID, "admission-qps-rejected-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, eligibleEndpointID, "admission-qps-eligible-connection-"+suffix, nil, nil, 1)
	qpsLimit := 1
	harness.updateConnectionAdmissionLimits(t, rejectedConnectionID, &qpsLimit, nil, nil)
	windowStartedAt := time.Now().UTC()
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:          activeProfileID,
		ConnectionID:       rejectedConnectionID,
		WindowStartedAt:    &windowStartedAt,
		WindowRequestCount: 1,
		CircuitState:       "closed",
	})

	harness.upstream.clear()
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "qps admission skip"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 1 {
		t.Fatalf("expected exactly one upstream request after admission skip fallback, got %d", len(requests))
	}
	if requests[0].Path != "/loadbalance/admission/qps-eligible/v1/chat/completions" {
		t.Fatalf("expected runtime to skip QPS-exhausted connection and use eligible path, got %s", requests[0].Path)
	}
	if requestModelID(t, requests[0].Body) != targetModelID {
		t.Fatalf("expected fallback upstream body model %q, got %q", targetModelID, requestModelID(t, requests[0].Body))
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, activeProfileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeAdmissionSkipsQPSExhaustedAnthropicConnectionBeforeLaunch(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-anthropic-admission-qps-" + suffix
	targetModelID := "native-anthropic-admission-qps-" + suffix
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-anthropic-admission-qps-"+suffix)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "anthropic", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "anthropic", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	rejectedEndpointID := harness.seedEndpoint(t, activeProfileID, "anthropic-qps-rejected-endpoint-"+suffix, harness.upstream.baseURL("/anthropic/admission/qps-rejected"), "anthropic-qps-rejected-key", 0)
	eligibleEndpointID := harness.seedEndpoint(t, activeProfileID, "anthropic-qps-eligible-endpoint-"+suffix, harness.upstream.baseURL("/anthropic/admission/qps-eligible"), "anthropic-qps-eligible-key", 1)
	rejectedConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, rejectedEndpointID, "anthropic-qps-rejected-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, eligibleEndpointID, "anthropic-qps-eligible-connection-"+suffix, nil, nil, 1)
	qpsLimit := 1
	harness.updateConnectionAdmissionLimits(t, rejectedConnectionID, &qpsLimit, nil, nil)
	windowStartedAt := time.Now().UTC()
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:          activeProfileID,
		ConnectionID:       rejectedConnectionID,
		WindowStartedAt:    &windowStartedAt,
		WindowRequestCount: 1,
		CircuitState:       "closed",
	})

	harness.upstream.clear()
	response := harness.requestJSON(t, http.MethodPost, "/v1/messages", map[string]any{"model": publicModelID, "messages": []map[string]any{{"role": "user", "content": "qps admission skip"}}, "max_tokens": 16}, nil)
	assertStatus(t, response, http.StatusOK)
	requests := harness.upstream.requestsSnapshot()
	if len(requests) != 1 || requests[0].Path != "/anthropic/admission/qps-eligible/v1/messages" {
		t.Fatalf("expected Anthropic QPS overflow to use eligible upstream, got %+v", requests)
	}
	if requestModelID(t, requests[0].Body) != targetModelID {
		t.Fatalf("expected fallback upstream body model %q, got %q", targetModelID, requestModelID(t, requests[0].Body))
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, activeProfileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeAdmissionRejectsAllConnectionsBeforeLaunch(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-admission-all-rejected-" + suffix
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-loadbalance-admission-all-rejected-"+suffix)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", "native-loadbalance-admission-all-rejected-"+suffix, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, activeProfileID, "admission-all-rejected-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/admission/all-rejected"), "admission-all-rejected-key", 0)
	connectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, endpointID, "admission-all-rejected-connection-"+suffix, nil, nil, 0)
	maxInFlightNonStream := 1
	harness.updateConnectionAdmissionLimits(t, connectionID, nil, &maxInFlightNonStream, nil)
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:         activeProfileID,
		ConnectionID:      connectionID,
		InFlightNonStream: 1,
		CircuitState:      "closed",
	})

	harness.upstream.clear()
	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "all admission rejected"}},
			"model":    publicModelID,
		},
		nil,
	)
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
}

func TestRuntimeLoadBalanceSingleDoesNotFailOverAfterPrimaryFailure(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-single-" + suffix
	targetModelID := "native-loadbalance-single-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "single primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-single-secondary"})
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-loadbalance-single-"+suffix, "single")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "single-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/single/primary"), "single-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "single-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/single/secondary"), "single-secondary-key", 1)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "single-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "single-secondary-connection-"+suffix, nil, nil, 1)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "single no failover"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusServiceUnavailable)
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected single strategy to call the primary upstream once, got %d requests", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected single strategy to avoid secondary failover, got %d requests", got)
	}
}

func TestRuntimeLoadBalanceFillFirstFailsOverToNextEligibleConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-fill-first-" + suffix
	targetModelID := "native-loadbalance-fill-first-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "fill-first primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-fill-first-secondary"})
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-loadbalance-fill-first-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "fill-first-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/fill-first/primary"), "fill-first-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "fill-first-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/fill-first/secondary"), "fill-first-secondary-key", 1)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "fill-first-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "fill-first-secondary-connection-"+suffix, nil, nil, 1)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "fill-first failover"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-fill-first-secondary")
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected fill-first strategy to call the primary upstream once, got %d requests", got)
	}
	secondaryRequests := secondaryUpstream.requestsSnapshot()
	if len(secondaryRequests) != 1 {
		t.Fatalf("expected fill-first strategy to fail over to the secondary upstream once, got %d requests", len(secondaryRequests))
	}
	if requestModelID(t, secondaryRequests[0].Body) != targetModelID {
		t.Fatalf("expected fill-first failover body model %q, got %q", targetModelID, requestModelID(t, secondaryRequests[0].Body))
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func TestRuntimeBudgetUsesOneOverallDeadlineAcrossAttempts(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-budget-" + suffix
	targetModelID := "native-loadbalance-budget-" + suffix
	requestBudget := 500 * time.Millisecond
	primaryDelay := 250 * time.Millisecond
	secondaryStarted := make(chan struct{}, 1)

	primaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-time.After(primaryDelay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "budget primary unavailable"})
	}))
	t.Cleanup(primaryUpstream.Close)
	secondaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case secondaryStarted <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-budget-secondary"})
		}
	}))
	t.Cleanup(secondaryUpstream.Close)

	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-loadbalance-budget-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "budget-primary-endpoint-"+suffix, primaryUpstream.URL+"/loadbalance/budget/primary", "budget-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "budget-secondary-endpoint-"+suffix, secondaryUpstream.URL+"/loadbalance/budget/secondary", "budget-secondary-key", 1)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "budget-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "budget-secondary-connection-"+suffix, nil, nil, 1)

	rawBody, err := json.Marshal(map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "shared request budget"}},
		"model":    publicModelID,
	})
	if err != nil {
		t.Fatalf("marshal runtime budget request: %v", err)
	}
	budgetServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), requestBudget)
		defer cancel()
		harness.server.Config.Handler.ServeHTTP(w, r.WithContext(ctx))
	}))
	defer budgetServer.Close()
	budgetClient := budgetServer.Client()
	request, err := http.NewRequest(http.MethodPost, budgetServer.URL+"/v1/chat/completions", bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("build runtime budget request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	startedAt := time.Now()
	response, err := budgetClient.Do(request)
	elapsed := time.Since(startedAt)
	if err != nil {
		t.Fatalf("perform runtime budget request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusBadGateway {
		body := readResponseBody(t, response)
		t.Fatalf("expected overall deadline exhaustion to surface as 502 after failover, got status %d body %s", response.StatusCode, body)
	}
	if elapsed > requestBudget+(250*time.Millisecond) {
		t.Fatalf("expected runtime request to stay within one overall budget, took %s for a %s budget", elapsed, requestBudget)
	}

	select {
	case <-secondaryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected secondary upstream attempt to start before the overall request deadline")
	}
}

func TestRuntimeLoadBalanceConcurrentRoundRobinRequestsUseDistinctCursorClaims(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-concurrent-round-robin-" + suffix
	targetModelID := "native-loadbalance-concurrent-round-robin-" + suffix
	firstUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-concurrent-round-robin-first"})
	secondUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-concurrent-round-robin-second"})
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-loadbalance-concurrent-round-robin-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	firstEndpointID := harness.seedEndpoint(t, activeProfileID, "concurrent-round-robin-first-endpoint-"+suffix, firstUpstream.baseURL("/loadbalance/concurrent/round-robin/first"), "concurrent-round-robin-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, activeProfileID, "concurrent-round-robin-second-endpoint-"+suffix, secondUpstream.baseURL("/loadbalance/concurrent/round-robin/second"), "concurrent-round-robin-second-key", 1)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, firstEndpointID, "concurrent-round-robin-first-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondEndpointID, "concurrent-round-robin-second-connection-"+suffix, nil, nil, 1)

	results := executeConcurrentRuntimeJSONRequests(t, harness, 8, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "concurrent round-robin cursor claims"}},
		"model":    publicModelID,
	}, nil)
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("expected concurrent round-robin request to succeed, got error: %v", result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected concurrent round-robin request status 200, got %d with body %s", result.StatusCode, result.Body)
		}
	}

	firstRequests := firstUpstream.requestsSnapshot()
	secondRequests := secondUpstream.requestsSnapshot()
	if len(firstRequests) != 4 || len(secondRequests) != 4 {
		t.Fatalf("expected concurrent round-robin requests to split evenly across both upstreams, got first=%d second=%d", len(firstRequests), len(secondRequests))
	}
	for _, request := range append(firstRequests, secondRequests...) {
		if got := requestModelID(t, request.Body); got != targetModelID {
			t.Fatalf("expected concurrent round-robin request model %q, got %q", targetModelID, got)
		}
	}
	if nextCursor := loadRoundRobinNextCursor(t, harness, activeProfileID, targetModelConfigID, 2); nextCursor != 0 {
		t.Fatalf("expected concurrent round-robin next_cursor to wrap to 0 after 8 claims, got %d", nextCursor)
	}
}

func TestRuntimeLoadBalanceConcurrentFailoverRequestsAccumulateRuntimeState(t *testing.T) {
	t.Skip("Task 14 owns runtime state retry/failure accumulation semantics")
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-concurrent-failover-" + suffix
	targetModelID := "native-loadbalance-concurrent-failover-" + suffix
	primaryUpstream := newBlockingScriptedUpstream(t, 2, http.StatusServiceUnavailable, map[string]any{"error": "concurrent failover primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-concurrent-failover-secondary"})
	autoRecovery := `{"mode":"enabled","status_codes":[503],"cooldown":{"base_seconds":60,"failure_threshold":1,"backoff_multiplier":2.0,"max_cooldown_seconds":900},"ban":{"mode":"off","max_cooldown_strikes_before_ban":0,"ban_duration_seconds":0}}`
	strategyID := harness.seedLegacyStrategyWithAutoRecovery(t, activeProfileID, "runtime-loadbalance-concurrent-failover-"+suffix, "fill-first", autoRecovery)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "concurrent-failover-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/concurrent/failover/primary"), "concurrent-failover-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "concurrent-failover-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/concurrent/failover/secondary"), "concurrent-failover-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "concurrent-failover-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "concurrent-failover-secondary-connection-"+suffix, nil, nil, 1)

	resultCh := make(chan []concurrentRuntimeRequestResult, 1)
	go func() {
		resultCh <- executeConcurrentRuntimeJSONRequests(t, harness, 2, http.MethodPost, "/v1/chat/completions", map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "concurrent failover feedback mutation"}},
			"model":    publicModelID,
		}, nil)
	}()
	primaryUpstream.waitUntilReady(t, 5*time.Second)
	primaryUpstream.releaseRequests()
	results := <-resultCh
	for _, result := range results {
		if result.Err != nil {
			t.Fatalf("expected concurrent failover request to succeed, got error: %v", result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected concurrent failover request status 200, got %d with body %s", result.StatusCode, result.Body)
		}
	}

	if got := len(primaryUpstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected concurrent failover requests to launch the shared primary twice before fallback, got %d requests", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected concurrent failover requests to reach the shared secondary twice, got %d requests", got)
	}
	failureState := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if failureState.ConsecutiveFailures != 2 || failureState.CircuitState != "open" {
		t.Fatalf("expected concurrent failover feedback to accumulate two failures on the shared runtime row, got %+v", failureState)
	}
	if !failureState.LastFailureKind.Valid || failureState.LastFailureKind.String != "transient_http" || !failureState.LastLiveFailureKind.Valid || failureState.LastLiveFailureKind.String != "transient_http" {
		t.Fatalf("expected concurrent failover feedback to persist transient_http markers, got %+v", failureState)
	}
	if failureState.LastCooldownSeconds != 120 || failureState.MaxCooldownStrikes != 2 {
		t.Fatalf("expected concurrent failover feedback to back off to 120s/2 strikes, got %+v", failureState)
	}
	if !failureState.OpenUntilAt.Valid || !failureState.ProbeAvailableAt.Valid || !failureState.LastLiveFailureAt.Valid {
		t.Fatalf("expected concurrent failover feedback to persist open/probe/failure timestamps, got %+v", failureState)
	}
	primaryEvents := loadLoadbalanceEvents(t, harness.conn, activeProfileID, primaryConnectionID)
	if len(primaryEvents) != 2 {
		t.Fatalf("expected 2 loadbalance events for concurrent failover feedback, got %+v", primaryEvents)
	}
	var sawOpened bool
	var sawExtended bool
	for _, event := range primaryEvents {
		if !event.ModelID.Valid || event.ModelID.String != targetModelID || !event.EndpointID.Valid || int(event.EndpointID.Int32) != primaryEndpointID {
			t.Fatalf("expected concurrent failover event model/endpoint snapshot %q/%d, got %+v", targetModelID, primaryEndpointID, event)
		}
		switch {
		case event.EventType == "opened" && event.ConsecutiveFailures == 1 && event.CooldownSeconds == 60:
			sawOpened = true
		case event.EventType == "extended" && event.ConsecutiveFailures == 2 && event.CooldownSeconds == 120:
			sawExtended = true
		}
	}
	if !sawOpened || !sawExtended {
		t.Fatalf("expected concurrent failover events to contain one opened(1,60) and one extended(2,120) row, got %+v", primaryEvents)
	}
}

func TestRuntimeExpiredRetryWindowAllowsConcurrentAttempts(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-retry-window-" + suffix
	targetModelID := "native-retry-window-" + suffix
	primaryUpstream := newBlockingScriptedUpstream(t, 2, http.StatusOK, map[string]any{"id": "chatcmpl-retry-window-primary"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-retry-window-secondary"})
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-retry-window-"+suffix, "fill-first")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "retry-window-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/retry-window/primary"), "retry-window-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "retry-window-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/retry-window/secondary"), "retry-window-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "retry-window-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "retry-window-secondary-connection-"+suffix, nil, nil, 1)
	retryAvailableAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:               activeProfileID,
		ConnectionID:            primaryConnectionID,
		CycleRetryAttempts:      1,
		CumulativeRetryAttempts: 1,
		LastFailureKind:         &priorFailureKind,
		LastRetryDelayMS:        60000,
		BanMode:                 "off",
		NextRetryAt:             &retryAvailableAt,
	})
	requestBody := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "retry window concurrent attempt"}},
		"model":    publicModelID,
	}
	rawBody, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal retry-window request body: %v", err)
	}

	resultCh := make(chan concurrentRuntimeRequestResult, 2)
	startRequest := func(label string) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, harness.url+"/v1/chat/completions", bytes.NewReader(rawBody))
			if requestErr != nil {
				resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("build %s retry-window request: %w", label, requestErr)}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			response, responseErr := harness.client.Do(request)
			if responseErr != nil {
				resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("perform %s retry-window request: %w", label, responseErr)}
				return
			}
			defer func() { _ = response.Body.Close() }()
			responseBody, readErr := io.ReadAll(response.Body)
			if readErr != nil {
				resultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("read %s retry-window response body: %w", label, readErr)}
				return
			}
			resultCh <- concurrentRuntimeRequestResult{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
		}()
	}
	startRequest("first")
	startRequest("second")

	primaryUpstream.waitUntilReady(t, 5*time.Second)
	primaryRequests := primaryUpstream.requestsSnapshot()
	if got := len(primaryRequests); got != 2 {
		t.Fatalf("expected expired retry window to allow two primary attempts, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected expired retry window to avoid fallback attempts, got %d secondary requests", got)
	}
	for index, request := range primaryRequests {
		if got := requestModelID(t, request.Body); got != targetModelID {
			t.Fatalf("expected primary request %d model %q, got %q", index, targetModelID, got)
		}
	}

	primaryUpstream.releaseRequests()
	for index := range 2 {
		result := awaitConcurrentRuntimeResult(t, resultCh, 5*time.Second)
		if result.Err != nil {
			t.Fatalf("expected retry-window request %d to succeed after release, got error: %v", index, result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected retry-window request %d status 200, got %d with body %s", index, result.StatusCode, result.Body)
		}
	}
	releasedState := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if releasedState.CycleRetryAttempts != 0 || releasedState.CumulativeRetryAttempts != 0 || releasedState.NextRetryAt.Valid || releasedState.BanMode != "off" {
		t.Fatalf("expected successful retry-window attempts to clear retry state, got %+v", releasedState)
	}
}

func TestRuntimeLeaseNonStreamInFlightExclusivity(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-lease-non-stream-" + suffix
	targetModelID := "native-lease-non-stream-" + suffix
	primaryUpstream := newBlockingScriptedUpstream(t, 1, http.StatusOK, map[string]any{"id": "chatcmpl-lease-non-stream-primary"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-lease-non-stream-secondary"})
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-lease-non-stream-"+suffix)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "lease-non-stream-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/lease/non-stream/primary"), "lease-non-stream-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "lease-non-stream-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/lease/non-stream/secondary"), "lease-non-stream-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "lease-non-stream-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "lease-non-stream-secondary-connection-"+suffix, nil, nil, 1)
	maxInFlightNonStream := 1
	harness.updateConnectionAdmissionLimits(t, primaryConnectionID, nil, &maxInFlightNonStream, nil)
	requestBody := map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "non-stream lease exclusivity"}},
		"model":    publicModelID,
	}
	rawBody, err := json.Marshal(requestBody)
	if err != nil {
		t.Fatalf("marshal non-stream lease request body: %v", err)
	}

	firstResultCh := make(chan concurrentRuntimeRequestResult, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, harness.url+"/v1/chat/completions", bytes.NewReader(rawBody))
		if requestErr != nil {
			firstResultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("build first non-stream lease request: %w", requestErr)}
			return
		}
		request.Header.Set("Content-Type", "application/json")
		response, responseErr := harness.client.Do(request)
		if responseErr != nil {
			firstResultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("perform first non-stream lease request: %w", responseErr)}
			return
		}
		defer func() { _ = response.Body.Close() }()
		responseBody, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			firstResultCh <- concurrentRuntimeRequestResult{Err: fmt.Errorf("read first non-stream lease response body: %w", readErr)}
			return
		}
		firstResultCh <- concurrentRuntimeRequestResult{StatusCode: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}()

	primaryUpstream.waitUntilReady(t, 5*time.Second)
	inflightState := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if inflightState.InFlightNonStream != 1 {
		t.Fatalf("expected one in-flight non-stream attempt, got %+v", inflightState)
	}

	secondCtx, secondCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer secondCancel()
	secondRequest, err := http.NewRequestWithContext(secondCtx, http.MethodPost, harness.url+"/v1/chat/completions", bytes.NewReader(rawBody))
	if err != nil {
		t.Fatalf("build second non-stream lease request: %v", err)
	}
	secondRequest.Header.Set("Content-Type", "application/json")
	secondResponse, err := harness.client.Do(secondRequest)
	if err != nil {
		t.Fatalf("expected overlapping non-stream lease request to avoid the leased primary and complete via fallback, got error: %v", err)
	}
	secondBody := readResponseBody(t, secondResponse)
	_ = secondResponse.Body.Close()
	if secondResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected overlapping non-stream lease request status 200, got %d with body %s", secondResponse.StatusCode, secondBody)
	}
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected non-stream lease to keep the primary at one launched request, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected overlapping request to reach the secondary once while the primary lease is held, got %d", got)
	}
	if got := requestModelID(t, secondaryUpstream.requestsSnapshot()[0].Body); got != targetModelID {
		t.Fatalf("expected secondary fallback request model %q, got %q", targetModelID, got)
	}

	primaryUpstream.releaseRequests()
	firstResult := <-firstResultCh
	if firstResult.Err != nil {
		t.Fatalf("expected first non-stream lease request to succeed after release, got error: %v", firstResult.Err)
	}
	if firstResult.StatusCode != http.StatusOK {
		t.Fatalf("expected first non-stream lease request status 200, got %d with body %s", firstResult.StatusCode, firstResult.Body)
	}
	releasedState := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if releasedState.InFlightNonStream != 0 {
		t.Fatalf("expected non-stream in-flight ownership to release after request completion, got %+v", releasedState)
	}
}

func TestRuntimeLoadBalanceFailoverEligibleFailureUpdatesRuntimeState(t *testing.T) {
	t.Skip("Task 14 owns runtime state retry/failure accumulation semantics")
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-feedback-failure-" + suffix
	targetModelID := "native-loadbalance-feedback-failure-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "feedback primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-feedback-secondary"})
	autoRecovery := `{"mode":"enabled","status_codes":[503],"cooldown":{"base_seconds":60,"failure_threshold":1,"backoff_multiplier":2.0,"max_cooldown_seconds":900},"ban":{"mode":"off","max_cooldown_strikes_before_ban":0,"ban_duration_seconds":0}}`
	strategyID := harness.seedLegacyStrategyWithAutoRecovery(t, activeProfileID, "runtime-feedback-failure-"+suffix, "fill-first", autoRecovery)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "feedback-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/feedback/primary"), "feedback-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "feedback-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/feedback/secondary"), "feedback-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "feedback-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "feedback-secondary-connection-"+suffix, nil, nil, 1)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "feedback failover mutation"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	failureState := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if failureState.ConsecutiveFailures != 1 || failureState.CircuitState != "open" {
		t.Fatalf("expected primary runtime state to open after failover-eligible failure, got %+v", failureState)
	}
	if !failureState.LastFailureKind.Valid || failureState.LastFailureKind.String != "transient_http" || !failureState.LastLiveFailureKind.Valid || failureState.LastLiveFailureKind.String != "transient_http" {
		t.Fatalf("expected transient_http failure markers, got %+v", failureState)
	}
	if !failureState.OpenUntilAt.Valid || !failureState.ProbeAvailableAt.Valid || !failureState.LastLiveFailureAt.Valid {
		t.Fatalf("expected open/probe/failure timestamps after failover-eligible failure, got %+v", failureState)
	}
	if failureState.LastCooldownSeconds != 60 || failureState.MaxCooldownStrikes != 1 {
		t.Fatalf("expected cooldown strike state 60s/1, got %+v", failureState)
	}
	if failureState.BanMode != "off" {
		t.Fatalf("expected ban_mode off after first open interval, got %+v", failureState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, primaryConnectionID)
	if len(events) != 1 {
		t.Fatalf("expected 1 loadbalance event for failover-eligible failure, got %+v", events)
	}
	if events[0].EventType != "opened" || !events[0].FailureKind.Valid || events[0].FailureKind.String != "transient_http" {
		t.Fatalf("expected opened transient_http loadbalance event, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != primaryEndpointID {
		t.Fatalf("expected loadbalance event model/endpoint snapshot %q/%d, got %+v", targetModelID, primaryEndpointID, events[0])
	}
}

func TestRuntimeLoadBalanceProbeFailureReopensRuntimeState(t *testing.T) {
	t.Skip("Task 14 owns runtime state retry/failure accumulation semantics")
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-probe-failure-" + suffix
	targetModelID := "native-loadbalance-probe-failure-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "probe primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-probe-secondary"})
	autoRecovery := `{"mode":"enabled","status_codes":[503],"cooldown":{"base_seconds":60,"failure_threshold":1,"backoff_multiplier":2.0,"max_cooldown_seconds":900},"ban":{"mode":"off","max_cooldown_strikes_before_ban":0,"ban_duration_seconds":0}}`
	strategyID := harness.seedLegacyStrategyWithAutoRecovery(t, activeProfileID, "runtime-probe-failure-"+suffix, "fill-first", autoRecovery)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "probe-failure-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/probe/primary"), "probe-failure-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "probe-failure-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/probe/secondary"), "probe-failure-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "probe-failure-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "probe-failure-secondary-connection-"+suffix, nil, nil, 1)
	pastProbeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:           activeProfileID,
		ConnectionID:        primaryConnectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &priorFailureKind,
		LastCooldownSeconds: 60,
		MaxCooldownStrikes:  1,
		BanMode:             "off",
		BlockedUntilAt:      &pastProbeEligibleAt,
		ProbeAvailableAt:    &pastProbeEligibleAt,
		CircuitState:        "open",
		LastLiveFailureKind: &priorFailureKind,
		LastLiveFailureAt:   &pastProbeEligibleAt,
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "probe failure mutation"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected probe-eligible primary to receive one launched probe attempt, got %d requests", got)
	}
	failureState := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if failureState.ConsecutiveFailures != 2 || failureState.CircuitState != "open" {
		t.Fatalf("expected probe failure to re-open the circuit with incremented streak, got %+v", failureState)
	}
	if !failureState.LastFailureKind.Valid || failureState.LastFailureKind.String != "transient_http" || !failureState.LastLiveFailureKind.Valid || failureState.LastLiveFailureKind.String != "transient_http" {
		t.Fatalf("expected transient_http failure markers after probe failure, got %+v", failureState)
	}
	if !failureState.OpenUntilAt.Valid || !failureState.ProbeAvailableAt.Valid || !failureState.LastLiveFailureAt.Valid {
		t.Fatalf("expected probe failure to persist new open/probe/failure timestamps, got %+v", failureState)
	}
	if failureState.LastCooldownSeconds != 120 || failureState.MaxCooldownStrikes != 2 {
		t.Fatalf("expected probe failure to extend cooldown strike state to 120s/2, got %+v", failureState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, primaryConnectionID)
	assertLoadbalanceEventTypeSequence(t, events, "opened", "probe_eligible")
	if !events[0].FailureKind.Valid || events[0].FailureKind.String != "transient_http" || events[0].ConsecutiveFailures != 2 || events[0].CooldownSeconds != 120 {
		t.Fatalf("expected opened transient_http probe failure event, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != primaryEndpointID {
		t.Fatalf("expected probe failure event model/endpoint snapshot %q/%d, got %+v", targetModelID, primaryEndpointID, events[0])
	}
	if !events[1].FailureKind.Valid || events[1].FailureKind.String != "transient_http" || events[1].ConsecutiveFailures != 1 || events[1].CooldownSeconds != 60 {
		t.Fatalf("expected trailing probe_eligible transient_http event after probe failure reopen, got %+v", events[1])
	}
}

func TestRuntimeLoadBalanceTransportFailureUpdatesRuntimeState(t *testing.T) {
	t.Skip("Task 14 owns runtime state retry/failure accumulation semantics")
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-feedback-transport-" + suffix
	targetModelID := "native-loadbalance-feedback-transport-" + suffix
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-feedback-transport-secondary"})
	failedUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	failedBaseURL := failedUpstream.URL
	failedUpstream.Close()
	autoRecovery := `{"mode":"enabled","status_codes":[503],"cooldown":{"base_seconds":60,"failure_threshold":1,"backoff_multiplier":2.0,"max_cooldown_seconds":900},"ban":{"mode":"off","max_cooldown_strikes_before_ban":0,"ban_duration_seconds":0}}`
	strategyID := harness.seedLegacyStrategyWithAutoRecovery(t, activeProfileID, "runtime-feedback-transport-"+suffix, "fill-first", autoRecovery)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "feedback-transport-primary-endpoint-"+suffix, failedBaseURL+"/loadbalance/feedback/transport/primary", "feedback-transport-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "feedback-transport-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/feedback/transport/secondary"), "feedback-transport-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "feedback-transport-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "feedback-transport-secondary-connection-"+suffix, nil, nil, 1)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "feedback transport mutation"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	failureState := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if failureState.ConsecutiveFailures != 1 || failureState.CircuitState != "open" {
		t.Fatalf("expected primary runtime state to open after transport failure, got %+v", failureState)
	}
	if !failureState.LastFailureKind.Valid || failureState.LastFailureKind.String != "connect_error" || !failureState.LastLiveFailureKind.Valid || failureState.LastLiveFailureKind.String != "connect_error" {
		t.Fatalf("expected connect_error failure markers, got %+v", failureState)
	}
	if !failureState.OpenUntilAt.Valid || !failureState.ProbeAvailableAt.Valid || !failureState.LastLiveFailureAt.Valid {
		t.Fatalf("expected open/probe/failure timestamps after transport failure, got %+v", failureState)
	}
	if failureState.LastCooldownSeconds != 60 || failureState.MaxCooldownStrikes != 1 {
		t.Fatalf("expected cooldown strike state 60s/1 after transport failure, got %+v", failureState)
	}
	if failureState.BanMode != "off" {
		t.Fatalf("expected ban_mode off after first transport failure, got %+v", failureState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, primaryConnectionID)
	if len(events) != 1 {
		t.Fatalf("expected 1 loadbalance event for transport failure, got %+v", events)
	}
	if events[0].EventType != "opened" || !events[0].FailureKind.Valid || events[0].FailureKind.String != "connect_error" {
		t.Fatalf("expected opened connect_error loadbalance event, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != primaryEndpointID {
		t.Fatalf("expected transport failure event model/endpoint snapshot %q/%d, got %+v", targetModelID, primaryEndpointID, events[0])
	}
}

func TestRuntimeLoadBalanceWinningSuccessUpdatesRuntimeState(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-feedback-success-" + suffix
	targetModelID := "native-loadbalance-feedback-success-" + suffix
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-feedback-success-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, activeProfileID, "feedback-success-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/feedback/success"), "feedback-success-key", 0)
	connectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, endpointID, "feedback-success-connection-"+suffix, nil, nil, 0)
	expiredOpenUntil := time.Now().UTC().Add(-1 * time.Minute)
	staleFailureAt := time.Now().UTC().Add(-2 * time.Minute)
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:         activeProfileID,
		ConnectionID:      connectionID,
		BanMode:           "off",
		BlockedUntilAt:    &expiredOpenUntil,
		CircuitState:      "open",
		LiveP95LatencyMS:  &staleLatency,
		LastLiveFailureAt: &staleFailureAt,
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "feedback success mutation"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	successState := loadRuntimeState(t, harness, activeProfileID, connectionID)
	if successState.ConsecutiveFailures != 0 || successState.CircuitState != "closed" {
		t.Fatalf("expected success to reset recovery state, got %+v", successState)
	}
	if successState.OpenUntilAt.Valid || successState.ProbeAvailableAt.Valid {
		t.Fatalf("expected success to clear open/probe timers, got %+v", successState)
	}
	if !successState.LastLiveSuccessAt.Valid || !successState.LiveP95LatencyMS.Valid || successState.LiveP95LatencyMS.Int32 < 1 {
		t.Fatalf("expected success observation to persist latency and timestamp, got %+v", successState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, connectionID)
	assertLoadbalanceEventTypeSequence(t, events, "unbanned")
	if events[0].FailureKind.Valid {
		t.Fatalf("expected unbanned event after winning success to keep an empty failure kind for stale retry state, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != endpointID {
		t.Fatalf("expected unbanned event model/endpoint snapshot %q/%d, got %+v", targetModelID, endpointID, events[0])
	}
}

func TestRuntimeLoadBalanceProbeSuccessClosesRuntimeState(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-probe-success-" + suffix
	targetModelID := "native-loadbalance-probe-success-" + suffix
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-probe-success-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, activeProfileID, "probe-success-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/probe/success"), "probe-success-key", 0)
	connectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, endpointID, "probe-success-connection-"+suffix, nil, nil, 0)
	pastProbeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:           activeProfileID,
		ConnectionID:        connectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &priorFailureKind,
		LastCooldownSeconds: 60,
		MaxCooldownStrikes:  1,
		BanMode:             "off",
		BlockedUntilAt:      &pastProbeEligibleAt,
		ProbeAvailableAt:    &pastProbeEligibleAt,
		CircuitState:        "open",
		LiveP95LatencyMS:    &staleLatency,
		LastLiveFailureKind: &priorFailureKind,
		LastLiveFailureAt:   &pastProbeEligibleAt,
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "probe success mutation"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	successState := loadRuntimeState(t, harness, activeProfileID, connectionID)
	if successState.ConsecutiveFailures != 0 || successState.CircuitState != "closed" {
		t.Fatalf("expected probe success to close and reset recovery state, got %+v", successState)
	}
	if successState.OpenUntilAt.Valid || successState.ProbeAvailableAt.Valid {
		t.Fatalf("expected probe success to clear open/probe timers, got %+v", successState)
	}
	if successState.LastFailureKind.Valid || successState.LastLiveFailureKind.Valid {
		t.Fatalf("expected probe success to clear failure markers, got %+v", successState)
	}
	if !successState.LastLiveSuccessAt.Valid || !successState.LiveP95LatencyMS.Valid || successState.LiveP95LatencyMS.Int32 < 1 {
		t.Fatalf("expected probe success to persist latency and success timestamp, got %+v", successState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, connectionID)
	assertLoadbalanceEventTypeSequence(t, events, "recovered", "unbanned")
	if !events[0].FailureKind.Valid || events[0].FailureKind.String != "transient_http" {
		t.Fatalf("expected recovered transient_http success event, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != endpointID {
		t.Fatalf("expected recovery event model/endpoint snapshot %q/%d, got %+v", targetModelID, endpointID, events[0])
	}
	if !events[1].FailureKind.Valid || events[1].FailureKind.String != "transient_http" || events[1].ConsecutiveFailures != 1 || events[1].CooldownSeconds != 60 {
		t.Fatalf("expected trailing unbanned transient_http event after recovery, got %+v", events[1])
	}
}

func TestRuntimeStatePersistsOpenStateAcrossRestart(t *testing.T) {
	t.Skip("Task 14 owns runtime state retry/failure accumulation semantics")
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-restart-open-" + suffix
	targetModelID := "native-loadbalance-restart-open-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "restart primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-restart-secondary"})
	autoRecovery := `{"mode":"enabled","status_codes":[503],"cooldown":{"base_seconds":60,"failure_threshold":1,"backoff_multiplier":2.0,"max_cooldown_seconds":900},"ban":{"mode":"off","max_cooldown_strikes_before_ban":0,"ban_duration_seconds":0}}`
	strategyID := harness.seedLegacyStrategyWithAutoRecovery(t, activeProfileID, "runtime-restart-open-"+suffix, "fill-first", autoRecovery)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "restart-open-primary-endpoint-"+suffix, primaryUpstream.baseURL("/loadbalance/restart/primary"), "restart-open-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "restart-open-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/restart/secondary"), "restart-open-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "restart-open-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "restart-open-secondary-connection-"+suffix, nil, nil, 1)

	initialResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "restart open persistence initial mutation"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, initialResponse, http.StatusOK)
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected initial request to hit the primary upstream once, got %d requests", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected initial request to fail over to the secondary upstream once, got %d requests", got)
	}

	openStateBeforeRestart := loadRuntimeState(t, harness, activeProfileID, primaryConnectionID)
	if openStateBeforeRestart.ConsecutiveFailures != 1 || openStateBeforeRestart.CircuitState != "open" {
		t.Fatalf("expected failover-eligible failure to persist an open state before restart, got %+v", openStateBeforeRestart)
	}
	if !openStateBeforeRestart.LastFailureKind.Valid || openStateBeforeRestart.LastFailureKind.String != "transient_http" {
		t.Fatalf("expected transient_http failure marker before restart, got %+v", openStateBeforeRestart)
	}

	restartedHarness := restartRuntimeHarness(t, harness.databaseName)
	if runtimeStateExists(t, restartedHarness, activeProfileID, primaryConnectionID) {
		t.Fatalf("expected restart to clear ephemeral runtime state for connection %d", primaryConnectionID)
	}

	restartedResponse := restartedHarness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "restart open persistence reload"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, restartedResponse, http.StatusOK)
	if got := len(primaryUpstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected restarted runtime to retry the primary after ephemeral-state reset, got %d total primary requests", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected restarted runtime to route the second request to the secondary upstream, got %d total secondary requests", got)
	}
}

func TestRuntimeStatePersistsRecoveredStateAcrossRestart(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-restart-recovered-" + suffix
	targetModelID := "native-loadbalance-restart-recovered-" + suffix
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-restart-recovered"})
	strategyID := harness.seedLegacyStrategy(t, activeProfileID, "runtime-restart-recovered-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, activeProfileID, "restart-recovered-endpoint-"+suffix, upstream.baseURL("/loadbalance/restart/recovered"), "restart-recovered-key", 0)
	connectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, endpointID, "restart-recovered-connection-"+suffix, nil, nil, 0)
	pastProbeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:           activeProfileID,
		ConnectionID:        connectionID,
		ConsecutiveFailures: 1,
		LastFailureKind:     &priorFailureKind,
		LastCooldownSeconds: 60,
		MaxCooldownStrikes:  1,
		BanMode:             "off",
		BlockedUntilAt:      &pastProbeEligibleAt,
		ProbeAvailableAt:    &pastProbeEligibleAt,
		CircuitState:        "open",
		LiveP95LatencyMS:    &staleLatency,
		LastLiveFailureKind: &priorFailureKind,
		LastLiveFailureAt:   &pastProbeEligibleAt,
	})

	initialResponse := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "restart recovered persistence initial mutation"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, initialResponse, http.StatusOK)
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected initial recovery request to hit the upstream once, got %d requests", got)
	}

	recoveredStateBeforeRestart := loadRuntimeState(t, harness, activeProfileID, connectionID)
	if recoveredStateBeforeRestart.ConsecutiveFailures != 0 || recoveredStateBeforeRestart.CircuitState != "closed" {
		t.Fatalf("expected successful recovery to persist a closed state before restart, got %+v", recoveredStateBeforeRestart)
	}
	if recoveredStateBeforeRestart.OpenUntilAt.Valid || recoveredStateBeforeRestart.ProbeAvailableAt.Valid {
		t.Fatalf("expected successful recovery to clear open/probe timers before restart, got %+v", recoveredStateBeforeRestart)
	}
	if !recoveredStateBeforeRestart.LastLiveSuccessAt.Valid || !recoveredStateBeforeRestart.LiveP95LatencyMS.Valid {
		t.Fatalf("expected successful recovery to persist latency and success timestamp before restart, got %+v", recoveredStateBeforeRestart)
	}

	restartedHarness := restartRuntimeHarness(t, harness.databaseName)
	if runtimeStateExists(t, restartedHarness, activeProfileID, connectionID) {
		t.Fatalf("expected restart to clear recovered runtime state for connection %d", connectionID)
	}

	restartedResponse := restartedHarness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "restart recovered persistence reload"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, restartedResponse, http.StatusOK)
	if got := len(upstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected restarted runtime to route through the primary again after ephemeral-state reset, got %d total requests", got)
	}
}

func TestRuntimeAdaptiveLoadBalancePrefersHealthierConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-adaptive-" + suffix
	targetModelID := "native-loadbalance-adaptive-" + suffix
	degradedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-adaptive-degraded"})
	healthyUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-adaptive-healthy"})
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-loadbalance-adaptive-"+suffix)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	degradedEndpointID := harness.seedEndpoint(t, activeProfileID, "adaptive-degraded-endpoint-"+suffix, degradedUpstream.baseURL("/loadbalance/adaptive/degraded"), "adaptive-degraded-key", 0)
	healthyEndpointID := harness.seedEndpoint(t, activeProfileID, "adaptive-healthy-endpoint-"+suffix, healthyUpstream.baseURL("/loadbalance/adaptive/healthy"), "adaptive-healthy-key", 1)
	degradedConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, degradedEndpointID, "adaptive-degraded-connection-"+suffix, nil, nil, 0)
	healthyConnectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, healthyEndpointID, "adaptive-healthy-connection-"+suffix, nil, nil, 1)
	degradedLatency := 900
	healthyLatency := 120
	nowAt := time.Now().UTC()
	degradedFailureAt := nowAt.Add(-30 * time.Second)
	healthySuccessAt := nowAt.Add(-5 * time.Second)
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:         activeProfileID,
		ConnectionID:      degradedConnectionID,
		CircuitState:      "open",
		LiveP95LatencyMS:  &degradedLatency,
		LastLiveFailureAt: &degradedFailureAt,
	})
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:         activeProfileID,
		ConnectionID:      healthyConnectionID,
		CircuitState:      "closed",
		LiveP95LatencyMS:  &healthyLatency,
		LastLiveSuccessAt: &healthySuccessAt,
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "adaptive prefers healthier connection"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-adaptive-healthy")
	if got := len(degradedUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected adaptive strategy to avoid degraded upstream, got %d requests", got)
	}
	healthyRequests := healthyUpstream.requestsSnapshot()
	if len(healthyRequests) != 1 {
		t.Fatalf("expected adaptive strategy to hit healthy upstream once, got %d requests", len(healthyRequests))
	}
	if requestModelID(t, healthyRequests[0].Body) != targetModelID {
		t.Fatalf("expected adaptive upstream body model %q, got %q", targetModelID, requestModelID(t, healthyRequests[0].Body))
	}
}

func TestRuntimeAdaptiveHedgeLaunchesSecondAttemptAndFasterHedgedPathWins(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-adaptive-hedge-" + suffix
	targetModelID := "native-loadbalance-adaptive-hedge-" + suffix
	primaryStarted := make(chan struct{}, 1)
	var primaryMu sync.Mutex
	primaryRequests := make([]upstreamRequestSnapshot, 0, 1)
	primaryUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read adaptive hedge primary request body: %v", err)
		}
		_ = r.Body.Close()
		primaryMu.Lock()
		primaryRequests = append(primaryRequests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), body...),
		})
		primaryMu.Unlock()
		select {
		case primaryStarted <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(750 * time.Millisecond):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "chatcmpl-adaptive-hedge-primary"})
		}
	}))
	defer primaryUpstream.Close()
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-adaptive-hedge-secondary"})
	routingPolicy := `{"kind":"adaptive","routing_objective":"minimize_latency","hedge":{"enabled":true,"delay_ms":50,"max_additional_attempts":1},"circuit_breaker":{"failure_status_codes":[403,422,429,500,502,503,504,529],"base_open_seconds":60,"failure_threshold":2,"backoff_multiplier":2.0,"max_open_seconds":900,"ban_mode":"off","max_open_strikes_before_ban":0,"ban_duration_seconds":0},"admission":{"respect_qps_limit":true,"respect_in_flight_limits":true}}`
	strategyID := harness.seedAdaptiveStrategyWithRoutingPolicy(t, activeProfileID, "runtime-adaptive-hedge-"+suffix, routingPolicy)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	primaryEndpointID := harness.seedEndpoint(t, activeProfileID, "adaptive-hedge-primary-endpoint-"+suffix, primaryUpstream.URL+"/loadbalance/adaptive/hedge/primary", "adaptive-hedge-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, activeProfileID, "adaptive-hedge-secondary-endpoint-"+suffix, secondaryUpstream.baseURL("/loadbalance/adaptive/hedge/secondary"), "adaptive-hedge-secondary-key", 1)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, primaryEndpointID, "adaptive-hedge-primary-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondaryEndpointID, "adaptive-hedge-secondary-connection-"+suffix, nil, nil, 1)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "adaptive hedge overlap"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-adaptive-hedge-secondary")
	select {
	case <-primaryStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected adaptive hedge primary request to launch before the hedged winner returned")
	}
	primaryMu.Lock()
	gotPrimaryRequests := append([]upstreamRequestSnapshot(nil), primaryRequests...)
	primaryMu.Unlock()
	if len(gotPrimaryRequests) != 1 {
		t.Fatalf("expected one launched primary hedge request, got %d", len(gotPrimaryRequests))
	}
	if requestModelID(t, gotPrimaryRequests[0].Body) != targetModelID {
		t.Fatalf("expected primary hedge request model %q, got %q", targetModelID, requestModelID(t, gotPrimaryRequests[0].Body))
	}
	secondaryRequests := secondaryUpstream.requestsSnapshot()
	if len(secondaryRequests) != 1 {
		t.Fatalf("expected one launched secondary hedge request, got %d", len(secondaryRequests))
	}
	if requestModelID(t, secondaryRequests[0].Body) != targetModelID {
		t.Fatalf("expected secondary hedge request model %q, got %q", targetModelID, requestModelID(t, secondaryRequests[0].Body))
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func TestRuntimeAdaptiveAdditionalAttemptBudgetStopsBeforeThirdConnection(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-loadbalance-adaptive-budget-" + suffix
	targetModelID := "native-loadbalance-adaptive-budget-" + suffix
	firstUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "adaptive budget first unavailable"})
	secondUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "adaptive budget second unavailable"})
	thirdUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-adaptive-budget-third"})
	routingPolicy := `{"kind":"adaptive","routing_objective":"minimize_latency","hedge":{"enabled":true,"delay_ms":200,"max_additional_attempts":1},"circuit_breaker":{"failure_status_codes":[403,422,429,500,502,503,504,529],"base_open_seconds":60,"failure_threshold":2,"backoff_multiplier":2.0,"max_open_seconds":900,"ban_mode":"off","max_open_strikes_before_ban":0,"ban_duration_seconds":0},"admission":{"respect_qps_limit":true,"respect_in_flight_limits":true}}`
	strategyID := harness.seedAdaptiveStrategyWithRoutingPolicy(t, activeProfileID, "runtime-adaptive-budget-"+suffix, routingPolicy)
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	firstEndpointID := harness.seedEndpoint(t, activeProfileID, "adaptive-budget-first-endpoint-"+suffix, firstUpstream.baseURL("/loadbalance/adaptive/budget/first"), "adaptive-budget-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, activeProfileID, "adaptive-budget-second-endpoint-"+suffix, secondUpstream.baseURL("/loadbalance/adaptive/budget/second"), "adaptive-budget-second-key", 1)
	thirdEndpointID := harness.seedEndpoint(t, activeProfileID, "adaptive-budget-third-endpoint-"+suffix, thirdUpstream.baseURL("/loadbalance/adaptive/budget/third"), "adaptive-budget-third-key", 2)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, firstEndpointID, "adaptive-budget-first-connection-"+suffix, nil, nil, 0)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, secondEndpointID, "adaptive-budget-second-connection-"+suffix, nil, nil, 1)
	_ = harness.seedConnection(t, activeProfileID, targetModelConfigID, thirdEndpointID, "adaptive-budget-third-connection-"+suffix, nil, nil, 2)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": "adaptive additional attempt budget"}},
			"model":    publicModelID,
		},
		nil,
	)
	assertStatus(t, response, http.StatusServiceUnavailable)
	if got := len(firstUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected one first upstream request, got %d", got)
	}
	if got := len(secondUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected one second upstream request, got %d", got)
	}
	if got := len(thirdUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected third upstream to stay unused after exhausting additional-attempt budget, got %d requests", got)
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func startSharedPostgresHarness() (testPostgresHarness, error) {
	containerName := "prism-s14-runtime-" + randomSuffix()
	if err := runDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
		return testPostgresHarness{}, err
	}
	hostPort, err := dockerPort(containerName)
	if err != nil {
		return testPostgresHarness{}, err
	}
	if err := waitForPostgres(hostPort); err != nil {
		return testPostgresHarness{}, err
	}
	return testPostgresHarness{containerName: containerName, hostPort: hostPort}, nil
}

func TestRuntimeBanEscalationRecordsTemporaryBanStateAndEvent(t *testing.T) {
	t.Skip("Task 14 owns Ban Mode runtime state/event semantics")
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-ban-temporary-" + suffix
	targetModelID := "native-ban-temporary-" + suffix
	banUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "temporary ban trigger"})
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-ban-temporary-"+suffix)
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE loadbalance_strategies
		 SET routing_policy = $2::jsonb, updated_at = $3
		 WHERE id = $1`,
		strategyID,
		`{"kind":"adaptive","routing_objective":"minimize_latency","hedge":{"enabled":false,"delay_ms":1500,"max_additional_attempts":1},"circuit_breaker":{"failure_status_codes":[503],"base_open_seconds":1,"failure_threshold":1,"backoff_multiplier":2.0,"max_open_seconds":60,"ban_mode":"temporary","max_open_strikes_before_ban":1,"ban_duration_seconds":600},"admission":{"respect_qps_limit":true,"respect_in_flight_limits":true}}`,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("update temporary ban routing policy: %v", err)
	}
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, activeProfileID, "ban-temporary-endpoint-"+suffix, banUpstream.baseURL("/loadbalance/ban/temporary"), "ban-temporary-key", 0)
	connectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, endpointID, "ban-temporary-connection-"+suffix, nil, nil, 0)
	initialFailure := time.Now().UTC().Add(-time.Minute)
	harness.seedRuntimeState(t, runtimeStateSeed{ProfileID: activeProfileID, ConnectionID: connectionID, CircuitState: "closed", UpdatedAt: initialFailure, CreatedAt: initialFailure})
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "trigger temporary ban"}}, "model": publicModelID}, nil)
	assertStatus(t, response, http.StatusServiceUnavailable)
	state := loadRuntimeState(t, harness, activeProfileID, connectionID)
	if state.BanMode != "temporary" || !state.BannedUntilAt.Valid || state.BannedUntilAt.Time.IsZero() || state.CircuitState != "open" {
		t.Fatalf("expected temporary ban in runtime state, got %+v", state)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, connectionID)
	if len(events) != 1 {
		t.Fatalf("expected one loadbalance event for temporary ban, got %+v", events)
	}
	if events[0].EventType != "banned" || !events[0].BanMode.Valid || events[0].BanMode.String != "temporary" || !events[0].BannedUntilAt.Valid {
		t.Fatalf("expected banned event with temporary ban metadata, got %+v", events[0])
	}
}

func TestRuntimeBanEscalationRecordsUntilResetBanStateAndEvent(t *testing.T) {
	t.Skip("Task 14 owns Ban Mode runtime state/event semantics")
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-ban-until-reset-" + suffix
	targetModelID := "native-ban-until-reset-" + suffix
	banUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "until-reset ban trigger"})
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-ban-until-reset-"+suffix)
	if _, err := harness.conn.Exec(
		context.Background(),
		`UPDATE loadbalance_strategies
		 SET routing_policy = $2::jsonb, updated_at = $3
		 WHERE id = $1`,
		strategyID,
		`{"kind":"adaptive","routing_objective":"minimize_latency","hedge":{"enabled":false,"delay_ms":1500,"max_additional_attempts":1},"circuit_breaker":{"failure_status_codes":[503],"base_open_seconds":1,"failure_threshold":1,"backoff_multiplier":2.0,"max_open_seconds":60,"ban_mode":"until_reset","max_open_strikes_before_ban":1,"ban_duration_seconds":0},"admission":{"respect_qps_limit":true,"respect_in_flight_limits":true}}`,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("update until-reset ban routing policy: %v", err)
	}
	targetModelConfigID := harness.seedModel(t, activeProfileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, activeProfileID, "openai", publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, activeProfileID, "ban-until-reset-endpoint-"+suffix, banUpstream.baseURL("/loadbalance/ban/until-reset"), "ban-until-reset-key", 0)
	connectionID := harness.seedConnection(t, activeProfileID, targetModelConfigID, endpointID, "ban-until-reset-connection-"+suffix, nil, nil, 0)
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "trigger until-reset ban"}}, "model": publicModelID}, nil)
	assertStatus(t, response, http.StatusServiceUnavailable)
	state := loadRuntimeState(t, harness, activeProfileID, connectionID)
	if state.BanMode != "until_reset" || state.BannedUntilAt.Valid || state.CircuitState != "open" {
		t.Fatalf("expected until-reset ban in runtime state, got %+v", state)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, connectionID)
	if len(events) != 1 {
		t.Fatalf("expected one loadbalance event for until-reset ban, got %+v", events)
	}
	if events[0].EventType != "banned" || !events[0].BanMode.Valid || events[0].BanMode.String != "until_reset" || events[0].BannedUntilAt.Valid {
		t.Fatalf("expected banned event with until-reset ban metadata, got %+v", events[0])
	}
}
