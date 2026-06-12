package runtime_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
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
	assertLatestRuntimeRouteReason(t, harness.conn, profileID, "context_overflow_provider_fallback")
	assertLatestRuntimeUsageRouteReason(t, harness.conn, profileID, "context_overflow_provider_fallback")
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

func TestAdapterGatedResponsesOverflowPromotesToChatOnlyTarget(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceVariant := "responses_reasoning_none"
	chatOnlyVariant := "chat_completions_reasoning_none"
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source responses overflow should promote"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:                  profileID,
		APIFamily:                  "openai",
		PublicModelID:              "overflow-responses-adapter-public-" + suffix,
		TargetModelID:              "overflow-responses-adapter-source-" + suffix,
		EndpointBaseURL:            sourceUpstream.baseURL("/overflow/responses-adapter/source"),
		EndpointAPIKey:             "overflow-responses-adapter-source-key",
		OpenAIProbeEndpointVariant: &sourceVariant,
		OpenAITextCapability:       runtimeStringPtr("responses_only"),
	})
	promotedModelID := "overflow-responses-adapter-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":      "chatcmpl-responses-adapter-promoted-" + suffix,
		"object":  "chat.completion",
		"created": 1710000000,
		"model":   promotedModelID,
		"choices": []map[string]any{{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": "promoted chat-only response"},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 5, "total_tokens": 12},
	})
	seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/responses-adapter/promoted"), "overflow-responses-adapter-promoted-key", &chatOnlyVariant, runtimeStringPtr("chat_completions_only"), 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":             "adapter-gated responses overflow should promote",
		"model":             route.PublicModelID,
		"max_output_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	payload := runtimeResponsePayload(t, response)
	if payload["id"] != "chatcmpl-responses-adapter-promoted-"+suffix || payload["object"] != "response" {
		t.Fatalf("expected translated promoted chat-only response to reach client, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-adapter/source/v1/responses",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-adapter/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
}

func TestResponsesOverflowPromotesToDualNativeTargetWithoutTranslation(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceVariant := "responses_reasoning_none"
	dualNativeVariant := "chat_completions_reasoning_none"
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source responses overflow should promote to dual-native"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:                  profileID,
		APIFamily:                  "openai",
		PublicModelID:              "overflow-responses-dual-native-public-" + suffix,
		TargetModelID:              "overflow-responses-dual-native-source-" + suffix,
		EndpointBaseURL:            sourceUpstream.baseURL("/overflow/responses-dual-native/source"),
		EndpointAPIKey:             "overflow-responses-dual-native-source-key",
		OpenAIProbeEndpointVariant: &sourceVariant,
		OpenAITextCapability:       runtimeStringPtr("responses_only"),
	})
	promotedModelID := "overflow-responses-dual-native-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":     "resp-responses-dual-native-promoted-" + suffix,
		"object": "response",
		"output": []map[string]any{{
			"type":    "message",
			"role":    "assistant",
			"content": []map[string]any{{"type": "output_text", "text": "promoted dual-native response"}},
		}},
		"usage": map[string]any{"input_tokens": 7, "output_tokens": 5, "total_tokens": 12},
	})
	seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/responses-dual-native/promoted"), "overflow-responses-dual-native-promoted-key", &dualNativeVariant, runtimeStringPtr("dual_native"), 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":             "responses overflow should promote natively to dual-native",
		"model":             route.PublicModelID,
		"max_output_tokens": 64,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	payload := runtimeResponsePayload(t, response)
	if payload["id"] != "resp-responses-dual-native-promoted-"+suffix || payload["object"] != "response" {
		t.Fatalf("expected native promoted dual-native response to reach client, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-dual-native/source/v1/responses",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-dual-native/promoted/v1/responses",
		ModelID: promotedModelID,
	}})
}

