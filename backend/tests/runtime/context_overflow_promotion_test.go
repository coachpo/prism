package runtime_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

const overflowAffinityCacheTestHeaderValue = "affinity-token"

func TestNonStreamOverflowPromotesOnce(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow should promote"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-promote-once-public-" + suffix,
		TargetModelID:   "overflow-promote-once-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/promote-once/source"),
		EndpointAPIKey:  "overflow-promote-once-source-key",
	})
	promotedModelID := "overflow-promote-once-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("promoted overflow becomes final"))
	_, promotedConnectionID := seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/promote-once/promoted"), "overflow-promote-once-promoted-key", nil, 32_768)
	thirdModelID := "overflow-promote-once-third-" + suffix
	thirdUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-third-should-not-run"})
	_, thirdConnectionID := seedRuntimePromotionNativeModel(t, harness, profileID, thirdModelID, thirdUpstream.baseURL("/overflow/promote-once/third"), "overflow-promote-once-third-key", nil, 65_536)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, promotedModelID, thirdModelID)

	response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "promote once only")
	assertStatus(t, response, http.StatusBadRequest)
	payload := runtimeResponsePayload(t, response)
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok || errorPayload["message"] != "promoted overflow becomes final" {
		t.Fatalf("expected promoted overflow payload to become final response, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/promote-once/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/promote-once/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
	assertNoScriptedUpstreamRequests(t, thirdUpstream, "third promotion target")
	if promotedConnectionID == thirdConnectionID {
		t.Fatalf("expected distinct promoted and third connections, got %d", promotedConnectionID)
	}
}

func TestPromotedAttemptBecomesFinalResponse(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow should be replaced"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-final-response-public-" + suffix,
		TargetModelID:   "overflow-final-response-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/final/source"),
		EndpointAPIKey:  "overflow-final-source-key",
	})
	promotedModelID := "overflow-final-response-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-promoted-success-" + suffix,
		"object": "chat.completion",
		"usage":  map[string]any{"prompt_tokens": 7, "completion_tokens": 5, "total_tokens": 12},
	})
	_, promotedConnectionID := seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/final/promoted"), "overflow-final-promoted-key", nil, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	sourceEndpointID := loadRuntimeEndpointIDForConnection(t, harness, route.ConnectionID)
	promotedEndpointID := loadRuntimeEndpointIDForConnection(t, harness, promotedConnectionID)

	response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "promoted response becomes final")
	assertStatus(t, response, http.StatusOK)
	payload := runtimeResponsePayload(t, response)
	if payload["id"] != "chatcmpl-promoted-success-"+suffix {
		t.Fatalf("expected promoted success payload to reach client, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/final/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/final/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 2, 2)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, promotedModelID)
	assertLatestRuntimeAttemptSequence(t, harness.conn, profileID, []runtimeRequestLogAttempt{{
		AttemptNumber: 1,
		ConnectionID:  route.ConnectionID,
		EndpointID:    sourceEndpointID,
		StatusCode:    http.StatusBadRequest,
		SuccessFlag:   false,
	}, {
		AttemptNumber: 2,
		ConnectionID:  promotedConnectionID,
		EndpointID:    promotedEndpointID,
		StatusCode:    http.StatusOK,
		SuccessFlag:   true,
	}})
}

func TestPromotionIneligibleReturnsOriginalSourceResponse(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow should stay final"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-ineligible-public-" + suffix,
		TargetModelID:   "overflow-ineligible-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/ineligible/source"),
		EndpointAPIKey:  "overflow-ineligible-source-key",
	})
	promotedModelID := "overflow-ineligible-disabled-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-disabled-should-not-run"})
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/ineligible/promoted"), "overflow-ineligible-promoted-key", nil, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)
	setRuntimeHarnessModelEnabled(t, harness, profileID, promotedModelID, false)

	response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "ineligible promotion keeps original source response")
	assertStatus(t, response, http.StatusBadRequest)
	payload := runtimeResponsePayload(t, response)
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok || errorPayload["message"] != "source overflow should stay final" {
		t.Fatalf("expected original source overflow payload to survive ineligible promotion, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/ineligible/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, promotedUpstream, "disabled promotion target")
}

func TestFacadeSelectionDoesNotReopenSiblings(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	selectedUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("selected facade child overflow"))
	alternateUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-facade-sibling-should-not-run"})
	route := seedOpenAIFacadeRoute(t, harness, profileID, "overflow-facade-public-"+suffix, []facadeTargetSeed{
		{ModelID: "overflow-facade-selected-" + suffix, EndpointBaseURL: selectedUpstream.baseURL("/overflow/facade/selected"), EndpointAPIKey: "overflow-facade-selected-key", Weight: 1},
		{ModelID: "overflow-facade-alternate-" + suffix, EndpointBaseURL: alternateUpstream.baseURL("/overflow/facade/alternate"), EndpointAPIKey: "overflow-facade-alternate-key", Weight: 1},
	})
	promotedModelID := "overflow-facade-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-facade-promoted-" + suffix,
		"object": "chat.completion",
		"usage":  map[string]any{"prompt_tokens": 9, "completion_tokens": 4, "total_tokens": 13},
	})
	_, promotedConnectionID := seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/facade/promoted"), "overflow-facade-promoted-key", nil, 32_768)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelIDs[0], promotedModelID)

	selectedEndpointID := loadRuntimeEndpointIDForConnection(t, harness, route.ConnectionIDs[0])
	promotedEndpointID := loadRuntimeEndpointIDForConnection(t, harness, promotedConnectionID)

	response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "facade overflow must not reopen siblings")
	assertStatus(t, response, http.StatusOK)
	payload := runtimeResponsePayload(t, response)
	if payload["id"] != "chatcmpl-facade-promoted-"+suffix {
		t.Fatalf("expected promoted facade response to reach client, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, selectedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/facade/selected/v1/chat/completions",
		ModelID: route.TargetModelIDs[0],
	}})
	assertNoScriptedUpstreamRequests(t, alternateUpstream, "alternate facade sibling")
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/facade/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 2, 2)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, promotedModelID)
	assertLatestRuntimeAttemptSequence(t, harness.conn, profileID, []runtimeRequestLogAttempt{{
		AttemptNumber: 1,
		ConnectionID:  route.ConnectionIDs[0],
		EndpointID:    selectedEndpointID,
		StatusCode:    http.StatusBadRequest,
		SuccessFlag:   false,
	}, {
		AttemptNumber: 2,
		ConnectionID:  promotedConnectionID,
		EndpointID:    promotedEndpointID,
		StatusCode:    http.StatusOK,
		SuccessFlag:   true,
	}})
}

