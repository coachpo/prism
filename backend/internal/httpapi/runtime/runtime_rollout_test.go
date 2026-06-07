package runtime

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

type rolloutPlanBuild func(*Service, *http.Request, []byte, RuntimeProxyConfigSnapshot, RuntimeOperationMatch, int, *planningSnapshot, string) (requestPlan, error)

func TestRuntimePlannerRolloutRequestedModelParity(t *testing.T) {
	tests := []struct {
		name     string
		snapshot func() *planningSnapshot
		path     string
		rawBody  []byte
		assert   func(t *testing.T, plans map[string]requestPlan)
	}{
		{
			name: "requested-model multi-hop terminal ordering",
			snapshot: func() *planningSnapshot {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "rollout-public-openai"},
					runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "rollout-mid-openai"},
					runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "rollout-target-openai"},
				)
				addRequestPlanProxyTarget(snapshot, "rollout-public-openai", "rollout-mid-openai")
				addRequestPlanProxyTarget(snapshot, "rollout-mid-openai", "rollout-target-openai")
				setRequestPlanConnectionContextWindow(snapshot, 1_003, 16_384)
				return snapshot
			},
			path:    "/v1/chat/completions",
			rawBody: []byte(`{"model":"rollout-public-openai","messages":[{"role":"user","content":"hello"}]}`),
			assert: func(t *testing.T, plans map[string]requestPlan) {
				t.Helper()
				for mode, plan := range plans {
					if got := dereferenceString(plan.ResolvedTargetModelID); got != "rollout-target-openai" {
						t.Fatalf("%s resolved target model = %q", mode, got)
					}
					if got := extractModelFromBody(plan.UpstreamBody); got != "rollout-target-openai" {
						t.Fatalf("%s upstream body model = %q", mode, got)
					}
				}
			},
		},
		{
			name: "requested-model cheapest eligible context selection",
			snapshot: func() *planningSnapshot {
				snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "rollout-cheap-openai"})
				model := snapshot.ModelsByID["rollout-cheap-openai"]
				setRequestPlanStrategyType(snapshot, model, "cheapest_eligible_context")
				snapshot.AccessTargetsBySourceModelID[model.ID] = nil
				contextWindowTokens := 20_000
				addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_801, 9_801, 0, requestPlanConnectionTargetOptions{contextWindowTokens: &contextWindowTokens, maxContextUtilization: 1.0})
				addRequestPlanConnectionTargetWithOptions(snapshot, model, 2_802, 9_802, 1, requestPlanConnectionTargetOptions{
					contextWindowTokens:   &contextWindowTokens,
					maxContextUtilization: 1.0,
					pricingTemplateSnapshot: &runtimePricingTemplateSnapshot{
						PricingUnit:         runtimePricingUnitPerMillion,
						PricingCurrencyCode: "USD",
						InputPrice:          "1",
						OutputPrice:         "1",
					},
				})
				return snapshot
			},
			path:    "/v1/chat/completions",
			rawBody: []byte(`{"model":"rollout-cheap-openai","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64}`),
			assert: func(t *testing.T, plans map[string]requestPlan) {
				t.Helper()
				for mode, plan := range plans {
					attempts := plan.orderedTerminalAttempts()
					if len(attempts) == 0 || attempts[0].Connection.ID != 2_802 {
						t.Fatalf("%s first terminal attempt = %+v", mode, attempts)
					}
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plans := buildRolloutPlans(t, test.snapshot, test.path, test.rawBody, test.path, "", buildRequestedModelRolloutPlan)
			assertRolloutPlanParity(t, plans["legacy"], plans["shadow"], "shadow")
			assertRolloutPlanParity(t, plans["legacy"], plans["enforced"], "enforced")
			assertRolloutPlannerTrace(t, plans["legacy"], config.RuntimeRoutingPlannerModeLegacy, false)
			assertRolloutPlannerTrace(t, plans["shadow"], config.RuntimeRoutingPlannerModeShadow, false)
			assertRolloutPlannerTrace(t, plans["enforced"], config.RuntimeRoutingPlannerModeEnforced, false)
			if test.assert != nil {
				test.assert(t, plans)
			}
		})
	}
}

