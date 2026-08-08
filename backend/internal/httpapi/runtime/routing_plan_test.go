package runtime

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func TestCompileRuntimeRoutingPlanBuildsCanonicalLookups(t *testing.T) {
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "target-b-openai"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "target-a-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "target-b-openai", 2)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "target-a-openai", 1)

	routingPlan, err := compileRuntimeRoutingPlan(snapshot)
	if err != nil {
		t.Fatalf("compile runtime routing plan: %v", err)
	}
	if err := validateRuntimeRoutingPlan(routingPlan); err != nil {
		t.Fatalf("validate runtime routing plan: %v", err)
	}
	requestedModel, ok := routingPlan.requestedModelByID("router-openai")
	if !ok || requestedModel.ModelID != "router-openai" {
		t.Fatalf("expected exact router model lookup, got model=%+v ok=%v", requestedModel, ok)
	}
	if _, ok := routingPlan.requestedModelByID("Router-OpenAI"); ok {
		t.Fatal("expected requested model lookup to remain exact and case-sensitive")
	}
	compiledRouter := routingPlan.ModelsByID["router-openai"]
	if len(compiledRouter.OrderedEnabledTargets) != 2 {
		t.Fatalf("expected two ordered router targets, got %+v", compiledRouter.OrderedEnabledTargets)
	}
	if compiledRouter.OrderedEnabledTargets[0].TargetModelID != "target-a-openai" {
		t.Fatalf("expected position-sorted first target target-a-openai, got %+v", compiledRouter.OrderedEnabledTargets[0])
	}
	if compiledRouter.OrderedEnabledTargets[1].TargetModelID != "target-b-openai" {
		t.Fatalf("expected position-sorted second target target-b-openai, got %+v", compiledRouter.OrderedEnabledTargets[1])
	}
	if connection, ok := routingPlan.TerminalTargetsByID[1_002]; !ok || connection.ModelConfigID != 2 {
		t.Fatalf("expected target-b terminal connection in compiled lookup, got connection=%+v ok=%v", connection, ok)
	}
}

func TestCompileRuntimeRoutingPlanOrdersMixedEnabledTargetsByPositionThenID(t *testing.T) {
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, 2)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "child-openai", 1)
	disabled := runtimeAccessTargetRecord{
		ID:                  9_000,
		ProfileID:           requestPlanTestProfileID,
		SourceModelConfigID: 1,
		TargetType:          runtimeAccessTargetTypeModel,
		TargetModelConfigID: intPtr(2),
		TargetModelID:       "child-openai",
		TargetModelEnabled:  true,
		Position:            0,
		IsEnabled:           false,
	}
	snapshot.AccessTargetsBySourceModelID[1] = append(snapshot.AccessTargetsBySourceModelID[1], disabled)

	routingPlan, err := compileRuntimeRoutingPlan(snapshot)
	if err != nil {
		t.Fatalf("compile runtime routing plan: %v", err)
	}
	if err := validateRuntimeRoutingPlan(routingPlan); err != nil {
		t.Fatalf("validate runtime routing plan: %v", err)
	}
	ordered := routingPlan.ModelsByConfigID[1].OrderedEnabledTargets
	if len(ordered) != 2 {
		t.Fatalf("expected disabled target to be skipped from ordered set, got %+v", ordered)
	}
	if ordered[0].TargetType != runtimeAccessTargetTypeModel || ordered[0].TargetModelID != "child-openai" {
		t.Fatalf("expected model target at lower position to sort first, got %+v", ordered[0])
	}
	if ordered[1].TargetType != runtimeAccessTargetTypeConnection || ordered[1].TargetConnectionID == nil || *ordered[1].TargetConnectionID != 2_001 {
		t.Fatalf("expected terminal target at higher position to sort second, got %+v", ordered[1])
	}
}

