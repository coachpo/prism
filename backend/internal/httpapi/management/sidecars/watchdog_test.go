package sidecars

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestMemoryWatchdogHoldAllowsReleasedHistoryWithActiveHold(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 60)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, Reason: watchdogReasonQuotaExceeded, ConditionHash: "active-hash", TargetPriority: DefaultQuotaExceededPriority, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create active hold: %v", err)
	}
	_, err = service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, Reason: "released_history", ConditionHash: "released-hash", TargetPriority: DefaultQuotaExceededPriority, Status: WatchdogHoldStatusReleased, ReleasedAt: &now})
	if err != nil {
		t.Fatalf("memory store should allow released hold history beside active hold: %v", err)
	}
}

func TestWatchdogSnapshotQuotaFieldsInertForHoldDecisions(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	recoverAt := now.Add(2 * time.Hour)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true, QuotaNextRecoverAt: &recoverAt})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true, QuotaNextRecoverAt: &recoverAt})

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile watchdog: %v", err)
	}
	if result.Reconciled || result.QuotaHeld != 0 || result.UnsupportedSkipped != 1 || result.ActionCount != 1 {
		t.Fatalf("snapshot quota fields must be inert except unsupported discovery history, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("snapshot quota fields must not patch upstream, got %v", got)
	}
	if upstream.statusPatchCount() != 0 {
		t.Fatalf("watchdog must not disable auth files via status patch")
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 0 {
		t.Fatalf("snapshot quota fields must not create holds, holds=%+v err=%v", holds, err)
	}
	if actions := listWatchdogActions(t, service, sidecar.ID); len(actions) != 1 || actions[0].ActionType != watchdogProbeStatusSkippedUnsupportedProvider || actions[0].Status != watchdogActionStatusSkipped {
		t.Fatalf("snapshot quota fields may only record unsupported discovery skip, got %+v", actions)
	}
}

func TestRepairLegacyDeprioritizeWatchdogAvoidsDuplicatePatch(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 10, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 20, FailureCount: DefaultFailureThreshold})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 20, FailureCount: DefaultFailureThreshold})
	service.store = &failingWatchdogStore{persistence: service.store, failCreateHold: 1}

	_, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if !errors.Is(err, errInjectedWatchdogStoreFailure) {
		t.Fatalf("expected injected hold create failure, got %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultQuotaExceededPriority}) {
		t.Fatalf("first legacy deprioritize should patch once before final DB failure, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("failed final hold write should not leave active hold, holds=%+v err=%v", holds, err)
	}

	now = now.Add(time.Minute)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 20, FailureCount: DefaultFailureThreshold})
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("legacy deprioritize repair reconcile: %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultQuotaExceededPriority}) {
		t.Fatalf("legacy deprioritize repair must not issue duplicate patch, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 1 {
		t.Fatalf("legacy deprioritize repair should create active hold, holds=%+v err=%v", holds, err)
	}
}

