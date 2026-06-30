package runtime_test

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestOpenAIModelsAPIListsActiveEnabledOpenAIModelsOnly(t *testing.T) {
	var sideEffectSubmits atomic.Int32
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{
			SideEffects: runtimeapi.RuntimeSideEffectOptions{Hooks: &runtimeapi.RuntimeSideEffectHooks{
				AfterSubmit: func(runtimeapi.RuntimeSideEffectSubmitResult) {
					sideEffectSubmits.Add(1)
				},
			}},
		},
	})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	openAIRoute := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "models-api-public-" + suffix,
		TargetModelID:   "models-api-target-" + suffix,
		EndpointBaseURL: harness.upstream.baseURL("/models-api/openai"),
		EndpointAPIKey:  "models-api-key",
	})
	disabledOpenAIModelID := "models-api-disabled-" + suffix
	disabledOpenAIConfigID := harness.seedModel(t, profileID, "openai", disabledOpenAIModelID, "native", nil)
	harness.seedModel(t, profileID, "anthropic", "models-api-anthropic-"+suffix, "native", nil)
	harness.seedModel(t, profileID, "gemini", "models-api-gemini-"+suffix, "native", nil)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET is_enabled = FALSE WHERE id = $1`, disabledOpenAIConfigID); err != nil {
		t.Fatalf("disable OpenAI model for /v1/models test: %v", err)
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})
	harness.upstream.clear()
	baseline := loadRuntimeRejectedRoutePersistenceCounts(t, harness.conn, profileID)

	response := harness.requestJSON(t, http.MethodGet, "/v1/models", nil, nil)

	assertStatus(t, response, http.StatusOK)
	if contentType := response.Header.Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("expected OpenAI models JSON content type, got %q", contentType)
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by"`
		} `json:"data"`
	}
	decodeJSONResponse(t, response, &payload)
	if payload.Object != "list" {
		t.Fatalf("expected list object, got %+v", payload)
	}
	models := map[string]struct{}{}
	lastID := ""
	for _, item := range payload.Data {
		if item.ID < lastID {
			t.Fatalf("expected OpenAI models to be sorted by id, got %q before %q in %+v", lastID, item.ID, payload.Data)
		}
		lastID = item.ID
		if item.Object != "model" || item.Created <= 0 || item.OwnedBy != "prism" {
			t.Fatalf("expected OpenAI model item to match OpenAI shape, got %+v", item)
		}
		models[item.ID] = struct{}{}
	}
	for _, want := range []string{openAIRoute.PublicModelID, openAIRoute.TargetModelID} {
		if _, ok := models[want]; !ok {
			t.Fatalf("expected /v1/models to include enabled OpenAI model %q, got %+v", want, payload.Data)
		}
	}
	for _, unwanted := range []string{disabledOpenAIModelID, "models-api-anthropic-" + suffix, "models-api-gemini-" + suffix} {
		if _, ok := models[unwanted]; ok {
			t.Fatalf("expected /v1/models to exclude %q, got %+v", unwanted, payload.Data)
		}
	}
	if got := len(harness.upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected /v1/models not to contact upstream providers, got %d requests", got)
	}
	if got := sideEffectSubmits.Load(); got != 0 {
		t.Fatalf("expected /v1/models not to submit runtime side effects, got %d submissions", got)
	}
	assertRuntimeRejectedRoutePersistenceCountsRemain(t, harness.conn, profileID, baseline, 500*time.Millisecond)
}
