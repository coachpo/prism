package runtime_test

import (
	"net/http"
	"strings"
	"testing"
)

const responsesMissingModelDetail = "Cannot determine model for routing. Include 'model' in the request body or use a Gemini-style model path."

func TestRuntimeResponsesMissingModelMatchesChatCompletions(t *testing.T) {
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
	if chatDetail != responsesMissingModelDetail {
		t.Fatalf("expected chat missing-model detail %q, got %q", responsesMissingModelDetail, chatDetail)
	}
	if responsesDetail := runtimeResponseDetail(t, responsesResponse); responsesDetail != chatDetail {
		t.Fatalf("expected Responses missing-model detail to match chat completions %q, got %q", chatDetail, responsesDetail)
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
	wantDetail := "Path '/v1/responses' is incompatible with api_family 'anthropic'. Use an api-family-native path."
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

func runtimeResponseDetail(t *testing.T, response *http.Response) string {
	t.Helper()
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	detail, _ := payload["detail"].(string)
	if detail == "" {
		t.Fatalf("expected response detail string, got %+v", payload)
	}
	return detail
}