func TestTranslatedTopLevelErrorPromotes(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("translated top-level overflow should promote"))
	chatVariant := "chat_completions_reasoning_none"
	route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "overflow-translated-top-level-public", "overflow-translated-top-level-source", sourceUpstream.baseURL("/overflow/translated-top-level/source"), "overflow-translated-top-level-source-key", chatVariant)
	promotedModelID := "overflow-translated-top-level-promoted-" + randomSuffix()
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":    "chatcmpl-translated-top-level-promoted",
		"model": promotedModelID,
		"choices": []map[string]any{{
			"index": 0,
			"message": map[string]any{
				"role":    "assistant",
				"content": "translated promotion success",
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 6, "total_tokens": 16},
	})
	_, promotedConnectionID := seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/translated-top-level/promoted"), "overflow-translated-top-level-promoted-key", &chatVariant, 32_768)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	sourceEndpointID := loadRuntimeEndpointIDForConnection(t, harness, route.ConnectionID)
	promotedEndpointID := loadRuntimeEndpointIDForConnection(t, harness, promotedConnectionID)

	response := performProxySelectorResponsesTextRequest(t, harness, route.PublicModelID, "translated top-level overflow", 64)
	assertStatus(t, response, http.StatusOK)
	payload := runtimeResponsePayload(t, response)
	if got, ok := payload["model"].(string); !ok || got != route.PublicModelID {
		t.Fatalf("expected translated promoted response model %q, got %+v", route.PublicModelID, payload["model"])
	}
	output := payload["output"].([]any)
	message := output[0].(map[string]any)
	parts := message["content"].([]any)
	if len(parts) != 1 || parts[0].(map[string]any)["text"].(string) != "translated promotion success" {
		t.Fatalf("expected translated promoted output text, got %+v", message["content"])
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/translated-top-level/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/translated-top-level/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 2, 2)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, promotedModelID)
	assertLatestRuntimeAttemptSequence(t, harness.conn, profileID, []runtimeRequestLogAttempt{{
		AttemptNumber: 1,
		ConnectionID:  route.ConnectionID,
		EndpointID:    sourceEndpointID,
		StatusCode:    http.StatusBadRequest,
		SuccessFlag:   false,
	}, {
		AttemptNumber: 2,
		ConnectionID:  promotedConnectionID,
		EndpointID:    promotedEndpointID,
		StatusCode:    http.StatusOK,
		SuccessFlag:   true,
	}})
}

