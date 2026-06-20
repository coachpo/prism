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

func TestRuntimeFillFirstIgnoresContextFitAfterContextRoutingRemoval(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "gpt-5-cheapest-no-fit-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "context-no-fit-"+suffix, "fill-first")
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
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/cheapest/no-fit/small/v1/chat/completions", ModelID: publicModelID}})
}

func TestResponsesEstimationUnavailablePassesThrough(t *testing.T) {
	harnessFactories := []struct {
		name    string
		factory func(testing.TB) *runtimeHarness
	}{
		{name: "legacy", factory: newRuntimeHarness},
		{name: "enforced", factory: newEnforcedRuntimeHarness},
	}

	for _, test := range harnessFactories {
		t.Run(test.name, func(t *testing.T) {
			harness := test.factory(t)
			profileID := harness.activeProfileID(t)
			upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "responses-estimation-unavailable"})
			route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "responses-estimation-unavailable-public", "responses-estimation-unavailable-target", upstream.baseURL("/responses/estimation-unavailable"), "responses-estimation-unavailable-key", "responses_reasoning_none", "responses_only")

			response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
				"model":                route.PublicModelID,
				"previous_response_id": "resp_123",
				"input":                "routable unavailable responses request",
			}, nil)
			assertStatus(t, response, http.StatusOK)
			assertProxySelectorRequestSequence(t, upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
				Path:    "/responses/estimation-unavailable/v1/responses",
				ModelID: route.TargetModelID,
			}})
		})
	}
}

func TestChatCompletionsEstimationUnavailablePassesThrough(t *testing.T) {
	harnessFactories := []struct {
		name    string
		factory func(testing.TB) *runtimeHarness
	}{
		{name: "legacy", factory: newRuntimeHarness},
		{name: "enforced", factory: newEnforcedRuntimeHarness},
	}

	for _, test := range harnessFactories {
		t.Run(test.name, func(t *testing.T) {
			harness := test.factory(t)
			profileID := harness.activeProfileID(t)
			upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chat-estimation-unavailable"})
			route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "chat-estimation-unavailable-public", "chat-estimation-unavailable-target", upstream.baseURL("/chat/estimation-unavailable"), "chat-estimation-unavailable-key", "chat_completions_reasoning_none", "chat_completions_only")

			response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
				"model": route.PublicModelID,
				"messages": []map[string]any{{
					"role": "user",
					"content": []map[string]any{{
						"type": "image_url",
						"image_url": map[string]any{
							"url":    "https://example.invalid/image.png",
							"detail": "high",
						},
					}},
				}},
			}, nil)
			assertStatus(t, response, http.StatusOK)
			assertProxySelectorRequestSequence(t, upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
				Path:    "/chat/estimation-unavailable/v1/chat/completions",
				ModelID: route.TargetModelID,
			}})
		})
	}
}

func TestContextWindowExceededNoFitRoutesAfterContextRoutingRemoval(t *testing.T) {
	harnessFactories := []struct {
		name    string
		factory func(testing.TB) *runtimeHarness
	}{
		{name: "legacy", factory: newRuntimeHarness},
		{name: "enforced", factory: newEnforcedRuntimeHarness},
	}

	for _, test := range harnessFactories {
		t.Run(test.name, func(t *testing.T) {
			harness := test.factory(t)
			profileID := harness.activeProfileID(t)
			suffix := randomSuffix()
			publicModelID := "gpt-5-context-window-exceeded-public-" + suffix
			strategyID := harness.seedLegacyStrategy(t, profileID, "context-window-exceeded-"+suffix, "fill-first")
			publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
			smallEndpointID := harness.seedEndpoint(t, profileID, "context-window-exceeded-small-"+suffix, harness.upstream.baseURL("/context-window-exceeded/small"), "context-window-exceeded-small-key", 0)
			largeEndpointID := harness.seedEndpoint(t, profileID, "context-window-exceeded-large-"+suffix, harness.upstream.baseURL("/context-window-exceeded/large"), "context-window-exceeded-large-key", 1)
			smallConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, smallEndpointID, "context-window-exceeded-small-connection-"+suffix, nil, nil, 0)
			largeConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, largeEndpointID, "context-window-exceeded-large-connection-"+suffix, nil, nil, 1)
			setRuntimeHarnessConnectionContextCapabilities(t, harness, smallConnectionID, 200, 4_096, 1.0)
			setRuntimeHarnessConnectionContextCapabilities(t, harness, largeConnectionID, 400, 4_096, 1.0)
			harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

			response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
				"messages":              []map[string]any{{"role": "user", "content": "oversized request"}},
				"model":                 publicModelID,
				"max_completion_tokens": 600,
			}, nil)
			assertStatus(t, response, http.StatusOK)
			assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/context-window-exceeded/small/v1/chat/completions", ModelID: publicModelID}})
		})
	}
}

func TestNonNativeOpenAITargetIsNotSelectedByGenericPlanner(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "translated-unsupported-shape-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "translated-unsupported-shape-"+suffix, "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "translated-unsupported-shape"})
	endpointID := harness.seedEndpoint(t, profileID, "translated-unsupported-shape-endpoint-"+suffix, upstream.baseURL("/translated/unsupported-shape"), "translated-unsupported-shape-key", 0)
	chatOnlyVariant := "chat_completions_reasoning_none"
	harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, endpointID, "translated-unsupported-shape-connection-"+suffix, nil, nil, 0, &chatOnlyVariant, runtimeStringPtr("chat_completions_only"))
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"model":                modelID,
		"previous_response_id": "resp_123",
		"input":                "unsupported translated responses shape",
	}, nil)
	assertOpenAIResponseTranslationUnsupported(t, response, "openai_chat_completions_to_responses", "chat_choices")
	assertProxySelectorRequestSequence(t, upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/translated/unsupported-shape/v1/chat/completions",
		ModelID: modelID,
	}})
}

func TestRuntimeFillFirstSelectsFirstNestedTerminalAfterContextRoutingRemoval(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "gpt-5-cheapest-nested-public-" + suffix
	childModelID := "gpt-5-cheapest-nested-child-" + suffix
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	publicStrategyID := harness.seedLegacyStrategy(t, profileID, "gpt-5-context-nested-public-"+suffix, "fill-first")
	childStrategyID := harness.seedLegacyStrategy(t, profileID, "gpt-5-cheapest-nested-child-"+suffix, "fill-first")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &publicStrategyID)
	childModelConfigID := harness.seedModel(t, profileID, "openai", childModelID, "native", &childStrategyID)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, childModelConfigID, 0)
	smallEndpointID := harness.seedEndpoint(t, profileID, "cheapest-nested-small-"+suffix, harness.upstream.baseURL("/cheapest/nested/small"), "cheapest-nested-small-key", 0)
	largeEndpointID := harness.seedEndpoint(t, profileID, "cheapest-nested-large-"+suffix, harness.upstream.baseURL("/cheapest/nested/large"), "cheapest-nested-large-key", 1)
	smallConnectionID := harness.seedConnection(t, profileID, childModelConfigID, smallEndpointID, "cheapest-nested-small-connection-"+suffix, nil, nil, 0)
	largeConnectionID := harness.seedConnection(t, profileID, childModelConfigID, largeEndpointID, "cheapest-nested-large-connection-"+suffix, nil, nil, 1)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, smallConnectionID, 400, 4_096, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, largeConnectionID, 1_000, 4_096, 1.0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "nested oversized request"}}, "model": publicModelID, "max_completion_tokens": 600}, nil)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/cheapest/nested/small/v1/chat/completions",
		ModelID: childModelID,
	}})
}