func TestRepairLegacyRestoreWatchdogAvoidsDuplicatePatch(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 20, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: DefaultQuotaExceededPriority})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	previousPriority := DefaultWorkingPriority
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("gemini"), Reason: watchdogReasonFailureThreshold, ConditionHash: "hash-legacy-restore-repair", PreviousPriority: &previousPriority, TargetPriority: DefaultQuotaExceededPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create legacy restore hold: %v", err)
	}
	service.store = &failingWatchdogStore{persistence: service.store, failUpdateHold: 1}

	_, err = service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if !errors.Is(err, errInjectedWatchdogStoreFailure) {
		t.Fatalf("expected injected hold release failure, got %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority}) {
		t.Fatalf("first legacy restore should patch once before final DB failure, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 1 {
		t.Fatalf("failed final restore write should leave active hold for repair, holds=%+v err=%v", holds, err)
	}

	now = now.Add(time.Minute)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("legacy restore repair reconcile: %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority}) {
		t.Fatalf("legacy restore repair must not issue duplicate patch, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("legacy restore repair should release hold, holds=%+v err=%v", holds, err)
	}
	if got := countWatchdogActions(listWatchdogActions(t, service, sidecar.ID), watchdogActionRestoreSkippedManualChange); got != 0 {
		t.Fatalf("legacy restore repair must not be treated as manual override, got %d manual-change actions", got)
	}
}

func TestWatchdogFailureThresholdSkipsWhenRecentRequestsMissing(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	snapshot := SidecarAuthSnapshot{FailedCount: intPtr(9)}
	condition := evaluateWatchdogCondition(snapshot, SidecarWatchdogPolicy{FailureThreshold: 1, FailureWindowSeconds: 60, FallbackCooldownSeconds: 60}, now)
	if condition.Triggered {
		t.Fatalf("expected missing recent_requests to be unobservable, got %+v", condition)
	}
}

func TestWatchdogRestoreProbeRestoresOriginalPriorityAfterHealthyProbe(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: DefaultQuotaExceededPriority, Provider: "codex"})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	previousPriority := DefaultWorkingPriority
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("codex"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-quota", PreviousPriority: &previousPriority, TargetPriority: DefaultQuotaExceededPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create active hold: %v", err)
	}

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("restore reconcile: %v", err)
	}
	if !result.Reconciled {
		t.Fatalf("expected restore to reconcile, got %+v", result)
	}
	if upstream.getAuthFilesCount() == 0 {
		t.Fatalf("restore must perform a fresh /auth-files preflight read")
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority}) {
		t.Fatalf("expected restore fields patch, got %v", got)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 0 {
		t.Fatalf("expected hold to be released, holds=%+v err=%v", holds, err)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	assertWatchdogAction(t, actions, watchdogActionRestore, watchdogActionStatusSucceeded)
}

func TestWatchdogRestoreRejectsLegacyAuthFilesEnvelope(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: DefaultQuotaExceededPriority, Provider: "codex"})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	previousPriority := DefaultWorkingPriority
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("codex"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-legacy-envelope", PreviousPriority: &previousPriority, TargetPriority: DefaultQuotaExceededPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create active hold: %v", err)
	}

	upstream.setAuthFilesPayload(syncAuthFixtureWithEnvelopeKey(t, "auth_files"))
	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err == nil || !strings.Contains(err.Error(), "files must be present") {
		t.Fatalf("expected restore preflight to reject legacy auth_files envelope, result=%+v err=%v", result, err)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("legacy envelope must not restore priority or fallback to stale data, got patches %v", got)
	}
}

func TestWatchdogSkipsStaleSnapshots(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogPolicy(t, service, sidecar.ID)
	seedWatchdogSnapshot(t, service, sidecar.ID, now.Add(-10*time.Minute), watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true})
	staleAt := now.Add(-5 * time.Minute)
	_, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAt, ManagementAuthState: ManagementAuthStateValid})
	if err != nil {
		t.Fatalf("mark snapshots stale: %v", err)
	}

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile stale watchdog: %v", err)
	}
	if !result.Skipped || result.SkipReason != watchdogActionSkippedStaleSnapshot {
		t.Fatalf("expected stale snapshot skip, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("stale watchdog must not patch upstream, got %v", got)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	assertWatchdogAction(t, actions, watchdogActionSkippedStaleSnapshot, watchdogActionStatusSkipped)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("repeat stale reconcile: %v", err)
	}
	actions = listWatchdogActions(t, service, sidecar.ID)
	if got := countWatchdogActions(actions, watchdogActionSkippedStaleSnapshot); got != 1 {
		t.Fatalf("stale skip action should be deduped, got %d actions: %+v", got, actions)
	}
}

func TestWatchdogQuotaStateStaleSnapshotsPreserveSafeView(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			writeSyncJSON(w, `{"files":[{"id":"auth-stale-safe","auth_index":"auth-stale-safe","name":"stale-safe.json","provider":"codex","label":"Stale Safe","status":"active","disabled":false,"priority":10}]}`)
			return
		}
		serveSyncFixturePath(t, w, r)
	}))
	defer server.Close()

	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 60)
	enableWatchdogPolicy(t, service, sidecar.ID)
	if _, err := service.SyncSidecar(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("sync sidecar: %v", err)
	}
	before := listAuthQuotaStates(t, service, sidecar.ID)
	if len(before) != 1 || before[0].QuotaBand != quotaBandError || before[0].ReasonCode == nil || *before[0].ReasonCode != "unknown" {
		t.Fatalf("expected safe quota-state view after sync, got %+v", before)
	}
	staleAt := now.Add(-5 * time.Minute)
	_, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAt, ManagementAuthState: ManagementAuthStateValid})
	if err != nil {
		t.Fatalf("mark snapshots stale: %v", err)
	}
	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile stale watchdog: %v", err)
	}
	if !result.Skipped || result.SkipReason != watchdogActionSkippedStaleSnapshot {
		t.Fatalf("expected stale snapshot skip, got %+v", result)
	}
	after := listAuthQuotaStates(t, service, sidecar.ID)
	if len(after) != len(before) || after[0].QuotaBand != before[0].QuotaBand || after[0].AuthID != before[0].AuthID {
		t.Fatalf("stale reconcile must preserve safe quota-state view, before=%+v after=%+v", before, after)
	}
}