func TestAdapterGatedUnsupportedTranslatedResponsesShapeDoesNotPromoteToChatOnlyTarget(t *testing.T) {
	harness := newEnforcedRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceVariant := "responses_reasoning_none"
	chatOnlyVariant := "chat_completions_reasoning_none"
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("unsupported translated shape source overflow stays final"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:                  profileID,
		APIFamily:                  "openai",
		PublicModelID:              "overflow-responses-unsupported-public-" + suffix,
		TargetModelID:              "overflow-responses-unsupported-source-" + suffix,
		EndpointBaseURL:            sourceUpstream.baseURL("/overflow/responses-unsupported/source"),
		EndpointAPIKey:             "overflow-responses-unsupported-source-key",
		OpenAIProbeEndpointVariant: &sourceVariant,
		OpenAITextCapability:       runtimeStringPtr("responses_only"),
	})
	promotedModelID := "overflow-responses-unsupported-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-responses-unsupported-should-not-run"})
	seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/responses-unsupported/promoted"), "overflow-responses-unsupported-promoted-key", &chatOnlyVariant, runtimeStringPtr("chat_completions_only"), 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":                "unsupported translated responses shape",
		"model":                route.PublicModelID,
		"previous_response_id": "resp_123",
	}, nil)
	assertStatus(t, response, http.StatusBadRequest)
	payload := runtimeResponsePayload(t, response)
	errorPayload, ok := payload["error"].(map[string]any)
	if !ok || errorPayload["message"] != "unsupported translated shape source overflow stays final" {
		t.Fatalf("expected original source overflow payload for unsupported translated shape, got %+v", payload)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-unsupported/source/v1/responses",
		ModelID: route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, promotedUpstream, "unsupported translated shape promotion target")
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
	waitForRuntimeTelemetryCounts(t, fixture.harness.conn, fixture.profileID, runtimeTelemetryCounts{RequestLogs: 3, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeRouteReason(t, fixture.harness.conn, fixture.profileID, "context_overflow_preflight")
	assertLatestRuntimeUsageRouteReason(t, fixture.harness.conn, fixture.profileID, "context_overflow_preflight")
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
	runChatStreamingPreDispatchPromotionSkipsSourceUpstream(t, "legacy-name")
}

func TestChatStreamingPreDispatchPromotionSkipsSourceUpstream(t *testing.T) {
	runChatStreamingPreDispatchPromotionSkipsSourceUpstream(t, "focused")
}

func TestChatStreamingPreDispatchPromotionRequestLogAndTraceMetadata(t *testing.T) {
	recorder := installRuntimePromotionTraceRecorder(t)
	runChatStreamingPreDispatchPromotionSkipsSourceUpstream(t, "observability")
	attrs := waitForRuntimePromotionTraceAttributes(t, recorder, "pre_dispatch_estimate")
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.trigger_phase", "pre_dispatch_estimate")
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.estimation_mode", "preflight_estimated")
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.estimation_method", "openai_chat_tokenizer_v1")
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.result", "promoted_success")
	assertRuntimePromotionTraceInt(t, attrs, "prism.context_overflow_promotion.source_attempt_count", 0)
	assertRuntimePromotionTraceInt(t, attrs, "prism.context_overflow_promotion.final_attempt_count", 1)
	assertRuntimePromotionTracePositiveInt(t, attrs, "prism.context_overflow_promotion.estimated_input_tokens")
	assertRuntimePromotionTracePositiveInt(t, attrs, "prism.context_overflow_promotion.reserved_output_tokens")
	assertRuntimePromotionTracePositiveInt(t, attrs, "prism.context_overflow_promotion.estimated_total_context_tokens")
}

func TestProviderOverflowPromotionRequestLogAndUsageMetadata(t *testing.T) {
	t.Run("non_stream_chat", func(t *testing.T) {
		recorder := installRuntimePromotionTraceRecorder(t)
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source provider overflow should promote"))
		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:       profileID,
			APIFamily:       "openai",
			PublicModelID:   "overflow-provider-observability-public-" + suffix,
			TargetModelID:   "overflow-provider-observability-source-" + suffix,
			EndpointBaseURL: sourceUpstream.baseURL("/overflow/provider-observability/source"),
			EndpointAPIKey:  "overflow-provider-observability-source-key",
		})
		promotedModelID := "overflow-provider-observability-promoted-" + suffix
		promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
			"id":     "chatcmpl-provider-observability-" + suffix,
			"object": "chat.completion",
			"usage":  map[string]any{"prompt_tokens": 11, "completion_tokens": 7, "total_tokens": 18},
		})
		_, promotedConnectionID := seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/provider-observability/promoted"), "overflow-provider-observability-promoted-key", nil, 32_768)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
		setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

		response := performProxySelectorChatRequest(t, harness, route.PublicModelID, "provider overflow observability")
		assertStatus(t, response, http.StatusOK)
		waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
		assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, promotedModelID)
		assertLatestProviderOverflowPromotionMetadata(t, harness, profileID, route.TargetModelID, promotedModelID, route.ConnectionID, promotedConnectionID)
		attrs := waitForRuntimePromotionTraceAttributes(t, recorder, "provider_overflow")
		assertProviderOverflowTraceMetadata(t, attrs)
	})

	t.Run("responses_streaming", func(t *testing.T) {
		recorder := installRuntimePromotionTraceRecorder(t)
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		suffix := randomSuffix()
		sourceVariant := "responses_reasoning_none"
		responsesOnlyCapability := "responses_only"
		sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source responses provider overflow should promote"))
		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:                  profileID,
			APIFamily:                  "openai",
			PublicModelID:              "overflow-provider-responses-public-" + suffix,
			TargetModelID:              "overflow-provider-responses-source-" + suffix,
			EndpointBaseURL:            sourceUpstream.baseURL("/overflow/provider-responses/source"),
			EndpointAPIKey:             "overflow-provider-responses-source-key",
			OpenAIProbeEndpointVariant: &sourceVariant,
			OpenAITextCapability:       &responsesOnlyCapability,
		})
		promotedModelID := "overflow-provider-responses-promoted-" + suffix
		promotedStream := "event: response.created\n" +
			"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-provider-" + suffix + "\"}}\n\n" +
			"event: response.output_text.delta\n" +
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"promoted provider stream\"}\n\n" +
			"event: response.completed\n" +
			"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-provider-" + suffix + "\",\"usage\":{\"input_tokens\":5,\"output_tokens\":3,\"total_tokens\":8}}}\n\n"
		promotedUpstream := newRawRuntimeUpstream(t, http.StatusOK, "text/event-stream", promotedStream)
		_, promotedConnectionID := seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/provider-responses/promoted"), "overflow-provider-responses-promoted-key", &sourceVariant, &responsesOnlyCapability, 32_768)
		setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
		setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
			"input":             "responses streaming provider overflow observability",
			"model":             route.PublicModelID,
			"max_output_tokens": 64,
			"stream":            true,
		}, nil)
		assertStatus(t, response, http.StatusOK)
		body := readResponseBody(t, response)
		if !strings.Contains(body, "promoted provider stream") || strings.Contains(body, "source responses provider overflow should promote") {
			t.Fatalf("expected promoted Responses SSE without source overflow body, got %q", body)
		}
		waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
		assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, promotedModelID)
		assertLatestProviderOverflowPromotionMetadata(t, harness, profileID, route.TargetModelID, promotedModelID, route.ConnectionID, promotedConnectionID)
		attrs := waitForRuntimePromotionTraceAttributes(t, recorder, "provider_overflow")
		assertProviderOverflowTraceMetadata(t, attrs)
	})
}

