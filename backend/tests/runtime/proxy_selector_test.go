package runtimetest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type selectorEndpointSeed struct {
	label    string
	baseURL  string
	apiKey   string
	position int
}

type selectorRouteSeed struct {
	profileID    int
	apiFamily    string
	prefix       string
	suffix       string
	strategyID   int
	strategyType string
	endpoints    []selectorEndpointSeed
}

type selectorRoute struct {
	publicModelID       string
	targetModelID       string
	targetModelConfigID int
	endpointIDs         []int
	connectionIDs       []int
}

func seedSelectorRoute(t *testing.T, harness *runtimeHarness, seed selectorRouteSeed) selectorRoute {
	t.Helper()
	if seed.suffix == "" {
		seed.suffix = randomSuffix()
	}
	if seed.apiFamily == "" {
		seed.apiFamily = "openai"
	}
	publicModelID := "proxy-" + seed.prefix + "-" + seed.suffix
	targetModelID := "native-" + seed.prefix + "-" + seed.suffix
	strategyID := seed.strategyID
	if strategyID == 0 {
		strategyType := seed.strategyType
		if strategyType == "" {
			strategyType = "round-robin"
		}
		strategyID = harness.seedLegacyStrategy(t, seed.profileID, "runtime-"+seed.prefix+"-"+seed.suffix, strategyType)
	}
	targetModelConfigID := harness.seedModel(t, seed.profileID, seed.apiFamily, targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, seed.profileID, seed.apiFamily, publicModelID, "proxy", nil)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	route := selectorRoute{
		publicModelID:       publicModelID,
		targetModelID:       targetModelID,
		targetModelConfigID: targetModelConfigID,
	}
	for _, endpoint := range seed.endpoints {
		apiKey := endpoint.apiKey
		if apiKey == "" {
			apiKey = seed.prefix + "-" + endpoint.label + "-key"
		}
		endpointID := harness.seedEndpoint(t, seed.profileID, seed.prefix+"-"+endpoint.label+"-endpoint-"+seed.suffix, endpoint.baseURL, apiKey, endpoint.position)
		connectionID := harness.seedConnection(t, seed.profileID, targetModelConfigID, endpointID, seed.prefix+"-"+endpoint.label+"-connection-"+seed.suffix, nil, nil, endpoint.position)
		route.endpointIDs = append(route.endpointIDs, endpointID)
		route.connectionIDs = append(route.connectionIDs, connectionID)
	}
	return route
}

func chatCompletionsBody(modelID string, content string) map[string]any {
	return map[string]any{"messages": []map[string]any{{"role": "user", "content": content}}, "model": modelID}
}

