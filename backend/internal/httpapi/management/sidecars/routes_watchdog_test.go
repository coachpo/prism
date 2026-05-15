package sidecars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestWatchdogPolicyProbeResponse(t *testing.T) {
	now := time.Date(2026, time.May, 11, 18, 0, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: 2, FailureWindowSeconds: 120, FallbackCooldownSeconds: 600, QuotaExceededPriority: 2, UsingPriority: 9, ManualOverridePauseSeconds: 300, ProbeConcurrency: 2, ProbeTimeoutSeconds: 10, ProbeBatchCooldownSeconds: intPtr(45), QuotaInventoryEnabled: boolPtr(false), InitialScanEnabled: boolPtr(false), RollingRefreshEnabled: boolPtr(false), RollingRefreshAfterSeconds: intPtr(7200)})
	if err != nil {
		t.Fatalf("seed watchdog policy: %v", err)
	}
	hiddenBatchAuthID := "hidden-batch-auth"
	_, err = service.store.PersistQuotaProbeDecision(t.Context(), SidecarQuotaPersistDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, hiddenBatchAuthID, now)}})
	if err != nil {
		t.Fatalf("seed hidden batch completion: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, watchdogPolicyRoutePath(sidecar.ID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get policy status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, "probe_last_batch_completed_at") || strings.Contains(body, hiddenBatchAuthID) {
		t.Fatalf("policy response leaked hidden fields: %s", body)
	}
	if body := recorder.Body.String(); !strings.Contains(body, `"probe_concurrency":2`) || strings.Contains(body, "probe_batch_size") {
		t.Fatalf("policy response used unexpected probe concurrency fields: %s", body)
	}
	var response watchdogPolicyResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	if response.ActiveRevision == nil || !response.Enabled || response.QuotaExceededPriority != 2 || response.UsingPriority != 9 || response.ErrorPriority != DefaultErrorPriority || response.ProbeConcurrency != 2 || response.ProbeTimeoutSeconds != 10 || response.ProbeBatchCooldownSeconds != 45 || response.QuotaInventoryEnabled || response.InitialScanEnabled || response.RollingRefreshEnabled || response.RollingRefreshAfterSeconds != 7200 || response.HasPendingChanges {
		t.Fatalf("unexpected policy response: %+v", response)
	}

	patch := watchdogPolicyPatchWithExpectedRevision(response.ActiveRevision.ID, `{"quota_exceeded_priority":3,"using_priority":8,"error_priority":3,"probe_concurrency":4,"probe_timeout_seconds":6,"probe_batch_cooldown_seconds":60,"probe_jitter_min_ms":25,"probe_jitter_max_ms":50,"cooldown_jitter_percent":10,"quota_inventory_enabled":true,"initial_scan_enabled":true,"rolling_refresh_enabled":true,"rolling_refresh_after_seconds":5400}`)
	patchRecorder := httptest.NewRecorder()
	router.ServeHTTP(patchRecorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(patch)))
	if patchRecorder.Code != http.StatusOK {
		t.Fatalf("patch policy status = %d body=%s", patchRecorder.Code, patchRecorder.Body.String())
	}
	if body := patchRecorder.Body.String(); !strings.Contains(body, `"probe_concurrency":4`) || strings.Contains(body, "probe_batch_size") {
		t.Fatalf("patched policy response used unexpected probe concurrency fields: %s", body)
	}
	var updated watchdogPolicyResponse
	decodeWatchdogRouteResponse(t, patchRecorder, &updated)
	if updated.PendingRevision == nil || !updated.HasPendingChanges || updated.QuotaExceededPriority != 2 || updated.PendingRevision.QuotaExceededPriority != 3 || updated.PendingRevision.UsingPriority != 8 || updated.PendingRevision.ErrorPriority != 3 || updated.PendingRevision.ProbeConcurrency != 4 || updated.PendingRevision.ProbeTimeoutSeconds != 6 || updated.PendingRevision.ProbeBatchCooldownSeconds != 60 || updated.PendingRevision.ProbeJitterMinMS != 25 || updated.PendingRevision.ProbeJitterMaxMS != 50 || updated.PendingRevision.CooldownJitterPercent != 10 || !updated.PendingRevision.QuotaInventoryEnabled || !updated.PendingRevision.InitialScanEnabled || !updated.PendingRevision.RollingRefreshEnabled || updated.PendingRevision.RollingRefreshAfterSeconds != 5400 {
		t.Fatalf("unexpected patched policy: %+v", updated)
	}
}

func TestWatchdogPolicyDefaultsExposeCanonicalFourBandThresholds(t *testing.T) {
	now := time.Date(2026, time.May, 15, 12, 0, 0, 0, time.UTC)
	_, router, sidecar := newWatchdogRouteTest(t, now)

	response := getWatchdogPolicyRoute(t, router, sidecar.ID)
	if response.WorkingPriority != DefaultWorkingPriority || response.EmptyQuotaPriority != DefaultEmptyQuotaPriority || response.InitialPriority != DefaultInitialPriority || response.ErrorPriority != DefaultErrorPriority {
		t.Fatalf("policy response did not expose canonical four-band defaults: %+v", response)
	}
	if response.ActiveRevision == nil || response.ActiveRevision.WorkingPriority != DefaultWorkingPriority || response.ActiveRevision.EmptyQuotaPriority != DefaultEmptyQuotaPriority || response.ActiveRevision.InitialPriority != DefaultInitialPriority || response.ActiveRevision.ErrorPriority != DefaultErrorPriority {
		t.Fatalf("active revision did not expose canonical four-band defaults: %+v", response.ActiveRevision)
	}
}