func runChatStreamingPreDispatchPromotionSkipsSourceUpstream(t *testing.T, slug string) {
	t.Helper()
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceUpstream := newScriptedUpstream(t, http.StatusInternalServerError, map[string]any{"error": "source upstream should not receive pre-dispatch promoted stream"})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "gpt-5-chat-predispatch-" + slug + "-" + suffix,
		TargetModelID:   "overflow-chat-predispatch-source-" + slug + "-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/chat-predispatch/" + slug + "/source"),
		EndpointAPIKey:  "overflow-chat-predispatch-source-key",
	})
	promotedModelID := "overflow-chat-predispatch-promoted-" + slug + "-" + suffix
	promotedStream := "data: {\"id\":\"chatcmpl-promoted-" + suffix + "\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"promoted chat predispatch\"}}]}\n\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":3,\"total_tokens\":8}}\n\n" +
		"data: [DONE]\n\n"
	promotedUpstream := newRawRuntimeUpstream(t, http.StatusOK, "text/event-stream", promotedStream)
	_, promotedConnectionID := seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/chat-predispatch/"+slug+"/promoted"), "overflow-chat-predispatch-promoted-key", nil, 4_096)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 256, 32, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"max_completion_tokens": 600,
		"messages":              []map[string]any{{"role": "user", "content": "streaming overflow should promote before source dispatch"}},
		"model":                 route.PublicModelID,
		"stream":                true,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if !strings.HasPrefix(body, "data: {\"id\":\"chatcmpl-promoted-") || !strings.Contains(body, "promoted chat predispatch") {
		t.Fatalf("expected client-visible bytes to begin with promoted Chat SSE, got %q", body)
	}
	assertNoScriptedUpstreamRequests(t, sourceUpstream, "chat streaming pre-dispatch source")
	promotedRequests := promotedUpstream.requestsSnapshot()
	assertProxySelectorRequestSequence(t, promotedRequests, []proxySelectorExpectedRequest{{
		Path:    "/overflow/chat-predispatch/" + slug + "/promoted/v1/chat/completions",
		ModelID: promotedModelID,
	}})
	assertChatStreamingPreDispatchPromotedRequestBody(t, promotedRequests[0], promotedModelID)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeRouteReason(t, harness.conn, profileID, "context_overflow_preflight")
	assertLatestRuntimeUsageRouteReason(t, harness.conn, profileID, "context_overflow_preflight")
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 1, 1)
	assertLatestRuntimeModelIdentity(t, harness.conn, profileID, route.PublicModelID, promotedModelID)
	assertLatestRuntimeAttemptSequence(t, harness.conn, profileID, []runtimeRequestLogAttempt{{
		AttemptNumber: 1,
		ConnectionID:  promotedConnectionID,
		EndpointID:    loadRuntimeEndpointIDForConnection(t, harness, promotedConnectionID),
		StatusCode:    http.StatusOK,
		SuccessFlag:   true,
	}})
	assertLatestChatPreDispatchPromotionMetadata(t, harness, profileID, route.TargetModelID, promotedModelID, route.ConnectionID, promotedConnectionID, 256, 4096)
}

func TestChatStreamingPreDispatchPromotionUnavailableEstimateUsesSource(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceStream := "data: {\"id\":\"chatcmpl-source-" + suffix + "\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"source chat stream\"}}]}\n\n" +
		"data: [DONE]\n\n"
	sourceUpstream := newRawRuntimeUpstream(t, http.StatusOK, "text/event-stream", sourceStream)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "gpt-5-chat-predispatch-unavailable-" + suffix,
		TargetModelID:   "overflow-chat-predispatch-unavailable-source-" + suffix,
		EndpointBaseURL: sourceUpstream.baseURL("/overflow/chat-predispatch/unavailable/source"),
		EndpointAPIKey:  "overflow-chat-predispatch-unavailable-source-key",
	})
	promotedModelID := "overflow-chat-predispatch-unavailable-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-unavailable-should-not-run"})
	seedRuntimePromotionNativeModel(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/chat-predispatch/unavailable/promoted"), "overflow-chat-predispatch-unavailable-promoted-key", nil, 4_096)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 256, 32, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"max_completion_tokens": 600,
		"messages": []map[string]any{{
			"role": "user",
			"content": []map[string]any{{
				"type":      "image_url",
				"image_url": map[string]any{"url": "https://example.invalid/image.png"},
			}},
		}},
		"model":  route.PublicModelID,
		"stream": true,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if !strings.HasPrefix(body, "data: {\"id\":\"chatcmpl-source-") || !strings.Contains(body, "source chat stream") {
		t.Fatalf("expected unavailable estimate path to stream source SSE, got %q", body)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/chat-predispatch/unavailable/source/v1/chat/completions",
		ModelID: route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, promotedUpstream, "unavailable estimate promotion target")
}

