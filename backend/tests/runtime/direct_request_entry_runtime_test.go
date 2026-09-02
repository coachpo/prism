package runtimetest

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

type directRequestOperationScenario struct {
	caseDefinition runtimeOperationRouteMatrixCase
	endpointPrefix string
	ignoredModel   string
	route          seededRuntimeRoute
	upstream       *routeMatrixUpstream
}

func TestDirectRequestQualificationCoversRegisteredOperationMatrix(t *testing.T) {
	var sideEffectSubmits atomic.Int32
	harness := newRuntimePhase0HarnessWithOptions(t, runtimePhase0HarnessOptions{
		RuntimeOptions: runtimeapi.Options{
			SideEffects: runtimeapi.RuntimeSideEffectOptions{Hooks: &runtimeapi.RuntimeSideEffectHooks{
				AfterSubmit: func(runtimeapi.RuntimeSideEffectSubmitResult) {
					sideEffectSubmits.Add(1)
				},
			}},
		},
	})
	profileID := harness.activeProfileID(t)
	cases := runtimeOperationRouteMatrixCases()
	assertDirectRequestOperationCoverage(t, cases)

	scenarios := make([]directRequestOperationScenario, 0, len(cases))
	for _, caseDefinition := range cases {
		slug := routeMatrixSlug(caseDefinition.operationName)
		upstream := newRouteMatrixUpstream(t, caseDefinition.responseContentType, caseDefinition.routeMatrixResponseBody(t))
		endpointPrefix := "/direct-entry-runtime/" + slug
		route := harness.seedProxyRoute(t, runtimeRouteSeed{
			ProfileID:             profileID,
			APIFamily:             caseDefinition.apiFamily,
			PublicModelID:         "direct-entry-parent-" + slug + "-" + randomSuffix(),
			TargetModelID:         "internal-model-target-" + slug + "-" + randomSuffix(),
			EndpointBaseURL:       upstream.baseURL(endpointPrefix),
			EndpointAPIKey:        "direct-entry-runtime-key-" + slug,
			CustomHeaders:         map[string]any{"X-Route-Matrix": "direct-entry-" + slug},
			OpenAITextCapability:  routeMatrixOpenAITextCapability(caseDefinition.operationName),
			OpenAIImageOperations: routeMatrixOpenAIImageOperations(caseDefinition.operationName),
		})
		var targetModelConfigID int
		if err := harness.conn.QueryRow(context.Background(), `SELECT id FROM model_configs WHERE profile_id = $1 AND model_id = $2`, profileID, route.TargetModelID).Scan(&targetModelConfigID); err != nil {
			t.Fatalf("load target model config id for %s: %v", caseDefinition.operationName, err)
		}
		if _, err := harness.conn.Exec(context.Background(), `UPDATE model_configs SET direct_request_enabled = FALSE WHERE profile_id = $1 AND id = $2`, profileID, targetModelConfigID); err != nil {
			t.Fatalf("mark %s internal-only: %v", caseDefinition.operationName, err)
		}
		scenarios = append(scenarios, directRequestOperationScenario{
			caseDefinition: caseDefinition,
			endpointPrefix: endpointPrefix,
			ignoredModel:   "ignored-body-model-" + slug,
			route:          route,
			upstream:       upstream,
		})
	}
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	baseline := loadRuntimeRejectedRoutePersistenceCounts(t, harness.conn, profileID)
	for _, scenario := range scenarios {
		t.Run("rejects internal "+scenario.caseDefinition.name, func(t *testing.T) {
			targetRoute := scenario.route
			targetRoute.PublicModelID = scenario.route.TargetModelID
			response := harness.requestJSON(
				t,
				http.MethodPost,
				scenario.caseDefinition.requestPath(targetRoute),
				scenario.caseDefinition.requestBody(targetRoute, scenario.ignoredModel),
				nil,
			)
			assertStatus(t, response, http.StatusNotFound)
			if got := len(scenario.upstream.requestsSnapshot()); got != 0 {
				t.Fatalf("non-entry %s ingress reached provider transport: %d requests", scenario.caseDefinition.operationName, got)
			}
			assertRuntimeRejectedRouteNoAdmissionState(t, harness.runtimeHarness, profileID, scenario.route.ConnectionID)
		})
	}
	if got := sideEffectSubmits.Load(); got != 0 {
		t.Fatalf("non-entry operation matrix submitted %d runtime side effects", got)
	}
	assertRuntimeRejectedRoutePersistenceCountsRemain(t, harness.conn, profileID, baseline, 500*time.Millisecond)

	for _, scenario := range scenarios {
		t.Run("routes parent "+scenario.caseDefinition.name, func(t *testing.T) {
			response := harness.requestJSON(
				t,
				http.MethodPost,
				scenario.caseDefinition.requestPath(scenario.route),
				scenario.caseDefinition.requestBody(scenario.route, scenario.ignoredModel),
				nil,
			)
			assertStatus(t, response, http.StatusOK)
			upstreamRequest := scenario.upstream.lastRequest(t)
			scenario.caseDefinition.assertModelSource(t, upstreamRequest, scenario.route, scenario.ignoredModel)
			if upstreamRequest.Path != scenario.caseDefinition.wantUpstreamPath(scenario.endpointPrefix, scenario.route) {
				t.Fatalf("unexpected upstream path for %s: %q", scenario.caseDefinition.operationName, upstreamRequest.Path)
			}
		})
	}

	modelsResponse := harness.requestJSON(t, http.MethodGet, "/v1/models", nil, nil)
	assertStatus(t, modelsResponse, http.StatusOK)
	var modelsPayload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decodeJSONResponse(t, modelsResponse, &modelsPayload)
	listed := map[string]bool{}
	for _, model := range modelsPayload.Data {
		listed[model.ID] = true
	}
	for _, scenario := range scenarios {
		if scenario.caseDefinition.apiFamily != "openai" {
			continue
		}
		if !listed[scenario.route.PublicModelID] {
			t.Fatalf("direct OpenAI parent missing from /v1/models: %q", scenario.route.PublicModelID)
		}
		if listed[scenario.route.TargetModelID] {
			t.Fatalf("non-entry OpenAI model leaked into /v1/models: %q", scenario.route.TargetModelID)
		}
	}
}

func assertDirectRequestOperationCoverage(t *testing.T, cases []runtimeOperationRouteMatrixCase) {
	t.Helper()
	covered := map[string]bool{}
	for _, caseDefinition := range cases {
		covered[caseDefinition.operationName] = true
	}
	for _, operation := range runtimeapi.RuntimeOperationCatalog() {
		if operation.ModelBindingSource == runtimeapi.RuntimeOperationModelBindingNone {
			continue
		}
		if !covered[operation.Name] {
			t.Fatalf("model-bound operation %q has no direct-entry qualification scenario", operation.Name)
		}
		delete(covered, operation.Name)
	}
	if len(covered) != 0 {
		t.Fatalf("direct-entry matrix contains operations outside the runtime registry: %+v", covered)
	}
}