func TestWatchdogPolicySaveCreatesPendingRevision(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 0, 0, 0, time.UTC)
	_, router, sidecar := newWatchdogRouteTest(t, now)
	initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
	activeID := initial.ActiveRevision.ID

	body := `{"expected_revision_id":` + strconv.FormatInt(activeID, 10) + `,"enabled":true,"probe_concurrency":5,"probe_timeout_seconds":9,"watchdog_sweep_interval_seconds":120}`
	response := patchWatchdogPolicyRoute(t, router, sidecar.ID, body, http.StatusOK)
	if !response.HasPendingChanges || response.ActiveRevision == nil || response.PendingRevision == nil {
		t.Fatalf("expected active and pending revisions: %+v", response)
	}
	if response.ActiveRevision.ID != activeID || response.ActiveRevision.ProbeConcurrency != initial.ActiveRevision.ProbeConcurrency || response.ProbeConcurrency != initial.ActiveRevision.ProbeConcurrency {
		t.Fatalf("save must not mutate active revision: before=%+v after=%+v", initial, response)
	}
	if response.PendingRevision.ID == activeID || response.PendingRevision.ProbeConcurrency != 5 || response.PendingRevision.ProbeTimeoutSeconds != 9 || response.PendingRevision.WatchdogSweepIntervalSeconds != 120 || !response.PendingRevision.Enabled {
		t.Fatalf("pending revision did not capture saved policy: %+v", response.PendingRevision)
	}
}

func TestWatchdogPolicyApplyActivatesPendingRevision(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 15, 0, 0, time.UTC)
	_, router, sidecar := newWatchdogRouteTest(t, now)
	initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
	activeID := initial.ActiveRevision.ID
	pending := patchWatchdogPolicyRoute(t, router, sidecar.ID, `{"expected_revision_id":`+strconv.FormatInt(activeID, 10)+`,"probe_concurrency":6,"probe_timeout_seconds":11}`, http.StatusOK)
	pendingID := pending.PendingRevision.ID

	applied := applyWatchdogPolicyRoute(t, router, sidecar.ID, pendingID, activeID, http.StatusOK)
	if applied.HasPendingChanges || applied.PendingRevision != nil || applied.ActiveRevision == nil {
		t.Fatalf("expected pending revision to be cleared after apply: %+v", applied)
	}
	if applied.ActiveRevision.ID != pendingID || applied.ActiveRevision.ProbeConcurrency != 6 || applied.ActiveRevision.ProbeTimeoutSeconds != 11 || applied.ActiveRevisionID == nil || *applied.ActiveRevisionID != pendingID {
		t.Fatalf("pending revision was not activated: %+v", applied)
	}
}

func TestWatchdogPolicyApplyDoesNotRestartActiveSweep(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 30, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
	activeID := initial.ActiveRevision.ID
	sweepStore := service.store.(watchdogSweepLifecyclePersistence)
	items := []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-pinned", AuthIndex: "idx-pinned", Provider: "codex"}}
	sweep, err := sweepStore.UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "route-sweep-pinned", SidecarID: sidecar.ID, PolicyRevisionID: activeID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: marshalWatchdogSweepItems(t, items), NextItemIndex: 1, StartedAt: now})
	if err != nil {
		t.Fatalf("seed active sweep: %v", err)
	}
	materializeWatchdogSweepTestItems(t, service, sweep, items)
	pending := patchWatchdogPolicyRoute(t, router, sidecar.ID, `{"expected_revision_id":`+strconv.FormatInt(activeID, 10)+`,"probe_concurrency":7}`, http.StatusOK)
	applied := applyWatchdogPolicyRoute(t, router, sidecar.ID, pending.PendingRevision.ID, activeID, http.StatusOK)
	if applied.ActiveSweep == nil || applied.ActiveSweep.PolicyRevisionID != activeID || applied.ActiveSweep.Progress.TotalItems != 1 || applied.ActiveSweep.Progress.PendingItems != 1 {
		t.Fatalf("apply should leave active sweep pinned with child progress: %+v", applied.ActiveSweep)
	}
	if applied.ActiveRevision.ID != pending.PendingRevision.ID || applied.ActiveRevision.ProbeConcurrency != 7 {
		t.Fatalf("active policy should move to pending revision for future sweeps: %+v", applied.ActiveRevision)
	}
	activeSweep, found, err := sweepStore.GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || !found || activeSweep.SweepID != "route-sweep-pinned" || activeSweep.Status != string(SidecarWatchdogSweepStatusRunning) {
		t.Fatalf("apply should not cancel active sweep: active=%+v found=%v err=%v", activeSweep, found, err)
	}
}

