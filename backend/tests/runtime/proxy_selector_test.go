package runtime_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type proxySelectorExpectedRequest struct {
	Path    string
	ModelID string
}

func TestRuntimeProxySelectorOrderedFallbackUsesPositionOrderAndSkipsUnroutableTarget(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-ordered-public-" + suffix
	firstTargetModelID := "proxy-selector-ordered-first-" + suffix
	secondTargetModelID := "proxy-selector-ordered-second-" + suffix
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-ordered-"+suffix, "round-robin")
	firstTargetConfigID := harness.seedModel(t, profileID, "openai", firstTargetModelID, "native", &strategyID)
	secondTargetConfigID := harness.seedModel(t, profileID, "openai", secondTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.setProxySelectionStrategy(t, publicModelConfigID, "ordered_fallback")
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, secondTargetConfigID, 1, 1, 0)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, firstTargetConfigID, 0, 1, 9)
	firstEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-ordered-first-"+suffix, harness.upstream.baseURL("/proxy-selector/ordered/first"), "proxy-selector-ordered-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-ordered-second-"+suffix, harness.upstream.baseURL("/proxy-selector/ordered/second"), "proxy-selector-ordered-second-key", 1)
	firstConnectionID := harness.seedConnection(t, profileID, firstTargetConfigID, firstEndpointID, "proxy-selector-ordered-first-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondTargetConfigID, secondEndpointID, "proxy-selector-ordered-second-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := performProxySelectorChatRequest(t, harness, publicModelID, "ordered fallback keeps position order")
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/ordered/first/v1/chat/completions",
		ModelID: firstTargetModelID,
	}})

	harness.upstream.clear()
	blockedUntilAt := time.Now().UTC().Add(10 * time.Minute)
	harness.seedRuntimeState(t, runtimeStateSeed{
		ProfileID:      profileID,
		ConnectionID:   firstConnectionID,
		BlockedUntilAt: &blockedUntilAt,
		CircuitState:   "open",
	})
	response = performProxySelectorChatRequest(t, harness, publicModelID, "ordered fallback skips unroutable target")
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/ordered/second/v1/chat/completions",
		ModelID: secondTargetModelID,
	}})
}

func TestRuntimeProxySelectorPriorityStaticUsesPriorityThenPosition(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-priority-public-" + suffix
	lowPositionTargetModelID := "proxy-selector-priority-low-position-" + suffix
	positionTwoTargetModelID := "proxy-selector-priority-position-two-" + suffix
	positionOneTargetModelID := "proxy-selector-priority-position-one-" + suffix

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-priority-"+suffix, "round-robin")
	lowPositionTargetConfigID := harness.seedModel(t, profileID, "openai", lowPositionTargetModelID, "native", &strategyID)
	positionTwoTargetConfigID := harness.seedModel(t, profileID, "openai", positionTwoTargetModelID, "native", &strategyID)
	positionOneTargetConfigID := harness.seedModel(t, profileID, "openai", positionOneTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.setProxySelectionStrategy(t, publicModelConfigID, "priority_static")
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, lowPositionTargetConfigID, 0, 1, 9)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, positionTwoTargetConfigID, 2, 1, 1)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, positionOneTargetConfigID, 1, 1, 1)
	lowPositionEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-priority-low-position-"+suffix, harness.upstream.baseURL("/proxy-selector/priority/low-position"), "proxy-selector-priority-low-position-key", 0)
	positionTwoEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-priority-position-two-"+suffix, harness.upstream.baseURL("/proxy-selector/priority/position-two"), "proxy-selector-priority-position-two-key", 1)
	positionOneEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-priority-position-one-"+suffix, harness.upstream.baseURL("/proxy-selector/priority/position-one"), "proxy-selector-priority-position-one-key", 2)
	harness.seedConnection(t, profileID, lowPositionTargetConfigID, lowPositionEndpointID, "proxy-selector-priority-low-position-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, positionTwoTargetConfigID, positionTwoEndpointID, "proxy-selector-priority-position-two-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, positionOneTargetConfigID, positionOneEndpointID, "proxy-selector-priority-position-one-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := performProxySelectorChatRequest(t, harness, publicModelID, "priority static chooses priority then position")
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/priority/position-one/v1/chat/completions",
		ModelID: positionOneTargetModelID,
	}})
}