func TestResponsesStreamingPreCommitOverflowPromotesToSSETarget(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceVariant := "responses_reasoning_none"
	responsesOnlyCapability := "responses_only"
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source responses stream overflow should promote"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:                  profileID,
		APIFamily:                  "openai",
		PublicModelID:              "overflow-responses-stream-public-" + suffix,
		TargetModelID:              "overflow-responses-stream-source-" + suffix,
		EndpointBaseURL:            sourceUpstream.baseURL("/overflow/responses-stream/source"),
		EndpointAPIKey:             "overflow-responses-stream-source-key",
		OpenAIProbeEndpointVariant: &sourceVariant,
		OpenAITextCapability:       &responsesOnlyCapability,
	})
	promotedModelID := "overflow-responses-stream-promoted-" + suffix
	promotedStream := "event: response.created\n" +
		"data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-promoted-" + suffix + "\"}}\n\n" +
		"event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"promoted stream bytes\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-promoted-" + suffix + "\",\"usage\":{\"input_tokens\":5,\"output_tokens\":3,\"total_tokens\":8}}}\n\n"
	promotedUpstream := newRawRuntimeUpstream(t, http.StatusOK, "text/event-stream", promotedStream)
	seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/responses-stream/promoted"), "overflow-responses-stream-promoted-key", &sourceVariant, &responsesOnlyCapability, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":             "responses streaming overflow should promote pre-commit",
		"model":             route.PublicModelID,
		"max_output_tokens": 64,
		"stream":            true,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if !strings.HasPrefix(body, "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp-promoted-") {
		t.Fatalf("expected client-visible bytes to begin with promoted SSE, got %q", body)
	}
	if strings.Contains(body, "source responses stream overflow should promote") || !strings.Contains(body, "promoted stream bytes") {
		t.Fatalf("expected promoted SSE body without source overflow, got %q", body)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-stream/source/v1/responses",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-stream/promoted/v1/responses",
		ModelID: promotedModelID,
	}})
}

func TestResponsesStreamingReplayBodyPreservesPreviousResponseAndStoreFields(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceVariant := "responses_reasoning_none"
	responsesOnlyCapability := "responses_only"
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source responses stream overflow should replay body"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:                  profileID,
		APIFamily:                  "openai",
		PublicModelID:              "overflow-responses-replay-body-public-" + suffix,
		TargetModelID:              "overflow-responses-replay-body-source-" + suffix,
		EndpointBaseURL:            sourceUpstream.baseURL("/overflow/responses-replay-body/source"),
		EndpointAPIKey:             "overflow-responses-replay-body-source-key",
		OpenAIProbeEndpointVariant: &sourceVariant,
		OpenAITextCapability:       &responsesOnlyCapability,
	})
	promotedModelID := "overflow-responses-replay-body-promoted-" + suffix
	promotedStream := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"promoted replay body stream\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp-replay-body-" + suffix + "\",\"usage\":{\"input_tokens\":5,\"output_tokens\":3,\"total_tokens\":8}}}\n\n"
	promotedUpstream := newRawRuntimeUpstream(t, http.StatusOK, "text/event-stream", promotedStream)
	seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/responses-replay-body/promoted"), "overflow-responses-replay-body-promoted-key", &sourceVariant, &responsesOnlyCapability, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	previousResponseID := "resp_previous_replay_" + suffix
	input := "responses streaming replay body input " + suffix
	metadataTraceID := "trace-replay-body-" + suffix
	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":                input,
		"metadata":             map[string]any{"trace_id": metadataTraceID, "purpose": "replay-body-contract"},
		"model":                route.PublicModelID,
		"max_output_tokens":    64,
		"previous_response_id": previousResponseID,
		"store":                false,
		"stream":               true,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if strings.Contains(body, "source responses stream overflow should replay body") || !strings.Contains(body, "promoted replay body stream") {
		t.Fatalf("expected promoted replay SSE body without source overflow, got %q", body)
	}

	sourceRequests := sourceUpstream.requestsSnapshot()
	promotedRequests := promotedUpstream.requestsSnapshot()
	assertProxySelectorRequestSequence(t, sourceRequests, []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-replay-body/source/v1/responses",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedRequests, []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-replay-body/promoted/v1/responses",
		ModelID: promotedModelID,
	}})
	assertResponsesStreamingReplayBodyFields(t, promotedRequests[0], previousResponseID, false, input, metadataTraceID)
}