func TestPlain429DoesNotPromote(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusTooManyRequests, map[string]any{
		"error": map[string]any{"message": "plain 429 must stay on the source response", "type": "server_error"},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-plain-429-public-" + suffix,
		TargetModelID:   "overflow-plain-429-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/plain-429/source"),
		EndpointAPIKey:  "overflow-plain-429-source-key",
	})
	promotedModelID := "overflow-plain-429-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-plain-429-should-not-run"})
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/plain-429/promoted"), "overflow-plain-429-promoted-key", nil, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "plain 429 must not promote")
	assertStatus(t, response, http.StatusTooManyRequests)
	payload := runtimeResponsePayload(t, response)
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok || errorPayload["message"] != "plain 429 must stay on the source response" {
		t.Fatalf("expected original plain 429 payload to survive, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/plain-429/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, promotedUpstream, "plain 429 promotion target")
}

func TestBodyConfirmed429Promotes(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusTooManyRequests, runtimeBodyConfirmed429OverflowPayload("body-confirmed 429 should promote"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-body-confirmed-429-public-" + suffix,
		TargetModelID:   "overflow-body-confirmed-429-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/body-confirmed-429/source"),
		EndpointAPIKey:  "overflow-body-confirmed-429-source-key",
	})
	promotedModelID := "overflow-body-confirmed-429-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "chatcmpl-body-confirmed-429-promoted",
		"object": "chat.completion",
		"usage":  map[string]any{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
	})
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/body-confirmed-429/promoted"), "overflow-body-confirmed-429-promoted-key", nil, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "body-confirmed 429 should promote")
	assertStatus(t, response, http.StatusOK)
	payload := runtimeResponsePayload(t, response)
	if payload["id"] != "chatcmpl-body-confirmed-429-promoted" {
		t.Fatalf("expected promoted success payload to replace body-confirmed 429, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/body-confirmed-429/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/body-confirmed-429/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
}

func TestOverflowAffinityCachePopulatesAfterSuccessfulPromotion(t *testing.T) {
	fixture := newOverflowAffinityCacheRuntimeFixture(t, "populate-success", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow learns target"), http.StatusOK, map[string]any{
		"id":     "chatcmpl-overflow-affinity-populated",
		"object": "chat.completion",
		"usage":  map[string]any{"prompt_tokens": 6, "completion_tokens": 4, "total_tokens": 10},
	})

	firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
	assertStatus(t, firstResponse, http.StatusOK)
	secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second matching request", overflowAffinityCacheHeaders())
	assertStatus(t, secondResponse, http.StatusOK)

	assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/populate-success/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}})
	assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/populate-success/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}, {
		Path:    "/overflow/affinity/populate-success/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}})
}