func TestWatchdogPolicyApplyAndRestartSupersedesActiveSweepAndChildItems(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 40, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
	activeID := initial.ActiveRevision.ID
	sweepStore := service.store.(watchdogSweepLifecyclePersistence)
	items := []watchdogSweepSnapshotItem{
		{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-restart-a", AuthIndex: "idx-restart-a", Provider: "codex"},
		{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-restart-b", AuthIndex: "idx-restart-b", Provider: "codex"},
	}
	sweep, err := sweepStore.UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "route-sweep-restart", SidecarID: sidecar.ID, PolicyRevisionID: activeID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: marshalWatchdogSweepItems(t, items), StartedAt: now})
	if err != nil {
		t.Fatalf("seed active sweep: %v", err)
	}
	materializeWatchdogSweepTestItems(t, service, sweep, items)
	itemStore := service.store.(watchdogSweepItemPersistence)
	leaseOwner := "route-restart-worker"
	leaseExpiresAt := now.Add(time.Minute)
	claimed, err := itemStore.ClaimWatchdogSweepItems(t.Context(), SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sidecar.ID, Limit: 1, LeaseOwner: leaseOwner, LeaseExpiresAt: leaseExpiresAt, ClaimedAt: now})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim one child before restart: claimed=%+v err=%v", claimed, err)
	}
	pending := patchWatchdogPolicyRoute(t, router, sidecar.ID, `{"expected_revision_id":`+strconv.FormatInt(activeID, 10)+`,"probe_concurrency":8}`, http.StatusOK)
	applied := applyAndRestartWatchdogPolicyRoute(t, router, sidecar.ID, pending.PendingRevision.ID, activeID, http.StatusOK)
	if applied.ActiveSweep != nil || applied.HasPendingChanges || applied.PendingRevision != nil {
		t.Fatalf("apply-and-restart should clear pending state and supersede active sweep: %+v", applied)
	}
	if applied.ActiveRevision == nil || applied.ActiveRevision.ID != pending.PendingRevision.ID || applied.ActiveRevision.ProbeConcurrency != 8 {
		t.Fatalf("apply-and-restart did not activate pending revision: %+v", applied.ActiveRevision)
	}
	activeSweep, found, err := sweepStore.GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || found {
		t.Fatalf("apply-and-restart should cancel active sweep: active=%+v found=%v err=%v", activeSweep, found, err)
	}
	afterRestartClaim, err := itemStore.ClaimWatchdogSweepItems(t.Context(), SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sidecar.ID, Limit: 2, LeaseOwner: "post-restart-worker", LeaseExpiresAt: leaseExpiresAt, ClaimedAt: now})
	if err != nil || len(afterRestartClaim) != 0 {
		t.Fatalf("superseded sweep should not lease children: claimed=%+v err=%v", afterRestartClaim, err)
	}
	childItems, err := itemStore.ListWatchdogSweepItems(t.Context(), sweep.SweepID)
	if err != nil || len(childItems) != len(items) {
		t.Fatalf("list superseded child items: items=%+v err=%v", childItems, err)
	}
	for _, item := range childItems {
		if item.Status != string(SidecarWatchdogSweepItemStatusSuperseded) || item.LeaseOwner != nil || item.LeaseExpiresAt != nil || item.CompletedAt == nil || stringValue(item.LastErrorCode) != watchdogPolicyRestartSupersedeReason {
			t.Fatalf("child item was not durably superseded: %+v", item)
		}
	}
	store := service.store.(*memorySidecarStore)
	store.mu.RLock()
	defer store.mu.RUnlock()
	if len(store.sweeps[sidecar.ID]) != 1 {
		t.Fatalf("expected retained cancelled sweep, got %+v", store.sweeps[sidecar.ID])
	}
	cancelled := store.sweeps[sidecar.ID][0]
	if cancelled.Status != string(SidecarWatchdogSweepStatusCancelled) || cancelled.FailureReason == nil || *cancelled.FailureReason != watchdogPolicyRestartSupersedeReason || cancelled.CompletedAt == nil || cancelled.LeaseExpiresAt != nil {
		t.Fatalf("active sweep was not durably superseded: %+v", cancelled)
	}
	if cancelled.RestartRequestedAt == nil || !cancelled.RestartRequestedAt.Equal(now) || cancelled.RestartTargetPolicyRevisionID == nil || *cancelled.RestartTargetPolicyRevisionID != pending.PendingRevision.ID || stringValue(cancelled.RestartReason) != watchdogPolicyRestartSupersedeReason {
		t.Fatalf("restart intent was not durable on cancelled sweep: %+v", cancelled)
	}
	if cancelled.CancelRequestedAt == nil || !cancelled.CancelRequestedAt.Equal(now) || stringValue(cancelled.CancelReason) != watchdogPolicyRestartSupersedeReason {
		t.Fatalf("cancel intent was not durable on cancelled sweep: %+v", cancelled)
	}
}