func TestResponsesStreamingPreviousResponsePromotedFailureBecomesFinal(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceVariant := "responses_reasoning_none"
	responsesOnlyCapability := "responses_only"
	sourceUpstream := newScriptedUpstream(t, http.StatusBadRequest, runtimeOverflowErrorPayload("source responses stream overflow should not reach client"))
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:                  profileID,
		APIFamily:                  "openai",
		PublicModelID:              "overflow-responses-prev-failure-public-" + suffix,
		TargetModelID:              "overflow-responses-prev-failure-source-" + suffix,
		EndpointBaseURL:            sourceUpstream.baseURL("/overflow/responses-prev-failure/source"),
		EndpointAPIKey:             "overflow-responses-prev-failure-source-key",
		OpenAIProbeEndpointVariant: &sourceVariant,
		OpenAITextCapability:       &responsesOnlyCapability,
	})
	promotedModelID := "overflow-responses-prev-failure-promoted-" + suffix
	promotedFailureBody := `{"error":{"message":"promoted target rejected previous_response_id","param":"previous_response_id","code":"invalid_previous_response_id"}}`
	promotedUpstream := newRawRuntimeUpstream(t, http.StatusConflict, "application/json", promotedFailureBody)
	_, promotedConnectionID := seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/responses-prev-failure/promoted"), "overflow-responses-prev-failure-promoted-key", &sourceVariant, &responsesOnlyCapability, 32_768)
	thirdModelID := "overflow-responses-prev-failure-third-" + suffix
	thirdUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "resp-prev-failure-third-should-not-run"})
	seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, thirdModelID, thirdUpstream.baseURL("/overflow/responses-prev-failure/third"), "overflow-responses-prev-failure-third-key", &sourceVariant, &responsesOnlyCapability, 65_536)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, promotedModelID, thirdModelID)

	sourceEndpointID := loadRuntimeEndpointIDForConnection(t, harness, route.ConnectionID)
	promotedEndpointID := loadRuntimeEndpointIDForConnection(t, harness, promotedConnectionID)
	previousResponseID := "resp_previous_rejected_" + suffix
	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":                "responses streaming promoted target should reject previous response id",
		"metadata":             map[string]any{"trace_id": "trace-prev-failure-" + suffix},
		"model":                route.PublicModelID,
		"max_output_tokens":    64,
		"previous_response_id": previousResponseID,
		"store":                true,
		"stream":               true,
	}, nil)
	assertStatus(t, response, http.StatusConflict)
	body := readResponseBody(t, response)
	if body != promotedFailureBody || strings.Contains(body, "source responses stream overflow should not reach client") {
		t.Fatalf("expected promoted previous_response_id failure body to become final, got %q", body)
	}

	sourceRequests := sourceUpstream.requestsSnapshot()
	promotedRequests := promotedUpstream.requestsSnapshot()
	assertProxySelectorRequestSequence(t, sourceRequests, []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-prev-failure/source/v1/responses",
		ModelID: route.TargetModelID,
	}})
	assertProxySelectorRequestSequence(t, promotedRequests, []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-prev-failure/promoted/v1/responses",
		ModelID: promotedModelID,
	}})
	assertResponsesStreamingReplayBodyFields(t, promotedRequests[0], previousResponseID, true, "responses streaming promoted target should reject previous response id", "trace-prev-failure-"+suffix)
	assertNoScriptedUpstreamRequests(t, thirdUpstream, "third responses streaming promotion target")
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	assertLatestRuntimeRouteReason(t, harness.conn, profileID, "context_overflow_provider_fallback")
	assertLatestRuntimeAttemptCounts(t, harness.conn, profileID, 2, 2)
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
		StatusCode:    http.StatusConflict,
		SuccessFlag:   false,
	}})
}