func TestOverflowAffinityCachePlain429DoesNotPopulate(t *testing.T) {
	fixture := newOverflowAffinityCacheRuntimeFixture(t, "plain-429", http.StatusTooManyRequests, map[string]any{
		"error": map[string]any{"message": "plain rate limit", "type": "server_error"},
	}, http.StatusOK, map[string]any{"id": "chatcmpl-plain-429-should-not-run"})

	firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "plain 429 first request", overflowAffinityCacheHeaders())
	assertStatus(t, firstResponse, http.StatusTooManyRequests)
	fixture.harness.runtimeService.RuntimeState().ResetProfile(fixture.profileID)
	secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "plain 429 second request", overflowAffinityCacheHeaders())
	assertStatus(t, secondResponse, http.StatusTooManyRequests)

	assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/plain-429/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}, {
		Path:    "/overflow/affinity/plain-429/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, fixture.promotedUpstream, "plain 429 promotion target")
}

func TestOverflowAffinityCacheSecondMatchingChatRequestStartsAtPromotionTarget(t *testing.T) {
	fixture := newOverflowAffinityCacheRuntimeFixture(t, "second-matching", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow learns target"), http.StatusBadRequest, runtimeOverflowErrorPayload("promoted overflow stays final"))
	firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
	assertStatus(t, firstResponse, http.StatusBadRequest)
	secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second matching request", overflowAffinityCacheHeaders())
	assertStatus(t, secondResponse, http.StatusBadRequest)

	assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/second-matching/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}})
	assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/second-matching/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}, {
		Path:    "/overflow/affinity/second-matching/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}})
}

func TestOverflowAffinityCacheSecondMatchingResponsesRequestStartsAtPromotionTarget(t *testing.T) {
	fixture := newOverflowAffinityCacheRuntimeFixture(t, "second-matching-responses", http.StatusBadRequest, runtimeOverflowErrorPayload("source responses overflow learns target"), http.StatusOK, map[string]any{
		"id":     "resp-overflow-affinity-promoted",
		"object": "response",
		"output": []map[string]any{},
		"usage":  map[string]any{"input_tokens": 6, "output_tokens": 4, "total_tokens": 10},
	})
	firstResponse := performOverflowAffinityResponsesTextRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing responses request", 64, overflowAffinityCacheHeaders())
	assertStatus(t, firstResponse, http.StatusOK)
	secondResponse := performOverflowAffinityResponsesTextRequest(t, fixture.harness, fixture.route.PublicModelID, "second matching responses request", 64, overflowAffinityCacheHeaders())
	assertStatus(t, secondResponse, http.StatusOK)

	assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/second-matching-responses/source/v1/responses",
		ModelID: fixture.route.TargetModelID,
	}})
	assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/second-matching-responses/promoted/v1/responses",
		ModelID: fixture.promotedModelID,
	}, {
		Path:    "/overflow/affinity/second-matching-responses/promoted/v1/responses",
		ModelID: fixture.promotedModelID,
	}})
}

