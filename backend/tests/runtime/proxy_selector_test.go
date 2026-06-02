package runtime_test

import (
	"context"
	"database/sql"
	"net/http"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type proxySelectorExpectedRequest struct {
	Path    string
	ModelID string
}

func TestRuntimeCheapestEligibleContextNoFitReturns413WithoutUpstreamAttemptOrBanMutation(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "cheapest-no-fit-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "cheapest-no-fit-"+suffix, "cheapest_eligible_context")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	smallEndpointID := harness.seedEndpoint(t, profileID, "cheapest-no-fit-small-"+suffix, harness.upstream.baseURL("/cheapest/no-fit/small"), "cheapest-no-fit-small-key", 0)
	largeEndpointID := harness.seedEndpoint(t, profileID, "cheapest-no-fit-large-"+suffix, harness.upstream.baseURL("/cheapest/no-fit/large"), "cheapest-no-fit-large-key", 1)
	smallConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, smallEndpointID, "cheapest-no-fit-small-connection-"+suffix, nil, nil, 0)
	largeConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, largeEndpointID, "cheapest-no-fit-large-connection-"+suffix, nil, nil, 1)
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, updated_at = $5 WHERE id = $1`, smallConnectionID, 200, 4096, 1.0, now); err != nil {
		t.Fatalf("update small cheapest-context connection capabilities: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, updated_at = $5 WHERE id = $1`, largeConnectionID, 400, 4096, 1.0, now); err != nil {
		t.Fatalf("update large cheapest-context connection capabilities: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	seededAt := time.Now().UTC().Add(-time.Minute)
	harness.seedRuntimeState(t, runtimeStateSeed{ProfileID: profileID, ConnectionID: smallConnectionID, CycleRetryAttempts: 2, CumulativeRetryAttempts: 5, BanMode: "off", UpdatedAt: seededAt, CreatedAt: seededAt})
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "oversized request"}}, "model": publicModelID, "max_completion_tokens": 600}, nil)
	assertStatus(t, response, http.StatusRequestEntityTooLarge)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	if got, _ := payload["error"].(string); got != "context_window_exceeded" {
		t.Fatalf("expected context_window_exceeded error, got %+v", payload)
	}
	if got, _ := payload["detail"].(string); got != "No configured target can fit the estimated request context." {
		t.Fatalf("expected pinned 413 detail, got %+v", payload)
	}
	if got, ok := payload["largest_usable_context_window_tokens"].(float64); !ok || int(got) != 400 {
		t.Fatalf("expected largest usable context window 400, got %+v", payload)
	}
	if got, ok := payload["estimated_total_context_tokens"].(float64); !ok || int(got) <= 400 {
		t.Fatalf("expected estimated total context tokens to exceed 400, got %+v", payload)
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected no upstream requests for planner-side 413, got %d", got)
	}
	state := loadRuntimeState(t, harness, profileID, smallConnectionID)
	if state.CycleRetryAttempts != 2 || state.CumulativeRetryAttempts != 5 || state.BanMode != "off" || state.NextRetryAt.Valid {
		t.Fatalf("expected no-fit planner rejection to leave runtime failure state untouched, got %+v", state)
	}
}

