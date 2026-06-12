package runtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimePlannerUsesCompiledPlanForRequestedModel(t *testing.T) {
	tests := []struct {
		name     string
		snapshot func() *planningSnapshot
		path     string
		rawBody  []byte
		assert   func(t *testing.T, plan requestPlan)
	}{
		{
			name: "requested-model multi-hop terminal ordering",
			snapshot: func() *planningSnapshot {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "planner-public-openai"},
					runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "planner-mid-openai"},
					runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "planner-target-openai"},
				)
				addRequestPlanProxyTarget(snapshot, "planner-public-openai", "planner-mid-openai")
				addRequestPlanProxyTarget(snapshot, "planner-mid-openai", "planner-target-openai")
				setRequestPlanConnectionContextWindow(snapshot, 1_003, 16_384)
				return snapshot
			},
			path:    "/v1/chat/completions",
			rawBody: []byte(`{"model":"planner-public-openai","messages":[{"role":"user","content":"hello"}]}`),
			assert: func(t *testing.T, plan requestPlan) {
				if got := dereferenceString(plan.ResolvedTargetModelID); got != "planner-target-openai" {
					t.Fatalf("resolved target model = %q", got)
				}
				if got := extractModelFromBody(plan.UpstreamBody); got != "planner-target-openai" {
					t.Fatalf("upstream body model = %q", got)
				}
			},
		},
		{
			name: "requested-model cheapest eligible context selection",
			snapshot: func() *planningSnapshot {
				snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-4o-planner-cheap-openai"})
				model := snapshot.ModelsByID["gpt-4o-planner-cheap-openai"]
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
			rawBody: []byte(`{"model":"gpt-4o-planner-cheap-openai","messages":[{"role":"user","content":"hello"}],"max_completion_tokens":64}`),
			assert: func(t *testing.T, plan requestPlan) {
				attempts := plan.orderedTerminalAttempts()
				if len(attempts) == 0 || attempts[0].Connection.ID != 2_802 {
					t.Fatalf("first terminal attempt = %+v", attempts)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			plan := buildCurrentRequestedModelPlan(t, test.snapshot(), test.path, test.rawBody)
			assertCurrentPlannerTrace(t, plan)
			if test.assert != nil {
				test.assert(t, plan)
			}
		})
	}
}

func TestRuntimePlannerUsesCompiledPlanForExplicitTarget(t *testing.T) {
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "planner-source-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "planner-promotion-openai"},
	)
	setRequestPlanConnectionContextWindow(snapshot, 1_002, 16_384)
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	plan, err := newRequestPlanUnitService().buildExplicitTargetRequestPlan(
		request,
		[]byte(`{"model":"planner-source-openai","messages":[{"role":"user","content":"promote"}]}`),
		RuntimeProxyConfigSnapshot{},
		requestPlanTestProfileID,
		snapshot,
		"planner-promotion-openai",
	)
	if err != nil {
		t.Fatalf("build explicit target plan: %v", err)
	}
	assertCurrentPlannerTrace(t, plan)
	if plan.RequestedModelID != "planner-promotion-openai" {
		t.Fatalf("requested model = %q", plan.RequestedModelID)
	}
	if got := dereferenceString(plan.ResolvedTargetModelID); got != "planner-promotion-openai" {
		t.Fatalf("resolved target model = %q", got)
	}
	if got := extractModelFromBody(plan.UpstreamBody); got != "planner-promotion-openai" {
		t.Fatalf("upstream body model = %q", got)
	}
}

func TestRuntimePlannerValidationBlocksInvalidCompiledPlan(t *testing.T) {
	service := newRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "compiled-valid-openai"})
	snapshot.ModelsByID["compiled-alias-openai"] = snapshot.ModelsByID["compiled-valid-openai"]
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

	_, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"compiled-valid-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err == nil || !strings.Contains(err.Error(), "Invalid runtime routing plan") {
		t.Fatalf("expected compiled planner validation error, got %v", err)
	}
}

func buildCurrentRequestedModelPlan(t *testing.T, snapshot *planningSnapshot, requestPath string, rawBody []byte) requestPlan {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, requestPath, nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	plan, err := newRequestPlanUnitService().buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build request plan: %v", err)
	}
	return plan
}

func assertCurrentPlannerTrace(t *testing.T, plan requestPlan) {
	t.Helper()
	if plan.ContextRouting == nil || plan.ContextRouting.PlannerTrace == nil {
		return
	}
	trace := plan.ContextRouting.PlannerTrace
	if trace.PlannerVersion == "" || trace.Decision == "" {
		t.Fatalf("planner trace should include current planner metadata, got %+v", trace)
	}
}