func TestOverflowAffinityCacheDifferentAffinityUsesSourceFirst(t *testing.T) {
	fixture := newOverflowAffinityCacheRuntimeFixture(t, "different-affinity", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow learns target"), http.StatusOK, map[string]any{"id": "chatcmpl-different-affinity-promoted"})

	firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
	assertStatus(t, firstResponse, http.StatusOK)
	secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second request with different affinity", map[string]string{"x-session-affinity": "different-affinity-token"})
	assertStatus(t, secondResponse, http.StatusOK)

	assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/different-affinity/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}, {
		Path:    "/overflow/affinity/different-affinity/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}})
	assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/different-affinity/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}, {
		Path:    "/overflow/affinity/different-affinity/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}})
}

func TestOverflowAffinityCacheMissingAffinityUsesSourceFirst(t *testing.T) {
	t.Run("missing affinity", func(t *testing.T) {
		fixture := newOverflowAffinityCacheRuntimeFixture(t, "missing-affinity", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow learns target"), http.StatusOK, map[string]any{"id": "chatcmpl-missing-affinity-promoted"})

		firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
		assertStatus(t, firstResponse, http.StatusOK)
		secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second request without affinity", nil)
		assertStatus(t, secondResponse, http.StatusOK)

		assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/overflow/affinity/missing-affinity/source/v1/chat/completions", ModelID: fixture.route.TargetModelID}, {Path: "/overflow/affinity/missing-affinity/source/v1/chat/completions", ModelID: fixture.route.TargetModelID}})
		assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/overflow/affinity/missing-affinity/promoted/v1/chat/completions", ModelID: fixture.promotedModelID}, {Path: "/overflow/affinity/missing-affinity/promoted/v1/chat/completions", ModelID: fixture.promotedModelID}})
	})
	t.Run("invalid affinity", func(t *testing.T) {
		fixture := newOverflowAffinityCacheRuntimeFixture(t, "invalid-affinity", http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow learns target"), http.StatusOK, map[string]any{"id": "chatcmpl-invalid-affinity-promoted"})

		firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
		assertStatus(t, firstResponse, http.StatusOK)
		secondResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "second request with invalid affinity", map[string]string{"x-session-affinity": strings.Repeat("x", 257)})
		assertStatus(t, secondResponse, http.StatusOK)

		assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/overflow/affinity/invalid-affinity/source/v1/chat/completions", ModelID: fixture.route.TargetModelID}, {Path: "/overflow/affinity/invalid-affinity/source/v1/chat/completions", ModelID: fixture.route.TargetModelID}})
		assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{Path: "/overflow/affinity/invalid-affinity/promoted/v1/chat/completions", ModelID: fixture.promotedModelID}, {Path: "/overflow/affinity/invalid-affinity/promoted/v1/chat/completions", ModelID: fixture.promotedModelID}})
	})
}

func TestOverflowAffinityCacheStreamingRequestUsesSourceFirst(t *testing.T) {
	fixture := newOverflowAffinityCacheRuntimeFixture(t, "streaming", http.StatusBadRequest, runtimeOverflowErrorPayload("streaming source overflow stays final"), http.StatusOK, map[string]any{"id": "chatcmpl-streaming-affinity-promoted"})

	firstResponse := performOverflowAffinityChatRequest(t, fixture.harness, fixture.route.PublicModelID, "first overflowing request", overflowAffinityCacheHeaders())
	assertStatus(t, firstResponse, http.StatusOK)
	streamingResponse := fixture.harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "streaming request must not use cache"}},
		"model":    fixture.route.PublicModelID,
		"stream":   true,
	}, overflowAffinityCacheHeaders())
	assertStatus(t, streamingResponse, http.StatusBadRequest)

	assertOverflowAffinityRequestSequence(t, fixture.sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/streaming/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}, {
		Path:    "/overflow/affinity/streaming/source/v1/chat/completions",
		ModelID: fixture.route.TargetModelID,
	}})
	assertOverflowAffinityRequestSequence(t, fixture.promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/affinity/streaming/promoted/v1/chat/completions",
		ModelID: fixture.promotedModelID,
	}})
}

