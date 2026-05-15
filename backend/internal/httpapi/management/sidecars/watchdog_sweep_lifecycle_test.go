package sidecars

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func TestWatchdogSweepIntervalAnchorsToCompletion(t *testing.T) {
	now := time.Date(2026, time.May, 12, 10, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	setWatchdogSweepInterval(t, service, sidecar.ID, 60)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-sweep-a", "idx-sweep-a", "codex", 10)

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("first sweep reconcile: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-sweep-a"}) {
		t.Fatalf("first sweep probe calls = %v", calls)
	}

	now = now.Add(59 * time.Second)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-sweep-b", "idx-sweep-b", "codex", 10)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("second sweep reconcile before interval: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-sweep-a"}) {
		t.Fatalf("sweep interval was not anchored to completion, calls=%v", calls)
	}

	now = now.Add(time.Second)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-sweep-b", "idx-sweep-b", "codex", 10)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("third sweep reconcile after interval: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-sweep-a", "idx-sweep-b"}) {
		t.Fatalf("next sweep did not start after completion plus interval, calls=%v", calls)
	}
}

func TestWatchdogSkipsOverlappingSweepTicks(t *testing.T) {
	now := time.Date(2026, time.May, 12, 11, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	revision := ensureWatchdogSweepRevision(t, service, sidecar.ID, 60)
	leaseExpiresAt := now.Add(time.Minute)
	snapshot := marshalWatchdogSweepItems(t, []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-overlap", AuthIndex: "idx-overlap", Provider: "codex"}})
	_, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-overlap", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: snapshot, LastHeartbeatAt: &now, LeaseExpiresAt: &leaseExpiresAt, StartedAt: now})
	if err != nil {
		t.Fatalf("seed running sweep: %v", err)
	}

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("reconcile while sweep lease is active: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); len(calls) != 0 {
		t.Fatalf("active sweep lease should suppress overlapping probe work, calls=%v", calls)
	}
	active, found, err := service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || !found || active.SweepID != "sweep-overlap" || active.Status != string(SidecarWatchdogSweepStatusRunning) {
		t.Fatalf("overlap reconcile changed active sweep: active=%+v found=%v err=%v", active, found, err)
	}
}

func TestWatchdogResumesPausedSweepBeforeStartingNewOne(t *testing.T) {
	now := time.Date(2026, time.May, 12, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	revision := ensureWatchdogSweepRevision(t, service, sidecar.ID, 60)
	completedAt := now.Add(-2 * time.Minute)
	_, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-completed", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusCompleted), SnapshotJSON: json.RawMessage(`[]`), StartedAt: completedAt.Add(-time.Second), CompletedAt: &completedAt})
	if err != nil {
		t.Fatalf("seed completed sweep: %v", err)
	}
	items := []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-paused", AuthIndex: "idx-paused", Provider: "codex"}}
	snapshot := marshalWatchdogSweepItems(t, items)
	sweep, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-paused", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: snapshot, StartedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("seed paused sweep: %v", err)
	}
	materializeWatchdogSweepTestItems(t, service, sweep, items)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-new", "idx-new", "codex", 10)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("resume paused sweep reconcile: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-paused"}) {
		t.Fatalf("paused sweep did not resume before new sweep, calls=%v", calls)
	}
	active, found, err := service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || found {
		t.Fatalf("paused sweep should complete without starting a successor: active=%+v found=%v err=%v", active, found, err)
	}
}

func TestWatchdogRecoversExpiredSweepLeaseBeforeResume(t *testing.T) {
	now := time.Date(2026, time.May, 12, 12, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	revision := ensureWatchdogSweepRevision(t, service, sidecar.ID, 60)
	expiredLease := now.Add(-time.Second)
	items := []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-recovered", AuthIndex: "idx-recovered", Provider: "codex"}}
	snapshot := marshalWatchdogSweepItems(t, items)
	sweep, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-expired-lease", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: snapshot, LastHeartbeatAt: &expiredLease, LeaseExpiresAt: &expiredLease, StartedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("seed expired running sweep: %v", err)
	}
	materializeWatchdogSweepTestItems(t, service, sweep, items)

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("recover expired sweep reconcile: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-recovered"}) {
		t.Fatalf("expired sweep lease should recover and resume pinned work, calls=%v", calls)
	}
	active, found, err := service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || found {
		t.Fatalf("recovered sweep should complete without leaving an active lease: active=%+v found=%v err=%v", active, found, err)
	}
}