func TestFacadeOrderedEligibleContextSkipsIneligibleTargets(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "facade-ordered-public-" + suffix
	firstTargetModelID := "facade-ordered-first-" + suffix
	blockedTargetModelID := "facade-ordered-blocked-" + suffix
	route := seedOpenAIFacadeRoute(t, harness, profileID, publicModelID, []facadeTargetSeed{
		{ModelID: blockedTargetModelID, EndpointBaseURL: harness.upstream.baseURL("/facade/ordered/blocked"), EndpointAPIKey: "facade-ordered-blocked-key"},
		{ModelID: firstTargetModelID, EndpointBaseURL: harness.upstream.baseURL("/facade/ordered/first"), EndpointAPIKey: "facade-ordered-first-key"},
	})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
	blockedUntilAt := time.Now().UTC().Add(10 * time.Minute)
	harness.seedRuntimeState(t, runtimeStateSeed{ProfileID: profileID, ConnectionID: route.ConnectionIDs[0], BlockedUntilAt: &blockedUntilAt, CircuitState: "open"})

	for requestIndex := 0; requestIndex < 4; requestIndex++ {
		response := performProxySelectorChatRequest(t, harness, publicModelID, "facade ordered eligible-only routing")
		assertStatus(t, response, http.StatusOK)
	}
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{
		{Path: "/facade/ordered/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/facade/ordered/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/facade/ordered/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/facade/ordered/first/v1/chat/completions", ModelID: firstTargetModelID},
	})
}

func TestFacadeOrderedEligibleContextDoesNotRetryAlternateTargetAfterUpstreamFailure(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "facade-no-retry-public-" + suffix
	primaryTargetModelID := "facade-no-retry-primary-" + suffix
	alternateTargetModelID := "facade-no-retry-alternate-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "selected facade target failed"})
	alternateUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "facade-no-retry-alternate"})
	seedOpenAIFacadeRoute(t, harness, profileID, publicModelID, []facadeTargetSeed{
		{ModelID: primaryTargetModelID, EndpointBaseURL: primaryUpstream.baseURL("/facade/no-retry/primary"), EndpointAPIKey: "facade-no-retry-primary-key"},
		{ModelID: alternateTargetModelID, EndpointBaseURL: alternateUpstream.baseURL("/facade/no-retry/alternate"), EndpointAPIKey: "facade-no-retry-alternate-key"},
	})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := performProxySelectorChatRequest(t, harness, publicModelID, "facade selected target failure should not retry sibling")
	assertStatus(t, response, http.StatusServiceUnavailable)
	primaryRequests := primaryUpstream.requestsSnapshot()
	if len(primaryRequests) != 1 {
		t.Fatalf("expected selected facade target to receive one upstream attempt, got %d", len(primaryRequests))
	}
	if got := requestModelID(t, primaryRequests[0].Body); got != primaryTargetModelID {
		t.Fatalf("expected failed facade upstream request model %q, got %q", primaryTargetModelID, got)
	}
	if got := len(alternateUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected alternate facade target to remain unattempted after selected target failure, got %d requests", got)
	}
}

func TestFacadeOrderedEligibleContextNoContextFitReturns413WithoutUpstreamAttempt(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "gpt-5-facade-no-fit-public-" + suffix
	route := seedOpenAIFacadeRoute(t, harness, profileID, publicModelID, []facadeTargetSeed{
		{ModelID: "facade-no-fit-small-" + suffix, EndpointBaseURL: harness.upstream.baseURL("/facade/no-fit/small"), EndpointAPIKey: "facade-no-fit-small-key"},
		{ModelID: "facade-no-fit-large-" + suffix, EndpointBaseURL: harness.upstream.baseURL("/facade/no-fit/large"), EndpointAPIKey: "facade-no-fit-large-key"},
	})
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionIDs[0], 200, 4_096, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionIDs[1], 400, 4_096, 1.0)
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "oversized facade request"}}, "model": publicModelID, "max_completion_tokens": 600}, nil)
	assertStatus(t, response, http.StatusRequestEntityTooLarge)
	payload := runtimeResponsePayload(t, response)
	if got, _ := payload["error"].(string); got != "context_window_exceeded" {
		t.Fatalf("expected facade context_window_exceeded error, got %+v", payload)
	}
	if got, _ := payload["detail"].(string); got != "No configured target can fit the estimated request context." {
		t.Fatalf("expected pinned facade 413 detail, got %+v", payload)
	}
	if got, ok := payload["largest_usable_context_window_tokens"].(float64); !ok || int(got) != 400 {
		t.Fatalf("expected facade largest usable context window 400, got %+v", payload)
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected no upstream requests for facade planner-side 413, got %d", got)
	}
}

func TestFacadeOrderedEligibleContextRejectsSelectedTranslatedChildWithoutNativeSiblingFallback(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "facade-translation-public-" + suffix
	chatOnlyVariant := "chat_completions_reasoning_none"
	responsesVariant := "responses_reasoning_none"
	nativeSibling := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "resp_facade_translation_native_sibling",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "native sibling"}},
		}},
		"usage": map[string]any{"input_tokens": 5, "output_tokens": 7, "total_tokens": 12},
	})
	seedOpenAIFacadeRoute(t, harness, profileID, publicModelID, []facadeTargetSeed{
		{
			ModelID:                    "facade-translation-target-" + suffix,
			EndpointBaseURL:            harness.upstream.baseURL("/facade/translation/chat-only"),
			EndpointAPIKey:             "facade-translation-key",
			OpenAIProbeEndpointVariant: &chatOnlyVariant,
			OpenAITextCapability:       runtimeStringPtr("chat_completions_only"),
		},
		{
			ModelID:                    "facade-translation-native-sibling-" + suffix,
			EndpointBaseURL:            nativeSibling.baseURL("/facade/translation/native-sibling"),
			EndpointAPIKey:             "facade-translation-native-sibling-key",
			OpenAIProbeEndpointVariant: &responsesVariant,
			OpenAITextCapability:       runtimeStringPtr("responses_only"),
		},
	})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": publicModelID, "input": "facade native-only rejection", "text": map[string]any{"format": "json_schema"}, "max_output_tokens": 64}, nil)
	assertOpenAIRequestTranslationUnsupported(t, response, "openai_responses_to_chat_completions", "responses_text_format")
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected unsupported facade translation to reject before provider transport, got %d upstream calls", got)
	}
	if got := len(nativeSibling.requestsSnapshot()); got != 0 {
		t.Fatalf("expected native facade sibling to remain unattempted after selected child rejection, got %d upstream calls", got)
	}
}

func TestFacadeOrderedEligibleContextNoEligibleTargetsReturns503(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "facade-no-eligible-public-" + suffix
	route := seedOpenAIFacadeRoute(t, harness, profileID, publicModelID, []facadeTargetSeed{
		{ModelID: "facade-no-eligible-first-" + suffix, EndpointBaseURL: harness.upstream.baseURL("/facade/no-eligible/first"), EndpointAPIKey: "facade-no-eligible-first-key"},
		{ModelID: "facade-no-eligible-second-" + suffix, EndpointBaseURL: harness.upstream.baseURL("/facade/no-eligible/second"), EndpointAPIKey: "facade-no-eligible-second-key"},
	})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
	blockedUntilAt := time.Now().UTC().Add(10 * time.Minute)
	for _, connectionID := range route.ConnectionIDs {
		harness.seedRuntimeState(t, runtimeStateSeed{ProfileID: profileID, ConnectionID: connectionID, BlockedUntilAt: &blockedUntilAt, CircuitState: "open"})
	}

	response := performProxySelectorChatRequest(t, harness, publicModelID, "all facade targets unroutable")
	assertStatus(t, response, http.StatusServiceUnavailable)
	payload := runtimeResponsePayload(t, response)
	if got, _ := payload["detail"].(string); got != "No eligible targets available for model '"+publicModelID+"'." {
		t.Fatalf("expected facade no-eligible 503 detail, got %+v", payload)
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected no upstream requests for facade 503, got %d", got)
	}
}