func TestRuntimePlannerRolloutExplicitTargetParity(t *testing.T) {
	plans := buildRolloutPlans(t, func() *planningSnapshot {
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "rollout-source-openai"},
			runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "rollout-promotion-openai"},
		)
		setRequestPlanConnectionContextWindow(snapshot, 1_002, 16_384)
		return snapshot
	}, "/v1/chat/completions", []byte(`{"model":"rollout-source-openai","messages":[{"role":"user","content":"promote"}]}`), "/v1/chat/completions", "rollout-promotion-openai", buildExplicitTargetRolloutPlan)

	assertRolloutPlanParity(t, plans["legacy"], plans["shadow"], "shadow")
	assertRolloutPlanParity(t, plans["legacy"], plans["enforced"], "enforced")
	for mode, plan := range plans {
		assertRolloutPlannerTrace(t, plan, config.RuntimeRoutingPlannerMode(mode), false)
		if plan.RequestedModelID != "rollout-promotion-openai" {
			t.Fatalf("%s requested model = %q", mode, plan.RequestedModelID)
		}
		if got := dereferenceString(plan.ResolvedTargetModelID); got != "rollout-promotion-openai" {
			t.Fatalf("%s resolved target model = %q", mode, got)
		}
		if got := extractModelFromBody(plan.UpstreamBody); got != "rollout-promotion-openai" {
			t.Fatalf("%s upstream body model = %q", mode, got)
		}
	}
}

func TestRuntimePlannerShadowReportsCompiledPlannerValidationMismatch(t *testing.T) {
	service := newShadowRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "shadow-valid-openai"})
	model := snapshot.ModelsByID["shadow-valid-openai"]
	snapshot.ModelsByID["shadow-alias-openai"] = model
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

	plan, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"shadow-valid-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("shadow mode should serve the legacy plan when compiled validation fails: %v", err)
	}
	assertRolloutPlannerTrace(t, plan, config.RuntimeRoutingPlannerModeShadow, true)
	comparison := plan.ContextRouting.PlannerTrace.ShadowComparisonResult
	if comparison.Result != runtimeShadowComparisonResultMismatch {
		t.Fatalf("expected shadow mismatch result, got %+v", comparison)
	}
	if !rolloutReasonsContain(comparison.MismatchReasons, "planner_validation") {
		t.Fatalf("expected planner_validation mismatch reason, got %+v", comparison.MismatchReasons)
	}
}

func buildRequestedModelRolloutPlan(service *Service, request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, operationMatch RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot, _ string) (requestPlan, error) {
	return service.buildRequestPlanFromSnapshot(request, rawBody, runtimeConfig, operationMatch, activeProfileID, snapshot)
}

func buildExplicitTargetRolloutPlan(service *Service, request *http.Request, rawBody []byte, runtimeConfig RuntimeProxyConfigSnapshot, _ RuntimeOperationMatch, activeProfileID int, snapshot *planningSnapshot, explicitTargetModelID string) (requestPlan, error) {
	return service.buildExplicitTargetRequestPlan(request, rawBody, runtimeConfig, activeProfileID, snapshot, explicitTargetModelID)
}

func buildRolloutPlans(t *testing.T, snapshotFactory func() *planningSnapshot, requestPath string, rawBody []byte, operationPath string, explicitTargetModelID string, build rolloutPlanBuild) map[string]requestPlan {
	t.Helper()
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, operationPath)
	modes := []struct {
		name    string
		service *Service
	}{
		{name: "legacy", service: newRequestPlanUnitService()},
		{name: "shadow", service: newShadowRequestPlanUnitService()},
		{name: "enforced", service: newEnforcedRequestPlanUnitService()},
	}
	plans := map[string]requestPlan{}
	for _, mode := range modes {
		req := httptest.NewRequest(http.MethodPost, requestPath, nil)
		plan, err := build(mode.service, req, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshotFactory(), explicitTargetModelID)
		if err != nil {
			t.Fatalf("%s build request plan: %v", mode.name, err)
		}
		plans[mode.name] = plan
	}
	return plans
}

