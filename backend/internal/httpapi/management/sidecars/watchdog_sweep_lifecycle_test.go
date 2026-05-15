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
	snapshot := marshalWatchdogSweepItems(t, []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-paused", AuthIndex: "idx-paused", Provider: "codex"}})
	_, err = service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-paused", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: snapshot, StartedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("seed paused sweep: %v", err)
	}
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
	snapshot := marshalWatchdogSweepItems(t, []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-recovered", AuthIndex: "idx-recovered", Provider: "codex"}})
	_, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-expired-lease", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: snapshot, LastHeartbeatAt: &expiredLease, LeaseExpiresAt: &expiredLease, StartedAt: now.Add(-time.Minute)})
	if err != nil {
		t.Fatalf("seed expired running sweep: %v", err)
	}

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

func TestWatchdogSweepSnapshotFreezesEligibilityAcrossBatches(t *testing.T) {
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

func marshalWatchdogSweepItems(t *testing.T, items []watchdogSweepSnapshotItem) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal watchdog sweep items: %v", err)
	}
	return raw
}