func TestProxySelectorTopologyCascadeShortTextSafeResponsesStayOnPrimaryChild(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := seedOpenAITopologyCascadeRoute(t, harness, profileID, "/proxy-selector/topology-cascade/short")

	response := performProxySelectorResponsesTextRequest(t, harness, route.PublicModelID, "short text-safe request", 32)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, route.PrimaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    route.PrimaryPathPrefix + "/v1/responses",
		ModelID: route.PrimaryModelID,
	}})
	assertNoScriptedUpstreamRequests(t, route.GPT54Upstream, "gpt-5.4 long-context tier")
	assertNoScriptedUpstreamRequests(t, route.DeepSeekUpstream, "deepseek fallback tier")
	assertNoScriptedUpstreamRequests(t, route.LaterNativeUpstream, "later native tier")
}

func TestProxySelectorTopologyCascadeOversizedCompatibleResponsesRouteToGPT54(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := seedOpenAITopologyCascadeRoute(t, harness, profileID, "/proxy-selector/topology-cascade/gpt54")

	response := performProxySelectorResponsesTextRequest(t, harness, route.PublicModelID, "oversized compatible request", 900)
	assertStatus(t, response, http.StatusOK)
	assertNoScriptedUpstreamRequests(t, route.PrimaryUpstream, "gpt-5.5 primary tier")
	assertProxySelectorRequestSequence(t, route.GPT54Upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    route.GPT54PathPrefix + "/v1/responses",
		ModelID: route.GPT54ModelID,
	}})
	assertNoScriptedUpstreamRequests(t, route.DeepSeekUpstream, "deepseek fallback tier")
	assertNoScriptedUpstreamRequests(t, route.LaterNativeUpstream, "later native tier")
}

func TestProxySelectorTopologyCascadeTranslatesToChatOnlyTargetAfterEarlierTiersAreIneligible(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := seedOpenAITopologyCascadeRoute(t, harness, profileID, "/proxy-selector/topology-cascade/deepseek")

	response := performProxySelectorResponsesTextRequest(t, harness, route.PublicModelID, "large text-safe request", 1800)
	assertStatus(t, response, http.StatusOK)
	assertNoScriptedUpstreamRequests(t, route.PrimaryUpstream, "gpt-5.5 primary tier")
	assertNoScriptedUpstreamRequests(t, route.GPT54Upstream, "gpt-5.4 long-context tier")
	assertProxySelectorRequestSequence(t, route.DeepSeekUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    route.DeepSeekPathPrefix + "/v1/chat/completions",
		ModelID: route.DeepSeekModelID,
	}})
	assertNoScriptedUpstreamRequests(t, route.LaterNativeUpstream, "later native tier")
}

func TestProxySelectorTopologyCascadeDirectGPT54UsesItsOwnLongContextTerminalPath(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	route := seedOpenAITopologyCascadeRoute(t, harness, profileID, "/proxy-selector/topology-cascade/direct-gpt54")

	response := performProxySelectorResponsesTextRequest(t, harness, route.GPT54ModelID, "direct gpt-5.4 request", 900)
	assertStatus(t, response, http.StatusOK)
	assertNoScriptedUpstreamRequests(t, route.PrimaryUpstream, "gpt-5.5 primary tier")
	assertProxySelectorRequestSequence(t, route.GPT54Upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    route.GPT54PathPrefix + "/v1/responses",
		ModelID: route.GPT54ModelID,
	}})
	assertNoScriptedUpstreamRequests(t, route.DeepSeekUpstream, "deepseek fallback tier")
}

func TestProxySelectorFillFirstKeepsEarlierContextFitCandidate(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "gpt-5-proxy-selector-preferred-band-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-fill-first-context-"+suffix, "fill-first")
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
		Path:    "/proxy-selector/preferred-context/discretionary/v1/chat/completions",
		ModelID: publicModelID,
	}})
}

func TestProxySelectorFillFirstSkipsEarlierContextIneligibleCandidate(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "gpt-5-proxy-selector-discretionary-fallback-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-context-fallback-"+suffix, "fill-first")
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
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, preferred_context_utilization_threshold = $5, updated_at = $6 WHERE id = $1`, expensiveConnectionID, 200, 4096, 1.0, 0.10, now); err != nil {
		t.Fatalf("update discretionary-fallback expensive connection capabilities: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, preferred_context_utilization_threshold = $5, updated_at = $6 WHERE id = $1`, cheapConnectionID, 1000, 4096, 1.0, 0.15, now); err != nil {
		t.Fatalf("update discretionary-fallback cheap connection capabilities: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"messages": []map[string]any{{"role": "user", "content": "no preferred candidates still route"}}, "model": publicModelID, "max_completion_tokens": 256}, nil)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/preferred-context/fallback-expensive/v1/chat/completions",
		ModelID: publicModelID,
	}})
}