func TestStreamingOverflowDoesNotPromote(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("streaming overflow must stay on the source response"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-streaming-public-" + suffix,
		TargetModelID:   "overflow-streaming-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/streaming/source"),
		EndpointAPIKey:  "overflow-streaming-source-key",
	})
	promotedModelID := "overflow-streaming-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-streaming-should-not-run"})
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/streaming/promoted"), "overflow-streaming-promoted-key", nil, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "streaming overflow must not promote"}},
		"model":    route.PublicModelID,
		"stream":   true,
	}, nil)
	assertStatus(t, response, http.StatusBadRequest)
	payload := runtimeResponsePayload(t, response)
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok || errorPayload["message"] != "streaming overflow must stay on the source response" {
		t.Fatalf("expected streaming overflow payload to survive unchanged, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/streaming/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, promotedUpstream, "streaming promotion target")
}

func TestPromotionDoesNotMultiHop(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source overflow should promote once"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-no-multihop-public-" + suffix,
		TargetModelID:   "overflow-no-multihop-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/no-multihop/source"),
		EndpointAPIKey:  "overflow-no-multihop-source-key",
	})
	promotedModelID := "overflow-no-multihop-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusTooManyRequests, runtimeBodyConfirmed429OverflowPayload("promoted overflow must stay final"))
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/no-multihop/promoted"), "overflow-no-multihop-promoted-key", nil, 32_768)
	thirdModelID := "overflow-no-multihop-third-" + suffix
	thirdUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-no-multihop-third-should-not-run"})
	seedRuntimePromotionNativeModel(t, harness, profileID, thirdModelID, thirdUpstream.baseURL("/overflow/no-multihop/third"), "overflow-no-multihop-third-key", nil, 65_536)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, promotedModelID, thirdModelID)

	response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "overflow promotion must not multi-hop")
	assertStatus(t, response, http.StatusTooManyRequests)
	payload := runtimeResponsePayload(t, response)
	if payload["code"] != "context_too_large" || payload["detail"] != "promoted overflow must stay final" {
		t.Fatalf("expected promoted overflow payload to stay final without multi-hop replay, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/no-multihop/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/no-multihop/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
	assertNoScriptedUpstreamRequests(t, thirdUpstream, "second promotion target")
}

func TestTranslatedFlatGatewayJSONSkipsPromotion(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	sourceUpstream := newScriptedUpstream(t, http.StatusTooManyRequests, map[string]any{
		"code":   "context_too_large",
		"detail": "translated flat gateway overflow should stay on the source response",
	})
	route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "overflow-translated-flat-public", "overflow-translated-flat-source", sourceUpstream.baseURL("/overflow/translated-flat/source"), "overflow-translated-flat-source-key", "chat_completions_reasoning_none")
	promotedModelID := "overflow-translated-flat-promoted-" + randomSuffix()
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "resp-translated-flat-should-not-run"})
	responsesVariant := "responses_reasoning_none"
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/translated-flat/promoted"), "overflow-translated-flat-promoted-key", &responsesVariant, 32_768)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := performProxySelectorResponsesTextRequest(t, harness, route.PublicModelID, "translated flat gateway overflow", 64)
	assertStatus(t, response, http.StatusTooManyRequests)
	payload := runtimeResponsePayload(t, response)
	if payload["code"] != "context_too_large" || payload["detail"] != "translated flat gateway overflow should stay on the source response" {
		t.Fatalf("expected translated flat gateway overflow body to survive unchanged, got %+v", payload)
	}
	if payload["error"] == "openai_response_translation_unsupported" {
		t.Fatalf("expected translated flat gateway overflow to skip promotion without translation rejection, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/translated-flat/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, promotedUpstream, "translated flat gateway promotion target")
}

type overflowAffinityCacheRuntimeFixture struct {
	harness          *runtimeHarness
	profileID        int
	route            seededRuntimeRoute
	sourceUpstream   *scriptedUpstream
	promotedModelID  string
	promotedUpstream *scriptedUpstream
}

func newOverflowAffinityCacheRuntimeFixture(t *testing.T, slug string, sourceStatus int, sourceBody map[string]any, promotedStatus int, promotedBody map[string]any) overflowAffinityCacheRuntimeFixture {
	t.Helper()
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, sourceStatus, sourceBody)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "overflow-affinity-" + slug + "-public-" + suffix,
		TargetModelID:   "overflow-affinity-" + slug + "-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/affinity/" + slug + "/source"),
		EndpointAPIKey:  "overflow-affinity-" + slug + "-source-key",
	})
	promotedModelID := "overflow-affinity-" + slug + "-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, promotedStatus, promotedBody)
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/affinity/"+slug+"/promoted"), "overflow-affinity-"+slug+"-promoted-key", nil, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)
	return overflowAffinityCacheRuntimeFixture{
		harness:          harness,
		profileID:        profileID,
		route:            route,
		sourceUpstream:   sourceUpstream,
		promotedModelID:  promotedModelID,
		promotedUpstream: promotedUpstream,
	}
}