func TestWatchdogSkipsAuthSnapshotFromPreviousSync(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	enableWatchdogPolicy(t, service, sidecar.ID)
	oldObservedAt := now.Add(-10 * time.Minute)
	seedWatchdogSnapshot(t, service, sidecar.ID, oldObservedAt, watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true})
	freshSyncAt := now
	staleAfter := freshSyncAt.Add(time.Hour)
	_, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: freshSyncAt, LastSuccessfulSyncAt: &freshSyncAt, SnapshotStaleAfter: &staleAfter, ManagementAuthState: ManagementAuthStateValid})
	if err != nil {
		t.Fatalf("mark later sync fresh: %v", err)
	}

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile old auth snapshot: %v", err)
	}
	if !result.Skipped || result.SkipReason != "no_watchdog_action_needed" {
		t.Fatalf("expected old per-auth snapshot to be ignored, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("old omitted auth snapshot must not patch upstream, got %v", got)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	if len(actions) != 0 {
		t.Fatalf("old omitted auth snapshot must not record watchdog actions, got %+v", actions)
	}
}

func TestWatchdogManualOverridePausesAutomation(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 5})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	previousPriority := DefaultWorkingPriority
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("gemini"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-quota", PreviousPriority: &previousPriority, TargetPriority: DefaultQuotaExceededPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create active hold: %v", err)
	}

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("manual override reconcile: %v", err)
	}
	if result.Reconciled {
		t.Fatalf("manual override must pause instead of restore, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("manual override pause must not patch upstream, got %v", got)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 1 || holds[0].Status != WatchdogHoldStatusPaused || holds[0].ManualPauseUntil == nil {
		t.Fatalf("expected paused hold with manual pause, holds=%+v err=%v", holds, err)
	}
	wantPauseUntil := now.Add(DefaultManualOverridePauseSeconds * time.Second)
	if !holds[0].ManualPauseUntil.Equal(wantPauseUntil) {
		t.Fatalf("manual pause until = %v, want %v", holds[0].ManualPauseUntil, wantPauseUntil)
	}

	now = now.Add(time.Minute)
	upstream.setAuth(watchdogUpstreamAuth{Priority: DefaultQuotaExceededPriority})
	result, err = service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("paused reconcile: %v", err)
	}
	if result.Reconciled || len(upstream.fieldPatchPriorities()) != 0 {
		t.Fatalf("watchdog acted during manual pause: result=%+v patches=%v", result, upstream.fieldPatchPriorities())
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	assertWatchdogAction(t, actions, watchdogActionRestoreSkippedManualChange, watchdogActionStatusSkipped)
	assertWatchdogAction(t, actions, watchdogActionRestoreSkippedManualPause, watchdogActionStatusSkipped)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("repeat paused reconcile: %v", err)
	}
	actions = listWatchdogActions(t, service, sidecar.ID)
	if got := countWatchdogActions(actions, watchdogActionRestoreSkippedManualPause); got != 1 {
		t.Fatalf("manual pause skip action should be deduped, got %d actions: %+v", got, actions)
	}
}

func TestWatchdogRestoreSkippedNeedsOperatorWhenPreviousPriorityMissing(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: DefaultQuotaExceededPriority})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("gemini"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-missing-previous", TargetPriority: DefaultQuotaExceededPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create active hold: %v", err)
	}

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("missing previous reconcile: %v", err)
	}
	if result.Reconciled || len(upstream.fieldPatchPriorities()) != 0 {
		t.Fatalf("missing previous priority must not restore, result=%+v patches=%v", result, upstream.fieldPatchPriorities())
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	assertWatchdogAction(t, actions, watchdogActionRestoreSkippedNeedsOperator, watchdogActionStatusSkipped)
}

const (
	watchdogAuthID    = "auth-gemini-primary"
	watchdogAuthIndex = "auth_001"
	watchdogAuthName  = "gemini-primary.json"
)

type watchdogUpstreamAuth struct {
	Priority           int
	Provider           string
	Disabled           bool
	Unavailable        bool
	QuotaExceeded      bool
	QuotaNextRecoverAt *time.Time
	FailureCount       int
}