func TestProxySelectorContextRankingTranslatedCandidateWinsByPolicyOrder(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-context-ranking-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-context-ranking-"+suffix, "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	translatedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "proxy-selector-context-ranking-translated-chat",
		"object": "chat.completion",
		"model":  publicModelID,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "translated policy candidate wins",
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 20},
	})
	nativeUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "proxy-selector-context-ranking-native-responses",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "native sibling should not run"}},
		}},
		"usage": map[string]any{"input_tokens": 7, "output_tokens": 13, "total_tokens": 20},
	})
	translatedEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-context-ranking-translated-"+suffix, translatedUpstream.baseURL("/proxy-selector/context-ranking/translated"), "proxy-selector-context-ranking-translated-key", 0)
	nativeEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-context-ranking-native-"+suffix, nativeUpstream.baseURL("/proxy-selector/context-ranking/native"), "proxy-selector-context-ranking-native-key", 1)
	translatedVariant := "chat_completions_reasoning_none"
	nativeVariant := "responses_reasoning_none"
	translatedConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, translatedEndpointID, "proxy-selector-context-ranking-translated-connection-"+suffix, nil, nil, 0, &translatedVariant, runtimeStringPtr("chat_completions_only"))
	nativeConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, nativeEndpointID, "proxy-selector-context-ranking-native-connection-"+suffix, nil, nil, 1, &nativeVariant, runtimeStringPtr("responses_only"))
	now := time.Now().UTC()
	for _, connectionID := range []int{translatedConnectionID, nativeConnectionID} {
		if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, updated_at = $5 WHERE id = $1`, connectionID, 16_384, 1_024, 1.0, now); err != nil {
			t.Fatalf("update context-ranking connection %d context capabilities: %v", connectionID, err)
		}
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"model":             publicModelID,
		"input":             "context ranking should choose the earlier translated sibling",
		"max_output_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, translatedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/context-ranking/translated/v1/chat/completions",
		ModelID: publicModelID,
	}})
	if got := len(nativeUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected later native candidate to remain unattempted when policy rank selects translated candidate, got %d upstream requests", got)
	}
	assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.responses")

	assertLatestProxySelectorAttribution(t, harness, profileID, translatedConnectionID, "openai.chat_completions", string(runtimeapi.TranslationModeOpenAIResponsesToChatCompletions), "/v1/chat/completions")
}

func TestProxySelectorContextRankingChatTranslatedCandidateWinsByPolicyOrder(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-context-ranking-chat-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-context-ranking-chat-"+suffix, "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	translatedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "proxy-selector-context-ranking-translated-responses",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "translated policy candidate wins"}},
		}},
		"usage": map[string]any{"input_tokens": 7, "output_tokens": 13, "total_tokens": 20},
	})
	nativeUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "proxy-selector-context-ranking-native-chat-completions",
		"object": "chat.completion",
		"model":  publicModelID,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "native sibling should not run",
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 20},
	})
	translatedEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-context-ranking-chat-translated-"+suffix, translatedUpstream.baseURL("/proxy-selector/context-ranking-chat/translated"), "proxy-selector-context-ranking-chat-translated-key", 0)
	nativeEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-context-ranking-chat-native-"+suffix, nativeUpstream.baseURL("/proxy-selector/context-ranking-chat/native"), "proxy-selector-context-ranking-chat-native-key", 1)
	translatedVariant := "responses_reasoning_none"
	nativeVariant := "chat_completions_reasoning_none"
	translatedConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, translatedEndpointID, "proxy-selector-context-ranking-chat-translated-connection-"+suffix, nil, nil, 0, &translatedVariant, runtimeStringPtr("responses_only"))
	nativeConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, nativeEndpointID, "proxy-selector-context-ranking-chat-native-connection-"+suffix, nil, nil, 1, &nativeVariant, runtimeStringPtr("chat_completions_only"))
	for _, connectionID := range []int{translatedConnectionID, nativeConnectionID} {
		setRuntimeHarnessConnectionContextCapabilities(t, harness, connectionID, 16_384, 1_024, 1.0)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":                 publicModelID,
		"messages":              []map[string]any{{"role": "user", "content": "context ranking should choose the earlier translated sibling"}},
		"max_completion_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, translatedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/context-ranking-chat/translated/v1/responses",
		ModelID: publicModelID,
	}})
	if got := len(nativeUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected later native candidate to remain unattempted when policy rank selects translated candidate, got %d upstream requests", got)
	}
	assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.chat_completions")
	assertLatestProxySelectorAttribution(t, harness, profileID, translatedConnectionID, "openai.responses", string(runtimeapi.TranslationModeOpenAIChatCompletionsToResponses), "/v1/responses")
}

func TestProxySelectorContextRankingResponsesIncludeLossyFallback(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-context-ranking-include-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-context-ranking-include-"+suffix, "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	translatedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "proxy-selector-context-ranking-include-translated-chat",
		"object": "chat.completion",
		"model":  publicModelID,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "translated include candidate wins",
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 20},
	})
	nativeUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "proxy-selector-context-ranking-include-native-responses",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "native sibling should not run"}},
		}},
		"usage": map[string]any{"input_tokens": 7, "output_tokens": 13, "total_tokens": 20},
	})
	translatedEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-context-ranking-include-translated-"+suffix, translatedUpstream.baseURL("/proxy-selector/context-ranking-include/translated"), "proxy-selector-context-ranking-include-translated-key", 0)
	nativeEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-context-ranking-include-native-"+suffix, nativeUpstream.baseURL("/proxy-selector/context-ranking-include/native"), "proxy-selector-context-ranking-include-native-key", 1)
	translatedVariant := "chat_completions_reasoning_none"
	nativeVariant := "responses_reasoning_none"
	translatedConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, translatedEndpointID, "proxy-selector-context-ranking-include-translated-connection-"+suffix, nil, nil, 0, &translatedVariant, runtimeStringPtr("chat_completions_only"))
	nativeConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, nativeEndpointID, "proxy-selector-context-ranking-include-native-connection-"+suffix, nil, nil, 1, &nativeVariant, runtimeStringPtr("responses_only"))
	now := time.Now().UTC()
	for _, connectionID := range []int{translatedConnectionID, nativeConnectionID} {
		if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, updated_at = $5 WHERE id = $1`, connectionID, 16_384, 1_024, 1.0, now); err != nil {
			t.Fatalf("update context-ranking include connection %d context capabilities: %v", connectionID, err)
		}
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"model":             publicModelID,
		"input":             "responses include should still choose the earlier translated sibling",
		"include":           []string{"file_search_call.results"},
		"max_output_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, translatedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/proxy-selector/context-ranking-include/translated/v1/chat/completions",
		ModelID: publicModelID,
	}})
	if got := len(nativeUpstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected later native candidate to remain unattempted when include fallback selects translated candidate, got %d upstream requests", got)
	}
	assertLatestRuntimeOperationName(t, harness.conn, profileID, "openai.responses")
	assertLatestProxySelectorAttribution(t, harness, profileID, translatedConnectionID, "openai.chat_completions", string(runtimeapi.TranslationModeOpenAIResponsesToChatCompletions), "/v1/chat/completions")
}

func assertLatestProxySelectorAttribution(t *testing.T, harness *runtimeHarness, profileID int, connectionID int, operationName string, translationModeValue string, requestPath string) {
	t.Helper()
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
		t.Fatalf("load proxy-selector request-log attribution: %v", err)
	}
	if !selectedTerminalTargetID.Valid || int(selectedTerminalTargetID.Int64) != connectionID {
		t.Fatalf("expected selected_terminal_target_id %d, got %+v", connectionID, selectedTerminalTargetID)
	}
	if !upstreamOperationName.Valid || upstreamOperationName.String != operationName {
		t.Fatalf("expected upstream_operation_name %s, got %+v", operationName, upstreamOperationName)
	}
	if !translationMode.Valid || translationMode.String != translationModeValue {
		t.Fatalf("expected operation_translation_mode %s, got %+v", translationModeValue, translationMode)
	}
	if !upstreamRequestPath.Valid || upstreamRequestPath.String != requestPath {
		t.Fatalf("expected upstream_request_path %s, got %+v", requestPath, upstreamRequestPath)
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
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, secondTargetConfigID, 1)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, firstTargetConfigID, 0)
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
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, lowPositionTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, positionTwoTargetConfigID, 2)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, positionOneTargetConfigID, 1)
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
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, fallbackTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, preferredTargetConfigID, 1)
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

func TestRuntimeProxySelectorFlatRoundRobinUsesPositionCursorSequence(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-flat-public-" + suffix
	firstTargetModelID := "proxy-selector-flat-first-" + suffix
	secondTargetModelID := "proxy-selector-flat-second-" + suffix

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-flat-"+suffix, "round-robin")
	firstTargetConfigID := harness.seedModel(t, profileID, "openai", firstTargetModelID, "native", &strategyID)
	secondTargetConfigID := harness.seedModel(t, profileID, "openai", secondTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, firstTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, secondTargetConfigID, 1)
	firstEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-flat-first-"+suffix, harness.upstream.baseURL("/proxy-selector/flat/first"), "proxy-selector-flat-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-flat-second-"+suffix, harness.upstream.baseURL("/proxy-selector/flat/second"), "proxy-selector-flat-second-key", 1)
	harness.seedConnection(t, profileID, firstTargetConfigID, firstEndpointID, "proxy-selector-flat-first-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondTargetConfigID, secondEndpointID, "proxy-selector-flat-second-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	for requestIndex := 0; requestIndex < 4; requestIndex++ {
		response := performProxySelectorChatRequest(t, harness, publicModelID, "flat round-robin deterministic cursor")
		assertStatus(t, response, http.StatusOK)
	}
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{
		{Path: "/proxy-selector/flat/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/proxy-selector/flat/second/v1/chat/completions", ModelID: secondTargetModelID},
		{Path: "/proxy-selector/flat/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/proxy-selector/flat/second/v1/chat/completions", ModelID: secondTargetModelID},
	})
}