func TestRuntimeProxySelectorPriorityStaticFallsBackWhenPreferredTargetIsUnroutable(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-priority-fallback-public-" + suffix
	preferredTargetModelID := "proxy-selector-priority-fallback-preferred-" + suffix
	fallbackTargetModelID := "proxy-selector-priority-fallback-next-" + suffix

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-priority-fallback-"+suffix, "round-robin")
	preferredTargetConfigID := harness.seedModel(t, profileID, "openai", preferredTargetModelID, "native", &strategyID)
	fallbackTargetConfigID := harness.seedModel(t, profileID, "openai", fallbackTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.setProxySelectionStrategy(t, publicModelConfigID, "priority_static")
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, fallbackTargetConfigID, 0, 1, 5)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, preferredTargetConfigID, 1, 1, 0)
	fallbackEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-priority-fallback-"+suffix, harness.upstream.baseURL("/proxy-selector/priority/fallback"), "proxy-selector-priority-fallback-key", 0)
	harness.seedConnection(t, profileID, fallbackTargetConfigID, fallbackEndpointID, "proxy-selector-priority-fallback-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := performProxySelectorChatRequest(t, harness, publicModelID, "priority static skips unroutable preferred target")
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/priority/fallback/v1/chat/completions",
		ModelID: fallbackTargetModelID,
	}})
}

func TestRuntimeProxySelectorWeightedStaticUsesDeterministicCursorSequence(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-weighted-public-" + suffix
	heavyTargetModelID := "proxy-selector-weighted-heavy-" + suffix
	lightTargetModelID := "proxy-selector-weighted-light-" + suffix

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-weighted-"+suffix, "round-robin")
	heavyTargetConfigID := harness.seedModel(t, profileID, "openai", heavyTargetModelID, "native", &strategyID)
	lightTargetConfigID := harness.seedModel(t, profileID, "openai", lightTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.setProxySelectionStrategy(t, publicModelConfigID, "weighted_static")
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, heavyTargetConfigID, 0, 3, 0)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, lightTargetConfigID, 1, 1, 0)
	heavyEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-weighted-heavy-"+suffix, harness.upstream.baseURL("/proxy-selector/weighted/heavy"), "proxy-selector-weighted-heavy-key", 0)
	lightEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-weighted-light-"+suffix, harness.upstream.baseURL("/proxy-selector/weighted/light"), "proxy-selector-weighted-light-key", 1)
	harness.seedConnection(t, profileID, heavyTargetConfigID, heavyEndpointID, "proxy-selector-weighted-heavy-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, lightTargetConfigID, lightEndpointID, "proxy-selector-weighted-light-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	for requestIndex := 0; requestIndex < 4; requestIndex++ {
		response := performProxySelectorChatRequest(t, harness, publicModelID, "weighted static deterministic cursor")
		assertStatus(t, response, http.StatusOK)
	}
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{
		{Path: "/proxy-selector/weighted/heavy/v1/chat/completions", ModelID: heavyTargetModelID},
		{Path: "/proxy-selector/weighted/heavy/v1/chat/completions", ModelID: heavyTargetModelID},
		{Path: "/proxy-selector/weighted/heavy/v1/chat/completions", ModelID: heavyTargetModelID},
		{Path: "/proxy-selector/weighted/light/v1/chat/completions", ModelID: lightTargetModelID},
	})
}