func TestWatchdogRecoversExpiredChildLeaseAfterProbeCrash(t *testing.T) {
	now := time.Date(2026, time.May, 12, 12, 45, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setProbeResponse("idx-crashed-child", watchdogProbeTestResponse{StatusCode: 200, Body: watchdogHealthyUsageBody()})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	revision := ensureWatchdogSweepRevision(t, service, sidecar.ID, 60)
	expiredParentLease := now.Add(-2 * time.Minute)
	items := []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-crashed-child", AuthIndex: "idx-crashed-child", Provider: "codex"}}
	sweep, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-crashed-child", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: marshalWatchdogSweepItems(t, items), LastHeartbeatAt: &expiredParentLease, LeaseExpiresAt: &expiredParentLease, StartedAt: now.Add(-3 * time.Minute)})
	if err != nil {
		t.Fatalf("seed crashed-child sweep: %v", err)
	}
	materializeWatchdogSweepTestItems(t, service, sweep, items)
	itemStore := service.store.(watchdogSweepItemPersistence)
	crashClaimedAt := now.Add(-2 * time.Minute)
	crashLeaseExpiresAt := now.Add(-time.Minute)
	crashedClaim, err := itemStore.ClaimWatchdogSweepItems(t.Context(), SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sidecar.ID, Limit: 1, LeaseOwner: "crashed-worker", LeaseExpiresAt: crashLeaseExpiresAt, ClaimedAt: crashClaimedAt})
	if err != nil || len(crashedClaim) != 1 || crashedClaim[0].AttemptToken != 1 {
		t.Fatalf("seed crashed child lease: claimed=%+v err=%v", crashedClaim, err)
	}

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("recover crashed child reconcile: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-crashed-child"}) {
		t.Fatalf("expired child lease should be reprobed exactly once, calls=%v", calls)
	}
	childItems, err := itemStore.ListWatchdogSweepItems(t.Context(), sweep.SweepID)
	if err != nil || len(childItems) != 1 || childItems[0].Status != string(SidecarWatchdogSweepItemStatusSucceeded) || childItems[0].AttemptToken != 2 || childItems[0].ResultObservationID == nil || childItems[0].LeaseOwner != nil {
		t.Fatalf("recovered child lease did not commit under a fresh token: items=%+v err=%v", childItems, err)
	}
	active, found, err := service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || found {
		t.Fatalf("recovered crashed-child sweep should complete: active=%+v found=%v err=%v", active, found, err)
	}
}

func TestWatchdogSweepSnapshotReplayFreezesEligibilityAcrossBatches(t *testing.T) {
	now := time.Date(2026, time.May, 12, 13, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-freeze-a", "idx-freeze-a", "codex", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-freeze-b", "idx-freeze-b", "codex", 10)

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("start frozen snapshot sweep: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-freeze-a"}) {
		t.Fatalf("first batch should probe only the first frozen auth, calls=%v", calls)
	}
	active, found, err := service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || !found || active.NextItemIndex != 1 || active.Status != string(SidecarWatchdogSweepStatusPaused) {
		t.Fatalf("first batch should pause a frozen sweep at index 1: active=%+v found=%v err=%v", active, found, err)
	}
	items, err := decodeWatchdogSweepSnapshot(active.SnapshotJSON)
	if err != nil {
		t.Fatalf("decode frozen sweep snapshot: %v", err)
	}
	if len(items) != 2 || items[1].AuthID != "auth-freeze-b" || items[1].AuthIndex != "idx-freeze-b" || items[1].Provider != "codex" {
		t.Fatalf("unexpected frozen snapshot ordering: %+v", items)
	}
	tamperedSnapshot := marshalWatchdogSweepItems(t, []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-freeze-b", AuthIndex: "idx-parent-mutated", Provider: "codex"}})
	_, err = service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: active.SweepID, SidecarID: active.SidecarID, PolicyRevisionID: active.PolicyRevisionID, Status: active.Status, SnapshotJSON: tamperedSnapshot, NextItemIndex: active.NextItemIndex, BatchIndex: active.BatchIndex, NextBatchAfter: cloneTimePtr(active.NextBatchAfter), PauseReason: cloneStringPtr(active.PauseReason), StartedAt: active.StartedAt})
	if err != nil {
		t.Fatalf("tamper parent snapshot metadata: %v", err)
	}

	now = now.Add(time.Second)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-freeze-b", "idx-mutated", "gemini", 10)
	now = now.Add(time.Duration(DefaultProbeBatchCooldownSeconds) * time.Second)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("resume frozen snapshot sweep: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-freeze-a", "idx-freeze-b"}) {
		t.Fatalf("resumed sweep must use frozen auth index/provider instead of live mutations, calls=%v", calls)
	}
	active, found, err = service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || found {
		t.Fatalf("frozen sweep should complete after the second batch: active=%+v found=%v err=%v", active, found, err)
	}
}