func TestRuntimeProxySelectorFlatRoundRobinSkipsUnroutableTargets(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-flat-routable-public-" + suffix
	firstTargetModelID := "proxy-selector-flat-routable-first-" + suffix
	unroutableTargetModelID := "proxy-selector-flat-routable-unroutable-" + suffix
	secondTargetModelID := "proxy-selector-flat-routable-second-" + suffix

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-flat-routable-"+suffix, "round-robin")
	firstTargetConfigID := harness.seedModel(t, profileID, "openai", firstTargetModelID, "native", &strategyID)
	unroutableTargetConfigID := harness.seedModel(t, profileID, "openai", unroutableTargetModelID, "native", &strategyID)
	secondTargetConfigID := harness.seedModel(t, profileID, "openai", secondTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, firstTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, unroutableTargetConfigID, 1)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, secondTargetConfigID, 2)
	firstEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-flat-routable-first-"+suffix, harness.upstream.baseURL("/proxy-selector/flat-routable/first"), "proxy-selector-flat-routable-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-flat-routable-second-"+suffix, harness.upstream.baseURL("/proxy-selector/flat-routable/second"), "proxy-selector-flat-routable-second-key", 1)
	harness.seedConnection(t, profileID, firstTargetConfigID, firstEndpointID, "proxy-selector-flat-routable-first-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, secondTargetConfigID, secondEndpointID, "proxy-selector-flat-routable-second-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	for requestIndex := 0; requestIndex < 4; requestIndex++ {
		response := performProxySelectorChatRequest(t, harness, publicModelID, "flat round-robin skips unroutable target")
		assertStatus(t, response, http.StatusOK)
	}
	assertProxySelectorRequestSequence(t, harness.upstream.requestsSnapshot(), []proxySelectorExpectedRequest{
		{Path: "/proxy-selector/flat-routable/first/v1/chat/completions", ModelID: firstTargetModelID},
		{Path: "/proxy-selector/flat-routable/second/v1/chat/completions", ModelID: secondTargetModelID},
		{Path: "/proxy-selector/flat-routable/second/v1/chat/completions", ModelID: secondTargetModelID},
		{Path: "/proxy-selector/flat-routable/first/v1/chat/completions", ModelID: firstTargetModelID},
	})
}

func TestRuntimeProxySelectorFlatModelTargetDoesNotRetryAlternateTargetAfterUpstreamFailure(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "proxy-selector-flat-no-retry-public-" + suffix
	primaryTargetModelID := "proxy-selector-flat-no-retry-primary-" + suffix
	alternateTargetModelID := "proxy-selector-flat-no-retry-alternate-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "selected target failed"})
	alternateUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-proxy-selector-alternate"})

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "proxy-selector-flat-no-retry-"+suffix, "single")
	primaryTargetConfigID := harness.seedModel(t, profileID, "openai", primaryTargetModelID, "native", &strategyID)
	alternateTargetConfigID := harness.seedModel(t, profileID, "openai", alternateTargetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, primaryTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, alternateTargetConfigID, 1)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-flat-no-retry-primary-"+suffix, primaryUpstream.baseURL("/proxy-selector/flat-no-retry/primary"), "proxy-selector-flat-no-retry-primary-key", 0)
	alternateEndpointID := harness.seedEndpoint(t, profileID, "proxy-selector-flat-no-retry-alternate-"+suffix, alternateUpstream.baseURL("/proxy-selector/flat-no-retry/alternate"), "proxy-selector-flat-no-retry-alternate-key", 1)
	harness.seedConnection(t, profileID, primaryTargetConfigID, primaryEndpointID, "proxy-selector-flat-no-retry-primary-connection-"+suffix, nil, nil, 0)
	harness.seedConnection(t, profileID, alternateTargetConfigID, alternateEndpointID, "proxy-selector-flat-no-retry-alternate-connection-"+suffix, nil, nil, 0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)

	response := performProxySelectorChatRequest(t, harness, publicModelID, "flat model target does not retry another proxy target")
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

func TestRuntimeModelRedirectReentersLoadBalancingAndPersistsRouteReason(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "redirect-model-public-" + suffix
	redirectedModelID := "redirect-model-target-" + suffix
	firstUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "redirect-model-first"})
	secondUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "redirect-model-second"})

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "redirect-model-target-"+suffix, "round-robin")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", nil)
	redirectedModelConfigID := harness.seedModel(t, profileID, "openai", redirectedModelID, "native", &strategyID)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, redirectedModelConfigID, 0)
	firstEndpointID := harness.seedEndpoint(t, profileID, "redirect-model-first-"+suffix, firstUpstream.baseURL("/redirect/model/first"), "redirect-model-first-key", 0)
	secondEndpointID := harness.seedEndpoint(t, profileID, "redirect-model-second-"+suffix, secondUpstream.baseURL("/redirect/model/second"), "redirect-model-second-key", 1)
	firstConnectionID := harness.seedConnection(t, profileID, redirectedModelConfigID, firstEndpointID, "redirect-model-first-connection-"+suffix, nil, nil, 0)
	secondConnectionID := harness.seedConnection(t, profileID, redirectedModelConfigID, secondEndpointID, "redirect-model-second-connection-"+suffix, nil, nil, 1)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, firstConnectionID, 16_384, 4_096, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, secondConnectionID, 16_384, 4_096, 1.0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	for requestIndex := 0; requestIndex < 2; requestIndex++ {
		response := performProxySelectorChatRequest(t, harness, publicModelID, "model redirect re-enters load balancing")
		assertStatus(t, response, http.StatusOK)
	}
	assertProxySelectorRequestSequence(t, firstUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/redirect/model/first/v1/chat/completions", ModelID: redirectedModelID}})
	assertProxySelectorRequestSequence(t, secondUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/redirect/model/second/v1/chat/completions", ModelID: redirectedModelID}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, publicModelID, redirectedModelID)
}

func TestRuntimeUpstreamRedirectPinsCandidateWithoutChangingModel(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	requestedModelID := "redirect-upstream-public-" + suffix
	unroutableModelID := "redirect-upstream-unroutable-" + suffix
	pinnedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "redirect-upstream-pinned"})

	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "redirect-upstream-"+suffix, "fill-first")
	requestedModelConfigID := harness.seedModel(t, profileID, "openai", requestedModelID, "native", &strategyID)
	unroutableModelConfigID := harness.seedModel(t, profileID, "openai", unroutableModelID, "native", &strategyID)
	harness.seedProxyTargetAtPosition(t, requestedModelConfigID, unroutableModelConfigID, 0)
	endpointID := harness.seedEndpoint(t, profileID, "redirect-upstream-pinned-"+suffix, pinnedUpstream.baseURL("/redirect/upstream/pinned"), "redirect-upstream-pinned-key", 0)
	connectionID := harness.seedConnection(t, profileID, requestedModelConfigID, endpointID, "redirect-upstream-pinned-connection-"+suffix, nil, nil, 1)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := performProxySelectorChatRequest(t, harness, requestedModelID, "upstream redirect pins connection")
	assertStatus(t, response, http.StatusOK)
	assertProxySelectorRequestSequence(t, pinnedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/redirect/upstream/pinned/v1/chat/completions", ModelID: requestedModelID}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, requestedModelID, requestedModelID)
	if modelConfigID := harness.modelConfigIDForConnection(t, connectionID); modelConfigID != requestedModelConfigID {
		t.Fatalf("expected pinned connection to stay owned by requested model config %d, got %d", requestedModelConfigID, modelConfigID)
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
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, lowerPositionTargetConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, selectedTargetConfigID, 1)
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

type facadeTargetSeed struct {
	ModelID                    string
	EndpointBaseURL            string
	EndpointAPIKey             string
	OpenAIProbeEndpointVariant *string
	OpenAITextCapability       *string
}

type seededFacadeRoute struct {
	PublicModelID  string
	TargetModelIDs []string
	ConnectionIDs  []int
}

type seededTopologyCascadeRoute struct {
	PublicModelID         string
	PrimaryModelID        string
	GPT54ModelID          string
	DeepSeekModelID       string
	LaterNativeModelID    string
	PrimaryPathPrefix     string
	GPT54PathPrefix       string
	DeepSeekPathPrefix    string
	LaterNativePathPrefix string
	PrimaryUpstream       *scriptedUpstream
	GPT54Upstream         *scriptedUpstream
	DeepSeekUpstream      *scriptedUpstream
	LaterNativeUpstream   *scriptedUpstream
}

func seedOpenAIFacadeRoute(t *testing.T, harness *runtimeHarness, profileID int, publicModelID string, targets []facadeTargetSeed) seededFacadeRoute {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	publicStrategyID := harness.seedLegacyStrategy(t, profileID, "facade-public-"+randomSuffix(), "fill-first")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &publicStrategyID)
	enableRuntimeHarnessFacadeModel(t, harness, publicModelConfigID)
	route := seededFacadeRoute{PublicModelID: publicModelID, TargetModelIDs: make([]string, 0, len(targets)), ConnectionIDs: make([]int, 0, len(targets))}
	for index, target := range targets {
		targetStrategyID := harness.seedLegacyStrategy(t, profileID, "facade-target-"+randomSuffix(), "fill-first")
		targetModelConfigID := harness.seedModel(t, profileID, "openai", target.ModelID, "native", &targetStrategyID)
		harness.seedProxyTargetAtPosition(t, publicModelConfigID, targetModelConfigID, index)
		endpointID := harness.seedEndpoint(t, profileID, "facade-endpoint-"+target.ModelID, target.EndpointBaseURL, target.EndpointAPIKey, index)
		connectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, targetModelConfigID, endpointID, "facade-connection-"+target.ModelID, nil, nil, 0, target.OpenAIProbeEndpointVariant, target.OpenAITextCapability)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, connectionID, 16_384, 1_024, 1.0)
		route.TargetModelIDs = append(route.TargetModelIDs, target.ModelID)
		route.ConnectionIDs = append(route.ConnectionIDs, connectionID)
	}
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return route
}