func TestWatchdogLateSweepItemCommitAfterSupersedeIsFenced(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 42, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
	activeID := initial.ActiveRevision.ID
	items := []watchdogSweepSnapshotItem{{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-late", AuthIndex: "idx-late", Provider: "codex"}}
	sweep, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "route-sweep-late", SidecarID: sidecar.ID, PolicyRevisionID: activeID, Status: string(SidecarWatchdogSweepStatusRunning), SnapshotJSON: marshalWatchdogSweepItems(t, items), StartedAt: now})
	if err != nil {
		t.Fatalf("seed active sweep: %v", err)
	}
	materializeWatchdogSweepTestItems(t, service, sweep, items)
	itemStore := service.store.(watchdogSweepItemPersistence)
	leaseOwner := "late-worker"
	leaseExpiresAt := now.Add(time.Minute)
	claimed, err := itemStore.ClaimWatchdogSweepItems(t.Context(), SidecarWatchdogSweepItemClaimInput{SweepID: sweep.SweepID, SidecarID: sidecar.ID, Limit: 1, LeaseOwner: leaseOwner, LeaseExpiresAt: leaseExpiresAt, ClaimedAt: now})
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claim child before supersede: claimed=%+v err=%v", claimed, err)
	}
	pending := patchWatchdogPolicyRoute(t, router, sidecar.ID, `{"expected_revision_id":`+strconv.FormatInt(activeID, 10)+`,"probe_timeout_seconds":12}`, http.StatusOK)
	applyAndRestartWatchdogPolicyRoute(t, router, sidecar.ID, pending.PendingRevision.ID, activeID, http.StatusOK)
	commit := SidecarWatchdogSweepItemCommitInput{SweepID: sweep.SweepID, SidecarID: sidecar.ID, ItemID: claimed[0].ID, ItemIndex: claimed[0].ItemIndex, AttemptToken: claimed[0].AttemptToken, LeaseOwner: leaseOwner, Status: string(SidecarWatchdogSweepItemStatusSucceeded), CompletedAt: now.Add(time.Second)}
	decision, err := service.store.PersistWatchdogProbeDecision(t.Context(), SidecarWatchdogProbeDecision{SidecarID: sidecar.ID, Observations: []SidecarWatchdogProbeObservationInput{testProbeObservationInput(sidecar.ID, "auth-late", now.Add(time.Second))}, SweepItemCommit: &commit})
	if err != nil {
		t.Fatalf("persist late commit: %v", err)
	}
	if decision.SweepItemCommit == nil || decision.SweepItemCommit.Outcome != SidecarWatchdogSweepItemCommitOutcomeStale {
		t.Fatalf("late commit should be fenced as stale: %+v", decision.SweepItemCommit)
	}
	if len(decision.Observations) != 0 || len(decision.QuotaStates) != 0 || decision.Policy != nil {
		t.Fatalf("fenced late commit must not mutate runtime state: %+v", decision)
	}
	childItems, err := itemStore.ListWatchdogSweepItems(t.Context(), sweep.SweepID)
	if err != nil || len(childItems) != 1 {
		t.Fatalf("list late child item: items=%+v err=%v", childItems, err)
	}
	if childItems[0].Status != string(SidecarWatchdogSweepItemStatusSuperseded) || childItems[0].ResultObservationID != nil {
		t.Fatalf("late commit mutated superseded child item: %+v", childItems[0])
	}
}

func TestWatchdogPolicyRejectsStaleExpectedRevision(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 45, 0, 0, time.UTC)
	_, router, sidecar := newWatchdogRouteTest(t, now)
	initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
	activeID := initial.ActiveRevision.ID
	pending := patchWatchdogPolicyRoute(t, router, sidecar.ID, `{"expected_revision_id":`+strconv.FormatInt(activeID, 10)+`,"probe_concurrency":4}`, http.StatusOK)

	patchWatchdogPolicyRoute(t, router, sidecar.ID, `{"expected_revision_id":`+strconv.FormatInt(activeID, 10)+`,"probe_concurrency":5}`, http.StatusConflict)
	applied := applyWatchdogPolicyRoute(t, router, sidecar.ID, pending.PendingRevision.ID, activeID, http.StatusOK)
	applyWatchdogPolicyRoute(t, router, sidecar.ID, activeID, activeID, http.StatusConflict)
	nextPending := patchWatchdogPolicyRoute(t, router, sidecar.ID, `{"expected_revision_id":`+strconv.FormatInt(applied.ActiveRevision.ID, 10)+`,"probe_concurrency":5}`, http.StatusOK)
	applyAndRestartWatchdogPolicyRoute(t, router, sidecar.ID, nextPending.PendingRevision.ID, activeID, http.StatusConflict)
	if applied.ActiveRevision.ID != pending.PendingRevision.ID {
		t.Fatalf("expected first apply to activate pending revision: %+v", applied)
	}
}

func TestWatchdogPolicyValidationRejectsMissingExpectedRevisionID(t *testing.T) {
	now := time.Date(2026, time.May, 15, 10, 50, 0, 0, time.UTC)
	_, router, sidecar := newWatchdogRouteTest(t, now)
	for _, method := range []string{http.MethodPut, http.MethodPatch} {
		t.Run(method, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(method, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(`{"probe_concurrency":4}`)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("policy save without expected revision status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), "expected_revision_id is required") {
				t.Fatalf("missing expected_revision_id response = %s", recorder.Body.String())
			}
		})
	}
}