func TestResponsesStreamingNormalSSEPreCommitDoesNotPromote(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	sourceVariant := "responses_reasoning_none"
	responsesOnlyCapability := "responses_only"
	sourceStream := "event: response.output_text.delta\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"source stream bytes\"}\n\n" +
		"event: response.completed\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":4,\"output_tokens\":2,\"total_tokens\":6}}}\n\n"
	sourceUpstream := newRawRuntimeUpstream(t, http.StatusOK, "text/event-stream", sourceStream)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:                  profileID,
		APIFamily:                  "openai",
		PublicModelID:              "overflow-responses-normal-stream-public-" + suffix,
		TargetModelID:              "overflow-responses-normal-stream-source-" + suffix,
		EndpointBaseURL:            sourceUpstream.baseURL("/overflow/responses-normal-stream/source"),
		EndpointAPIKey:             "overflow-responses-normal-stream-source-key",
		OpenAIProbeEndpointVariant: &sourceVariant,
		OpenAITextCapability:       &responsesOnlyCapability,
	})
	promotedModelID := "overflow-responses-normal-stream-promoted-" + suffix
	promotedUpstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "resp-normal-stream-should-not-run"})
	seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, promotedModelID, promotedUpstream.baseURL("/overflow/responses-normal-stream/promoted"), "overflow-responses-normal-stream-promoted-key", &sourceVariant, &responsesOnlyCapability, 32_768)
	setRuntimeHarnessConnectionContextCapabilities(t, harness, route.ConnectionID, 16_384, 1_024, 1.0)
	setRuntimeHarnessPromotionTarget(t, harness, profileID, route.TargetModelID, promotedModelID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input":             "normal responses SSE should stream unchanged",
		"model":             route.PublicModelID,
		"max_output_tokens": 64,
		"stream":            true,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	body := readResponseBody(t, response)
	if body != strings.TrimSpace(sourceStream) {
		t.Fatalf("expected source SSE stream unchanged, got %q", body)
	}
	assertProxySelectorRequestSequence(t, sourceUpstream.requestsSnapshot(), []proxySelectorExpectedRequest{{
		Path:    "/overflow/responses-normal-stream/source/v1/responses",
		ModelID: route.TargetModelID,
	}})
	assertNoScriptedUpstreamRequests(t, promotedUpstream, "normal responses stream promotion target")
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

type rawRuntimeUpstream struct {
	server      *httptest.Server
	mu          sync.Mutex
	requests    []upstreamRequestSnapshot
	statusCode  int
	contentType string
	body        string
}

func newRawRuntimeUpstream(t *testing.T, statusCode int, contentType string, body string) *rawRuntimeUpstream {
	t.Helper()
	upstream := &rawRuntimeUpstream{statusCode: statusCode, contentType: contentType, body: body}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read raw runtime upstream request body: %v", err)
		}
		_ = r.Body.Close()
		upstream.mu.Lock()
		upstream.requests = append(upstream.requests, upstreamRequestSnapshot{
			Method:  r.Method,
			URL:     r.URL.String(),
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    append([]byte(nil), requestBody...),
		})
		upstream.mu.Unlock()
		if upstream.contentType != "" {
			w.Header().Set("Content-Type", upstream.contentType)
		}
		w.WriteHeader(upstream.statusCode)
		_, _ = io.WriteString(w, upstream.body)
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *rawRuntimeUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func (u *rawRuntimeUpstream) requestsSnapshot() []upstreamRequestSnapshot {
	u.mu.Lock()
	defer u.mu.Unlock()
	cloned := make([]upstreamRequestSnapshot, len(u.requests))
	copy(cloned, u.requests)
	return cloned
}

func assertChatStreamingPreDispatchPromotedRequestBody(t *testing.T, request upstreamRequestSnapshot, wantModelID string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		t.Fatalf("decode chat streaming promoted request body %q: %v", string(request.Body), err)
	}
	if got, _ := payload["model"].(string); got != wantModelID {
		t.Fatalf("expected promoted chat model %q, got %+v", wantModelID, payload)
	}
	if got, ok := payload["stream"].(bool); !ok || !got {
		t.Fatalf("expected promoted chat stream=true, got %+v", payload)
	}
	if got, ok := payload["max_completion_tokens"].(float64); !ok || int(got) != 600 {
		t.Fatalf("expected promoted chat max_completion_tokens=600, got %+v", payload)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("expected promoted chat messages to be preserved, got %+v", payload)
	}
	message, ok := messages[0].(map[string]any)
	if !ok || message["content"] != "streaming overflow should promote before source dispatch" {
		t.Fatalf("expected promoted chat message content to be preserved, got %+v", payload)
	}
}

func assertLatestChatPreDispatchPromotionMetadata(t *testing.T, harness *runtimeHarness, profileID int, wantSourceModelID string, wantPromotedModelID string, wantSourceConnectionID int, wantPromotedConnectionID int, wantSourceWindow int, wantPromotedWindow int) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)
	assertChatPreDispatchPromotionMetadataRow(t, harness, profileID, ingressRequestID, "request_logs", wantSourceModelID, wantPromotedModelID, wantSourceConnectionID, wantPromotedConnectionID, wantSourceWindow, wantPromotedWindow)
	assertChatPreDispatchPromotionMetadataRow(t, harness, profileID, ingressRequestID, "usage_request_events", wantSourceModelID, wantPromotedModelID, wantSourceConnectionID, wantPromotedConnectionID, wantSourceWindow, wantPromotedWindow)
}

func assertChatPreDispatchPromotionMetadataRow(t *testing.T, harness *runtimeHarness, profileID int, ingressRequestID string, table string, wantSourceModelID string, wantPromotedModelID string, wantSourceConnectionID int, wantPromotedConnectionID int, wantSourceWindow int, wantPromotedWindow int) {
	t.Helper()
	contextRouting := loadLatestRuntimeContextRoutingForIngress(t, harness, profileID, ingressRequestID, table)
	promotion := asMapRuntime(t, contextRouting["context_overflow_promotion"])
	label := "chat pre-dispatch " + table
	if promotion["trigger_phase"] != "pre_dispatch_estimate" || promotion["trigger_classifier"] != "estimated_context_exceeds_usable_window" || promotion["estimation_mode"] != "preflight_estimated" || promotion["estimation_method"] != "openai_chat_tokenizer_v1" || promotion["result"] != "promoted_success" {
		t.Fatalf("expected %s promotion metadata strings, got %+v", label, promotion)
	}
	assertTranslatedPromotionNumber(t, label, promotion, "source_attempt_count", 0)
	assertTranslatedPromotionNumber(t, label, promotion, "final_attempt_count", 1)
	assertTranslatedPromotionNumber(t, label, promotion, "from_selected_terminal_target_id", wantSourceConnectionID)
	assertTranslatedPromotionNumber(t, label, promotion, "to_selected_terminal_target_id", wantPromotedConnectionID)
	assertTranslatedPromotionNumber(t, label, promotion, "from_usable_context_window_tokens", wantSourceWindow)
	assertTranslatedPromotionNumber(t, label, promotion, "to_usable_context_window_tokens", wantPromotedWindow)
	if promotion["from_resolved_target_model_id"] != wantSourceModelID || promotion["to_resolved_target_model_id"] != wantPromotedModelID {
		t.Fatalf("expected %s model metadata %q -> %q, got %+v", label, wantSourceModelID, wantPromotedModelID, promotion)
	}
	estimatedTotal, ok := promotion["estimated_total_context_tokens"].(float64)
	if !ok || int(estimatedTotal) <= wantSourceWindow || int(estimatedTotal) > wantPromotedWindow {
		t.Fatalf("expected %s estimated total to exceed source and fit promoted windows, got %+v", label, promotion)
	}
	if _, ok := promotion["estimated_input_tokens"].(float64); !ok {
		t.Fatalf("expected %s estimated_input_tokens metadata, got %+v", label, promotion)
	}
	if _, ok := promotion["reserved_output_tokens"].(float64); !ok {
		t.Fatalf("expected %s reserved_output_tokens metadata, got %+v", label, promotion)
	}
}

