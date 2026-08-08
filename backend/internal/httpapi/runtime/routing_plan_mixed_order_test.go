package runtime

import (
	"net/http"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

// Mixed-order contract tests: Model Target rows and Terminal Target rows are
// type-neutral peers of the source model's authored (position, id) order. The
// three strategies run once over the mixed enabled rows; a Model Target peer
// recursively resolves through the child model's own strategy and contributes
// one contiguous block.

func mixedOrderRequestPlanStrategy(legacyStrategyType string) loadbalance.RuntimeStrategy {
	return loadbalance.RuntimeStrategy{ID: requestPlanTestStrategyID, Name: "test " + legacyStrategyType, LegacyStrategyType: &legacyStrategyType}
}

func buildMixedOrderRequestPlanForTest(t *testing.T, service *Service, snapshot *planningSnapshot) requestPlan {
	t.Helper()
	return mustBuildRequestPlanForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
}

func assertRequestPlanAttemptConnectionIDs(t *testing.T, plan requestPlan, wantConnections []int) {
	t.Helper()
	if len(plan.TerminalAttempts) != len(wantConnections) {
		t.Fatalf("expected %d terminal attempts, got %+v", len(wantConnections), plan.TerminalAttempts)
	}
	for index, wantConnectionID := range wantConnections {
		if got := plan.TerminalAttempts[index].Connection.ID; got != wantConnectionID {
			t.Fatalf("expected attempt %d connection %d, got %d", index, wantConnectionID, got)
		}
	}
}

// router openai model ID 1 with three peers: terminal 2001 at position 0,
// model target (child 2) at position 1, terminal 2002 at position 2.
func newMixedOrderRouterSnapshot(childConnections int, childStrategy loadbalance.RuntimeStrategy) *planningSnapshot {
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, 0)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "child-openai", 1)
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_002, 9_002, 2)
	for index := 0; index < childConnections; index++ {
		addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["child-openai"], 2_101+index, 9_101+index, index)
	}
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("fill-first")
	snapshot.StrategiesByModelID[2] = childStrategy
	return snapshot
}

func TestMixedOrderFillFirstFollowsAuthoredRowOrderWithContiguousChildBlock(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))

	plan := buildMixedOrderRequestPlanForTest(t, service, snapshot)
	// T1(2001) -> child block (2101) -> T2(2002); the child block stays between
	// the two terminal peers and is not promoted above T1.
	assertRequestPlanAttemptConnectionIDs(t, plan, []int{2_001, 2_101, 2_002})
}

func TestMixedOrderChildBlockStaysContiguousBetweenTerminalPeers(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newMixedOrderRouterSnapshot(2, mixedOrderRequestPlanStrategy("fill-first"))

	plan := buildMixedOrderRequestPlanForTest(t, service, snapshot)
	// Child resolves two attempts; both must remain one contiguous block
	// occupying the model peer slot: T1, [childA, childB], T2.
	assertRequestPlanAttemptConnectionIDs(t, plan, []int{2_001, 2_101, 2_102, 2_002})
}

func TestMixedOrderSingleResolvesOnlyFirstEnabledPeer(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()

	snapshot := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("single")
	// First enabled mixed peer is terminal 2001: single must not reach the
	// model peer or terminal 2002.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_001})

	modelFirst := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	modelFirst.AccessTargetsBySourceModelID[1] = nil
	modelFirst.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(modelFirst, "router-openai", "child-openai", 0)
	addRequestPlanConnectionTarget(modelFirst, modelFirst.ModelsByID["router-openai"], 2_001, 9_001, 1)
	addRequestPlanConnectionTarget(modelFirst, modelFirst.ModelsByID["child-openai"], 2_101, 9_101, 0)
	modelFirst.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("single")
	modelFirst.StrategiesByModelID[2] = mixedOrderRequestPlanStrategy("fill-first")
	// First enabled mixed peer is the model target: single resolves the child
	// block and must not fall back to terminal 2001.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, modelFirst), []int{2_101})
}

func TestMixedOrderSingleFailsWhenFirstPeerHasNoEligibleLeaf(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "empty-child-openai", 0)
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, 1)
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("single")

	err := buildRequestPlanErrorForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	// single keeps its single-peer semantics: the zero-leaf model peer fails the
	// whole model instead of advancing to the terminal peer.
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "No eligible targets available")
}

func TestMixedOrderZeroLeafChildSkipsToNextMixedPeer(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "empty-child-openai", 0)
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, 1)
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("fill-first")

	// fill-first: the zero-leaf model peer contributes nothing and the terminal
	// peer at position 1 is reached as the next authored row.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_001})
}