type watchdogUpstream struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	auth   watchdogUpstreamAuth

	authFilesPayload  string
	fieldPatches      []int
	statusPatchCalls  int
	getAuthFilesCalls int
}

func newWatchdogUpstream(t *testing.T, auth watchdogUpstreamAuth) *watchdogUpstream {
	t.Helper()
	auth.Priority = watchdogTestCanonicalPriority(auth.Priority)
	upstream := &watchdogUpstream{t: t, auth: auth}
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.serveHTTP))
	return upstream
}

func (u *watchdogUpstream) Close() { u.server.Close() }

func (u *watchdogUpstream) URL() string { return u.server.URL }

func (u *watchdogUpstream) setAuth(auth watchdogUpstreamAuth) {
	u.mu.Lock()
	defer u.mu.Unlock()
	auth.Priority = watchdogTestCanonicalPriority(auth.Priority)
	u.auth = auth
	u.authFilesPayload = ""
}

func (u *watchdogUpstream) setAuthFilesPayload(payload string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authFilesPayload = payload
}

func (u *watchdogUpstream) fieldPatchPriorities() []int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int(nil), u.fieldPatches...)
}

func (u *watchdogUpstream) statusPatchCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.statusPatchCalls
}

func (u *watchdogUpstream) getAuthFilesCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.getAuthFilesCalls
}

func (u *watchdogUpstream) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Management-Key") != "sync-secret" {
		u.t.Errorf("expected management key header, got %q", r.Header.Get("X-Management-Key"))
	}
	switch r.URL.Path {
	case "/v0/management/api-call":
		writeWatchdogJSON(w, map[string]any{"status_code": http.StatusOK, "body": watchdogHealthyUsageBody()})
	case "/v0/management/auth-files":
		u.mu.Lock()
		u.getAuthFilesCalls++
		auth := u.auth
		authFilesPayload := u.authFilesPayload
		u.mu.Unlock()
		if authFilesPayload != "" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(authFilesPayload))
			return
		}
		writeWatchdogJSON(w, map[string]any{"files": []any{watchdogAuthPayload(auth)}})
	case "/v0/management/auth-files/fields":
		if r.Method != http.MethodPatch {
			u.t.Errorf("expected PATCH /auth-files/fields, got %s", r.Method)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var patch struct {
			Name     string `json:"name"`
			Priority *int   `json:"priority"`
		}
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			u.t.Errorf("decode fields patch: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if patch.Name != watchdogAuthName || patch.Priority == nil {
			u.t.Errorf("unexpected fields patch payload: %+v", patch)
		}
		u.mu.Lock()
		u.fieldPatches = append(u.fieldPatches, *patch.Priority)
		u.auth.Priority = *patch.Priority
		u.mu.Unlock()
		writeWatchdogJSON(w, map[string]any{"status": "ok", "updated": patch.Name, "priority": patch.Priority})
	case "/v0/management/auth-files/status":
		u.mu.Lock()
		u.statusPatchCalls++
		u.mu.Unlock()
		u.t.Errorf("watchdog must not patch /auth-files/status")
		w.WriteHeader(http.StatusTeapot)
	default:
		u.t.Errorf("unexpected management path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func newWatchdogTestService(t *testing.T, now func() time.Time) *Service {
	t.Helper()
	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{Now: now})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	return service
}

func enableWatchdogPolicy(t *testing.T, service *Service, sidecarID int) {
	t.Helper()
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecarID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds})
	if err != nil {
		t.Fatalf("enable watchdog policy: %v", err)
	}
}

func seedWatchdogSnapshot(t *testing.T, service *Service, sidecarID int, observedAt time.Time, auth watchdogUpstreamAuth) {
	t.Helper()
	markWatchdogSnapshotsFresh(t, service, sidecarID, observedAt)
	recentRequests, err := json.Marshal([]map[string]any{{"window_start": observedAt.Add(-time.Minute).Format(time.RFC3339), "window_end": observedAt.Format(time.RFC3339), "failure_count": auth.FailureCount}})
	if err != nil {
		t.Fatalf("marshal recent requests: %v", err)
	}
	snapshotJSON, err := json.Marshal(watchdogAuthPayload(auth))
	if err != nil {
		t.Fatalf("marshal auth snapshot: %v", err)
	}
	priority := watchdogTestCanonicalPriority(auth.Priority)
	_, err = service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecarID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Name: watchdogAuthName, Provider: stringPtr("gemini"), Label: stringPtr("Gemini primary"), Status: stringPtr("active"), Disabled: boolPtr(auth.Disabled), Unavailable: boolPtr(auth.Unavailable), Priority: intPtr(priority), QuotaExceeded: boolPtr(auth.QuotaExceeded), QuotaNextRecoverAt: cloneTimePtr(auth.QuotaNextRecoverAt), FailedCount: intPtr(auth.FailureCount), RecentRequestsJSON: recentRequests, ModelStatesJSON: json.RawMessage(`{}`), SnapshotJSON: snapshotJSON, ObservedAt: observedAt})
	if err != nil {
		t.Fatalf("save watchdog snapshot: %v", err)
	}
}