func TestCompileRuntimeRoutingPlanValidationRejectsModelKeyMismatch(t *testing.T) {
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "actual-openai"})
	model := snapshot.ModelsByID["actual-openai"]
	delete(snapshot.ModelsByID, "actual-openai")
	snapshot.ModelsByID["alias-openai"] = model

	routingPlan, err := compileRuntimeRoutingPlan(snapshot)
	if err != nil {
		t.Fatalf("compile runtime routing plan: %v", err)
	}
	err = validateRuntimeRoutingPlan(routingPlan)
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "model map key \"alias-openai\" does not match model_id \"actual-openai\"")
}

func TestCompileRuntimeRoutingPlanReportsStableValidationIssues(t *testing.T) {
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "actual-openai"})
	model := snapshot.ModelsByID["actual-openai"]
	delete(snapshot.ModelsByID, "actual-openai")
	snapshot.ModelsByID["alias-openai"] = model
	routingPlan, err := compileRuntimeRoutingPlan(snapshot)
	if err != nil {
		t.Fatalf("compile runtime routing plan: %v", err)
	}

	err = validateRuntimeRoutingPlan(routingPlan)
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	issues, ok := domainErr.Fields["routing_plan_issues"].([]runtimeRoutingPlanValidationIssue)
	if !ok {
		t.Fatalf("expected structured routing_plan_issues, got %+v", domainErr.Fields)
	}
	want := []runtimeRoutingPlanValidationIssue{
		{Code: "model_id_key_mismatch", Path: `plan.models_by_id["alias-openai"]`, Message: `model map key "alias-openai" does not match model_id "actual-openai"`},
		{Code: "model_config_missing_model_lookup", Path: "plan.models_by_config_id[1]", Message: `model config lookup for "actual-openai" has no model-id lookup`},
	}
	if len(issues) != len(want) {
		t.Fatalf("expected %d validation issues, got %+v", len(want), issues)
	}
	for index := range want {
		if issues[index] != want[index] {
			t.Fatalf("expected validation issue %d %+v, got %+v", index, want[index], issues[index])
		}
	}
}

func TestBuildRequestPlanFromSnapshotCompilesRoutingPlanLazily(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "lazy-openai"})
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	rawBody := []byte(`{"model":"lazy-openai","messages":[]}`)

	if snapshot.routingPlan != nil {
		t.Fatal("expected routing plan to compile lazily")
	}
	firstPlan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build first request plan: %v", err)
	}
	compiled := snapshot.routingPlan
	if compiled == nil {
		t.Fatal("expected first request plan build to compile routing plan")
	}
	snapshot.ModelsByID = map[string]runtimeModelRecord{}
	snapshot.AccessTargetsBySourceModelID = map[int][]runtimeAccessTargetRecord{}
	snapshot.TerminalTargetsByID = map[int]runtimeConnection{}
	snapshot.StrategiesByModelID = map[int]loadbalance.RuntimeStrategy{}

	secondPlan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build second request plan from compiled routing plan: %v", err)
	}
	if snapshot.routingPlan != compiled {
		t.Fatal("expected snapshot to reuse the first compiled routing plan")
	}
	if firstPlan.RequestedModelID != "lazy-openai" || secondPlan.RequestedModelID != "lazy-openai" {
		t.Fatalf("expected both plans to preserve requested model, got first=%q second=%q", firstPlan.RequestedModelID, secondPlan.RequestedModelID)
	}
}

func TestBuildRequestPlanFromSnapshotReturnsRoutingPlanValidationDomainError(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "actual-openai"})
	model := snapshot.ModelsByID["actual-openai"]
	delete(snapshot.ModelsByID, "actual-openai")
	snapshot.ModelsByID["alias-openai"] = model
	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)

	_, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"actual-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "Invalid runtime routing plan")
	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domain error, got %v", err)
	}
	issues, ok := domainErr.Fields["routing_plan_issues"].([]runtimeRoutingPlanValidationIssue)
	if !ok || len(issues) == 0 {
		t.Fatalf("expected structured routing_plan_issues on planning error, got %+v", domainErr.Fields)
	}
	if issues[0].Code != "model_id_key_mismatch" || issues[0].Path != `plan.models_by_id["alias-openai"]` {
		t.Fatalf("expected stable first validation issue, got %+v", issues[0])
	}
}