func TestMixedOrderRoundRobinRotatesMixedRowsOnce(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("round-robin")

	// Claim 1: offset 0 -> [T1, M1, T2].
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_001, 2_101, 2_002})
	// Claim 2: offset 1 -> [M1, T2, T1]; every direct mixed row occupies one
	// rotation slot and the child block follows its row.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_101, 2_002, 2_001})
	// Claim 3: offset 2 -> [T2, T1, M1].
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_002, 2_001, 2_101})
}

func TestMixedOrderRoundRobinUnavailablePeerConsumesRotationSlot(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("round-robin")
	bannedUntilAt := service.nowUTC().Add(10 * time.Minute)
	seededAt := service.nowUTC().Add(-time.Minute)
	service.runtimeState.SeedConnectionState(requestPlanTestProfileID, 1, 2_001, loadbalance.RuntimeConnectionState{ConnectionID: 2_001, BanMode: "temporary", BannedUntilAt: &bannedUntilAt}, seededAt, seededAt)

	// T1 is banned but still occupies a parent rotation slot: it is skipped by
	// eligibility, not removed from the cursor modulus.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_101, 2_002})
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_101, 2_002})
	// Claim 3 rotates past the banned row: [T2, T1, M1] -> T2, then child.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_002, 2_101})
}

func TestMixedOrderRoundRobinChildCursorIsIndependent(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newMixedOrderRouterSnapshot(2, mixedOrderRequestPlanStrategy("round-robin"))
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("round-robin")

	// Request 1: parent offset 0 -> [T1, M1, T2]; child offset 0 -> [A, B].
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_001, 2_101, 2_102, 2_002})
	// Request 2: parent offset 1 -> [M1, T2, T1]; child offset 1 -> [B, A].
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_102, 2_101, 2_002, 2_001})
}

func TestMixedOrderRoundRobinNewSetHashStartsFreshCursor(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("round-robin")

	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_001, 2_101, 2_002})
	// Reorder: terminal 2001 moves from position 0 to position 2. The mixed set
	// hash changes, so the new set claims a fresh cursor (offset 0).
	reordered := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))
	reordered.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("round-robin")
	reordered.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanConnectionTarget(reordered, reordered.ModelsByID["router-openai"], 2_001, 9_001, 2)
	addRequestPlanModelTargetWithMetadata(reordered, "router-openai", "child-openai", 0)
	addRequestPlanConnectionTarget(reordered, reordered.ModelsByID["router-openai"], 2_002, 9_002, 1)

	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, reordered), []int{2_101, 2_002, 2_001})
}

func TestMixedOrderRoundRobinRestoredSetHashContinuesCursor(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("round-robin")

	// Set A claim 1 -> offset 0, claim 2 -> offset 1.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_001, 2_101, 2_002})
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_101, 2_002, 2_001})

	// Set B (reordered) claims its own fresh cursor.
	reordered := newMixedOrderRouterSnapshot(1, mixedOrderRequestPlanStrategy("fill-first"))
	reordered.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("round-robin")
	reordered.AccessTargetsBySourceModelID[1] = nil
	addRequestPlanModelTargetWithMetadata(reordered, "router-openai", "child-openai", 0)
	addRequestPlanConnectionTarget(reordered, reordered.ModelsByID["router-openai"], 2_001, 9_001, 1)
	addRequestPlanConnectionTarget(reordered, reordered.ModelsByID["router-openai"], 2_002, 9_002, 2)
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, reordered), []int{2_101, 2_001, 2_002})

	// Set A reappears with an identical hash: its existing cursor continues
	// (claim 3 -> offset 2) instead of restarting at offset 0.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_002, 2_001, 2_101})
}

func TestMixedOrderDisabledRowsAreExcludedButDoNotShiftAuthoredPeers(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	snapshot.AccessTargetsBySourceModelID[2] = nil
	disabledTerminal := runtimeAccessTargetRecord{
		ID:                        9_001,
		ProfileID:                 requestPlanTestProfileID,
		SourceModelConfigID:       1,
		TargetType:                runtimeAccessTargetTypeConnection,
		TargetConnectionID:        intPtr(2_001),
		TargetConnectionProfileID: requestPlanTestProfileID,
		TargetConnectionAPIFamily: "openai",
		Position:                  0,
		IsEnabled:                 false,
	}
	snapshot.AccessTargetsBySourceModelID[1] = append(snapshot.AccessTargetsBySourceModelID[1], disabledTerminal)
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "child-openai", 1)
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_002, 9_002, 2)
	addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["child-openai"], 2_101, 9_101, 0)
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("fill-first")

	// Disabled rows leave the strategy input but their authored position is
	// untouched: the effective peers are [M1, T2].
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_101, 2_002})
}