func anthropicMessagesBody(modelID string, content string, maxTokens int) map[string]any {
	return map[string]any{"model": modelID, "messages": []map[string]any{{"role": "user", "content": content}}, "max_tokens": maxTokens}
}

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
		chatCompletionsBody(openAIRoute.PublicModelID, "proxy parity"),
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
		anthropicMessagesBody(anthropicRoute.PublicModelID, "header merge", 1),
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
		chatCompletionsBody(route.PublicModelID, "caller user agent"),
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
		chatCompletionsBody(route.PublicModelID, "custom user agent"),
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
		chatCompletionsBody(route.PublicModelID, "blocked user agent"),
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
	eligibleEndpointKey := "eligible-upstream-key"
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-blocked",
		endpoints: []selectorEndpointSeed{
			{label: "blocked", baseURL: harness.upstream.baseURL("/loadbalance/blocked"), apiKey: "blocked-upstream-key", position: 0},
			{label: "eligible", baseURL: harness.upstream.baseURL("/loadbalance/eligible"), apiKey: eligibleEndpointKey, position: 1},
		},
	})
	blockedUntilAt := time.Now().UTC().Add(10 * time.Minute)
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:      activeProfileID,
		ConnectionID:   route.connectionIDs[0],
		BanMode:        "off",
		BlockedUntilAt: &blockedUntilAt,
		CircuitState:   "open",
	})

	harness.upstream.clear()
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "blocked connection skip"), nil)
	assertStatus(t, response, http.StatusOK)
	upstreamRequest := harness.upstream.lastRequest(t)
	if upstreamRequest.Path != "/loadbalance/eligible/v1/chat/completions" {
		t.Fatalf("expected runtime to skip blocked connection and use eligible path, got %s", upstreamRequest.Path)
	}
	if upstreamRequest.Headers.Get("Authorization") != "Bearer "+eligibleEndpointKey {
		t.Fatalf("expected runtime to use eligible upstream key, got %q", upstreamRequest.Headers.Get("Authorization"))
	}
	if requestModelID(t, upstreamRequest.Body) != route.targetModelID {
		t.Fatalf("expected upstream body model %q, got %q", route.targetModelID, requestModelID(t, upstreamRequest.Body))
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
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(publicModelID, "proxy target eligibility"), nil)
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
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(modelID, "direct resolved target"), nil)
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
		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(firstModelID, "trigger private ban"), nil)
		assertStatus(t, response, http.StatusServiceUnavailable)
	}
	state, ok := harness.runtimeService.RuntimeState().SnapshotConnectionState(profileID, firstConnectionID)
	if !ok || state.BanMode != "temporary" || state.BannedUntilAt == nil || state.CumulativeRetryAttempts != 3 {
		t.Fatalf("expected first model to temporarily ban its private connection, got ok=%v state=%+v", ok, state)
	}

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(secondModelID, "use peer private primary then fallback"), nil)
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
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-loadbalance-admission-qps-"+suffix)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		prefix:     "loadbalance-admission-qps",
		suffix:     suffix,
		strategyID: strategyID,
		endpoints: []selectorEndpointSeed{
			{label: "rejected", baseURL: harness.upstream.baseURL("/loadbalance/admission/qps-rejected"), position: 0},
			{label: "eligible", baseURL: harness.upstream.baseURL("/loadbalance/admission/qps-eligible"), position: 1},
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
			{label: "rejected", baseURL: harness.upstream.baseURL("/anthropic/admission/qps-rejected"), position: 0},
			{label: "eligible", baseURL: harness.upstream.baseURL("/anthropic/admission/qps-eligible"), position: 1},
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
		endpoints:  []selectorEndpointSeed{{label: "only", baseURL: harness.upstream.baseURL("/loadbalance/admission/all-rejected"), position: 0}},
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
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "all admission rejected"), nil)
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
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "single primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-single-secondary"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "loadbalance-single",
		strategyType: "single",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/single/primary"), position: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/single/secondary"), position: 1},
		},
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "single no failover"), nil)
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
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "fill-first primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-fill-first-secondary"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "loadbalance-fill-first",
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/fill-first/primary"), position: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/fill-first/secondary"), position: 1},
		},
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "fill-first failover"), nil)
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-fill-first-secondary")
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected fill-first strategy to call the primary upstream once, got %d requests", got)
	}
	secondaryRequests := secondaryUpstream.requestsSnapshot()
	if len(secondaryRequests) != 1 {
		t.Fatalf("expected fill-first strategy to fail over to the secondary upstream once, got %d requests", len(secondaryRequests))
	}
	if requestModelID(t, secondaryRequests[0].Body) != route.targetModelID {
		t.Fatalf("expected fill-first failover body model %q, got %q", route.targetModelID, requestModelID(t, secondaryRequests[0].Body))
	}
	assertLatestRuntimeAttemptCounts(t, harness.conn, activeProfileID, 2, 2)
}

func TestRuntimeBudgetUsesOneOverallDeadlineAcrossAttempts(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
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

	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "loadbalance-budget",
		suffix:       suffix,
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.URL + "/loadbalance/budget/primary", position: 0},
			{label: "secondary", baseURL: secondaryUpstream.URL + "/loadbalance/budget/secondary", position: 1},
		},
	})

	rawBody, err := json.Marshal(chatCompletionsBody(route.publicModelID, "shared request budget"))
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
	firstUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-concurrent-round-robin-first"})
	secondUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-concurrent-round-robin-second"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-concurrent-round-robin",
		endpoints: []selectorEndpointSeed{
			{label: "first", baseURL: firstUpstream.baseURL("/loadbalance/concurrent/round-robin/first"), position: 0},
			{label: "second", baseURL: secondUpstream.baseURL("/loadbalance/concurrent/round-robin/second"), position: 1},
		},
	})

	results := executeConcurrentRuntimeJSONRequests(t, harness, 8, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "concurrent round-robin cursor claims"), nil)
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
		if got := requestModelID(t, request.Body); got != route.targetModelID {
			t.Fatalf("expected concurrent round-robin request model %q, got %q", route.targetModelID, got)
		}
	}
	if nextCursor := loadRoundRobinNextCursor(t, harness, activeProfileID, route.targetModelConfigID, 2); nextCursor != 0 {
		t.Fatalf("expected concurrent round-robin next_cursor to wrap to 0 after 8 claims, got %d", nextCursor)
	}
}