func TestRuntimeProxySelectorWeightedStaticExcludesUnroutableTargetsFromWeight(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-weighted-routable-public-" + suffix
	firstTargetModelID := "proxy-selector-weighted-routable-first-" + suffix
	unroutableTargetModelID := "proxy-selector-weighted-routable-unroutable-" + suffix
	secondTargetModelID := "proxy-selector-weighted-routable-second-" + suffix

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-weighted-routable-"+suffix, "round-robin")
	firstTargetConfigID := harness.seedModel(t, profileID, "openai", firstTargetModelID, "native", &strategyID)
	unroutableTargetConfigID := harness.seedModel(t, profileID, "openai", unroutableTargetModelID, "native", &strategyID)
	secondTargetConfigID := harness.seedModel(t, profileID, "openai", secondTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.setProxySelectionStrategy(t, publicModelConfigID, "weighted_static")
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, firstTargetConfigID, 0, 1, 0)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, unroutableTargetConfigID, 1, 100, 0)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, secondTargetConfigID, 2, 1, 0)
	firstEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-weighted-routable-first-"+suffix, harness.upstream.baseURL("/proxy-selector/weighted-routable/first"), "proxy-selector-weighted-routable-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-weighted-routable-second-"+suffix, harness.upstream.baseURL("/proxy-selector/weighted-routable/second"), "proxy-selector-weighted-routable-second-key", 1)
	harness.seedConnection(t, profileID, firstTargetConfigID, firstEndpointID, "proxy-selector-weighted-routable-first-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondTargetConfigID, secondEndpointID, "proxy-selector-weighted-routable-second-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	for requestIndex := 0; requestIndex < 4; requestIndex++ {
		response := performProxySelectorChatRequest(t, harness, publicModelID, "weighted static excludes unroutable target")
		assertStatus(t, response, http.StatusOK)
	}
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{
		{Path: "/proxy-selector/weighted-routable/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/proxy-selector/weighted-routable/second/v1/chat/completions", ModelID: secondTargetModelID},
		{Path: "/proxy-selector/weighted-routable/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/proxy-selector/weighted-routable/second/v1/chat/completions", ModelID: secondTargetModelID},
	})
}

func TestRuntimeProxySelectorWeightedStaticDoesNotRetryAlternateTargetAfterUpstreamFailure(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-weighted-no-retry-public-" + suffix
	primaryTargetModelID := "proxy-selector-weighted-no-retry-primary-" + suffix
	alternateTargetModelID := "proxy-selector-weighted-no-retry-alternate-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "selected target failed"})
	alternateUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-proxy-selector-alternate"})

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-weighted-no-retry-"+suffix, "single")
	primaryTargetConfigID := harness.seedModel(t, profileID, "openai", primaryTargetModelID, "native", &strategyID)
	alternateTargetConfigID := harness.seedModel(t, profileID, "openai", alternateTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.setProxySelectionStrategy(t, publicModelConfigID, "weighted_static")
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, primaryTargetConfigID, 0, 1, 0)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, alternateTargetConfigID, 1, 1, 0)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-weighted-no-retry-primary-"+suffix, primaryUpstream.baseURL("/proxy-selector/weighted-no-retry/primary"), "proxy-selector-weighted-no-retry-primary-key", 0)
	alternateEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-weighted-no-retry-alternate-"+suffix, alternateUpstream.baseURL("/proxy-selector/weighted-no-retry/alternate"), "proxy-selector-weighted-no-retry-alternate-key", 1)
	harness.seedConnection(t, profileID, primaryTargetConfigID, primaryEndpointID, "proxy-selector-weighted-no-retry-primary-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, alternateTargetConfigID, alternateEndpointID, "proxy-selector-weighted-no-retry-alternate-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := performProxySelectorChatRequest(t, harness, publicModelID, "weighted static does not retry another proxy target")
	assertStatus(t, response, http.StatusServiceUnavailable)
	primaryRequests := primaryUpstream.requestsSnapshot()
	if len(primaryRequests) != 1 {
		t.Fatalf("expected selected primary target to receive one upstream attempt, got %d", len(primaryRequests))
	}
	if requestModelID(t, primaryRequests[0].Body) != primaryTargetModelID {
		t.Fatalf("expected failed upstream request model %q, got %q", primaryTargetModelID, requestModelID(t, primaryRequests[0].Body))
	}
	if got := len(alternateUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected alternate proxy target to remain unattempted after selected target failure, got %d requests", got)
	}
}