func TestMixedOrderCompatibilityMissRecordsFirstErrorInEffectiveOrder(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	snapshot.AccessTargetsBySourceModelID[2] = nil
	// Terminal 2001 is responses_only and incompatible with the chat/completions
	// ingress; child 2101 is dual_native and compatible.
	addRequestPlanConnectionTargetWithOptions(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)})
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "child-openai", 1)
	addRequestPlanConnectionTargetWithOptions(snapshot, snapshot.ModelsByID["child-openai"], 2_101, 9_101, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityDualNative)})
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("fill-first")

	// A later compatible peer wins; the earlier compatibility miss is bypassed.
	assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), []int{2_101})

	// All peers incompatible: return the first compatibility error in effective
	// mixed order (the terminal peer at position 0).
	allIncompatible := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
	)
	allIncompatible.AccessTargetsBySourceModelID[1] = nil
	allIncompatible.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanConnectionTargetWithOptions(allIncompatible, allIncompatible.ModelsByID["router-openai"], 2_001, 9_001, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)})
	addRequestPlanModelTargetWithMetadata(allIncompatible, "router-openai", "child-openai", 1)
	addRequestPlanConnectionTargetWithOptions(allIncompatible, allIncompatible.ModelsByID["child-openai"], 2_101, 9_101, 0, requestPlanConnectionTargetOptions{openAITextCapability: stringPtr(providerauth.OpenAITextCapabilityResponsesOnly)})
	allIncompatible.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("fill-first")

	err := buildRequestPlanErrorForTest(t, service, allIncompatible, "/v1/chat/completions", []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	assertPlanDomainError(t, err, http.StatusBadRequest, "cannot translate")
}

func TestMixedOrderAllPeersIneligibleReturnsNoEligible(t *testing.T) {
	service := newEnforcedRequestPlanUnitService()
	snapshot := newRequestPlanSnapshot(
		runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
		runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "empty-child-openai"},
	)
	snapshot.AccessTargetsBySourceModelID[1] = nil
	snapshot.AccessTargetsBySourceModelID[2] = nil
	addRequestPlanModelTargetWithMetadata(snapshot, "router-openai", "empty-child-openai", 0)
	snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("fill-first")

	err := buildRequestPlanErrorForTest(t, service, snapshot, "/v1/chat/completions", []byte(`{"model":"router-openai","messages":[]}`), RuntimeProxyConfigSnapshot{})
	assertPlanDomainError(t, err, http.StatusServiceUnavailable, "No eligible targets available")
}

func TestMixedOrderAuthoredOrderMatrixCoversAllFourInterleavings(t *testing.T) {
	// SPEC §14.2: T1,T2,M1,M2 / M1,T1,M2,T2 / T1,M1,T2,M2 / M1,M2,T1,T2 must
	// all resolve by authored mixed position under fill-first.
	matrix := []struct {
		name      string
		positions []int // 0 = terminal 2001, 1 = terminal 2002, 2 = model target, 3 = child connection
		want      []int
	}{
		{name: "terminals-first", positions: []int{0, 1, 2, 3}, want: []int{2_001, 2_002, 2_101}},
		{name: "model-terminal-alternating", positions: []int{1, 3, 0, 2}, want: []int{2_101, 2_001, 2_002}},
		{name: "terminal-model-terminal", positions: []int{0, 2, 1, 3}, want: []int{2_001, 2_101, 2_002}},
		{name: "models-first", positions: []int{2, 3, 0, 1}, want: []int{2_101, 2_001, 2_002}},
	}
	for _, test := range matrix {
		t.Run(test.name, func(t *testing.T) {
			service := newEnforcedRequestPlanUnitService()
			snapshot := newRequestPlanSnapshot(
				runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "router-openai"},
				runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "child-openai"},
			)
			snapshot.AccessTargetsBySourceModelID[1] = nil
			snapshot.AccessTargetsBySourceModelID[2] = nil
			addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_001, 9_001, test.positions[0])
			addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["router-openai"], 2_002, 9_002, test.positions[1])
			addRequestPlanModelTargetAtPosition(snapshot, "router-openai", "child-openai", test.positions[2])
			addRequestPlanConnectionTarget(snapshot, snapshot.ModelsByID["child-openai"], 2_101, 9_101, test.positions[3])
			snapshot.StrategiesByModelID[1] = mixedOrderRequestPlanStrategy("fill-first")

			assertRequestPlanAttemptConnectionIDs(t, buildMixedOrderRequestPlanForTest(t, service, snapshot), test.want)
		})
	}
}