func overflowAffinityCacheHeaders() map[string]string {
	return map[string]string{"x-session-affinity": overflowAffinityCacheTestHeaderValue}
}

func performOverflowAffinityChatRequest(t *testing.T, harness *runtimeHarness, modelID string, content string, headers map[string]string) *http.Response {
	t.Helper()
	return harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": content}},
		"model":    modelID,
	}, headers)
}

func performOverflowAffinityResponsesTextRequest(t *testing.T, harness *runtimeHarness, modelID string, input string, maxOutputTokens int, headers map[string]string) *http.Response {
	t.Helper()
	return harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":             input,
		"model":             modelID,
		"max_output_tokens": maxOutputTokens,
	}, headers)
}

func assertOverflowAffinityRequestSequence(t *testing.T, requests []upstreamRequestSnapshot, want []proxySelectorExpectedRequest) {
	t.Helper()
	if len(requests) != len(want) {
		actual := make([]proxySelectorExpectedRequest, 0, len(requests))
		for _, request := range requests {
			actual = append(actual, proxySelectorExpectedRequest{Path: request.Path, ModelID: requestModelID(t, request.Body)})
		}
		t.Fatalf("expected %d upstream requests, got %d path/model pairs: %+v", len(want), len(requests), actual)
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

func runtimeOverflowErrorPayload(message string) map[string]any {
	return map[string]any{"error": map[string]any{"message": message, "type": "invalid_request_error", "code": "context_length_exceeded"}}
}

func runtimeBodyConfirmed429OverflowPayload(detail string) map[string]any {
	return map[string]any{"code": "context_too_large", "detail": detail}
}

func seedRuntimePromotionNativeModel(t *testing.T, harness *runtimeHarness, profileID int, modelID string, endpointBaseURL string, endpointAPIKey string, openAIProbeEndpointVariant *string, contextWindowTokens int) (int, int) {
	t.Helper()
	strategyID := harness.seedLegacyStrategy(t, profileID, "overflow-promotion-native-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	endpointID := harness.seedEndpoint(t, profileID, "overflow-promotion-endpoint-"+randomSuffix(), endpointBaseURL, endpointAPIKey, 0)
	connectionID := harness.seedConnectionWithOpenAIProbeVariant(t, profileID, modelConfigID, endpointID, "overflow-promotion-connection-"+randomSuffix(), nil, nil, 0, openAIProbeEndpointVariant)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, connectionID, contextWindowTokens, 1_024, 1.0)
	return modelConfigID, connectionID
}

func setRuntimeHarnessPromotionTarget(t *testing.T, harness *runtimeHarness, profileID int, sourceModelID string, targetModelID string) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET context_overflow_promotion_target_id = $3, updated_at = $4 WHERE profile_id = $1 AND model_id = $2`, profileID, sourceModelID, targetModelID, now); err != nil {
		t.Fatalf("set runtime promotion target %q -> %q: %v", sourceModelID, targetModelID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
}

func setRuntimeHarnessModelEnabled(t *testing.T, harness *runtimeHarness, profileID int, modelID string, enabled bool) {
	t.Helper()
	now := time.Now().UTC()
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET is_enabled = $3, updated_at = $4 WHERE profile_id = $1 AND model_id = $2`, profileID, modelID, enabled, now); err != nil {
		t.Fatalf("set runtime model enabled=%t for %q: %v", enabled, modelID, err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
}

func loadRuntimeEndpointIDForConnection(t *testing.T, harness *runtimeHarness, connectionID int) int {
	t.Helper()
	var endpointID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT endpoint_id FROM connections WHERE id = $1`, connectionID).Scan(&endpointID); err != nil {
		t.Fatalf("load endpoint id for connection %d: %v", connectionID, err)
	}
	return endpointID
}