func TestQuotaRouteResponsesHideInternalFields(t *testing.T) {
	now := time.Date(2026, time.May, 11, 19, 0, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	authIndex := "private-route-auth-index"
	_, err := service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: "auth-route-hidden", AuthIndex: &authIndex, Name: "route-hidden.json", Provider: stringPtr("codex"), Priority: intPtr(9), SnapshotJSON: json.RawMessage(`{"id":"auth-route-hidden"}`), ObservedAt: now})
	if err != nil {
		t.Fatalf("seed auth snapshot: %v", err)
	}
	stateStore, ok := service.store.(authQuotaStateStore)
	if !ok {
		t.Fatalf("store does not support quota states")
	}
	_, err = stateStore.UpsertAuthQuotaState(t.Context(), SidecarAuthQuotaStateInput{SidecarID: sidecar.ID, AuthID: "auth-route-hidden", AuthIndex: &authIndex, AuthName: stringPtr("route-hidden.json"), Provider: stringPtr("codex"), SnapshotObservedAt: &now, QuotaBand: "quota_exceeded", ProbeStatus: stringPtr(watchdogProbeStatusSucceeded), LastProbedAt: &now})
	if err != nil {
		t.Fatalf("seed quota state: %v", err)
	}
	privateScanCursor := "private-scan-cursor"
	completedAt := now
	_, err = service.store.CreateQuotaScanRun(t.Context(), SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusCompleted, CursorAuthID: &privateScanCursor, PlannedCount: 2, AttemptedCount: 1, CompletedAt: &completedAt})
	if err != nil {
		t.Fatalf("seed quota scan projection: %v", err)
	}

	stateRecorder := httptest.NewRecorder()
	router.ServeHTTP(stateRecorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-states", nil))
	if stateRecorder.Code != http.StatusOK {
		t.Fatalf("quota states status = %d body=%s", stateRecorder.Code, stateRecorder.Body.String())
	}
	if body := stateRecorder.Body.String(); strings.Contains(body, `"auth_index":`) || strings.Contains(body, authIndex) || !strings.Contains(body, `"auth_index_present":true`) {
		t.Fatalf("quota state response leaked internal auth index or missed presence flag: %s", body)
	}

	currentRecorder := httptest.NewRecorder()
	router.ServeHTTP(currentRecorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-scans/current", nil))
	if currentRecorder.Code != http.StatusNoContent {
		t.Fatalf("current quota scan status = %d body=%s", currentRecorder.Code, currentRecorder.Body.String())
	}
	listRecorder := httptest.NewRecorder()
	router.ServeHTTP(listRecorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-scans", nil))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("quota scan list status = %d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	if body := listRecorder.Body.String(); strings.Contains(body, "cursor_auth_id") || strings.Contains(body, privateScanCursor) {
		t.Fatalf("quota scan response leaked private cursor: %s", body)
	}
}

func TestPriorityStateDerivedForQuotaStateResponses(t *testing.T) {
	now := time.Date(2026, time.May, 15, 11, 0, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: 90, UsingPriority: 99, ErrorPriority: 10, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeConcurrency: DefaultProbeConcurrency, ProbeTimeoutSeconds: DefaultProbeTimeoutSeconds, ProbeBatchCooldownSeconds: intPtr(DefaultProbeBatchCooldownSeconds), QuotaInventoryEnabled: boolPtr(true), InitialScanEnabled: boolPtr(true), RollingRefreshEnabled: boolPtr(true), RollingRefreshAfterSeconds: intPtr(DefaultRollingRefreshAfterSeconds)})
	if err != nil {
		t.Fatalf("seed watchdog policy: %v", err)
	}
	stateStore := service.store.(authQuotaStateStore)
	cases := []struct {
		authID        string
		priority      int
		quotaBand     string
		priorityState string
	}{
		{authID: "auth-working", priority: 100, quotaBand: quotaBandUsing, priorityState: watchdogPriorityStateWorking},
		{authID: "auth-empty", priority: 90, quotaBand: quotaBandQuotaExceeded, priorityState: watchdogPriorityStateEmptyQuota},
		{authID: "auth-initial", priority: 50, quotaBand: quotaBandUsing, priorityState: watchdogPriorityStateInitial},
		{authID: "auth-error", priority: 9, quotaBand: quotaBandError, priorityState: watchdogPriorityStateError},
	}
	for _, tc := range cases {
		authIndex := "idx-" + tc.authID
		if _, err := service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: tc.authID, AuthIndex: &authIndex, Name: tc.authID + ".json", Provider: stringPtr("codex"), Priority: intPtr(tc.priority), SnapshotJSON: json.RawMessage(`{}`), ObservedAt: now}); err != nil {
			t.Fatalf("seed auth snapshot %s: %v", tc.authID, err)
		}
		if _, err := stateStore.UpsertAuthQuotaState(t.Context(), SidecarAuthQuotaStateInput{SidecarID: sidecar.ID, AuthID: tc.authID, AuthIndex: &authIndex, AuthName: stringPtr(tc.authID + ".json"), Provider: stringPtr("codex"), SnapshotObservedAt: &now, QuotaBand: tc.quotaBand, ProbeStatus: stringPtr(watchdogProbeStatusSucceeded), LastProbedAt: &now}); err != nil {
			t.Fatalf("seed quota state %s: %v", tc.authID, err)
		}
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-states", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("quota states status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response quotaStateListResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	items := map[string]quotaStateResponse{}
	for _, item := range response.Items {
		items[item.AuthID] = item
	}
	for _, tc := range cases {
		item := items[tc.authID]
		if item.QuotaBand != tc.quotaBand || item.PriorityState != tc.priorityState || item.CurrentPriority == nil || *item.CurrentPriority != tc.priority {
			t.Fatalf("quota response for %s kept quota_band/current_priority or derived wrong priority_state: %+v", tc.authID, item)
		}
	}
}