func assertLatestProviderOverflowPromotionMetadata(t *testing.T, harness *runtimeHarness, profileID int, wantSourceModelID string, wantPromotedModelID string, wantSourceConnectionID int, wantPromotedConnectionID int) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, harness.conn, profileID)
	assertProviderOverflowPromotionMetadataRow(t, harness, profileID, ingressRequestID, "request_logs", wantSourceModelID, wantPromotedModelID, wantSourceConnectionID, wantPromotedConnectionID)
	assertProviderOverflowPromotionMetadataRow(t, harness, profileID, ingressRequestID, "usage_request_events", wantSourceModelID, wantPromotedModelID, wantSourceConnectionID, wantPromotedConnectionID)
}

func assertProviderOverflowPromotionMetadataRow(t *testing.T, harness *runtimeHarness, profileID int, ingressRequestID string, table string, wantSourceModelID string, wantPromotedModelID string, wantSourceConnectionID int, wantPromotedConnectionID int) {
	t.Helper()
	contextRouting := loadLatestRuntimeContextRoutingForIngress(t, harness, profileID, ingressRequestID, table)
	promotion := asMapRuntime(t, contextRouting["context_overflow_promotion"])
	label := "provider overflow " + table
	if promotion["trigger_phase"] != "provider_overflow" || promotion["trigger_error_code"] != "context_length_exceeded" || promotion["trigger_classifier"] != "error_code" || promotion["result"] != "promoted_success" {
		t.Fatalf("expected %s trigger/result metadata, got %+v", label, promotion)
	}
	assertTranslatedPromotionNumber(t, label, promotion, "trigger_status", http.StatusBadRequest)
	assertTranslatedPromotionNumber(t, label, promotion, "source_attempt_count", 1)
	assertTranslatedPromotionNumber(t, label, promotion, "final_attempt_count", 2)
	assertTranslatedPromotionNumber(t, label, promotion, "from_selected_terminal_target_id", wantSourceConnectionID)
	assertTranslatedPromotionNumber(t, label, promotion, "to_selected_terminal_target_id", wantPromotedConnectionID)
	if promotion["from_resolved_target_model_id"] != wantSourceModelID || promotion["to_resolved_target_model_id"] != wantPromotedModelID {
		t.Fatalf("expected %s model metadata %q -> %q, got %+v", label, wantSourceModelID, wantPromotedModelID, promotion)
	}
}

func loadLatestRuntimeContextRoutingForIngress(t *testing.T, harness *runtimeHarness, profileID int, ingressRequestID string, table string) map[string]any {
	t.Helper()
	query := map[string]string{
		"request_logs":         `SELECT COALESCE(context_routing, '{}'::jsonb) FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		"usage_request_events": `SELECT COALESCE(context_routing, '{}'::jsonb) FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
	}[table]
	if query == "" {
		t.Fatalf("unsupported context routing table %q", table)
	}
	var rawContextRouting []byte
	if err := harness.conn.QueryRow(context.Background(), query, profileID, ingressRequestID).Scan(&rawContextRouting); err != nil {
		t.Fatalf("load latest %s context routing: %v", table, err)
	}
	var contextRouting map[string]any
	if err := json.Unmarshal(rawContextRouting, &contextRouting); err != nil {
		t.Fatalf("decode latest %s context routing %q: %v", table, string(rawContextRouting), err)
	}
	return contextRouting
}

func installRuntimePromotionTraceRecorder(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	previousProvider := otel.GetTracerProvider()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(previousProvider)
	})
	return recorder
}