func seedOpenAITopologyCascadeRoute(t *testing.T, harness *runtimeHarness, profileID int, pathPrefix string) seededTopologyCascadeRoute {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	publicStrategyID := harness.seedLegacyStrategy(t, profileID, "topology-cascade-public-"+randomSuffix(), "fill-first")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", "gpt-5.5", "proxy", &publicStrategyID)
	enableRuntimeHarnessFacadeModel(t, harness, publicModelConfigID)
	primaryStrategyID := harness.seedLegacyStrategy(t, profileID, "topology-cascade-primary-"+randomSuffix(), "fill-first")
	gpt54StrategyID := harness.seedLegacyStrategy(t, profileID, "topology-cascade-gpt54-"+randomSuffix(), "fill-first")
	deepSeekStrategyID := harness.seedLegacyStrategy(t, profileID, "topology-cascade-deepseek-"+randomSuffix(), "fill-first")
	laterNativeStrategyID := harness.seedLegacyStrategy(t, profileID, "topology-cascade-later-native-"+randomSuffix(), "fill-first")
	primaryModelConfigID := harness.seedModel(t, profileID, "openai", "gpt-5.5-primary", "native", &primaryStrategyID)
	gpt54ModelConfigID := harness.seedModel(t, profileID, "openai", "gpt-5.4", "native", &gpt54StrategyID)
	deepSeekModelConfigID := harness.seedModel(t, profileID, "openai", "deepseek-v4-flash", "native", &deepSeekStrategyID)
	laterNativeModelConfigID := harness.seedModel(t, profileID, "openai", "gpt-5.3-later-native", "native", &laterNativeStrategyID)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, primaryModelConfigID, 0)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, gpt54ModelConfigID, 1)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, deepSeekModelConfigID, 2)
	harness.seedProxyTargetAtPosition(t, publicModelConfigID, laterNativeModelConfigID, 3)
	responsesVariant := "responses_reasoning_none"
	chatOnlyVariant := "chat_completions_reasoning_none"
	primaryPathPrefix := pathPrefix + "/primary"
	gpt54PathPrefix := pathPrefix + "/gpt-5-4"
	deepSeekPathPrefix := pathPrefix + "/deepseek"
	laterNativePathPrefix := pathPrefix + "/later-native"
	primaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "resp_topology_cascade_primary",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "primary child"}},
		}},
		"usage": map[string]any{"input_tokens": 7, "output_tokens": 11, "total_tokens": 18},
	})
	gpt54Upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "resp_topology_cascade_gpt54",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "gpt-5.4 long context"}},
		}},
		"usage": map[string]any{"input_tokens": 9, "output_tokens": 15, "total_tokens": 24},
	})
	deepSeekUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl_topology_cascade_deepseek",
		"object": "chat.completion",
		"model":  "deepseek-v4-flash",
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "deepseek fallback",
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 14, "total_tokens": 24},
	})
	laterNativeUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id": "resp_topology_cascade_later_native",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "later native"}},
		}},
		"usage": map[string]any{"input_tokens": 11, "output_tokens": 17, "total_tokens": 28},
	})
	primaryEndpointID := harness.seedEndpoint(t, profileID, "topology-cascade-primary-endpoint-"+randomSuffix(), primaryUpstream.baseURL(primaryPathPrefix), "topology-cascade-primary-key", 0)
	gpt54EndpointID := harness.seedEndpoint(t, profileID, "topology-cascade-gpt54-endpoint-"+randomSuffix(), gpt54Upstream.baseURL(gpt54PathPrefix), "topology-cascade-gpt54-key", 1)
	deepSeekEndpointID := harness.seedEndpoint(t, profileID, "topology-cascade-deepseek-endpoint-"+randomSuffix(), deepSeekUpstream.baseURL(deepSeekPathPrefix), "topology-cascade-deepseek-key", 2)
	laterNativeEndpointID := harness.seedEndpoint(t, profileID, "topology-cascade-later-native-endpoint-"+randomSuffix(), laterNativeUpstream.baseURL(laterNativePathPrefix), "topology-cascade-later-native-key", 3)
	primaryConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, primaryModelConfigID, primaryEndpointID, "topology-cascade-primary-connection-"+randomSuffix(), nil, nil, 0, &responsesVariant, runtimeStringPtr("responses_only"))
	gpt54ConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, gpt54ModelConfigID, gpt54EndpointID, "topology-cascade-gpt54-connection-"+randomSuffix(), nil, nil, 0, &responsesVariant, runtimeStringPtr("responses_only"))
	deepSeekConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, deepSeekModelConfigID, deepSeekEndpointID, "topology-cascade-deepseek-connection-"+randomSuffix(), nil, nil, 0, &chatOnlyVariant, runtimeStringPtr("chat_completions_only"))
	laterNativeConnectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, laterNativeModelConfigID, laterNativeEndpointID, "topology-cascade-later-native-connection-"+randomSuffix(), nil, nil, 0, &responsesVariant, runtimeStringPtr("responses_only"))
	setRuntimeHarnessConnectionContextCapabilities(t, harness, primaryConnectionID, 400, 64, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, gpt54ConnectionID, 1_400, 64, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, deepSeekConnectionID, 2_400, 64, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, laterNativeConnectionID, 3_200, 64, 1.0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	return seededTopologyCascadeRoute{
		PublicModelID:         "gpt-5.5",
		PrimaryModelID:        "gpt-5.5-primary",
		GPT54ModelID:          "gpt-5.4",
		DeepSeekModelID:       "deepseek-v4-flash",
		LaterNativeModelID:    "gpt-5.3-later-native",
		PrimaryPathPrefix:     primaryPathPrefix,
		GPT54PathPrefix:       gpt54PathPrefix,
		DeepSeekPathPrefix:    deepSeekPathPrefix,
		LaterNativePathPrefix: laterNativePathPrefix,
		PrimaryUpstream:       primaryUpstream,
		GPT54Upstream:         gpt54Upstream,
		DeepSeekUpstream:      deepSeekUpstream,
		LaterNativeUpstream:   laterNativeUpstream,
	}
}