func markWatchdogSnapshotsFresh(t *testing.T, service *Service, sidecarID int, now time.Time) {
	t.Helper()
	staleAfter := now.Add(2 * time.Hour)
	_, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecarID, LastSyncAt: now, LastSuccessfulSyncAt: &now, SnapshotStaleAfter: &staleAfter, ManagementAuthState: ManagementAuthStateValid})
	if err != nil {
		t.Fatalf("mark watchdog snapshots fresh: %v", err)
	}
}

func watchdogTestCanonicalPriority(priority int) int {
	if priority > 0 && priority < DefaultEmptyQuotaPriority {
		return DefaultWorkingPriority
	}
	return priority
}

func watchdogAuthPayload(auth watchdogUpstreamAuth) map[string]any {
	provider := auth.Provider
	if provider == "" {
		provider = "gemini"
	}
	payload := map[string]any{"id": watchdogAuthID, "auth_index": watchdogAuthIndex, "name": watchdogAuthName, "provider": provider, "label": "Gemini primary", "status": "active", "disabled": auth.Disabled, "unavailable": auth.Unavailable, "priority": auth.Priority, "failed": auth.FailureCount, "recent_requests": []any{map[string]any{"window_start": "2026-05-10T11:59:00Z", "window_end": "2026-05-10T12:00:00Z", "failure_count": auth.FailureCount}}}
	if auth.QuotaExceeded || auth.QuotaNextRecoverAt != nil {
		quota := map[string]any{"exceeded": auth.QuotaExceeded, "reason": "rate_limit"}
		if auth.QuotaNextRecoverAt != nil {
			quota["next_recover_at"] = auth.QuotaNextRecoverAt.Format(time.RFC3339)
		}
		payload["quota"] = quota
	}
	return payload
}

func writeWatchdogJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func listWatchdogActions(t *testing.T, service *Service, sidecarID int) []SidecarWatchdogAction {
	t.Helper()
	lister, ok := service.store.(actionHistoryPersistence)
	if !ok {
		t.Fatalf("store does not support action history")
	}
	actions, err := lister.ListWatchdogActions(t.Context(), sidecarID)
	if err != nil {
		t.Fatalf("list watchdog actions: %v", err)
	}
	return actions
}

func assertWatchdogAction(t *testing.T, actions []SidecarWatchdogAction, actionType string, status string) {
	t.Helper()
	for _, action := range actions {
		if action.ActionType == actionType && action.Status == status {
			return
		}
	}
	t.Fatalf("missing watchdog action %s/%s in %+v", actionType, status, actions)
}

func countWatchdogActions(actions []SidecarWatchdogAction, actionType string) int {
	count := 0
	for _, action := range actions {
		if action.ActionType == actionType {
			count++
		}
	}
	return count
}

var errInjectedWatchdogStoreFailure = errors.New("injected watchdog store failure")

type failingWatchdogStore struct {
	persistence
	failCreateHold int
	failUpdateHold int
}

func (s *failingWatchdogStore) CreateWatchdogHold(ctx context.Context, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if s.failCreateHold > 0 {
		s.failCreateHold--
		return SidecarWatchdogHold{}, errInjectedWatchdogStoreFailure
	}
	return s.persistence.CreateWatchdogHold(ctx, input)
}

func (s *failingWatchdogStore) UpdateWatchdogHold(ctx context.Context, id int, input SidecarWatchdogHoldInput) (SidecarWatchdogHold, error) {
	if s.failUpdateHold > 0 {
		s.failUpdateHold--
		return SidecarWatchdogHold{}, errInjectedWatchdogStoreFailure
	}
	return s.persistence.UpdateWatchdogHold(ctx, id, input)
}

func (s *failingWatchdogStore) ListWatchdogActions(ctx context.Context, sidecarID int) ([]SidecarWatchdogAction, error) {
	return s.persistence.(actionHistoryPersistence).ListWatchdogActions(ctx, sidecarID)
}
