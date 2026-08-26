package runtimetest

import (
	"context"
	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"net/http"
	"testing"
	"time"
)

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