func TestRuntimeExpiredRetryWindowAllowsConcurrentAttempts(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	primaryUpstream := newBlockingScriptedUpstream(t, 2, http.StatusOK, map[string]any{"id": "chatcmpl-retry-window-primary"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-retry-window-secondary"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:    activeProfileID,
		prefix:       "retry-window",
		strategyType: "fill-first",
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/retry-window/primary"), position: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/retry-window/secondary"), position: 1},
		},
	})
	retryAvailableAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:               activeProfileID,
		ConnectionID:            route.connectionIDs[0],
		CycleRetryAttempts:      1,
		CumulativeRetryAttempts: 1,
		LastFailureKind:         &priorFailureKind,
		LastRetryDelayMS:        60000,
		BanMode:                 "off",
		NextRetryAt:             &retryAvailableAt,
	})
	requestBody := chatCompletionsBody(route.publicModelID, "retry window concurrent attempt")
	resultChans := []<-chan concurrentRuntimeRequestResult{
		startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil),
		startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil),
	}

	primaryUpstream.waitUntilReady(t, 5*time.Second)
	primaryRequests := primaryUpstream.requestsSnapshot()
	if got := len(primaryRequests); got != 2 {
		t.Fatalf("expected expired retry window to allow two primary attempts, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected expired retry window to avoid fallback attempts, got %d secondary requests", got)
	}
	for index, request := range primaryRequests {
		if got := requestModelID(t, request.Body); got != route.targetModelID {
			t.Fatalf("expected primary request %d model %q, got %q", index, route.targetModelID, got)
		}
	}

	primaryUpstream.releaseRequests()
	for index, resultCh := range resultChans {
		result := awaitAsyncRequest(t, resultCh, 5*time.Second)
		if result.Err != nil {
			t.Fatalf("expected retry-window request %d to succeed after release, got error: %v", index, result.Err)
		}
		if result.StatusCode != http.StatusOK {
			t.Fatalf("expected retry-window request %d status 200, got %d with body %s", index, result.StatusCode, result.Body)
		}
	}
	releasedState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if releasedState.CycleRetryAttempts != 0 || releasedState.CumulativeRetryAttempts != 0 || releasedState.NextRetryAt.Valid || releasedState.BanMode != "off" {
		t.Fatalf("expected successful retry-window attempts to clear retry state, got %+v", releasedState)
	}
}

func TestRuntimeLeaseNonStreamInFlightExclusivity(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	primaryUpstream := newBlockingScriptedUpstream(t, 1, http.StatusOK, map[string]any{"id": "chatcmpl-lease-non-stream-primary"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-lease-non-stream-secondary"})
	strategyID := harness.seedAdaptiveStrategy(t, activeProfileID, "runtime-lease-non-stream-"+suffix)
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID:  activeProfileID,
		prefix:     "lease-non-stream",
		suffix:     suffix,
		strategyID: strategyID,
		endpoints: []selectorEndpointSeed{
			{label: "primary", baseURL: primaryUpstream.baseURL("/loadbalance/lease/non-stream/primary"), position: 0},
			{label: "secondary", baseURL: secondaryUpstream.baseURL("/loadbalance/lease/non-stream/secondary"), position: 1},
		},
	})
	maxInFlightNonStream := 1
	harness.updateConnectionAdmissionLimits(t, route.connectionIDs[0], nil, &maxInFlightNonStream, nil)
	requestBody := chatCompletionsBody(route.publicModelID, "non-stream lease exclusivity")
	firstResultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil)

	primaryUpstream.waitUntilReady(t, 5*time.Second)
	inflightState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if inflightState.InFlightNonStream != 1 {
		t.Fatalf("expected one in-flight non-stream attempt, got %+v", inflightState)
	}

	secondResponse := performPriorityRequest(t, harness.client, 2*time.Second, http.MethodPost, harness.url+"/v1/chat/completions", requestBody, nil)
	secondBody := readResponseBody(t, secondResponse)
	if secondResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected overlapping non-stream lease request status 200, got %d with body %s", secondResponse.StatusCode, secondBody)
	}
	if got := len(primaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected non-stream lease to keep the primary at one launched request, got %d", got)
	}
	if got := len(secondaryUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected overlapping request to reach the secondary once while the primary lease is held, got %d", got)
	}
	if got := requestModelID(t, secondaryUpstream.requestsSnapshot()[0].Body); got != route.targetModelID {
		t.Fatalf("expected secondary fallback request model %q, got %q", route.targetModelID, got)
	}

	primaryUpstream.releaseRequests()
	firstResult := awaitAsyncRequest(t, firstResultCh, 5*time.Second)
	if firstResult.Err != nil {
		t.Fatalf("expected first non-stream lease request to succeed after release, got error: %v", firstResult.Err)
	}
	if firstResult.StatusCode != http.StatusOK {
		t.Fatalf("expected first non-stream lease request status 200, got %d with body %s", firstResult.StatusCode, firstResult.Body)
	}
	releasedState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if releasedState.InFlightNonStream != 0 {
		t.Fatalf("expected non-stream in-flight ownership to release after request completion, got %+v", releasedState)
	}
}

