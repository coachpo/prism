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
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "target-b-openai", 2, 3, 2)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "target-a-openai", 1, 5, 1)

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
	if len(compiledRouter.PeerTiers) != 2 {
		t.Fatalf("expected two priority peer tiers, got %+v", compiledRouter.PeerTiers)
	}
	if compiledRouter.PeerTiers[0].TargetPriority != 1 || compiledRouter.PeerTiers[0].WeightedPeerSet.TotalWeight != 5 {
		t.Fatalf("expected priority 1 peer tier weight 5, got %+v", compiledRouter.PeerTiers[0])
	}
	if compiledRouter.PeerTiers[1].TargetPriority != 2 || compiledRouter.PeerTiers[1].WeightedPeerSet.TotalWeight != 3 {
		t.Fatalf("expected priority 2 peer tier weight 3, got %+v", compiledRouter.PeerTiers[1])
	}
	if connection, ok := routingPlan.TerminalTargetsByID[1_002]; !ok || connection.ModelConfigID != 2 {
		t.Fatalf("expected target-b terminal connection in compiled lookup, got connection=%+v ok=%v", connection, ok)
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
	snapshot.ConnectionsByID = map[int]runtimeConnection{}
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

func TestBuildRequestPlanFromSnapshotWeightedPeerTierUsesPriorityBeforePosition(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "low-priority-openai"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "high-priority-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "low-priority-openai", 0, 1, 10)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "high-priority-openai", 1, 1, 1)
	setRequestPlanConnectionContextWindow(snapshot, 1_002, 16_384)
	setRequestPlanConnectionContextWindow(snapshot, 1_003, 16_384)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	plan, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build weighted priority-tier request plan: %v", err)
	}
	if len(plan.TerminalAttempts) != 1 {
		t.Fatalf("expected one selected peer terminal path, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TargetModel.ModelID; got != "high-priority-openai" {
		t.Fatalf("expected priority tier to select high-priority-openai, got %q", got)
	}
}

func TestBuildRequestPlanFromSnapshotWeightedPeerTierExcludesIneligibleWeights(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "eligible-first-openai"},
		runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "blocked-openai"},
		runtimeModelRecord{ID: 4, APIFamily: "openai", ModelID: "eligible-second-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "eligible-first-openai", 0, 1, 0)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "blocked-openai", 1, 100, 0)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "eligible-second-openai", 2, 1, 0)
	setRequestPlanConnectionContextWindow(snapshot, 1_002, 16_384)
	setRequestPlanConnectionContextWindow(snapshot, 1_003, 16_384)
	setRequestPlanConnectionContextWindow(snapshot, 1_004, 16_384)
	blockedUntil := service.nowUTC().Add(time.Minute)
	seededAt := service.nowUTC().Add(-time.Minute)
	service.runtimeState.SeedConnectionState(requestPlanTestProfileID, 3, 1_003, loadbalance.RuntimeConnectionState{ConnectionID: 1_003, BanMode: "temporary", BannedUntilAt: &blockedUntil}, seededAt, seededAt)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	wantModels := []string{"eligible-first-openai", "eligible-second-openai", "eligible-first-openai", "eligible-second-openai"}
	for index, wantModelID := range wantModels {
		plan, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		if err != nil {
			t.Fatalf("build eligible-only weighted request plan %d: %v", index, err)
		}
		if len(plan.TerminalAttempts) != 1 {
			t.Fatalf("expected one selected weighted peer attempt on iteration %d, got %+v", index, plan.TerminalAttempts)
		}
		if got := plan.TerminalAttempts[0].TargetModel.ModelID; got != wantModelID {
			t.Fatalf("expected iteration %d selected peer %q, got %q", index, wantModelID, got)
		}
	}
}

func TestBuildRequestPlanFromSnapshotWeightedPeerFallsBackToTerminalOnlyWhenNoPeerSurvivesPreflight(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-peer-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "empty-peer-openai", 1, 10, 0)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	plan, err := service.buildRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build terminal fallback request plan: %v", err)
	}
	if len(plan.TerminalAttempts) != 1 {
		t.Fatalf("expected only terminal fallback attempt, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TargetModel.ModelID; got != "router-openai" {
		t.Fatalf("expected terminal fallback to keep router model, got %q", got)
	}
	if got := plan.TerminalAttempts[0].Connection.ID; got != 1_001 {
		t.Fatalf("expected router terminal connection 1001, got %d", got)
	}
}

func setRequestPlanConnectionContextWindow(snapshot *planningSnapshot, connectionID int, contextWindowTokens int) {
	connection := snapshot.ConnectionsByID[connectionID]
	connection.ContextWindowTokens = intPtr(contextWindowTokens)
	connection.DefaultOutputTokenReserve = 1_024
	connection.MaxContextUtilization = 1.0
	snapshot.ConnectionsByID[connectionID] = connection
}
