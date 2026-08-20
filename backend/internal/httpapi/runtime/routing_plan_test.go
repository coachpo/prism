package runtime

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
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
	firstPlan, err := service.buildTestRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
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

	secondPlan, err := service.buildTestRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
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

	_, err := service.buildTestRequestPlanFromSnapshot(request, []byte(`{"model":"actual-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
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
	plan, err := service.buildTestRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
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
	plan, err := service.buildTestRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
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
	// Model and Terminal Targets are one authored peer sequence. The terminal
	// peer at position 0 is selected before a zero-leaf model peer at position 1.
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-peer-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "empty-peer-openai", 1)

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
	plan, err := service.buildTestRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
	if err != nil {
		t.Fatalf("build mixed-order request plan: %v", err)
	}
	if len(plan.TerminalAttempts) != 1 {
		t.Fatalf("expected only position-0 terminal peer attempt, got %+v", plan.TerminalAttempts)
	}
	if got := plan.TerminalAttempts[0].TargetModel.ModelID; got != "router-openai" {
		t.Fatalf("expected terminal peer to keep router model, got %q", got)
	}
	if got := plan.TerminalAttempts[0].Connection.ID; got != 1_001 {
		t.Fatalf("expected router terminal connection 1001, got %d", got)
	}

	// Reverse authored order: the zero-leaf model is position 0 and the
	// terminal peer is position 1. The next mixed peer is still considered.
	reverse := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-peer-openai"},
	)
	reverse.AccessTargetsBySourceModelID[1] = nil
	reverse.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanConnectionTarget(reverse, reverse.ModelsByID["router-openai"], 1_001, 9_001, 1)
	addRequestPlanModelTargetWithMetadata(reverse, "router-openai", "empty-peer-openai", 0)

	plan, err = service.buildTestRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, reverse)
	if err != nil {
		t.Fatalf("build reversed mixed-order request plan: %v", err)
	}
	if len(plan.TerminalAttempts) != 1 || plan.TerminalAttempts[0].Connection.ID != 1_001 {
		t.Fatalf("expected reversed order to resolve terminal peer, got %+v", plan.TerminalAttempts)
	}
}

func TestBuildRequestPlanFromSnapshotUsesAuthoredMixedOrder(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "child-openai", 0)
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, 1)

	plan := mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	if len(plan.TerminalAttempts) != 2 || plan.TerminalAttempts[0].Connection.ID != 1_002 || plan.TerminalAttempts[1].Connection.ID != 2_001 {
		t.Fatalf("expected authored mixed order child then terminal, got %+v", plan.TerminalAttempts)
	}
}

func TestBuildRequestPlanFromSnapshotSingleUsesFirstAuthoredMixedPeer(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "empty-child-openai", 0)
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, 1)
	strategyType := "single"
	snapshot.StrategiesByModelID[1] = loadbalance.RuntimeStrategy{ID: requestPlanTestStrategyID, Name: "single", LegacyStrategyType: &strategyType}

	_, err := service.buildTestRequestPlanFromSnapshot(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil), []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions"), requestPlanTestProfileID, snapshot)
	if err == nil {
		t.Fatal("expected single to keep the first zero-leaf mixed peer and fail closed")
	}
}

// requestPlanFixedNow is 2023-11-14T22:13:20Z: Tuesday (ISO 2 -> bit1),
// local minute 1333 in UTC. All schedule windows below are designed around
// these three numbers.
var requestPlanFixedNow = time.Unix(1_700_000_000, 0).UTC()

// requestPlanMondayOnlySchedule is closed at requestPlanFixedNow (mask 1 =
// Monday only, all day).
func requestPlanMondayOnlySchedule() terminaltarget.CompiledRoutingSchedule {
	return terminaltarget.CompileRoutingSchedule("UTC", []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 1440}})
}

// requestPlanTuesdayOnlySchedule is open at requestPlanFixedNow (mask 2 =
// Tuesday only, all day).
func requestPlanTuesdayOnlySchedule() terminaltarget.CompiledRoutingSchedule {
	return terminaltarget.CompileRoutingSchedule("UTC", []terminaltarget.Window{{WeekdayMask: 2, StartMinute: 0, EndMinute: 1440}})
}

func withRoutingSchedule(snapshot *planningSnapshot, connectionID int, schedule terminaltarget.CompiledRoutingSchedule) {
	connection := snapshot.TerminalTargetsByID[connectionID]
	connection.RoutingSchedule = schedule
	snapshot.TerminalTargetsByID[connectionID] = connection
}

func TestBuildRequestPlanFromSnapshotRoutingScheduleGate(t *testing.T) {
	// Table-driven: exclusion keeps peer order (both OpenAI and Gemini
	// families), nested pure-schedule emits the closed code, nested mixed and
	// subtree-mixed causes must NOT emit it, and a diamond authorization
	// graph still emits it because the observation sets dedupe by connection.
	requestBody := func(modelID string) []byte {
		return []byte(`{"model":"` + modelID + `","messages":[]}`)
	}
	cases := []struct {
		name       string
		apiFamily  string
		build      func(t *testing.T) (*Service, *planningSnapshot)
		wantModels []string
		wantCode   string
	}{
		{
			name: "exclusion keeps peer order openai",
			build: func(t *testing.T) (*Service, *planningSnapshot) {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
					runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "first-openai"},
					runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "middle-openai"},
					runtimeModelRecord{ID: 4, APIFamily: "openai", ModelID: "last-openai"},
				)
				snapshot.AccessTargetsBySourceModelID[1] = nil
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "first-openai", 0)
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "middle-openai", 1)
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "last-openai", 2)
				withRoutingSchedule(snapshot, 1003, requestPlanMondayOnlySchedule())
				return newEnforcedRequestPlanUnitService(), snapshot
			},
			wantModels: []string{"first-openai", "last-openai"},
		},
		{
			name:      "exclusion keeps peer order gemini",
			apiFamily: "gemini",
			build: func(t *testing.T) (*Service, *planningSnapshot) {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "router-gemini"},
					runtimeModelRecord{ID: 2, APIFamily: "gemini", ModelID: "first-gemini"},
					runtimeModelRecord{ID: 3, APIFamily: "gemini", ModelID: "middle-gemini"},
					runtimeModelRecord{ID: 4, APIFamily: "gemini", ModelID: "last-gemini"},
				)
				snapshot.AccessTargetsBySourceModelID[1] = nil
				addRequestPlanModelTargetWithMetadata(snapshot, "router-gemini", "first-gemini", 0)
				addRequestPlanModelTargetWithMetadata(snapshot, "router-gemini", "middle-gemini", 1)
				addRequestPlanModelTargetWithMetadata(snapshot, "router-gemini", "last-gemini", 2)
				withRoutingSchedule(snapshot, 1003, requestPlanMondayOnlySchedule())
				return newEnforcedRequestPlanUnitService(), snapshot
			},
			wantModels: []string{"first-gemini", "last-gemini"},
		},
		{
			name: "nested pure schedule emits closed code",
			build: func(t *testing.T) (*Service, *planningSnapshot) {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
					runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "peer-openai"},
				)
				snapshot.AccessTargetsBySourceModelID[1] = nil
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "peer-openai", 0)
				withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
				return newEnforcedRequestPlanUnitService(), snapshot
			},
			wantCode: terminalTargetScheduleClosedErrorCode,
		},
		{
			name:      "nested pure schedule emits closed code gemini",
			apiFamily: "gemini",
			build: func(t *testing.T) (*Service, *planningSnapshot) {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "gemini", ModelID: "router-gemini"},
					runtimeModelRecord{ID: 2, APIFamily: "gemini", ModelID: "peer-gemini"},
				)
				snapshot.AccessTargetsBySourceModelID[1] = nil
				addRequestPlanModelTargetWithMetadata(snapshot, "router-gemini", "peer-gemini", 0)
				withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
				return newEnforcedRequestPlanUnitService(), snapshot
			},
			wantCode: terminalTargetScheduleClosedErrorCode,
		},
		{
			name: "nested mixed schedule plus banned peer does not emit",
			build: func(t *testing.T) (*Service, *planningSnapshot) {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
					runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "peer-openai"},
				)
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "peer-openai", 0)
				withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
				service := newEnforcedRequestPlanUnitService()
				bannedUntil := service.nowUTC().Add(time.Minute)
				seededAt := service.nowUTC().Add(-time.Minute)
				service.runtimeState.SeedConnectionState(requestPlanTestProfileID, 1, 1001, loadbalance.RuntimeConnectionState{ConnectionID: 1001, BanMode: "temporary", BannedUntilAt: &bannedUntil}, seededAt, seededAt)
				return service, snapshot
			},
			wantCode: "",
		},
		{
			name: "subtree mixed schedule plus banned sibling does not emit",
			build: func(t *testing.T) (*Service, *planningSnapshot) {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
					runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "peer-openai"},
				)
				snapshot.AccessTargetsBySourceModelID[1] = nil
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "peer-openai", 0)
				withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
				addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["peer-openai"], 1003, 20_003, 1)
				service := newEnforcedRequestPlanUnitService()
				bannedUntil := service.nowUTC().Add(time.Minute)
				seededAt := service.nowUTC().Add(-time.Minute)
				service.runtimeState.SeedConnectionState(requestPlanTestProfileID, 2, 1003, loadbalance.RuntimeConnectionState{ConnectionID: 1003, BanMode: "temporary", BannedUntilAt: &bannedUntil}, seededAt, seededAt)
				return service, snapshot
			},
			wantCode: "",
		},
		{
			name: "diamond graph dedupes by connection and emits",
			build: func(t *testing.T) (*Service, *planningSnapshot) {
				snapshot := newRequestPlanSnapshot(
					runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
					runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "left-openai"},
					runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "right-openai"},
				)
				snapshot.AccessTargetsBySourceModelID[1] = nil
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "left-openai", 0)
				addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "right-openai", 1)
				// Both children reach the same terminal connection 1002.
				withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
				snapshot.AccessTargetsBySourceModelID[3] = nil
				addRequestPlanConnectionTargetWithOptions(snapshot, snapshot.ModelsByID["right-openai"], 1002, 30_002, 0, requestPlanConnectionTargetOptions{routingSchedule: requestPlanMondayOnlySchedule()})
				return newEnforcedRequestPlanUnitService(), snapshot
			},
			wantCode: terminalTargetScheduleClosedErrorCode,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, snapshot := tc.build(t)
			apiFamily := tc.apiFamily
			if apiFamily == "" {
				apiFamily = "openai"
			}
			routerModelID := "router-" + apiFamily
			var request *http.Request
			var rawBody []byte
			if apiFamily == "gemini" {
				request = httptest.NewRequest(http.MethodPost, "/v1beta/models/"+routerModelID+":generateContent", nil)
				rawBody = []byte(`{"contents":[{"parts":[{"text":"hi"}]}]}`)
			} else {
				request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				rawBody = requestBody(routerModelID)
			}
			operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
			plan, err := service.buildTestRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
			if tc.wantModels != nil {
				if err != nil {
					t.Fatalf("expected plan to succeed, got %v", err)
				}
				if len(plan.TerminalAttempts) != len(tc.wantModels) {
					t.Fatalf("expected %d attempts, got %+v", len(tc.wantModels), plan.TerminalAttempts)
				}
				for index, wantModelID := range tc.wantModels {
					if got := plan.TerminalAttempts[index].TargetModel.ModelID; got != wantModelID {
						t.Fatalf("expected attempt %d model %q, got %q", index, wantModelID, got)
					}
				}
				return
			}
			if err == nil {
				t.Fatalf("expected routing failure, got plan %+v", plan)
			}
			assertPlanDomainErrorCode(t, err, http.StatusServiceUnavailable, tc.wantCode)
		})
	}
}

func TestRoutingSchedulePartialExclusionDetail(t *testing.T) {
	buildFixture := func() (*Service, *planningSnapshot) {
		service := newEnforcedRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
			runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "peer-openai"},
		)
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "peer-openai", 0)
		withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
		bannedUntil := service.nowUTC().Add(time.Minute)
		seededAt := service.nowUTC().Add(-time.Minute)
		service.runtimeState.SeedConnectionState(requestPlanTestProfileID, 1, 1001, loadbalance.RuntimeConnectionState{ConnectionID: 1001, BanMode: "temporary", BannedUntilAt: &bannedUntil}, seededAt, seededAt)
		return service, snapshot
	}

	t.Run("closed hint", func(t *testing.T) {
		service, snapshot := buildFixture()
		_, err := service.buildTestRequestPlanFromSnapshot(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil), []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions"), requestPlanTestProfileID, snapshot)
		if err == nil {
			t.Fatalf("expected mixed-cause routing failure")
		}
		var domainErr *domainError
		if !errors.As(err, &domainErr) {
			t.Fatalf("expected domain error, got %v", err)
		}
		if domainErr.ErrorCode != "" {
			t.Fatalf("expected no stable code for a mixed cause, got %q", domainErr.ErrorCode)
		}
		if !strings.Contains(domainErr.Detail, "1 of 2 terminal targets were outside their routing window.") {
			t.Fatalf("expected mixed closed hint in detail, got %q", domainErr.Detail)
		}
	})

	t.Run("unresolvable hint", func(t *testing.T) {
		service, snapshot := buildFixture()
		withRoutingSchedule(snapshot, 1002, terminaltarget.CompileRoutingSchedule("Not/AZone", []terminaltarget.Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 60}}))
		_, err := service.buildTestRequestPlanFromSnapshot(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil), []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, mustResolveRuntimeOperation(t, http.MethodPost, "/v1/chat/completions"), requestPlanTestProfileID, snapshot)
		if err == nil {
			t.Fatalf("expected mixed-cause routing failure")
		}
		var domainErr *domainError
		if !errors.As(err, &domainErr) {
			t.Fatalf("expected domain error, got %v", err)
		}
		if domainErr.ErrorCode != "" {
			t.Fatalf("expected no stable code for a mixed cause, got %q", domainErr.ErrorCode)
		}
		if !strings.Contains(domainErr.Detail, "1 of 2 terminal targets have an unresolvable routing timezone.") {
			t.Fatalf("expected mixed unresolvable hint in detail, got %q", domainErr.Detail)
		}
	})
}

func TestRoutingScheduleExclusionsKeepTargetSetHashStable(t *testing.T) {
	build := func(closed bool) []runtimeAccessTargetRecord {
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"})
		if closed {
			withRoutingSchedule(snapshot, 1001, requestPlanMondayOnlySchedule())
		}
		return orderRuntimeAccessTargets(requestPlanTestProfileID, 1, snapshot.StrategiesByModelID[1], snapshot.AccessTargetsBySourceModelID[1], nil)
	}
	withoutSchedule := runtimeAccessTargetSetHash(build(false))
	withSchedule := runtimeAccessTargetSetHash(build(true))
	if withoutSchedule != withSchedule {
		t.Fatalf("expected the round-robin set hash to ignore routing schedules: %q != %q", withoutSchedule, withSchedule)
	}
}

func TestRoutingSchedulePreferredSelectionSkew(t *testing.T) {
	t.Run("mild shape three peers one closed", func(t *testing.T) {
		service := newEnforcedRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
			runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "peer-a-openai"},
			runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "peer-b-openai"},
			runtimeModelRecord{ID: 4, APIFamily: "openai", ModelID: "peer-c-openai"},
		)
		roundRobin := "round-robin"
		snapshot.StrategiesByModelID[1] = loadbalance.RuntimeStrategy{ID: requestPlanTestStrategyID, Name: "router round robin", LegacyStrategyType: &roundRobin}
		snapshot.AccessTargetsBySourceModelID[1] = nil
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "peer-a-openai", 0)
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "peer-b-openai", 1)
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "peer-c-openai", 2)
		withRoutingSchedule(snapshot, 1003, requestPlanMondayOnlySchedule()) // middle peer closed
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
		preferred := map[string]int{}
		for i := 0; i < 6; i++ {
			plan, err := service.buildTestRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
			if err != nil {
				t.Fatalf("build round %d: %v", i, err)
			}
			preferred[plan.TerminalAttempts[0].TargetModel.ModelID]++
		}
		if preferred["peer-a-openai"] != 2 || preferred["peer-c-openai"] != 4 {
			t.Fatalf("expected 2:4 preference peer-a:peer-c (cursor lands on peer-a one of three positions), got %+v", preferred)
		}
	})

	t.Run("collective window shape five peers three closed", func(t *testing.T) {
		service := newEnforcedRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
			runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "night-a-openai"},
			runtimeModelRecord{ID: 3, APIFamily: "openai", ModelID: "night-b-openai"},
			runtimeModelRecord{ID: 4, APIFamily: "openai", ModelID: "night-c-openai"},
			runtimeModelRecord{ID: 5, APIFamily: "openai", ModelID: "day-d-openai"},
			runtimeModelRecord{ID: 6, APIFamily: "openai", ModelID: "day-e-openai"},
		)
		roundRobin := "round-robin"
		snapshot.StrategiesByModelID[1] = loadbalance.RuntimeStrategy{ID: requestPlanTestStrategyID, Name: "router round robin", LegacyStrategyType: &roundRobin}
		snapshot.AccessTargetsBySourceModelID[1] = nil
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "night-a-openai", 0)
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "night-b-openai", 1)
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "night-c-openai", 2)
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "day-d-openai", 3)
		addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "day-e-openai", 4)
		withRoutingSchedule(snapshot, 1002, requestPlanMondayOnlySchedule())
		withRoutingSchedule(snapshot, 1003, requestPlanMondayOnlySchedule())
		withRoutingSchedule(snapshot, 1004, requestPlanMondayOnlySchedule())
		request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, request.URL.Path)
		preferred := map[string]int{}
		for i := 0; i < 10; i++ {
			plan, err := service.buildTestRequestPlanFromSnapshot(request, []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
			if err != nil {
				t.Fatalf("build round %d: %v", i, err)
			}
			preferred[plan.TerminalAttempts[0].TargetModel.ModelID]++
		}
		// Cursor lands on night-a/b/c/day-d four positions out of five, so
		// the first pick is day-d; only landing on day-e picks day-e first:
		// (k+1)/N = 4/5 -> 8:2 over ten resolutions.
		if preferred["day-d-openai"] != 8 || preferred["day-e-openai"] != 2 {
			t.Fatalf("expected 8:2 preference day-d:day-e, got %+v", preferred)
		}
	})
}
