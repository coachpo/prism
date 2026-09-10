package runtime

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
)

func TestRouteExplanationUsesRealFrozenPlannerWithoutSharedClaims(t *testing.T) {
	for _, kind := range []string{"single", "fill-first", "round-robin"} {
		t.Run(kind, func(t *testing.T) {
			service := newEnforcedRequestPlanUnitService()
			snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, ProfileID: 1, APIFamily: "openai", ModelID: "entry"}, runtimeModelRecord{ID: 2, ProfileID: 1, APIFamily: "openai", ModelID: "child"})
			addRequestPlanModelTargetWithMetadata(snapshot, "entry", "child", 2)
			snapshot.StrategiesByModelID[1] = loadbalance.RuntimeStrategy{ID: requestPlanTestStrategyID, Name: kind, LegacyStrategyType: &kind}
			targets := sortedEnabledRuntimeAccessTargets(snapshot.AccessTargetsBySourceModelID[1])
			service.runtimeState.ClaimRoundRobinTargetCursor(1, 1, requestPlanTestStrategyID, runtimeAccessTargetSetHash(targets), len(targets))
			service.runtimeState.SeedConnectionState(1, 1, 1001, loadbalance.RuntimeConnectionState{ConnectionID: 1001, BanMode: "off", WindowRequestCount: 7}, requestPlanFixedNow, requestPlanFixedNow)
			before, _ := service.runtimeState.CloneForObservation(1, 4096)
			explanation, err := service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.chat_completions", requestPlanFixedNow)
			if err != nil {
				t.Fatal(err)
			}
			repeat, err := service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.chat_completions", requestPlanFixedNow)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(explanation.Candidates, repeat.Candidates) {
				t.Fatal("observation advanced shared planner ordering")
			}
			operation := mustResolveRuntimeOperation(t, "POST", "/v1/chat/completions").Operation
			real, err := service.resolveExecutionTargetFromSnapshot(1, snapshot, snapshot.ModelsByID["entry"], operation, requestPlanFixedNow)
			if err != nil {
				t.Fatal(err)
			}
			ids := []int{}
			for _, row := range explanation.Candidates {
				if row.PlannerPosition != nil {
					ids = append(ids, row.TerminalTargetID)
				}
			}
			want := []int{}
			for _, attempt := range real.TerminalAttempts {
				want = append(want, attempt.Connection.ID)
			}
			if !reflect.DeepEqual(ids, want) {
				t.Fatalf("explanation %v != real planner %v", ids, want)
			}
			// Clone contains exactly pre-observation counters; normal planner does not reserve.
			refs := []loadbalance.RuntimeConnectionRef{{ConnectionID: 1001, ModelConfigID: 1}}
			if !reflect.DeepEqual(before.SnapshotConnectionStates(1, refs), service.runtimeState.SnapshotConnectionStates(1, refs)) {
				t.Fatal("observation changed capacity or ban")
			}
			if explanation.Completeness != "partial" && kind != "single" {
				t.Fatal("unobserved child lost missing label")
			}
		})
	}
}

func TestRouteExplanationClosedAndBannedFacts(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, ProfileID: 1, APIFamily: "openai", ModelID: "entry"})
	withRoutingSchedule(snapshot, 1001, requestPlanMondayOnlySchedule())
	explanation, err := service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.chat_completions", requestPlanFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Candidates[0].Reason != "schedule_closed" {
		t.Fatalf("%+v", explanation)
	}
	service.runtimeState.SeedConnectionState(1, 1, 1001, loadbalance.RuntimeConnectionState{ConnectionID: 1001, BanMode: "until_reset"}, requestPlanFixedNow, requestPlanFixedNow)
	explanation, err = service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.chat_completions", requestPlanFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if explanation.Candidates[0].Reason != "banned" {
		t.Fatalf("%+v", explanation)
	}
}

func TestRouteExplanationRetainsDisabledAndMissingTargets(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, ProfileID: 1, APIFamily: "openai", ModelID: "entry"})
	snapshot.AccessTargetsBySourceModelID[1][0].IsEnabled = false
	result, err := service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.chat_completions", requestPlanFixedNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Exclusions) != 1 || result.Exclusions[0].Reason != "target_disabled" {
		t.Fatalf("%+v", result)
	}
	if _, err := service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.models", requestPlanFixedNow); err == nil {
		t.Fatal("non-model operation must not be explained")
	}
}

func TestRouteExplanationExpandedGraphBudget(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, ProfileID: 1, APIFamily: "openai", ModelID: "entry"})
	// A legitimate repeated leaf expands beyond the read budget without any
	// runtime side effects. No result may pretend this is an empty graph.
	target := snapshot.AccessTargetsBySourceModelID[1][0]
	for i := 0; i < routeExplanationLimit; i++ {
		target.ID++
		snapshot.AccessTargetsBySourceModelID[1] = append(snapshot.AccessTargetsBySourceModelID[1], target)
	}
	if _, err := service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.chat_completions", requestPlanFixedNow); err == nil || !strings.Contains(err.Error(), "expanded targets") {
		t.Fatalf("expected bounded observation failure, got %v", err)
	}
}

func TestRouteExplanationKeepsRepeatedLeafPathsAndSampledCapacity(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, ProfileID: 1, APIFamily: "openai", ModelID: "entry"}, runtimeModelRecord{ID: 2, ProfileID: 1, APIFamily: "openai", ModelID: "child"}, runtimeModelRecord{ID: 3, ProfileID: 1, APIFamily: "openai", ModelID: "leaf"})
	addRequestPlanModelTargetWithMetadata(snapshot, "entry", "child", 1)
	addRequestPlanModelTargetWithMetadata(snapshot, "entry", "leaf", 2)
	addRequestPlanModelTargetWithMetadata(snapshot, "child", "leaf", 1)
	connection := snapshot.TerminalTargetsByID[1003]
	connection.QPSLimit = intPtr(1)
	snapshot.TerminalTargetsByID[1003] = connection
	now := requestPlanFixedNow
	service.runtimeState.SeedConnectionState(1, 3, 1003, loadbalance.RuntimeConnectionState{ConnectionID: 1003, BanMode: "off", WindowStartedAt: &now, WindowRequestCount: 1}, now, now)
	result, err := service.explainRouteSnapshot(context.Background(), snapshot, 1, "openai.chat_completions", now)
	if err != nil {
		t.Fatal(err)
	}
	paths := [][]string{}
	for _, row := range result.Candidates {
		if row.TerminalTargetID == 1003 {
			paths = append(paths, row.Path)
			if row.Capacity != "sampled_limit_reached" || row.PlannerPosition == nil {
				t.Fatalf("capacity must not masquerade as planner rejection: %+v", row)
			}
		}
	}
	if !reflect.DeepEqual(paths, [][]string{{"entry", "child", "leaf"}, {"entry", "leaf"}}) {
		t.Fatalf("repeated leaf lost path/order: %v", paths)
	}
}