func newShadowRequestPlanUnitService() *Service {
	service := newRequestPlanUnitService()
	service.plannerMode = config.RuntimeRoutingPlannerModeShadow
	service.openAITerminalTranslationMode = config.OpenAITerminalTranslationModeSafeOnly
	return service
}

func assertRolloutPlanParity(t *testing.T, want requestPlan, got requestPlan, mode string) {
	t.Helper()
	if got.RequestedModelID != want.RequestedModelID {
		t.Fatalf("%s requested model = %q, want %q", mode, got.RequestedModelID, want.RequestedModelID)
	}
	if dereferenceString(got.ResolvedTargetModelID) != dereferenceString(want.ResolvedTargetModelID) {
		t.Fatalf("%s resolved target = %q, want %q", mode, dereferenceString(got.ResolvedTargetModelID), dereferenceString(want.ResolvedTargetModelID))
	}
	if intPointerValue(got.selectedTerminalTargetID()) != intPointerValue(want.selectedTerminalTargetID()) {
		t.Fatalf("%s selected terminal = %d, want %d", mode, intPointerValue(got.selectedTerminalTargetID()), intPointerValue(want.selectedTerminalTargetID()))
	}
	if got.EffectiveRequestPath != want.EffectiveRequestPath {
		t.Fatalf("%s effective path = %q, want %q", mode, got.EffectiveRequestPath, want.EffectiveRequestPath)
	}
	if extractModelFromBody(got.UpstreamBody) != extractModelFromBody(want.UpstreamBody) {
		t.Fatalf("%s upstream model = %q, want %q", mode, extractModelFromBody(got.UpstreamBody), extractModelFromBody(want.UpstreamBody))
	}
	assertRolloutAttemptParity(t, want.orderedTerminalAttempts(), got.orderedTerminalAttempts(), mode)
}

func assertRolloutAttemptParity(t *testing.T, want []runtimeTerminalAttempt, got []runtimeTerminalAttempt, mode string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s terminal attempt count = %d, want %d", mode, len(got), len(want))
	}
	for index := range want {
		if got[index].Connection.ID != want[index].Connection.ID || got[index].TargetModel.ModelID != want[index].TargetModel.ModelID {
			t.Fatalf("%s terminal attempt %d = %+v, want %+v", mode, index, got[index], want[index])
		}
		if normalizedRuntimeTranslationMode(got[index].TranslationMode) != normalizedRuntimeTranslationMode(want[index].TranslationMode) {
			t.Fatalf("%s terminal attempt %d translation = %q, want %q", mode, index, got[index].TranslationMode, want[index].TranslationMode)
		}
	}
}

func assertRolloutPlannerTrace(t *testing.T, plan requestPlan, mode config.RuntimeRoutingPlannerMode, wantComparison bool) {
	t.Helper()
	if plan.ContextRouting == nil || plan.ContextRouting.PlannerTrace == nil {
		t.Fatalf("expected planner trace for mode %q, got %+v", mode, plan.ContextRouting)
	}
	trace := plan.ContextRouting.PlannerTrace
	if trace.PlannerMode != string(mode) {
		t.Fatalf("planner mode = %q, want %q", trace.PlannerMode, mode)
	}
	if (trace.ShadowComparisonResult != nil) != wantComparison {
		t.Fatalf("shadow comparison presence = %v, want %v: %+v", trace.ShadowComparisonResult != nil, wantComparison, trace.ShadowComparisonResult)
	}
}

func intPointerValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func rolloutReasonsContain(reasons []string, want string) bool {
	return slices.Contains(reasons, want)
}
