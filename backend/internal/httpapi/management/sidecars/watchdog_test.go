package sidecars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestMemoryWatchdogHoldAllowsReleasedHistoryWithActiveHold(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 60)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, Reason: watchdogReasonQuotaExceeded, ConditionHash: "active-hash", TargetPriority: 0, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create active hold: %v", err)
	}
	_, err = service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, Reason: "released_history", ConditionHash: "released-hash", TargetPriority: 0, Status: WatchdogHoldStatusReleased, ReleasedAt: &now})
	if err != nil {
		t.Fatalf("memory store should allow released hold history beside active hold: %v", err)
	}
}

func TestWatchdogDeprioritizesQuotaWithoutDisabling(t *testing.T) {
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
	if !result.Reconciled {
		t.Fatalf("expected watchdog to reconcile, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{0}) {
		t.Fatalf("expected one priority=0 fields patch, got %v", got)
	}
	if upstream.statusPatchCount() != 0 {
		t.Fatalf("watchdog must not disable auth files via status patch")
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 1 {
		t.Fatalf("expected one active hold, holds=%+v err=%v", holds, err)
	}
	if holds[0].PreviousPriority == nil || *holds[0].PreviousPriority != 20 || holds[0].HoldUntil == nil || !holds[0].HoldUntil.Equal(recoverAt) {
		t.Fatalf("hold did not preserve previous priority/recover time: %+v", holds[0])
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	assertWatchdogAction(t, actions, watchdogActionDeprioritize, watchdogActionStatusSucceeded)
}

func TestWatchdogFailureThresholdSkipsWhenRecentRequestsMissing(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	snapshot := SidecarAuthSnapshot{FailedCount: intPtr(9)}
	condition := evaluateWatchdogCondition(snapshot, SidecarWatchdogPolicy{FailureThreshold: 1, FailureWindowSeconds: 60, FallbackCooldownSeconds: 60}, now)
	if condition.Triggered {
		t.Fatalf("expected missing recent_requests to be unobservable, got %+v", condition)
	}
}

func TestWatchdogRestoresOriginalPriorityAfterHold(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	recoverAt := now.Add(time.Minute)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true, QuotaNextRecoverAt: &recoverAt})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 20, QuotaExceeded: true, QuotaNextRecoverAt: &recoverAt})
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("initial deprioritize: %v", err)
	}

	now = recoverAt.Add(time.Second)
	upstream.setAuth(watchdogUpstreamAuth{Priority: 0})
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
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
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{0, 20}) {
		t.Fatalf("expected deprioritize then restore fields patches, got %v", got)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 0 {
		t.Fatalf("expected hold to be released, holds=%+v err=%v", holds, err)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	assertWatchdogAction(t, actions, watchdogActionDeprioritize, watchdogActionStatusSucceeded)
	assertWatchdogAction(t, actions, watchdogActionRestore, watchdogActionStatusSucceeded)
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
	previousPriority := 20
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("gemini"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-quota", PreviousPriority: &previousPriority, TargetPriority: 0, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive, LastActionID: intPtr(1)})
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
	upstream.setAuth(watchdogUpstreamAuth{Priority: 0})
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
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 0})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	enableWatchdogPolicy(t, service, sidecar.ID)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("gemini"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-missing-previous", TargetPriority: 0, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
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

	fieldPatches      []int
	statusPatchCalls  int
	getAuthFilesCalls int
}

func newWatchdogUpstream(t *testing.T, auth watchdogUpstreamAuth) *watchdogUpstream {
	t.Helper()
	upstream := &watchdogUpstream{t: t, auth: auth}
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.serveHTTP))
	return upstream
}

func (u *watchdogUpstream) Close() { u.server.Close() }

func (u *watchdogUpstream) URL() string { return u.server.URL }

func (u *watchdogUpstream) setAuth(auth watchdogUpstreamAuth) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.auth = auth
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
	case "/v0/management/auth-files":
		u.mu.Lock()
		u.getAuthFilesCalls++
		auth := u.auth
		u.mu.Unlock()
		writeWatchdogJSON(w, map[string]any{"auth_files": []any{watchdogAuthPayload(auth)}})
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
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecarID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, DeprioritizedPriority: DefaultDeprioritizedPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds})
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
	_, err = service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecarID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Name: watchdogAuthName, Provider: stringPtr("gemini"), Label: stringPtr("Gemini primary"), Status: stringPtr("active"), Disabled: boolPtr(auth.Disabled), Unavailable: boolPtr(auth.Unavailable), Priority: intPtr(auth.Priority), QuotaExceeded: boolPtr(auth.QuotaExceeded), QuotaNextRecoverAt: cloneTimePtr(auth.QuotaNextRecoverAt), FailedCount: intPtr(auth.FailureCount), RecentRequestsJSON: recentRequests, ModelStatesJSON: json.RawMessage(`{}`), SnapshotJSON: snapshotJSON, ObservedAt: observedAt})
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

func watchdogAuthPayload(auth watchdogUpstreamAuth) map[string]any {
	payload := map[string]any{"id": watchdogAuthID, "auth_index": watchdogAuthIndex, "name": watchdogAuthName, "provider": "gemini", "label": "Gemini primary", "status": "active", "disabled": auth.Disabled, "unavailable": auth.Unavailable, "priority": auth.Priority, "failed": auth.FailureCount, "recent_requests": []any{map[string]any{"window_start": "2026-05-10T11:59:00Z", "window_end": "2026-05-10T12:00:00Z", "failure_count": auth.FailureCount}}}
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