func enableRuntimeHarnessFacadeModel(t *testing.T, harness *runtimeHarness, modelConfigID int) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET facade_enabled = TRUE, facade_selection_policy = 'ordered_eligible_context', facade_fallback_policy = 'skip_ineligible_targets', updated_at = $2 WHERE id = $1`, modelConfigID, now); err != nil {
		t.Fatalf("enable runtime facade model %d: %v", modelConfigID, err)
	}
}

func setRuntimeHarnessConnectionContextCapabilities(t *testing.T, harness *runtimeHarness, connectionID int, contextWindowTokens int, defaultOutputTokenReserve int, maxContextUtilization float64) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET context_window_tokens = $2, default_output_token_reserve = $3, max_context_utilization = $4, updated_at = $5 WHERE id = $1`, connectionID, contextWindowTokens, defaultOutputTokenReserve, maxContextUtilization, now); err != nil {
		t.Fatalf("update runtime facade connection %d context capabilities: %v", connectionID, err)
	}
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

func performProxySelectorResponsesTextRequest(t *testing.T, harness *runtimeHarness, modelID string, input string, maxOutputTokens int) *http.Response {
	t.Helper()
	return harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/responses",
		map[string]any{
			"input":             input,
			"model":             modelID,
			"max_output_tokens": maxOutputTokens,
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

func assertNoScriptedUpstreamRequests(t *testing.T, upstream *scriptedUpstream, name string) {
	t.Helper()
	if got := len(upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected %s to stay unattempted, got %d requests", name, got)
	}
}

func assertOpenAIRequestTranslationUnsupported(t *testing.T, response *http.Response, wantMode string, wantReason string) {
	t.Helper()
	assertStatus(t, response, http.StatusBadRequest)
	payload := runtimeResponsePayload(t, response)
	if payload["error"] != "openai_request_translation_unsupported" || payload["translation_mode"] != wantMode || payload["unsupported_reason"] != wantReason {
		t.Fatalf("expected unsupported OpenAI request translation %s/%s, got %+v", wantMode, wantReason, payload)
	}
}

func assertOpenAIResponseTranslationUnsupported(t *testing.T, response *http.Response, wantMode string, wantReason string) {
	t.Helper()
	assertStatus(t, response, http.StatusBadGateway)
	payload := runtimeResponsePayload(t, response)
	if payload["error"] != "openai_response_translation_unsupported" || payload["translation_mode"] != wantMode || payload["unsupported_reason"] != wantReason {
		t.Fatalf("expected unsupported OpenAI response translation %s/%s, got %+v", wantMode, wantReason, payload)
	}
}

func (h *runtimeHarness) setProxySelectionStrategy(tb testing.TB, modelConfigID int, strategy string) {
	tb.Helper()
	_ = h
	_ = modelConfigID
	_ = strategy
	tb.Skip("legacy proxy selection strategies were removed; unified access targets use each model's legacy strategy")
}

func TestRuntimeProxySelectorRetriesProvider429AndPersistsRouteReason(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "retry-429-public-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusTooManyRequests, map[string]any{"error": "primary rate limited"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-retry-429-secondary"})

	seedRetryPolicyNativeRoute(t, harness, profileID, modelID, primaryUpstream.baseURL("/retry/429/primary"), secondaryUpstream.baseURL("/retry/429/secondary"))
	response := performProxySelectorChatRequest(t, harness, modelID, "provider 429 should retry before commit")
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-retry-429-secondary")
	assertProxySelectorRequestSequence(t, primaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/retry/429/primary/v1/chat/completions", ModelID: modelID}})
	assertProxySelectorRequestSequence(t, secondaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/retry/429/secondary/v1/chat/completions", ModelID: modelID}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeProxySelectorRetriesConfiguredProvider5xxAndPersistsRouteReason(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "retry-5xx-public-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "primary unavailable"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-retry-5xx-secondary"})

	seedRetryPolicyNativeRoute(t, harness, profileID, modelID, primaryUpstream.baseURL("/retry/5xx/primary"), secondaryUpstream.baseURL("/retry/5xx/secondary"))
	response := performProxySelectorChatRequest(t, harness, modelID, "configured provider 5xx should retry before commit")
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-retry-5xx-secondary")
	assertProxySelectorRequestSequence(t, primaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/retry/5xx/primary/v1/chat/completions", ModelID: modelID}})
	assertProxySelectorRequestSequence(t, secondaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/retry/5xx/secondary/v1/chat/completions", ModelID: modelID}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
}

func TestRuntimeProxySelectorDoesNotRetryAuthOrValidationProviderErrors(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "retry-no-auth-validation-public-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusForbidden, map[string]any{"error": "auth failed"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-no-retry-secondary"})

	seedRetryPolicyNativeRoute(t, harness, profileID, modelID, primaryUpstream.baseURL("/retry/no-auth/primary"), secondaryUpstream.baseURL("/retry/no-auth/secondary"))
	response := performProxySelectorChatRequest(t, harness, modelID, "provider auth error must not retry")
	assertStatus(t, response, http.StatusForbidden)
	assertProxySelectorRequestSequence(t, primaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/retry/no-auth/primary/v1/chat/completions", ModelID: modelID}})
	assertNoScriptedUpstreamRequests(t, secondaryUpstream, "auth-error secondary candidate")
}

type retryPolicyTimeoutTransport struct {
	base http.RoundTripper
}

func (transport retryPolicyTimeoutTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Host == "retry-connect-timeout.invalid" {
		return nil, retryPolicyTimeoutError{}
	}
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(request)
}

type retryPolicyTimeoutError struct{}

func (retryPolicyTimeoutError) Error() string { return "connect timeout" }
func (retryPolicyTimeoutError) Timeout() bool { return true }

func TestRuntimeProxySelectorRetriesConnectTimeoutAndPersistsRouteReason(t *testing.T) {
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{RuntimeOptions: runtimeapi.Options{HTTPClient: &http.Client{Transport: retryPolicyTimeoutTransport{}}}})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "retry-connect-timeout-public-" + suffix
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-retry-connect-timeout-secondary"})

	seedRetryPolicyNativeRoute(t, harness, profileID, modelID, "http://retry-connect-timeout.invalid/retry/connect-timeout/primary", secondaryUpstream.baseURL("/retry/connect-timeout/secondary"))
	response := performProxySelectorChatRequest(t, harness, modelID, "connect timeout should retry before commit")
	assertStatus(t, response, http.StatusOK)
	assertResponseField(t, response, "id", "chatcmpl-retry-connect-timeout-secondary")
	assertProxySelectorRequestSequence(t, secondaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/retry/connect-timeout/secondary/v1/chat/completions", ModelID: modelID}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
}

func seedRetryPolicyNativeRoute(t *testing.T, harness *runtimeHarness, profileID int, modelID string, primaryBaseURL string, secondaryBaseURL string) {
	t.Helper()
	releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
	strategyID := harness.seedLegacyStrategy(t, profileID, "retry-policy-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	primaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-primary-"+randomSuffix(), primaryBaseURL, "retry-policy-primary-key", 0)
	secondaryEndpointID := harness.seedEndpoint(t, profileID, "retry-policy-secondary-"+randomSuffix(), secondaryBaseURL, "retry-policy-secondary-key", 1)
	primaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "retry-policy-primary-connection-"+randomSuffix(), nil, nil, 0)
	secondaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "retry-policy-secondary-connection-"+randomSuffix(), nil, nil, 1)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, primaryConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, secondaryConnectionID, 16_384, 1_024, 1.0)
	releaseRefresh()
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.runtimeService.RuntimeState().ResetProfile(profileID)
}