func TestBuildRequestPlanFromSnapshotModelPeersPreserveStrategyOrder(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "low-priority-openai"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "high-priority-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "low-priority-openai", 0)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "high-priority-openai", 1)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	plan, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build strategy-ordered peer request plan: %v", err)
	}
	wantModels := []string{"low-priority-openai", "high-priority-openai"}
	if len(plan.TerminalAttempts) != len(wantModels) {
		t.Fatalf("expected strategy-ordered peer terminal paths, got %+v", plan.TerminalAttempts)
	}
	for index, wantModelID := range wantModels {
		if got := plan.TerminalAttempts[index].TargetModel.ModelID; got != wantModelID {
			t.Fatalf("expected peer attempt %d model %q, got %q", index, wantModelID, got)
		}
	}
}

func TestBuildRequestPlanFromSnapshotModelPeersExcludeIneligibleTargets(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "eligible-first-openai"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "blocked-openai"},
		runtimeModelRecord{ID: 4, APIFamily: "openai", ModelID: "eligible-second-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "eligible-first-openai", 0)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "blocked-openai", 1)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "eligible-second-openai", 2)
	blockedUntil := service.nowUTC().Add(time.Minute)
	seededAt := service.nowUTC().Add(-time.Minute)
	service.runtimeState.SeedConnectionState(requestPlanTestProfileID, 3, 1_003, loadbalance.RuntimeConnectionState{ConnectionID: 1_003, BanMode: "temporary", BannedUntilAt: &blockedUntil}, seededAt, seededAt)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	plan, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build eligible-only peer request plan: %v", err)
	}
	wantModels := []string{"eligible-first-openai", "eligible-second-openai"}
	if len(plan.TerminalAttempts) != len(wantModels) {
		t.Fatalf("expected eligible peer attempts only, got %+v", plan.TerminalAttempts)
	}
	for index, wantModelID := range wantModels {
		if got := plan.TerminalAttempts[index].TargetModel.ModelID; got != wantModelID {
			t.Fatalf("expected peer attempt %d model %q, got %q", index, wantModelID, got)
		}
	}
}

func TestBuildRequestPlanFromSnapshotZeroLeafModelPeerKeepsFollowingTerminalPeerInMixedOrder(t *testing.T) {
	// Router owns one terminal target (position 0) and one model target pointing at a
	// zero-leaf child (position 1). Mixed fill-first order must resolve the terminal
	// peer first by position; the zero-leaf model peer contributes nothing and the
	// router terminal is not a separate fallback tier.
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-peer-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "empty-peer-openai", 1)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	plan, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build mixed-order request plan: %v", err)
	}
	if len(plan.TerminalAttempts) != 1 {
		t.Fatalf("expected only the position-0 terminal peer attempt, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TargetModel.ModelID; got != "router-openai" {
		t.Fatalf("expected position-0 terminal peer to keep router model, got %q", got)
	}
	if got := plan.TerminalAttempts[0].Connection.ID; got != 1_001 {
		t.Fatalf("expected router terminal connection 1001, got %d", got)
	}

	// Reverse authored order: zero-leaf model peer at position 0, terminal peer at
	// position 1. Mixed order must still reach the terminal peer as the next row,
	// not because of a terminal fallback tier.
	reverse := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-peer-openai"},
	)
	reverse.AccessTargetsBySourceModelID[1] = nil
	reverse.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanConnectionTarget(reverse, reverse.ModelsByID["router-openai"], 1_001, 9_001, 1)
	addRequestPlanModelTargetWithMetadata(reverse, "router-openai", "empty-peer-openai", 0)

	plan, err = service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, reverse)
	if err != nil {
		t.Fatalf("build reversed mixed-order request plan: %v", err)
	}
	if len(plan.TerminalAttempts) != 1 {
		t.Fatalf("expected reversed order to still resolve one terminal attempt, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].Connection.ID; got != 1_001 {
		t.Fatalf("expected reversed order terminal connection 1001, got %d", got)
	}
}