func TestActionHistoryDistinguishesPatchedVsAlreadyAtTarget(t *testing.T) {
	now := time.Date(2026, time.May, 15, 11, 15, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: 90, UsingPriority: 99, ErrorPriority: 10, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeConcurrency: DefaultProbeConcurrency, ProbeTimeoutSeconds: DefaultProbeTimeoutSeconds, ProbeBatchCooldownSeconds: intPtr(DefaultProbeBatchCooldownSeconds)})
	if err != nil {
		t.Fatalf("seed watchdog policy: %v", err)
	}
	previousPriority := 99
	targetPriority := 90
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-patched"), AuthIndex: stringPtrFromNonEmpty("idx-patched"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionDeprioritize, Status: watchdogActionStatusSucceeded, Reason: stringPtrFromNonEmpty(watchdogReasonQuotaExceeded), PreviousPriority: &previousPriority, TargetPriority: &targetPriority})
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-already"), AuthIndex: stringPtrFromNonEmpty("idx-already"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionDeprioritize, Status: watchdogActionStatusSucceeded, Reason: stringPtrFromNonEmpty(watchdogActionMutationOutcomeAlreadyAtTarget), PreviousPriority: &previousPriority, TargetPriority: &targetPriority})
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-skipped"), AuthIndex: stringPtrFromNonEmpty("idx-skipped"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionDeprioritize, Status: watchdogActionStatusSkipped, Reason: stringPtrFromNonEmpty("current priority no longer matches selected priority"), PreviousPriority: &previousPriority, TargetPriority: &targetPriority})
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-failed"), AuthIndex: stringPtrFromNonEmpty("idx-failed"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionDeprioritize, Status: watchdogActionStatusFailed, Reason: stringPtrFromNonEmpty(watchdogReasonQuotaExceeded), PreviousPriority: &previousPriority, TargetPriority: &targetPriority, ErrorMessage: stringPtrFromNonEmpty("patch failed")})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/actions", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("actions status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response actionHistoryListResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	items := map[string]actionRecordResponse{}
	for _, item := range response.Items {
		items[stringValue(item.AuthID)] = item
	}
	wants := map[string]string{"auth-patched": watchdogActionMutationOutcomePatched, "auth-already": watchdogActionMutationOutcomeAlreadyAtTarget, "auth-skipped": watchdogActionMutationOutcomeSkipped, "auth-failed": watchdogActionMutationOutcomeFailed}
	for authID, wantOutcome := range wants {
		item := items[authID]
		if item.MutationOutcome != wantOutcome || item.PreviousPriorityState != watchdogPriorityStateWorking || item.TargetPriorityState != watchdogPriorityStateEmptyQuota || item.PriorityState != watchdogPriorityStateEmptyQuota {
			t.Fatalf("action %s outcome/priority states mismatch: %+v", authID, item)
		}
		if item.PreviousPriority == nil || *item.PreviousPriority != previousPriority || item.TargetPriority == nil || *item.TargetPriority != targetPriority {
			t.Fatalf("action %s did not preserve raw priorities: %+v", authID, item)
		}
	}
}

func TestMissingPriorityMapsToInitial(t *testing.T) {
	now := time.Date(2026, time.May, 15, 11, 30, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	stateStore := service.store.(authQuotaStateStore)
	authIndex := "idx-missing-priority"
	if _, err := service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: "auth-missing-priority", AuthIndex: &authIndex, Name: "missing-priority.json", Provider: stringPtr("codex"), SnapshotJSON: json.RawMessage(`{}`), ObservedAt: now}); err != nil {
		t.Fatalf("seed auth snapshot: %v", err)
	}
	if _, err := stateStore.UpsertAuthQuotaState(t.Context(), SidecarAuthQuotaStateInput{SidecarID: sidecar.ID, AuthID: "auth-missing-priority", AuthIndex: &authIndex, AuthName: stringPtr("missing-priority.json"), Provider: stringPtr("codex"), SnapshotObservedAt: &now, QuotaBand: quotaBandError, ProbeStatus: stringPtr(watchdogProbeStatusSucceeded), LastProbedAt: &now}); err != nil {
		t.Fatalf("seed quota state: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-states", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("quota states status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if body := recorder.Body.String(); strings.Contains(body, `"current_priority":0`) || strings.Contains(body, "priority 0") {
		t.Fatalf("missing priority response must not render a priority-0 fallback: %s", body)
	}
	var response quotaStateListResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	if len(response.Items) != 1 || response.Items[0].CurrentPriority != nil || response.Items[0].PriorityState != watchdogPriorityStateInitial {
		t.Fatalf("missing priority should preserve nil current_priority and derive initial state: %+v", response.Items)
	}
}

func TestWatchdogPolicyValidationRejectsProbeTimeoutBudgetAndPriorities(t *testing.T) {
	maxBudget := watchdogProbeConcurrencyBudgetMaxSeconds()
	oversizedTimeout := strconv.Itoa(maxBudget + 1)
	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{name: "negative quota exceeded priority", body: `{"quota_exceeded_priority":-1}`, wantDetail: "quota_exceeded_priority"},
		{name: "negative using priority", body: `{"using_priority":-1}`, wantDetail: "using_priority"},
		{name: "quota exceeded is above using", body: `{"quota_exceeded_priority":4,"using_priority":3}`, wantDetail: "quota_exceeded_priority must be \\u003c= using_priority"},
		{name: "zero working priority band", body: `{"working_priority":0}`, wantDetail: "working_priority"},
		{name: "zero empty quota priority band", body: `{"empty_quota_priority":0}`, wantDetail: "empty_quota_priority"},
		{name: "zero initial priority band", body: `{"initial_priority":0}`, wantDetail: "initial_priority"},
		{name: "zero error priority band", body: `{"error_priority":0}`, wantDetail: "error_priority"},
		{name: "zero probe concurrency", body: `{"probe_concurrency":0}`, wantDetail: "probe_concurrency"},
		{name: "zero probe timeout", body: `{"probe_timeout_seconds":0}`, wantDetail: "probe_timeout_seconds"},
		{name: "timeout exceeds worker budget", body: `{"probe_timeout_seconds":` + oversizedTimeout + `}`, wantDetail: "worker budget"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.May, 11, 18, 15, 0, 0, time.UTC)
			_, router, sidecar := newWatchdogRouteTest(t, now)
			initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
			recorder := httptest.NewRecorder()
			body := watchdogPolicyPatchWithExpectedRevision(initial.ActiveRevision.ID, tt.body)
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(body)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("patch policy status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tt.wantDetail) {
				t.Fatalf("validation response %q missing %q", recorder.Body.String(), tt.wantDetail)
			}
		})
	}
}