func TestRuntimeProxySelectorDoesNotRetryValidationProviderErrors(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	modelID := "retry-no-validation-public-" + suffix
	primaryUpstream := newScriptedUpstream(t, http.StatusUnprocessableEntity, map[string]any{"error": "validation failed"})
	secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-validation-secondary"})

	seedRetryPolicyNativeRoute(t, harness, profileID, modelID, primaryUpstream.baseURL("/retry/no-validation/primary"), secondaryUpstream.baseURL("/retry/no-validation/secondary"))
	response := performProxySelectorChatRequest(t, harness, modelID, "provider validation error must not retry")
	assertStatus(t, response, http.StatusUnprocessableEntity)
	assertProxySelectorRequestSequence(t, primaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/retry/no-validation/primary/v1/chat/completions", ModelID: modelID}})
	assertNoScriptedUpstreamRequests(t, secondaryUpstream, "validation-error secondary candidate")
}

func TestResolveModelAccessFromRoutingPlanFlatPoolPreservedForOrdinaryModels(t *testing.T) {
	t.Run("single", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "single primary fails"})
		secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "single-secondary-should-not-run"})
		modelID := "flat-preserved-single-" + suffix
		releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
		strategyID := harness.seedLegacyStrategy(t, profileID, "flat-preserved-single-"+suffix, "single")
		modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
		primaryEndpointID := harness.seedEndpoint(t, profileID, "flat-single-primary-"+suffix, primaryUpstream.baseURL("/flat-preserved/single/primary"), "flat-single-primary-key", 0)
		secondaryEndpointID := harness.seedEndpoint(t, profileID, "flat-single-secondary-"+suffix, secondaryUpstream.baseURL("/flat-preserved/single/secondary"), "flat-single-secondary-key", 1)
		primaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "flat-single-primary-connection-"+suffix, nil, nil, 0)
		secondaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "flat-single-secondary-connection-"+suffix, nil, nil, 1)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, primaryConnectionID, 16_384, 1_024, 1.0)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, secondaryConnectionID, 16_384, 1_024, 1.0)
		releaseRefresh()
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
		response := performProxySelectorChatRequest(t, harness, modelID, "ordinary single keeps first terminal only")
		assertStatus(t, response, http.StatusServiceUnavailable)
		assertProxySelectorRequestSequence(t, primaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/flat-preserved/single/primary/v1/chat/completions", ModelID: modelID}})
		assertNoScriptedUpstreamRequests(t, secondaryUpstream, "single secondary")
	})

	t.Run("fill_first", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		primaryUpstream := newScriptedUpstream(t, http.StatusServiceUnavailable, map[string]any{"error": "fill-first primary retries"})
		secondaryUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "flat-fill-secondary", "object": "chat.completion", "usage": map[string]any{"prompt_tokens": 6, "completion_tokens": 3, "total_tokens": 9}})
		modelID := "flat-preserved-fill-" + suffix
		releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
		strategyID := harness.seedLegacyStrategy(t, profileID, "flat-preserved-fill-"+suffix, "fill-first")
		modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
		primaryEndpointID := harness.seedEndpoint(t, profileID, "flat-fill-primary-"+suffix, primaryUpstream.baseURL("/flat-preserved/fill/primary"), "flat-fill-primary-key", 0)
		secondaryEndpointID := harness.seedEndpoint(t, profileID, "flat-fill-secondary-"+suffix, secondaryUpstream.baseURL("/flat-preserved/fill/secondary"), "flat-fill-secondary-key", 1)
		primaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, primaryEndpointID, "flat-fill-primary-connection-"+suffix, nil, nil, 0)
		secondaryConnectionID := harness.seedConnection(t, profileID, modelConfigID, secondaryEndpointID, "flat-fill-secondary-connection-"+suffix, nil, nil, 1)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, primaryConnectionID, 16_384, 1_024, 1.0)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, secondaryConnectionID, 16_384, 1_024, 1.0)
		releaseRefresh()
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
		response := performProxySelectorChatRequest(t, harness, modelID, "ordinary fill-first keeps flat retry pool")
		assertStatus(t, response, http.StatusOK)
		assertProxySelectorRequestSequence(t, primaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/flat-preserved/fill/primary/v1/chat/completions", ModelID: modelID}})
		assertProxySelectorRequestSequence(t, secondaryUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/flat-preserved/fill/secondary/v1/chat/completions", ModelID: modelID}})
	})

	t.Run("round_robin", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		firstUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "flat-round-first"})
		secondUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "flat-round-second"})
		modelID := "flat-preserved-round-" + suffix
		releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
		strategyID := harness.seedLegacyStrategy(t, profileID, "flat-preserved-round-"+suffix, "round-robin")
		modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
		firstEndpointID := harness.seedEndpoint(t, profileID, "flat-round-first-"+suffix, firstUpstream.baseURL("/flat-preserved/round/first"), "flat-round-first-key", 0)
		secondEndpointID := harness.seedEndpoint(t, profileID, "flat-round-second-"+suffix, secondUpstream.baseURL("/flat-preserved/round/second"), "flat-round-second-key", 1)
		firstConnectionID := harness.seedConnection(t, profileID, modelConfigID, firstEndpointID, "flat-round-first-connection-"+suffix, nil, nil, 0)
		secondConnectionID := harness.seedConnection(t, profileID, modelConfigID, secondEndpointID, "flat-round-second-connection-"+suffix, nil, nil, 1)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, firstConnectionID, 16_384, 1_024, 1.0)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, secondConnectionID, 16_384, 1_024, 1.0)
		releaseRefresh()
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
		assertStatus(t, performProxySelectorChatRequest(t, harness, modelID, "ordinary round robin first"), http.StatusOK)
		assertStatus(t, performProxySelectorChatRequest(t, harness, modelID, "ordinary round robin second"), http.StatusOK)
		assertProxySelectorRequestSequence(t, firstUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/flat-preserved/round/first/v1/chat/completions", ModelID: modelID}})
		assertProxySelectorRequestSequence(t, secondUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/flat-preserved/round/second/v1/chat/completions", ModelID: modelID}})
	})

	t.Run("fill_first", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		firstUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "flat-first", "object": "chat.completion", "usage": map[string]any{"prompt_tokens": 6, "completion_tokens": 3, "total_tokens": 9}})
		secondUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "second-should-not-win"})
		modelID := "gpt-5-flat-preserved-fill-first-" + suffix
		releaseRefresh := harness.suspendRuntimeSnapshotRefresh()
		strategyID := harness.seedLegacyStrategy(t, profileID, "flat-preserved-fill-first-"+suffix, "fill-first")
		modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
		firstEndpointID := harness.seedEndpoint(t, profileID, "flat-fill-first-first-"+suffix, firstUpstream.baseURL("/flat-preserved/fill-first/first"), "flat-fill-first-first-key", 0)
		secondEndpointID := harness.seedEndpoint(t, profileID, "flat-fill-first-second-"+suffix, secondUpstream.baseURL("/flat-preserved/fill-first/second"), "flat-fill-first-second-key", 1)
		firstConnectionID := harness.seedConnection(t, profileID, modelConfigID, firstEndpointID, "flat-fill-first-first-connection-"+suffix, nil, nil, 0)
		secondConnectionID := harness.seedConnection(t, profileID, modelConfigID, secondEndpointID, "flat-fill-first-second-connection-"+suffix, nil, nil, 1)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, firstConnectionID, 16_384, 1_024, 1.0)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, secondConnectionID, 16_384, 1_024, 1.0)
		releaseRefresh()
		harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
		response := performProxySelectorChatRequest(t, harness, modelID, "ordinary fill-first keeps policy ranking")
		assertStatus(t, response, http.StatusOK)
		assertProxySelectorRequestSequence(t, firstUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/flat-preserved/fill-first/first/v1/chat/completions", ModelID: modelID}})
		assertNoScriptedUpstreamRequests(t, secondUpstream, "second fill-first candidate")
	})
}