func TestWatchdogSweepReplayRejectsSnapshotOnlyParentWork(t *testing.T) {
	now := time.Date(2026, time.May, 12, 13, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	revision := ensureWatchdogSweepRevision(t, service, sidecar.ID, 60)
	snapshot := marshalWatchdogSweepItems(t, []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-snapshot-only", AuthIndex: "idx-snapshot-only", Provider: "codex"}})
	_, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-snapshot-only", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: snapshot, StartedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("seed snapshot-only sweep: %v", err)
	}

	_, err = service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err == nil {
		t.Fatal("expected snapshot-only sweep replay to fail")
	}
	if calls := upstream.apiCallAuthIndexes(); len(calls) != 0 {
		t.Fatalf("snapshot-only sweep must not execute probes, calls=%v", calls)
	}
}

func TestWatchdogApplyAndRestartCreatesFreshSweepPinnedToNewRevision(t *testing.T) {
	now := time.Date(2026, time.May, 15, 11, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	state, err := service.getWatchdogPolicyRevisionState(t.Context(), sidecar.ID)
	if err != nil || state.ActiveRevision == nil {
		t.Fatalf("load active revision: state=%+v err=%v", state, err)
	}
	activeID := state.ActiveRevision.ID
	oldItems := []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-old", AuthIndex: "idx-old", Provider: "codex"}}
	oldSweep, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-old-revision", SidecarID: sidecar.ID, PolicyRevisionID: activeID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: marshalWatchdogSweepItems(t, oldItems), StartedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("seed old active sweep: %v", err)
	}
	materializeWatchdogSweepTestItems(t, service, oldSweep, oldItems)
	input := watchdogPolicyRevisionInputFromRevision(*state.ActiveRevision)
	input.ProbeTimeoutSeconds = 6
	pendingState, err := service.savePendingWatchdogPolicyRevision(t.Context(), input, &activeID)
	if err != nil || pendingState.PendingRevision == nil {
		t.Fatalf("save pending revision: state=%+v err=%v", pendingState, err)
	}
	targetID := pendingState.PendingRevision.ID
	if _, err := service.applyAndRestartWatchdogPolicyRevision(t.Context(), sidecar.ID, targetID, activeID); err != nil {
		t.Fatalf("apply and restart revision: %v", err)
	}
	now = now.Add(time.Second)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-fresh", "idx-fresh", "codex", DefaultWorkingPriority)
	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile replacement sweep: %v", err)
	}
	if result.Probed != 1 || !slices.Equal(upstream.apiCallAuthIndexes(), []string{"idx-fresh"}) {
		t.Fatalf("replacement sweep did not execute fresh revision work: result=%+v calls=%v", result, upstream.apiCallAuthIndexes())
	}
	store := service.store.(*memorySidecarStore)
	store.mu.RLock()
	defer store.mu.RUnlock()
	var replacement *SidecarWatchdogSweep
	for _, sweep := range store.sweeps[sidecar.ID] {
		if sweep.PolicyRevisionID == targetID {
			copy := sweep
			replacement = &copy
			break
		}
	}
	if replacement == nil || replacement.SweepID == oldSweep.SweepID || replacement.Status != string(SidecarWatchdogSweepStatusCompleted) {
		t.Fatalf("replacement sweep was not completed on new revision: replacement=%+v all=%+v", replacement, store.sweeps[sidecar.ID])
	}
	children := store.sweepItems[replacement.SweepID]
	if len(children) != 1 || children[0].PolicyRevisionID != targetID || children[0].Status != string(SidecarWatchdogSweepItemStatusSucceeded) {
		t.Fatalf("replacement child work was not pinned to new revision: %+v", children)
	}
}

func setWatchdogSweepInterval(t *testing.T, service *Service, sidecarID int, seconds int) {
	t.Helper()
	policy, err := service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecarID)
	if err != nil {
		t.Fatalf("load watchdog policy: %v", err)
	}
	policy.RollingRefreshAfterSeconds = seconds
	service.store.(*memorySidecarStore).policies[sidecarID] = policy
}

func ensureWatchdogSweepRevision(t *testing.T, service *Service, sidecarID int, seconds int) SidecarWatchdogPolicyRevision {
	t.Helper()
	setWatchdogSweepInterval(t, service, sidecarID, seconds)
	policy, err := service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecarID)
	if err != nil {
		t.Fatalf("load watchdog policy for revision: %v", err)
	}
	revision, err := service.store.(watchdogPolicyRevisionLifecyclePersistence).EnsureActiveWatchdogPolicyRevision(t.Context(), policy)
	if err != nil {
		t.Fatalf("ensure watchdog policy revision: %v", err)
	}
	return revision
}

func materializeWatchdogSweepTestItems(t *testing.T, service *Service, sweep SidecarWatchdogSweep, items []watchdogSweepSnapshotItem) {
	t.Helper()
	if err := service.materializeWatchdogSweepItems(t.Context(), sweep, items); err != nil {
		t.Fatalf("materialize watchdog sweep items: %v", err)
	}
}

func marshalWatchdogSweepItems(t *testing.T, items []watchdogSweepSnapshotItem) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal watchdog sweep items: %v", err)
	}
	return raw
}