func TestWatchdogPolicyValidationAcceptsConcurrentTimeoutWithinPerProbeBudget(t *testing.T) {
	now := time.Date(2026, time.May, 11, 18, 30, 0, 0, time.UTC)
	_, router, sidecar := newWatchdogRouteTest(t, now)
	initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
	maxBudget := watchdogProbeConcurrencyBudgetMaxSeconds()
	body := watchdogPolicyPatchWithExpectedRevision(initial.ActiveRevision.ID, `{"probe_concurrency":`+strconv.Itoa(MaxProbeConcurrency)+`,"probe_timeout_seconds":`+strconv.Itoa(maxBudget)+`}`)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(body)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("patch policy status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	responseBody := recorder.Body.String()
	if !strings.Contains(responseBody, `"probe_concurrency":`+strconv.Itoa(MaxProbeConcurrency)) || !strings.Contains(responseBody, `"probe_timeout_seconds":`+strconv.Itoa(maxBudget)) {
		t.Fatalf("policy response missing concurrent per-probe budget settings: %s", responseBody)
	}
}

func TestWatchdogPolicyRejectsInvalidCooldown(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{name: "probe batch cooldown", body: `{"probe_batch_cooldown_seconds":0}`, wantDetail: "probe_batch_cooldown_seconds"},
		{name: "rolling refresh after", body: `{"rolling_refresh_after_seconds":0}`, wantDetail: "rolling_refresh_after_seconds"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.May, 11, 18, 45, 0, 0, time.UTC)
			_, router, sidecar := newWatchdogRouteTest(t, now)
			initial := getWatchdogPolicyRoute(t, router, sidecar.ID)
			recorder := httptest.NewRecorder()
			body := watchdogPolicyPatchWithExpectedRevision(initial.ActiveRevision.ID, tt.body)
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecar.ID), strings.NewReader(body)))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("patch policy status = %d body=%s", recorder.Code, recorder.Body.String())
			}
			if !strings.Contains(recorder.Body.String(), tt.wantDetail) {
				t.Fatalf("validation response %q missing %q", recorder.Body.String(), tt.wantDetail)
			}
		})
	}
}

func TestRedactWatchdogActionHistoryProbeUnsupportedAndQuotaHoldResponse(t *testing.T) {
	now := time.Date(2026, time.May, 11, 18, 30, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	rawReason := `{"body":"secret-body","headers":{"Authorization":"Bearer raw-token"},"email":"person@example.com"}`
	rawError := `usage body parse failed near {"account_id":"acct_123","body":"secret-body","token":"raw-token"}`
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-probe"), AuthIndex: stringPtrFromNonEmpty("idx-probe"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogProbeStatusFailedParse, Status: watchdogActionStatusFailed, Reason: &rawReason, ErrorMessage: &rawError})
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-unsupported"), AuthIndex: stringPtrFromNonEmpty("idx-unsupported"), Provider: stringPtrFromNonEmpty("gemini"), ActionType: watchdogProbeStatusSkippedUnsupportedProvider, Status: watchdogActionStatusSkipped, Reason: stringPtrFromNonEmpty("unsupported provider body should not leak")})
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-success"), AuthIndex: stringPtrFromNonEmpty("idx-success"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogProbeStatusSucceeded, Status: watchdogActionStatusSucceeded, Reason: stringPtrFromNonEmpty("success raw body ignored")})
	holdID := 42
	holdUntil := now.Add(5 * time.Hour)
	createWatchdogRouteAction(t, service, SidecarWatchdogActionInput{SidecarID: sidecar.ID, HoldID: &holdID, AuthID: stringPtrFromNonEmpty("auth-held"), AuthIndex: stringPtrFromNonEmpty("idx-held"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionRestoreSkippedUnhealthy, Status: watchdogActionStatusSkipped, Reason: stringPtrFromNonEmpty("quota_exceeded:five_hour"), HoldUntil: &holdUntil})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/actions", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("actions status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	body := recorder.Body.String()
	for _, forbidden := range []string{"secret-body", "Bearer raw-token", "person@example.com", "acct_123", "raw-token", "\"body\"", "Authorization"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("action history leaked %q in %s", forbidden, body)
		}
	}
	var response actionHistoryListResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	itemsByType := map[string]actionRecordResponse{}
	for _, item := range response.Items {
		itemsByType[item.ActionType] = item
	}
	failed := itemsByType[watchdogProbeStatusFailedParse]
	if failed.Reason == nil || *failed.Reason != watchdogProbeStatusFailedParse || failed.ErrorMessage == nil || *failed.ErrorMessage != watchdogProbeStatusFailedParse {
		t.Fatalf("probe parse failure should be generic and distinct, got %+v", failed)
	}
	if _, ok := itemsByType[watchdogProbeStatusSkippedUnsupportedProvider]; !ok {
		t.Fatalf("missing unsupported-provider probe action in %+v", response.Items)
	}
	if _, ok := itemsByType[watchdogProbeStatusSucceeded]; !ok {
		t.Fatalf("missing probe success action in %+v", response.Items)
	}
	quota := itemsByType[watchdogActionQuotaHoldExtended]
	if quota.Reason == nil || *quota.Reason != "quota_exceeded:five_hour" || quota.HoldUntil == nil || !quota.HoldUntil.Equal(holdUntil) {
		t.Fatalf("quota hold extension not shaped distinctly: %+v", quota)
	}
}

func TestQuotaScanRoutesReturnExpectedStatusCodes(t *testing.T) {
	now := time.Date(2026, time.May, 11, 19, 30, 0, 0, time.UTC)
	service, router, sidecar := newWatchdogRouteTest(t, now)
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, UsingPriority: DefaultUsingPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeConcurrency: DefaultProbeConcurrency, ProbeTimeoutSeconds: DefaultProbeTimeoutSeconds, ProbeBatchCooldownSeconds: intPtr(DefaultProbeBatchCooldownSeconds), QuotaInventoryEnabled: boolPtr(true), InitialScanEnabled: boolPtr(false), RollingRefreshEnabled: boolPtr(false), RollingRefreshAfterSeconds: intPtr(DefaultRollingRefreshAfterSeconds)})
	if err != nil {
		t.Fatalf("seed watchdog policy: %v", err)
	}

	current := httptest.NewRecorder()
	router.ServeHTTP(current, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-scans/current", nil))
	if current.Code != http.StatusNoContent {
		t.Fatalf("expected inactive current scan to return %d, got %d body=%s", http.StatusNoContent, current.Code, current.Body.String())
	}
	if body := current.Body.String(); body != "" {
		t.Fatalf("expected inactive current scan response body to be empty, got %q", body)
	}

	completedAt := now
	run, err := service.store.CreateQuotaScanRun(t.Context(), SidecarQuotaScanRunInput{SidecarID: sidecar.ID, ScanType: quotaScanTypeManual, Status: quotaScanStatusCompleted, PlannedCount: 1, AttemptedCount: 1, CompletedAt: &completedAt})
	if err != nil {
		t.Fatalf("seed quota scan projection: %v", err)
	}
	cancel := httptest.NewRecorder()
	router.ServeHTTP(cancel, httptest.NewRequest(http.MethodPost, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/quota-scans/"+strconv.Itoa(run.ID)+"/cancel", nil))
	if cancel.Code != http.StatusAccepted {
		t.Fatalf("expected cancel projection to return %d, got %d body=%s", http.StatusAccepted, cancel.Code, cancel.Body.String())
	}
}

func newWatchdogRouteTest(t *testing.T, now time.Time) (*Service, http.Handler, SidecarInstance) {
	t.Helper()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 900)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return service, router, sidecar
}

func watchdogPolicyRoutePath(sidecarID int) string {
	return "/sidecars/" + strconv.Itoa(sidecarID) + "/watchdog-policy"
}

func getWatchdogPolicyRoute(t *testing.T, router http.Handler, sidecarID int) watchdogPolicyResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, watchdogPolicyRoutePath(sidecarID), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("get watchdog policy status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	var response watchdogPolicyResponse
	decodeWatchdogRouteResponse(t, recorder, &response)
	if response.ActiveRevision == nil {
		t.Fatalf("watchdog policy response missing active revision: %+v", response)
	}
	return response
}

func patchWatchdogPolicyRoute(t *testing.T, router http.Handler, sidecarID int, body string, wantStatus int) watchdogPolicyResponse {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, watchdogPolicyRoutePath(sidecarID), strings.NewReader(body)))
	if recorder.Code != wantStatus {
		t.Fatalf("patch watchdog policy status = %d want=%d body=%s", recorder.Code, wantStatus, recorder.Body.String())
	}
	var response watchdogPolicyResponse
	if wantStatus == http.StatusOK {
		decodeWatchdogRouteResponse(t, recorder, &response)
	}
	return response
}