func TestProxySelectorPreferredContextPreferredBandWinsOverCheaperDiscretionaryTarget(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-preferred-band-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-preferred-band-"+suffix, "cheapest_eligible_context")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	preferredEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-preferred-band-preferred-"+suffix, harness.upstream.baseURL("/proxy-selector/preferred-context/preferred"), "proxy-selector-preferred-band-preferred-key", 0)
	discretionaryEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-preferred-band-discretionary-"+suffix, harness.upstream.baseURL("/proxy-selector/preferred-context/discretionary"), "proxy-selector-preferred-band-discretionary-key", 1)
	preferredConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, preferredEndpointID, "proxy-selector-preferred-band-preferred-connection-"+suffix, nil, nil, 1)
	discretionaryConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, discretionaryEndpointID, "proxy-selector-preferred-band-discretionary-connection-"+suffix, nil, nil, 0)
	preferredPricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "proxy-selector-preferred-band-expensive-"+suffix, "USD", "5", "5", "0", "0", "0")
	discretionaryPricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "proxy-selector-preferred-band-cheap-"+suffix, "USD", "1", "1", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, preferredConnectionID, preferredPricingTemplateID)
	attachRuntimeConnectionPricingTemplate(t, harness, discretionaryConnectionID, discretionaryPricingTemplateID)
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, preferred_context_utilization_threshold = $5, updated_at = $6 WHERE id = $1`, preferredConnectionID, 1000, 4096, 1.0, 1.0, now); err != nil {
		t.Fatalf("update preferred-band preferred connection capabilities: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, preferred_context_utilization_threshold = $5, updated_at = $6 WHERE id = $1`, discretionaryConnectionID, 1000, 4096, 1.0, 0.10, now); err != nil {
		t.Fatalf("update preferred-band discretionary connection capabilities: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "preferred band wins over cheaper discretionary"}}, "model": publicModelID, "max_completion_tokens": 256}, nil)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/preferred-context/preferred/v1/chat/completions",
		ModelID: publicModelID,
	}})
}

func TestProxySelectorPreferredContextFallsBackToDiscretionaryWhenNoPreferredCandidatesExist(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-discretionary-fallback-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-discretionary-fallback-"+suffix, "cheapest_eligible_context")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	expensiveEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-discretionary-fallback-expensive-"+suffix, harness.upstream.baseURL("/proxy-selector/preferred-context/fallback-expensive"), "proxy-selector-discretionary-fallback-expensive-key", 0)
	cheapEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-discretionary-fallback-cheap-"+suffix, harness.upstream.baseURL("/proxy-selector/preferred-context/fallback-cheap"), "proxy-selector-discretionary-fallback-cheap-key", 1)
	expensiveConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, expensiveEndpointID, "proxy-selector-discretionary-fallback-expensive-connection-"+suffix, nil, nil, 0)
	cheapConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, cheapEndpointID, "proxy-selector-discretionary-fallback-cheap-connection-"+suffix, nil, nil, 1)
	expensivePricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "proxy-selector-discretionary-fallback-expensive-pricing-"+suffix, "USD", "4", "4", "0", "0", "0")
	cheapPricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "proxy-selector-discretionary-fallback-cheap-pricing-"+suffix, "USD", "1", "1", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, expensiveConnectionID, expensivePricingTemplateID)
	attachRuntimeConnectionPricingTemplate(t, harness, cheapConnectionID, cheapPricingTemplateID)
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, preferred_context_utilization_threshold = $5, updated_at = $6 WHERE id = $1`, expensiveConnectionID, 1000, 4096, 1.0, 0.10, now); err != nil {
		t.Fatalf("update discretionary-fallback expensive connection capabilities: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, preferred_context_utilization_threshold = $5, updated_at = $6 WHERE id = $1`, cheapConnectionID, 1000, 4096, 1.0, 0.15, now); err != nil {
		t.Fatalf("update discretionary-fallback cheap connection capabilities: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "no preferred candidates still route"}}, "model": publicModelID, "max_completion_tokens": 256}, nil)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/preferred-context/fallback-cheap/v1/chat/completions",
		ModelID: publicModelID,
	}})
}

