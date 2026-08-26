package runtimetest

import (
	"context"
	"fmt"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"net/http"
	"testing"
	"time"
)

type selectorEndpointSeed struct {
	label    string
	baseURL  string
	apiKey   string
	priority int
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
		endpointID := harness.seedEndpoint(t, seed.profileID, seed.prefix+"-"+endpoint.label+"-endpoint-"+seed.suffix, endpoint.baseURL, apiKey)
		connectionID := harness.seedConnection(t, seed.profileID, targetModelConfigID, endpointID, seed.prefix+"-"+endpoint.label+"-connection-"+seed.suffix, nil, nil, endpoint.priority)
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
			"user-agent":        "declared-by-connection/1.0",
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
	// Client headers outside the protocol allowlist never reach an upstream,
	// blocklist rule or not. A blocklist cannot deliver the anti-fingerprinting
	// this filter exists for, because every IDE keeps inventing new headers;
	// operators declare what an upstream needs via connection.custom_headers
	// instead (X-Allow-Smoke above). The dropped header stays visible in the
	// audit trail — see TestRuntimeAuditHeaderScrubPersistsRedactedOnly — so the
	// filter removes the leak without also removing the evidence.
	if upstreamRequest.Headers.Get("X-Client-Kept") != "" {
		t.Fatalf("expected unlisted client header to be withheld from the upstream, got %q", upstreamRequest.Headers.Get("X-Client-Kept"))
	}
	// The caller's User-Agent identifies the client more precisely than any
	// other header, so it never crosses; what the upstream sees is what the
	// connection declared, identically on every request regardless of caller.
	if got := upstreamRequest.Headers.Get("User-Agent"); got != "declared-by-connection/1.0" {
		t.Fatalf("expected the connection-declared User-Agent, got %q", got)
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
	// With nothing declared on the connection the upstream learns nothing about
	// the caller: Prism sends an empty User-Agent rather than relaying
	// "claude-cli/..." or falling back to Go's default.
	if firstUpstreamUA := harness.upstream.lastRequest(t).Headers.Get("User-Agent"); firstUpstreamUA != "" {
		t.Fatalf("expected the caller user-agent to be withheld from the upstream, got %q", firstUpstreamUA)
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
			{label: "blocked", baseURL: harness.upstream.baseURL("/loadbalance/blocked"), apiKey: "blocked-upstream-key", priority: 0},
			{label: "eligible", baseURL: harness.upstream.baseURL("/loadbalance/eligible"), apiKey: eligibleEndpointKey, priority: 1},
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
	blockedEndpointID := harness.seedEndpoint(t, activeProfileID, "blocked-target-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/target-blocked"), "blocked-target-key")
	eligibleEndpointID := harness.seedEndpoint(t, activeProfileID, "eligible-target-endpoint-"+suffix, harness.upstream.baseURL("/loadbalance/target-eligible"), "eligible-target-key")
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
	endpointID := harness.seedEndpoint(t, profileID, "direct-resolved-target-endpoint-"+suffix, harness.upstream.baseURL("/direct-resolved-target"), "direct-resolved-target-key")
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
	primaryEndpointID := harness.seedEndpoint(t, profileID, "private-ban-primary-endpoint-"+suffix, primaryUpstream.baseURL("/private-ban/primary"), "private-ban-primary-key")
	fallbackEndpointID := harness.seedEndpoint(t, profileID, "private-ban-fallback-endpoint-"+suffix, fallbackUpstream.baseURL("/private-ban/fallback"), "private-ban-fallback-key")
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