func watchdogPolicyPatchWithExpectedRevision(expectedRevisionID int64, body string) string {
	trimmed := strings.TrimSpace(body)
	trimmed = strings.TrimPrefix(trimmed, "{")
	trimmed = strings.TrimSuffix(trimmed, "}")
	trimmed = strings.TrimSpace(trimmed)
	prefix := `{"expected_revision_id":` + strconv.FormatInt(expectedRevisionID, 10)
	if trimmed == "" {
		return prefix + `}`
	}
	return prefix + `,` + trimmed + `}`
}

func applyWatchdogPolicyRoute(t *testing.T, router http.Handler, sidecarID int, targetRevisionID int64, expectedRevisionID int64, wantStatus int) watchdogPolicyResponse {
	t.Helper()
	return applyWatchdogPolicyRouteAt(t, router, sidecarID, "/apply", targetRevisionID, expectedRevisionID, wantStatus)
}

func applyAndRestartWatchdogPolicyRoute(t *testing.T, router http.Handler, sidecarID int, targetRevisionID int64, expectedRevisionID int64, wantStatus int) watchdogPolicyResponse {
	t.Helper()
	return applyWatchdogPolicyRouteAt(t, router, sidecarID, "/apply-and-restart", targetRevisionID, expectedRevisionID, wantStatus)
}

func applyWatchdogPolicyRouteAt(t *testing.T, router http.Handler, sidecarID int, suffix string, targetRevisionID int64, expectedRevisionID int64, wantStatus int) watchdogPolicyResponse {
	t.Helper()
	body := `{"target_revision_id":` + strconv.FormatInt(targetRevisionID, 10) + `,"expected_revision_id":` + strconv.FormatInt(expectedRevisionID, 10) + `}`
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, watchdogPolicyRoutePath(sidecarID)+suffix, strings.NewReader(body)))
	if recorder.Code != wantStatus {
		t.Fatalf("apply watchdog policy%s status = %d want=%d body=%s", suffix, recorder.Code, wantStatus, recorder.Body.String())
	}
	var response watchdogPolicyResponse
	if wantStatus == http.StatusOK {
		decodeWatchdogRouteResponse(t, recorder, &response)
	}
	return response
}

func decodeWatchdogRouteResponse(t *testing.T, recorder *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
		t.Fatalf("decode route response: %v body=%s", err, recorder.Body.String())
	}
}

func createWatchdogRouteAction(t *testing.T, service *Service, input SidecarWatchdogActionInput) SidecarWatchdogAction {
	t.Helper()
	action, err := service.store.CreateWatchdogAction(t.Context(), input)
	if err != nil {
		t.Fatalf("create watchdog action: %v", err)
	}
	return action
}