func TestProxySelectorNativeResponsesSupportWinsOverTranslatedCandidate(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-native-support-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-native-support-"+suffix, "cheapest_eligible_context")
	modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	translatedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "proxy-selector-translated-should-not-run"})
	nativeUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "proxy-selector-native-responses",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "native support wins"}},
		}},
		"usage": map[string]any{"input_tokens": 7, "output_tokens": 13, "total_tokens": 20},
	})
	translatedEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-native-support-translated-"+suffix, translatedUpstream.baseURL("/proxy-selector/native-support/translated"), "proxy-selector-native-support-translated-key", 0)
	nativeEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-native-support-native-"+suffix, nativeUpstream.baseURL("/proxy-selector/native-support/native"), "proxy-selector-native-support-native-key", 1)
	translatedVariant := "chat_completions_reasoning_none"
	nativeVariant := "responses_reasoning_none"
	translatedConnectionID := harness.seedConnectionWithOpenAIProbeVariant(t, profileID, modelConfigID, translatedEndpointID, "proxy-selector-native-support-translated-connection-"+suffix, nil, nil, 0, &translatedVariant)
	nativeConnectionID := harness.seedConnectionWithOpenAIProbeVariant(t, profileID, modelConfigID, nativeEndpointID, "proxy-selector-native-support-native-connection-"+suffix, nil, nil, 1, &nativeVariant)
	now := time.Now().UTC()
	for _, connectionID := range []int{translatedConnectionID, nativeConnectionID} {
		if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, updated_at = $5 WHERE id = $1`, connectionID, 16_384, 1_024, 1.0, now); err != nil {
			t.Fatalf("update native-support connection %d context capabilities: %v", connectionID, err)
		}
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"model":             publicModelID,
		"input":             "native responses support should win over translated sibling",
		"text":              map[string]any{"format": "json_schema"},
		"max_output_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	if got := len(translatedUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected translated chat-only candidate to be skipped when native responses support exists, got %d upstream requests", got)
	}
	assertProxySelectorRequestSequence(t, nativeUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/native-support/native/v1/responses",
		ModelID: publicModelID,
	}})
	assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.responses")

	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)
	var selectedTerminalTargetID sql.NullInt64
	var upstreamOperationName sql.NullString
	var translationMode sql.NullString
	var upstreamRequestPath sql.NullString
	if err := harness.conn.QueryRow(
		context.Background(),
		`SELECT selected_terminal_target_id, upstream_operation_name, operation_translation_mode, upstream_request_path FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&selectedTerminalTargetID, &upstreamOperationName, &translationMode, &upstreamRequestPath); err != nil {
		t.Fatalf("load proxy-selector native-support request-log attribution: %v", err)
	}
	if !selectedTerminalTargetID.Valid || int(selectedTerminalTargetID.Int64) != nativeConnectionID {
		t.Fatalf("expected native-support selected_terminal_target_id %d, got %+v", nativeConnectionID, selectedTerminalTargetID)
	}
	if !upstreamOperationName.Valid || upstreamOperationName.String != "openai.responses" {
		t.Fatalf("expected native-support upstream_operation_name openai.responses, got %+v", upstreamOperationName)
	}
	if !translationMode.Valid || translationMode.String != "none" {
		t.Fatalf("expected native-support operation_translation_mode none, got %+v", translationMode)
	}
	if !upstreamRequestPath.Valid || upstreamRequestPath.String != "/v1/responses" {
		t.Fatalf("expected native-support upstream_request_path /v1/responses, got %+v", upstreamRequestPath)
	}
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
	_ = h
	_ = modelConfigID
	_ = strategy
	tb.Skip("legacy proxy selection strategies were removed; unified access targets use each model's legacy strategy")
}

func (h *runtimeHarness) seedProxyTargetWithMetadata(tb testing.TB, sourceModelConfigID int, targetModelConfigID int, position int, weight int, targetPriority int) {
	tb.Helper()
	now := time.Now().UTC()
	if _, err := h.conn.Exec(
		context.Background(),
		`INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at)
		 SELECT profile_id, id, 'model', $2, $3, $4, $5, TRUE, $6, $6 FROM model_configs WHERE id = $1`,
		sourceModelConfigID,
		targetModelConfigID,
		position,
		weight,
		targetPriority,
		now,
	); err != nil {
		tb.Fatalf("insert runtime model access target: %v", err)
	}
}