func TestRuntimeRequestLogsPreserveProxySelectorResolvedTargetIdentity(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-request-log-public-" + suffix
	lowerPositionTargetModelID := "proxy-selector-request-log-lower-position-" + suffix
	selectedTargetModelID := "proxy-selector-request-log-selected-" + suffix
	lowerPositionUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-proxy-selector-lower-position"})
	selectedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-proxy-selector-request-log-" + suffix,
		"object": "chat.completion",
		"usage": map[string]any{
			"prompt_tokens":     6,
			"completion_tokens": 4,
			"total_tokens":      10,
		},
	})

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-request-log-"+suffix, "round-robin")
	lowerPositionTargetConfigID := harness.seedModel(t, profileID, "openai", lowerPositionTargetModelID, "native", &strategyID)
	selectedTargetConfigID := harness.seedModel(t, profileID, "openai", selectedTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	harness.setProxySelectionStrategy(t, publicModelConfigID, "priority_static")
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, lowerPositionTargetConfigID, 0, 1, 5)
	harness.seedProxyTargetWithMetadata(t, publicModelConfigID, selectedTargetConfigID, 1, 1, 0)
	lowerPositionEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-request-log-lower-position-"+suffix, lowerPositionUpstream.baseURL("/proxy-selector/request-log/lower-position"), "proxy-selector-request-log-lower-position-key", 0)
	selectedEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-request-log-selected-"+suffix, selectedUpstream.baseURL("/proxy-selector/request-log/selected"), "proxy-selector-request-log-selected-key", 1)
	harness.seedConnection(t, profileID, lowerPositionTargetConfigID, lowerPositionEndpointID, "proxy-selector-request-log-lower-position-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, selectedTargetConfigID, selectedEndpointID, "proxy-selector-request-log-selected-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := performProxySelectorChatRequest(t, harness, publicModelID, "request logs preserve proxy selector identity")
	assertStatus(t, response, http.StatusOK)
	if got := len(lowerPositionUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected lower-position target to remain unattempted for request-log selector case, got %d requests", got)
	}
	selectedRequests := selectedUpstream.requestsSnapshot()
	if len(selectedRequests) != 1 {
		t.Fatalf("expected selected target to receive one request-log selector attempt, got %d", len(selectedRequests))
	}
	if requestModelID(t, selectedRequests[0].Body) != selectedTargetModelID {
		t.Fatalf("expected selected request-log upstream model %q, got %q", selectedTargetModelID, requestModelID(t, selectedRequests[0].Body))
	}
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, publicModelID, selectedTargetModelID)
}

func performProxySelectorChatRequest(t *testing.T, harness *runtimeHarness, modelID string, content string) *http.Response {
	t.Helper()
	return harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"messages": []map[string]any{{"role": "user", "content": content}},
			"model":    modelID,
		},
		nil,
	)
}

func assertProxySelectorRequestSequence(t *testing.T, requests []upstreamRequestSnapshot, want []proxySelectorExpectedRequest) {
	t.Helper()
	if len(requests) != len(want) {
		t.Fatalf("expected %d upstream requests, got %d: %+v", len(want), len(requests), requests)
	}
	for index, expected := range want {
		if requests[index].Path != expected.Path {
			t.Fatalf("expected upstream request %d path %q, got %q", index, expected.Path, requests[index].Path)
		}
		if got := requestModelID(t, requests[index].Body); got != expected.ModelID {
			t.Fatalf("expected upstream request %d model %q, got %q", index, expected.ModelID, got)
		}
	}
}

func runtimeModelProxySelectionStrategy(modelType string) any {
	if modelType == "proxy" {
		return "ordered_fallback"
	}
	return nil
}

func (h *runtimeHarness) setProxySelectionStrategy(tb testing.TB, modelConfigID int, strategy string) {
	tb.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`UPDATE model_configs SET proxy_selection_strategy = $2, updated_at = $3 WHERE id = $1`,
		modelConfigID,
		strategy,
		now,
	); err != nil {
		tb.Fatalf("update proxy selection strategy for model config %d: %v", modelConfigID, err)
	}
	h.refreshRuntimeSnapshot(tb, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{h.profileIDForModelConfig(tb, modelConfigID)}})
}

func (h *runtimeHarness) seedProxyTargetWithMetadata(tb testing.TB, sourceModelConfigID int, targetModelConfigID int, position int, weight int, targetPriority int) {
	tb.Helper()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position, weight, target_priority) VALUES ($1, $2, $3, $4, $5)`,
		sourceModelConfigID,
		targetModelConfigID,
		position,
		weight,
		targetPriority,
	); err != nil {
		tb.Fatalf("insert runtime proxy target: %v", err)
	}
}