func waitForRuntimePromotionTraceAttributes(t *testing.T, recorder *tracetest.SpanRecorder, triggerPhase string) map[string]attribute.Value {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		for _, span := range recorder.Ended() {
			attrs := runtimePromotionTraceAttributesByKey(span.Attributes())
			if attrs["prism.context_overflow_promotion.trigger_phase"].AsString() == triggerPhase {
				return attrs
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected trace attrs with context overflow promotion trigger_phase=%q, got spans %+v", triggerPhase, runtimePromotionTraceSpanSummary(recorder.Ended()))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runtimePromotionTraceAttributesByKey(attrs []attribute.KeyValue) map[string]attribute.Value {
	byKey := make(map[string]attribute.Value, len(attrs))
	for _, attr := range attrs {
		byKey[string(attr.Key)] = attr.Value
	}
	return byKey
}

func runtimePromotionTraceSpanSummary(spans []sdktrace.ReadOnlySpan) map[string][]attribute.KeyValue {
	summary := make(map[string][]attribute.KeyValue, len(spans))
	for _, span := range spans {
		summary[span.Name()] = append(summary[span.Name()], span.Attributes()...)
	}
	return summary
}

func assertProviderOverflowTraceMetadata(t *testing.T, attrs map[string]attribute.Value) {
	t.Helper()
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.trigger_phase", "provider_overflow")
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.trigger_code", "context_length_exceeded")
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.trigger_classifier", "error_code")
	assertRuntimePromotionTraceString(t, attrs, "prism.context_overflow_promotion.result", "promoted_success")
	assertRuntimePromotionTraceInt(t, attrs, "prism.context_overflow_promotion.trigger_status", http.StatusBadRequest)
	assertRuntimePromotionTraceInt(t, attrs, "prism.context_overflow_promotion.source_attempt_count", 1)
	assertRuntimePromotionTraceInt(t, attrs, "prism.context_overflow_promotion.final_attempt_count", 2)
}

func assertRuntimePromotionTraceString(t *testing.T, attrs map[string]attribute.Value, key string, want string) {
	t.Helper()
	value, ok := attrs[key]
	if !ok || value.AsString() != want {
		t.Fatalf("expected trace attr %s=%q, got %+v", key, want, attrs)
	}
}

func assertRuntimePromotionTraceInt(t *testing.T, attrs map[string]attribute.Value, key string, want int) {
	t.Helper()
	value, ok := attrs[key]
	if !ok || int(value.AsInt64()) != want {
		t.Fatalf("expected trace attr %s=%d, got %+v", key, want, attrs)
	}
}

func assertRuntimePromotionTracePositiveInt(t *testing.T, attrs map[string]attribute.Value, key string) {
	t.Helper()
	value, ok := attrs[key]
	if !ok || value.AsInt64() <= 0 {
		t.Fatalf("expected positive trace attr %s, got %+v", key, attrs)
	}
}

func assertResponsesStreamingReplayBodyFields(t *testing.T, request upstreamRequestSnapshot, wantPreviousResponseID string, wantStore bool, wantInput string, wantMetadataTraceID string) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		t.Fatalf("decode responses streaming replay request body %q: %v", string(request.Body), err)
	}
	if got, _ := payload["previous_response_id"].(string); got != wantPreviousResponseID {
		t.Fatalf("expected replay previous_response_id %q, got %+v", wantPreviousResponseID, payload)
	}
	if got, ok := payload["store"].(bool); !ok || got != wantStore {
		t.Fatalf("expected replay store=%t, got %+v", wantStore, payload)
	}
	if got, _ := payload["input"].(string); got != wantInput {
		t.Fatalf("expected replay input %q, got %+v", wantInput, payload)
	}
	metadata, ok := payload["metadata"].(map[string]any)
	if !ok || metadata["trace_id"] != wantMetadataTraceID {
		t.Fatalf("expected replay metadata.trace_id %q, got %+v", wantMetadataTraceID, payload)
	}
	if got, ok := payload["stream"].(bool); !ok || !got {
		t.Fatalf("expected replay stream=true, got %+v", payload)
	}
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
	return seedRuntimePromotionNativeModelWithOpenAITextCapability(t, harness, profileID, modelID, endpointBaseURL, endpointAPIKey, openAIProbeEndpointVariant, defaultRuntimeHarnessOpenAITextCapability(), contextWindowTokens)
}

func seedRuntimePromotionNativeModelWithOpenAITextCapability(t *testing.T, harness *runtimeHarness, profileID int, modelID string, endpointBaseURL string, endpointAPIKey string, openAIProbeEndpointVariant *string, openAITextCapability *string, contextWindowTokens int) (int, int) {
	t.Helper()
	strategyID := harness.seedLegacyStrategy(t, profileID, "overflow-promotion-native-"+randomSuffix(), "fill-first")
	modelConfigID := harness.seedModel(t, profileID, "openai", modelID, "native", &strategyID)
	openAIVendorID := loadVendorIDByKey(t, harness.conn, "openai")
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET vendor_id = $1 WHERE id = $2`, openAIVendorID, modelConfigID); err != nil {
		t.Fatalf("mark promotion model %q with OpenAI vendor: %v", modelID, err)
	}
	endpointID := harness.seedEndpoint(t, profileID, "overflow-promotion-endpoint-"+randomSuffix(), endpointBaseURL, endpointAPIKey, 0)
	connectionID := harness.seedConnectionWithOpenAIProbeVariantAndTextCapability(t, profileID, modelConfigID, endpointID, "overflow-promotion-connection-"+randomSuffix(), nil, nil, 0, openAIProbeEndpointVariant, openAITextCapability)
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