func TestRuntimeLoadBalanceWinningSuccessUpdatesRuntimeState(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-feedback-success",
		suffix:    suffix,
		endpoints: []selectorEndpointSeed{{label: "only", baseURL: harness.upstream.baseURL("/loadbalance/feedback/success"), position: 0}},
	})
	expiredOpenUntil := time.Now().UTC().Add(-1 * time.Minute)
	staleFailureAt := time.Now().UTC().Add(-2 * time.Minute)
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:         activeProfileID,
		ConnectionID:      route.connectionIDs[0],
		BanMode:           "off",
		BlockedUntilAt:    &expiredOpenUntil,
		CircuitState:      "open",
		LiveP95LatencyMS:  &staleLatency,
		LastLiveFailureAt: &staleFailureAt,
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "feedback success mutation"), nil)
	assertStatus(t, response, http.StatusOK)
	successState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
	if successState.ConsecutiveFailures != 0 || successState.CircuitState != "closed" {
		t.Fatalf("expected success to reset recovery state, got %+v", successState)
	}
	if successState.OpenUntilAt.Valid || successState.ProbeAvailableAt.Valid {
		t.Fatalf("expected success to clear open/probe timers, got %+v", successState)
	}
	if !successState.LastLiveSuccessAt.Valid || !successState.LiveP95LatencyMS.Valid || successState.LiveP95LatencyMS.Int32 < 1 {
		t.Fatalf("expected success observation to persist latency and timestamp, got %+v", successState)
	}
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, route.connectionIDs[0])
	assertLoadbalanceEventTypeSequence(t, events, "unbanned")
	if events[0].FailureKind.Valid {
		t.Fatalf("expected unbanned event after winning success to keep an empty failure kind for stale retry state, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != route.targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != route.endpointIDs[0] {
		t.Fatalf("expected unbanned event model/endpoint snapshot %q/%d, got %+v", route.targetModelID, route.endpointIDs[0], events[0])
	}
}

func TestRuntimeLoadBalanceProbeSuccessClosesRuntimeState(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-probe-success",
		suffix:    suffix,
		endpoints: []selectorEndpointSeed{{label: "only", baseURL: harness.upstream.baseURL("/loadbalance/probe/success"), position: 0}},
	})
	pastProbeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:           activeProfileID,
		ConnectionID:        route.connectionIDs[0],
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

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "probe success mutation"), nil)
	assertStatus(t, response, http.StatusOK)
	successState := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
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
	events := loadLoadbalanceEvents(t, harness.conn, activeProfileID, route.connectionIDs[0])
	assertLoadbalanceEventTypeSequence(t, events, "recovered", "unbanned")
	if !events[0].FailureKind.Valid || events[0].FailureKind.String != "transient_http" {
		t.Fatalf("expected recovered transient_http success event, got %+v", events[0])
	}
	if !events[0].ModelID.Valid || events[0].ModelID.String != route.targetModelID || !events[0].EndpointID.Valid || int(events[0].EndpointID.Int32) != route.endpointIDs[0] {
		t.Fatalf("expected recovery event model/endpoint snapshot %q/%d, got %+v", route.targetModelID, route.endpointIDs[0], events[0])
	}
	if !events[1].FailureKind.Valid || events[1].FailureKind.String != "transient_http" || events[1].ConsecutiveFailures != 1 || events[1].CooldownSeconds != 60 {
		t.Fatalf("expected trailing unbanned transient_http event after recovery, got %+v", events[1])
	}
}

func TestRuntimeStatePersistsRecoveredStateAcrossRestart(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-restart-recovered"})
	route := seedSelectorRoute(t, harness, selectorRouteSeed{
		profileID: activeProfileID,
		prefix:    "loadbalance-restart-recovered",
		suffix:    suffix,
		endpoints: []selectorEndpointSeed{{label: "only", baseURL: upstream.baseURL("/loadbalance/restart/recovered"), position: 0}},
	})
	pastProbeEligibleAt := time.Now().UTC().Add(-1 * time.Minute)
	priorFailureKind := "transient_http"
	staleLatency := 999
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:           activeProfileID,
		ConnectionID:        route.connectionIDs[0],
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

	initialResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "restart recovered persistence initial mutation"), nil)
	assertStatus(t, initialResponse, http.StatusOK)
	if got := len(upstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected initial recovery request to hit the upstream once, got %d requests", got)
	}

	recoveredStateBeforeRestart := loadRuntimeState(t, harness, activeProfileID, route.connectionIDs[0])
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
	if runtimeStateExists(t, restartedHarness, activeProfileID, route.connectionIDs[0]) {
		t.Fatalf("expected restart to clear recovered runtime state for connection %d", route.connectionIDs[0])
	}

	restartedResponse := restartedHarness.requestJSON(t, http.MethodPost, "/v1/chat/completions", chatCompletionsBody(route.publicModelID, "restart recovered persistence reload"), nil)
	assertStatus(t, restartedResponse, http.StatusOK)
	if got := len(upstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected restarted runtime to route through the primary again after ephemeral-state reset, got %d total requests", got)
	}
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
