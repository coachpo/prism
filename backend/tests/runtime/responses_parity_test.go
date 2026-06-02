package runtime_test

import (
	"net/http"
	"strings"
	"testing"
)

const chatMissingModelDetail = "Cannot determine model for routing. Operation 'openai.chat_completions' binds models from the body."
const responsesMissingModelDetail = "Cannot determine model for routing. Operation 'openai.responses' binds models from the body."

func TestRuntimeResponsesMissingModelUsesOperationSpecificDetail(t *testing.T) {
	harness := newRuntimeHarness(t)

	chatResponse := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "missing model"}},
	}, nil)
	responsesResponse := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input": "missing model",
	}, nil)

	assertStatus(t, chatResponse, http.StatusBadRequest)
	assertStatus(t, responsesResponse, http.StatusBadRequest)
	chatDetail := runtimeResponseDetail(t, chatResponse)
	if chatDetail != chatMissingModelDetail {
		t.Fatalf("expected chat missing-model detail %q, got %q", chatMissingModelDetail, chatDetail)
	}
	if responsesDetail := runtimeResponseDetail(t, responsesResponse); responsesDetail != responsesMissingModelDetail {
		t.Fatalf("expected Responses missing-model detail %q, got %q", responsesMissingModelDetail, responsesDetail)
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected missing-model requests to stop before upstream, got %d upstream requests", got)
	}
}

func TestRuntimeResponsesRejectsAnthropicPathCompatibility(t *testing.T) {
	harness := newRuntimeHarness(t)
	activeProfileID := harness.activeProfileID(t)
	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       activeProfileID,
		APIFamily:       "anthropic",
		PublicModelID:   "responses-anthropic-public-" + randomSuffix(),
		TargetModelID:   "responses-anthropic-target-" + randomSuffix(),
		EndpointBaseURL: harness.upstream.baseURL("/responses/anthropic"),
		EndpointAPIKey:  "responses-anthropic-key",
	})

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
		"input": "wrong family",
		"model": route.PublicModelID,
	}, nil)

	assertStatus(t, response, http.StatusBadRequest)
	wantDetail := "Operation 'openai.responses' is incompatible with api_family 'anthropic'. Use an operation that matches the resolved model api_family."
	if detail := runtimeResponseDetail(t, response); detail != wantDetail {
		t.Fatalf("expected Responses path compatibility detail %q, got %q", wantDetail, detail)
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected incompatible Responses request to stop before upstream, got %d upstream requests", got)
	}
}

func TestRuntimeResponsesMalformedJSONUsesMissingModelBadRequest(t *testing.T) {
	harness := newRuntimeHarness(t)
	request, err := http.NewRequest(http.MethodPost, harness.url+"/v1/responses", strings.NewReader(`{"model":`))
	if err != nil {
		t.Fatalf("build malformed Responses request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := harness.client.Do(request)
	if err != nil {
		t.Fatalf("perform malformed Responses request: %v", err)
	}
	t.Cleanup(func() { _ = response.Body.Close() })

	assertStatus(t, response, http.StatusBadRequest)
	if detail := runtimeResponseDetail(t, response); detail != responsesMissingModelDetail {
		t.Fatalf("expected malformed Responses request to follow model-resolution bad request %q, got %q", responsesMissingModelDetail, detail)
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected malformed Responses request to stop before upstream, got %d upstream requests", got)
	}
}

func TestRuntimeResponsesRejectUnsupportedTranslatedShapesBeforeUpstream(t *testing.T) {
	t.Run("responses previous_response_id on chat-only target", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "responses-parity-previous-response-id"})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "responses-parity-previous-response-id-public", "responses-parity-previous-response-id-target", upstream.baseURL("/responses/parity/previous-response-id"), "responses-parity-previous-response-id-key", "chat_completions_reasoning_none")

		response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{
			"model":                route.PublicModelID,
			"previous_response_id": "resp_123",
			"input": []map[string]any{{
				"type":    "message",
				"role":    "user",
				"content": []map[string]any{{"type": "input_text", "text": "unsupported translated responses shape"}},
			}},
		}, nil)
		assertStatus(t, response, http.StatusBadRequest)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "context_estimation_unavailable" || payload["detail"] != "Preflight context estimation is unavailable for this request shape." {
			t.Fatalf("expected responses previous_response_id rejection to stay on the preflight estimation contract, got %+v", payload)
		}
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected unsupported translated responses shape to stop before upstream, got %d upstream requests", got)
		}
	})

	t.Run("chat multi-choice on responses-only target", func(t *testing.T) {
		harness := newRuntimeHarness(t)
		profileID := harness.activeProfileID(t)
		upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "responses-parity-chat-multi-choice"})
		route := seedTranslatedOpenAIProxyRoute(t, harness, profileID, "responses-parity-chat-multi-choice-public", "responses-parity-chat-multi-choice-target", upstream.baseURL("/responses/parity/chat-multi-choice"), "responses-parity-chat-multi-choice-key", "responses_reasoning_none")

		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model":    route.PublicModelID,
			"messages": []map[string]any{{"role": "user", "content": "unsupported translated chat shape"}},
			"n":        2,
		}, nil)
		assertStatus(t, response, http.StatusBadRequest)
		payload := runtimeResponsePayload(t, response)
		if payload["error"] != "openai_request_translation_unsupported" || payload["detail"] != "Prism cannot translate this OpenAI request shape for the selected target." || payload["unsupported_reason"] != "chat_multi_choice" {
			t.Fatalf("expected chat multi-choice translated rejection payload, got %+v", payload)
		}
		if got := len(upstream.requestsSnapshot()); got != 0 {
			t.Fatalf("expected unsupported translated chat shape to stop before upstream, got %d upstream requests", got)
		}
	})
}

func runtimeResponsePayload(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

func runtimeResponseDetail(t *testing.T, response *http.Response) string {
	t.Helper()
	payload := runtimeResponsePayload(t, response)
	detail, _ := payload["detail"].(string)
	if detail == "" {
		t.Fatalf("expected response detail string, got %+v", payload)
	}
	return detail
}
