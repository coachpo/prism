package sidecars

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type watchdogProbeTestUpstream struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex

	calls            []string
	fieldPatches     []int
	fieldPatchStatus int
	fieldPatchHook   func()
	authFiles        map[string]map[string]any
	authFilesPayload string
	authFilesCalls   int
	responses        map[string]watchdogProbeTestResponse
}

type watchdogProbeTestResponse struct {
	StatusCode int
	Body       string
	Delay      time.Duration
}

func newWatchdogProbeTestUpstream(t *testing.T) *watchdogProbeTestUpstream {
	t.Helper()
	upstream := &watchdogProbeTestUpstream{
		t:         t,
		authFiles: map[string]map[string]any{},
		responses: map[string]watchdogProbeTestResponse{},
	}
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.serveHTTP))
	return upstream
}

func (u *watchdogProbeTestUpstream) Close() { u.server.Close() }

func (u *watchdogProbeTestUpstream) URL() string { return u.server.URL }

func (u *watchdogProbeTestUpstream) setProbeResponse(authIndex string, response watchdogProbeTestResponse) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.responses[authIndex] = response
}

func (u *watchdogProbeTestUpstream) setAuthFile(authID string, authIndex string, provider string, priority int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authFiles[authID] = watchdogProbeAuthPayload(authID, authIndex, provider, priority)
}

func (u *watchdogProbeTestUpstream) setAuthFileRaw(authID string, file map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	clone := make(map[string]any, len(file))
	maps.Copy(clone, file)
	u.authFiles[authID] = clone
}

func (u *watchdogProbeTestUpstream) setFieldPatchStatus(status int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fieldPatchStatus = status
}

func (u *watchdogProbeTestUpstream) setFieldPatchHook(hook func()) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fieldPatchHook = hook
}

func (u *watchdogProbeTestUpstream) fieldPatchPriorities() []int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int(nil), u.fieldPatches...)
}

func (u *watchdogProbeTestUpstream) apiCallAuthIndexes() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.calls...)
}

func (u *watchdogProbeTestUpstream) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Management-Key") != "sync-secret" {
		u.t.Errorf("expected management key header, got %q", r.Header.Get("X-Management-Key"))
	}
	switch r.URL.Path {
	case "/v0/management/api-call":
		u.serveAPICall(w, r)
	case "/v0/management/auth-files":
		u.serveAuthFiles(w)
	case "/v0/management/auth-files/fields":
		u.serveFieldsPatch(w, r)
	default:
		u.t.Errorf("unexpected management path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *watchdogProbeTestUpstream) serveAPICall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		u.t.Errorf("expected POST /api-call, got %s", r.Method)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var request CLIProxyAPICallRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		u.t.Errorf("decode api-call request: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	u.mu.Lock()
	u.calls = append(u.calls, request.AuthIndex)
	response, ok := u.responses[request.AuthIndex]
	u.mu.Unlock()
	if !ok {
		response = watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()}
	}
	if response.Delay > 0 {
		time.Sleep(response.Delay)
	}
	writeWatchdogJSON(w, map[string]any{"status_code": response.StatusCode, "body": response.Body})
}

func (u *watchdogProbeTestUpstream) serveAuthFiles(w http.ResponseWriter) {
	u.mu.Lock()
	files := make([]map[string]any, 0, len(u.authFiles))
	for _, file := range u.authFiles {
		clone := make(map[string]any, len(file))
		maps.Copy(clone, file)
		files = append(files, clone)
	}
	u.mu.Unlock()
	writeWatchdogJSON(w, map[string]any{"files": files})
}

func (u *watchdogProbeTestUpstream) serveFieldsPatch(w http.ResponseWriter, r *http.Request) {
	var patch struct {
		Name     string `json:"name"`
		Priority *int   `json:"priority"`
	}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		u.t.Errorf("decode fields patch: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if patch.Priority == nil {
		u.t.Errorf("missing priority in fields patch: %+v", patch)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	u.mu.Lock()
	hook := u.fieldPatchHook
	u.mu.Unlock()
	if hook != nil {
		hook()
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fieldPatches = append(u.fieldPatches, *patch.Priority)
	if u.fieldPatchStatus >= 400 {
		w.WriteHeader(u.fieldPatchStatus)
		writeWatchdogJSON(w, map[string]any{"error": "injected patch failure"})
		return
	}
	for authID, file := range u.authFiles {
		if file["name"] == patch.Name {
			file["priority"] = *patch.Priority
			u.authFiles[authID] = file
			break
		}
	}
	writeWatchdogJSON(w, map[string]any{"status": "ok"})
}

func TestWatchdogProbeObservationDeprioritizesExhaustedQuota(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 0, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-probe-deprioritize", "idx-probe-deprioritize", "codex", 10)
	upstream.setProbeResponse("idx-probe-deprioritize", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogExhaustedUsageBody(resetAt)})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 81)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-probe-deprioritize", "idx-probe-deprioritize", "codex", 10)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile exhausted probe: %v", err)
	}
	if !result.Reconciled {
		t.Fatalf("expected exhausted probe to reconcile, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{0}) {
		t.Fatalf("expected one deprioritize patch, got %v", got)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 1 {
		t.Fatalf("expected active quota hold, holds=%+v err=%v", holds, err)
	}
	if holds[0].PreviousPriority == nil || *holds[0].PreviousPriority != 10 || holds[0].HoldUntil == nil || !holds[0].HoldUntil.Equal(resetAt) {
		t.Fatalf("hold did not preserve previous priority/reset time: %+v", holds[0])
	}
	observations, err := service.store.ListWatchdogProbeObservations(t.Context(), sidecar.ID, 10)
	if err != nil || len(observations) != 1 || observations[0].ProbeStatus != watchdogProbeStatusSucceeded || !observations[0].QuotaExceeded {
		t.Fatalf("expected persisted exhausted probe observation, observations=%+v err=%v", observations, err)
	}
	assertWatchdogAction(t, listWatchdogActions(t, service, sidecar.ID), watchdogActionDeprioritize, watchdogActionStatusSucceeded)
}

func TestWatchdogDeprioritizePersistsPendingDecisionBeforePriorityPatch(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 10, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-pending-deprioritize", "idx-pending-deprioritize", "codex", 10)
	upstream.setProbeResponse("idx-pending-deprioritize", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogExhaustedUsageBody(resetAt)})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 81)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-pending-deprioritize", "idx-pending-deprioritize", "codex", 10)
	pendingCheck := expectPendingPatchActionBeforeFieldPatch(upstream, service, sidecar.ID, watchdogActionDeprioritize, DefaultQuotaExceededPriority)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile exhausted probe: %v", err)
	}
	if !result.Reconciled {
		t.Fatalf("expected exhausted probe to reconcile, got %+v", result)
	}
	assertPendingPatchActionObserved(t, pendingCheck)
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{0}) {
		t.Fatalf("expected one deprioritize patch, got %v", got)
	}
	assertWatchdogAction(t, listWatchdogActions(t, service, sidecar.ID), watchdogActionDeprioritize, watchdogActionStatusSucceeded)
}

func TestWatchdogRestorePersistsPendingDecisionBeforePriorityPatch(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 20, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-pending-restore", "idx-pending-restore", "codex", 0)
	upstream.setProbeResponse("idx-pending-restore", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 82)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-pending-restore", "idx-pending-restore", now.Add(-time.Minute))
	pendingCheck := expectPendingPatchActionBeforeFieldPatch(upstream, service, sidecar.ID, watchdogActionRestore, 10)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile healthy restore probe: %v", err)
	}
	if !result.Reconciled || result.Restored != 1 {
		t.Fatalf("expected healthy probe to restore, got %+v", result)
	}
	assertPendingPatchActionObserved(t, pendingCheck)
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{10}) {
		t.Fatalf("expected one restore patch, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("restore should release hold, holds=%+v err=%v", holds, err)
	}
}

func TestWatchdogRepairsPendingDeprioritizeAction(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 25, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	tests := []struct {
		name         string
		livePriority int
		wantPatches  []int
	}{
		{name: "replays unapplied pending patch", livePriority: 10, wantPatches: []int{0}},
		{name: "finalizes already applied pending patch", livePriority: 0, wantPatches: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := newWatchdogProbeTestUpstream(t)
			defer upstream.Close()
			upstream.setAuthFile("auth-repair-deprioritize", "idx-repair-deprioritize", "codex", tt.livePriority)
			service := newWatchdogTestService(t, func() time.Time { return now })
			sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 83)
			enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
			reason := watchdogReasonQuotaExceeded
			previousPriority := 10
			targetPriority := DefaultQuotaExceededPriority
			authName := "auth-repair-deprioritize.json"
			pending, err := service.store.CreateWatchdogAction(t.Context(), SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-repair-deprioritize"), AuthName: &authName, AuthIndex: stringPtrFromNonEmpty("idx-repair-deprioritize"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionDeprioritize, Reason: &reason, PreviousPriority: &previousPriority, TargetPriority: &targetPriority, HoldUntil: &resetAt, Status: watchdogActionStatusPending})
			if err != nil {
				t.Fatalf("create pending deprioritize action: %v", err)
			}
			createWatchdogPendingQueueRow(t, service, pending)

			result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
			if err != nil {
				t.Fatalf("repair pending deprioritize: %v", err)
			}
			if !result.Reconciled || result.QuotaHeld != 1 {
				t.Fatalf("expected pending deprioritize repair to converge, got %+v", result)
			}
			if got := upstream.fieldPatchPriorities(); !slices.Equal(got, tt.wantPatches) {
				t.Fatalf("unexpected repaired deprioritize patches: got %v want %v", got, tt.wantPatches)
			}
			actions := listWatchdogActions(t, service, sidecar.ID)
			if len(actions) != 1 || actions[0].ID != pending.ID || actions[0].Status != watchdogActionStatusSucceeded || actions[0].CompletedAt == nil || actions[0].HoldID == nil {
				t.Fatalf("pending deprioritize action was not consumed in place: %+v", actions)
			}
			holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
			if err != nil || len(holds) != 1 {
				t.Fatalf("expected repaired deprioritize hold, holds=%+v err=%v", holds, err)
			}
			if holds[0].PreviousPriority == nil || *holds[0].PreviousPriority != previousPriority {
				t.Fatalf("repaired hold lost the previous priority state: %+v", holds[0])
			}
		})
	}
}

func TestWatchdogRepairPendingDeprioritizeConfirmsAuthName(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 26, 0, 0, time.UTC)
	resetAt := now.Add(2 * time.Hour)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	live := watchdogProbeAuthPayload("auth-repair-name", "idx-repair-name", "codex", 10)
	live["name"] = "renamed-auth.json"
	upstream.setAuthFileRaw("auth-repair-name", live)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 86)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	reason := watchdogReasonQuotaExceeded
	previousPriority := 10
	targetPriority := DefaultQuotaExceededPriority
	selectedName := "auth-repair-name.json"
	pending, err := service.store.CreateWatchdogAction(t.Context(), SidecarWatchdogActionInput{SidecarID: sidecar.ID, AuthID: stringPtrFromNonEmpty("auth-repair-name"), AuthName: &selectedName, AuthIndex: stringPtrFromNonEmpty("idx-repair-name"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionDeprioritize, Reason: &reason, PreviousPriority: &previousPriority, TargetPriority: &targetPriority, HoldUntil: &resetAt, Status: watchdogActionStatusPending})
	if err != nil {
		t.Fatalf("create pending deprioritize action: %v", err)
	}
	createWatchdogPendingQueueRow(t, service, pending)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("repair pending deprioritize with renamed auth: %v", err)
	}
	if result.Reconciled || result.QuotaHeld != 0 || result.ActionCount != 1 {
		t.Fatalf("renamed pending deprioritize must skip without reconciling, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("renamed pending deprioritize must not replay patch, got %v", got)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	if len(actions) != 1 || actions[0].ID != pending.ID || actions[0].Status != watchdogActionStatusSkipped || stringValue(actions[0].Reason) != "current auth name no longer matches selected auth" {
		t.Fatalf("pending deprioritize action did not record auth-name preflight skip: %+v", actions)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("auth-name mismatch must not create holds, holds=%+v err=%v", holds, err)
	}
}

func TestWatchdogRepairsPendingRestoreAction(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 27, 0, 0, time.UTC)
	tests := []struct {
		name         string
		livePriority int
		wantPatches  []int
	}{
		{name: "replays unapplied pending patch", livePriority: 0, wantPatches: []int{10}},
		{name: "finalizes already applied pending patch", livePriority: 10, wantPatches: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := newWatchdogProbeTestUpstream(t)
			defer upstream.Close()
			upstream.setAuthFile("auth-repair-restore", "idx-repair-restore", "codex", tt.livePriority)
			service := newWatchdogTestService(t, func() time.Time { return now })
			sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 84)
			enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
			hold := createWatchdogProbeHold(t, service, sidecar.ID, "auth-repair-restore", "idx-repair-restore", now.Add(-time.Minute))
			restorePriority := 10
			selectedName := "auth-repair-restore.json"
			pending, err := service.store.CreateWatchdogAction(t.Context(), SidecarWatchdogActionInput{SidecarID: sidecar.ID, HoldID: &hold.ID, AuthID: stringPtrFromNonEmpty("auth-repair-restore"), AuthName: &selectedName, AuthIndex: stringPtrFromNonEmpty("idx-repair-restore"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionRestore, Reason: &hold.Reason, PreviousPriority: &restorePriority, TargetPriority: &restorePriority, HoldUntil: hold.HoldUntil, Status: watchdogActionStatusPending})
			if err != nil {
				t.Fatalf("create pending restore action: %v", err)
			}
			createWatchdogPendingQueueRow(t, service, pending)

			result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
			if err != nil {
				t.Fatalf("repair pending restore: %v", err)
			}
			if !result.Reconciled || result.Restored != 1 {
				t.Fatalf("expected pending restore repair to converge, got %+v", result)
			}
			if got := upstream.fieldPatchPriorities(); !slices.Equal(got, tt.wantPatches) {
				t.Fatalf("unexpected repaired restore patches: got %v want %v", got, tt.wantPatches)
			}
			actions := listWatchdogActions(t, service, sidecar.ID)
			if len(actions) != 1 || actions[0].ID != pending.ID || actions[0].Status != watchdogActionStatusSucceeded || actions[0].CompletedAt == nil {
				t.Fatalf("pending restore action was not consumed in place: %+v", actions)
			}
			if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
				t.Fatalf("pending restore repair should release hold, holds=%+v err=%v", holds, err)
			}
		})
	}
}

func TestWatchdogRepairPendingRestoreConfirmsAuthName(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 28, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	live := watchdogProbeAuthPayload("auth-repair-restore-name", "idx-repair-restore-name", "codex", 0)
	live["name"] = "renamed-auth.json"
	upstream.setAuthFileRaw("auth-repair-restore-name", live)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 84)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	hold := createWatchdogProbeHold(t, service, sidecar.ID, "auth-repair-restore-name", "idx-repair-restore-name", now.Add(-time.Minute))
	restorePriority := 10
	selectedName := "auth-repair-restore-name.json"
	pending, err := service.store.CreateWatchdogAction(t.Context(), SidecarWatchdogActionInput{SidecarID: sidecar.ID, HoldID: &hold.ID, AuthID: stringPtrFromNonEmpty("auth-repair-restore-name"), AuthName: &selectedName, AuthIndex: stringPtrFromNonEmpty("idx-repair-restore-name"), Provider: stringPtrFromNonEmpty("codex"), ActionType: watchdogActionRestore, Reason: &hold.Reason, PreviousPriority: &restorePriority, TargetPriority: &restorePriority, HoldUntil: hold.HoldUntil, Status: watchdogActionStatusPending})
	if err != nil {
		t.Fatalf("create pending restore action: %v", err)
	}
	createWatchdogPendingQueueRow(t, service, pending)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("repair pending restore with renamed auth: %v", err)
	}
	if result.Reconciled || result.Restored != 0 || result.ActionCount != 1 {
		t.Fatalf("renamed pending restore must skip without reconciling, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("renamed pending restore must not replay patch, got %v", got)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	if len(actions) != 1 || actions[0].ID != pending.ID || actions[0].Status != watchdogActionStatusSkipped || stringValue(actions[0].Reason) != "current auth name no longer matches selected auth" {
		t.Fatalf("pending restore action did not record auth-name preflight skip: %+v", actions)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 1 {
		t.Fatalf("auth-name mismatch must preserve hold for manual review, holds=%+v err=%v", holds, err)
	}
}

func TestWatchdogProbeDeprioritizeIgnoresUpstreamNon200(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setProbeResponse("idx-probe-non200", watchdogProbeTestResponse{StatusCode: http.StatusTooManyRequests, Body: `{"error":"rate_limited"}`})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 82)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-probe-non200", "idx-probe-non200", "codex", 10)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile non-200 probe: %v", err)
	}
	if !result.Skipped || result.SkipReason != "no_watchdog_action_needed" {
		t.Fatalf("non-200 probe should only persist the failed observation, got %+v", result)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("non-200 probe must not patch priority, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("non-200 probe must not create holds, holds=%+v err=%v", holds, err)
	}
	observations, err := service.store.ListWatchdogProbeObservations(t.Context(), sidecar.ID, 10)
	if err != nil || len(observations) != 1 || observations[0].ProbeStatus != watchdogProbeStatusFailedStatus || observations[0].UpstreamStatusCode == nil || *observations[0].UpstreamStatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected exact failed-status observation, observations=%+v err=%v", observations, err)
	}
}

func TestWatchdogProbeDeprioritizeValidatesLiveAuthBeforePatch(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 45, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	live := watchdogProbeAuthPayload("auth-probe-mismatch", "idx-probe-mismatch", "codex", 10)
	live["name"] = "renamed-auth.json"
	upstream.setAuthFileRaw("auth-probe-mismatch", live)
	upstream.setProbeResponse("idx-probe-mismatch", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogExhaustedUsageBody(resetAt)})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 83)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-probe-mismatch", "idx-probe-mismatch", "codex", 10)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile mismatched live auth: %v", err)
	}
	if result.Reconciled || len(upstream.fieldPatchPriorities()) != 0 {
		t.Fatalf("mismatched live auth must not patch, result=%+v patches=%v", result, upstream.fieldPatchPriorities())
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("mismatched live auth must not create holds, holds=%+v err=%v", holds, err)
	}
	assertWatchdogAction(t, listWatchdogActions(t, service, sidecar.ID), watchdogActionDeprioritize, watchdogActionStatusSkipped)
}

func TestRepairDeprioritizeWatchdogAvoidsDuplicatePatch(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 15, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-probe-repair", "idx-probe-repair", "codex", 10)
	upstream.setProbeResponse("idx-probe-repair", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogExhaustedUsageBody(resetAt)})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 84)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-probe-repair", "idx-probe-repair", "codex", 10)
	service.store = &failingProbeDecisionStore{persistence: service.store, failCreateHoldDecisions: 1}

	_, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if !errors.Is(err, errInjectedProbeDecisionFailure) {
		t.Fatalf("expected injected final hold write failure, got %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{0}) {
		t.Fatalf("first run should patch once before final DB failure, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("failed final hold write should not leave an active hold, holds=%+v err=%v", holds, err)
	}

	now = now.Add(time.Minute)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("repair reconcile: %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{0}) {
		t.Fatalf("repair must not issue a duplicate patch, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 1 {
		t.Fatalf("repair should create the active hold, holds=%+v err=%v", holds, err)
	}
}

func TestRepairRestoreWatchdogAvoidsDuplicatePatch(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-restore-repair", "idx-restore-repair", "codex", 0)
	upstream.setProbeResponse("idx-restore-repair", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 86)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-restore-repair", "idx-restore-repair", now.Add(-time.Minute))
	service.store = &failingProbeDecisionStore{persistence: service.store, failUpdateHoldDecisions: 1}

	_, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if !errors.Is(err, errInjectedProbeDecisionFailure) {
		t.Fatalf("expected injected final restore write failure, got %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{10}) {
		t.Fatalf("first restore should patch once before final DB failure, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 1 {
		t.Fatalf("failed final release write should leave the active hold for repair, holds=%+v err=%v", holds, err)
	}

	now = now.Add(time.Minute)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("restore repair reconcile: %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{10}) {
		t.Fatalf("restore repair must not issue a duplicate patch, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("restore repair should release the hold, holds=%+v err=%v", holds, err)
	}
	if got := countWatchdogActions(listWatchdogActions(t, service, sidecar.ID), watchdogActionRestoreSkippedManualChange); got != 0 {
		t.Fatalf("restore repair must not be treated as manual override, got %d manual-change actions", got)
	}
}

func TestWatchdogPatchRepairDoesNotLeaveActiveHoldWhenDeprioritizePatchFails(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 45, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-probe-patch-fails", "idx-probe-patch-fails", "codex", 10)
	upstream.setProbeResponse("idx-probe-patch-fails", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogExhaustedUsageBody(resetAt)})
	upstream.setFieldPatchStatus(http.StatusInternalServerError)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 85)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-probe-patch-fails", "idx-probe-patch-fails", "codex", 10)

	_, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err == nil {
		t.Fatalf("expected patch failure")
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{0}) {
		t.Fatalf("expected exactly one attempted patch, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("failed patch must not leave active hold, holds=%+v err=%v", holds, err)
	}
	assertWatchdogAction(t, listWatchdogActions(t, service, sidecar.ID), watchdogActionDeprioritize, watchdogActionStatusFailed)
}

func TestPrivacyRawProbeWatchdogDoesNotPersistBodiesHeadersTokens(t *testing.T) {
	now := time.Date(2026, time.May, 11, 13, 15, 0, 0, time.UTC)
	resetAt := now.Add(time.Hour)
	forbidden := []string{"probe-body-secret", "probe-token-secret", "acct-probe-secret", "person@example.test", "Chatgpt-Account-Id", "Authorization"}
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-privacy-probe", "idx-privacy-probe", "codex", 10)
	upstream.setProbeResponse("idx-privacy-probe", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogSensitiveUsageBody(t, resetAt)})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 87)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-privacy-probe", "idx-privacy-probe", "codex", 10)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil || !result.Reconciled || result.Probed != 1 || result.QuotaHeld != 1 {
		t.Fatalf("expected sensitive raw probe to reconcile with sanitized summary, result=%+v err=%v", result, err)
	}
	observations, err := service.store.ListWatchdogProbeObservations(t.Context(), sidecar.ID, 10)
	if err != nil || len(observations) != 1 {
		t.Fatalf("expected one sanitized probe observation, observations=%+v err=%v", observations, err)
	}
	assertNoWatchdogSensitiveStrings(t, observations, forbidden)
	actions := listWatchdogActions(t, service, sidecar.ID)
	assertWatchdogAction(t, actions, watchdogActionDeprioritize, watchdogActionStatusSucceeded)
	assertNoWatchdogSensitiveStrings(t, actions, forbidden)
	publicActions := make([]actionRecordResponse, 0, len(actions))
	for _, action := range actions {
		publicActions = append(publicActions, buildActionRecordResponse(action))
	}
	assertNoWatchdogSensitiveStrings(t, publicActions, forbidden)
	message := watchdogErrorMessage(errors.New(`api-call failed with body probe-body-secret headers Authorization Bearer probe-token-secret for person@example.test`))
	assertNoWatchdogSensitiveStrings(t, message, forbidden)
}

func TestLeakUnsupportedProviderWatchdogSkipDoesNotFloodActionHistory(t *testing.T) {
	now := time.Date(2026, time.May, 11, 13, 30, 0, 0, time.UTC)
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 0, Provider: "gemini"})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 88)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	previousPriority := 10
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("gemini"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-unsupported-flood", PreviousPriority: &previousPriority, TargetPriority: 0, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create unsupported provider hold: %v", err)
	}

	first, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil || first.UnsupportedSkipped != 1 || first.ActionCount != 1 || first.Reconciled {
		t.Fatalf("expected first unsupported skip to record once without reconcile, result=%+v err=%v", first, err)
	}
	second, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil || second.UnsupportedSkipped != 1 || second.ActionCount != 0 || second.Reconciled {
		t.Fatalf("expected repeated unsupported skip to be deduped, result=%+v err=%v", second, err)
	}
	if got := countWatchdogActions(listWatchdogActions(t, service, sidecar.ID), watchdogProbeStatusSkippedUnsupportedProvider); got != 1 {
		t.Fatalf("unsupported provider skips should not flood action history, got %d", got)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("unsupported provider skip must not patch priority, got %v", got)
	}
}

func TestWatchdogDiscoveryUnsupportedProviderSkipsRecordActionHistoryOnce(t *testing.T) {
	now := time.Date(2026, time.May, 11, 15, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 89)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported-a", "idx-unsupported-a", "gemini", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported-b", "idx-unsupported-b", "gemini", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported-c", "idx-unsupported-c", "openai", 10)

	first, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil || first.UnsupportedSkipped != 3 || first.ActionCount != 2 || first.Reconciled {
		t.Fatalf("expected provider-grouped unsupported discovery skips, result=%+v err=%v", first, err)
	}
	second, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil || second.UnsupportedSkipped != 3 || second.ActionCount != 0 || second.Reconciled {
		t.Fatalf("expected repeated discovery skips to dedupe action history, result=%+v err=%v", second, err)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	if got := countWatchdogActions(actions, watchdogProbeStatusSkippedUnsupportedProvider); got != 2 {
		t.Fatalf("unsupported discovery skips should record once per provider, got %d actions=%+v", got, actions)
	}
	providers := map[string]bool{}
	for _, action := range actions {
		if action.ActionType == watchdogProbeStatusSkippedUnsupportedProvider {
			providers[stringValue(action.Provider)] = true
		}
	}
	if !providers["gemini"] || !providers["openai"] || len(providers) != 2 {
		t.Fatalf("unsupported discovery skip providers were not represented safely: %+v actions=%+v", providers, actions)
	}
	if calls := upstream.apiCallAuthIndexes(); len(calls) != 0 {
		t.Fatalf("unsupported discovery skips must not call api probes, got %v", calls)
	}
}

func TestWatchdogBatchBudgetIsPerSidecar(t *testing.T) {
	now := time.Date(2026, time.May, 11, 12, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecarA := createSyncTestSidecar(t, service, upstream.URL(), true, 71)
	sidecarB := createSyncTestSidecar(t, service, upstream.URL(), true, 72)
	enableWatchdogProbePolicy(t, service, sidecarA.ID, 1, 5)
	enableWatchdogProbePolicy(t, service, sidecarB.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecarA.ID, now)
	markWatchdogSnapshotsFresh(t, service, sidecarB.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecarA.ID, now, "auth-a1", "idx-a1", "codex", 10)
	seedWatchdogProbeSnapshot(t, service, sidecarA.ID, now, "auth-a2", "idx-a2", "codex", 10)
	seedWatchdogProbeSnapshot(t, service, sidecarB.ID, now, "auth-b1", "idx-b1", "codex", 10)
	seedWatchdogProbeSnapshot(t, service, sidecarB.ID, now, "auth-b2", "idx-b2", "codex", 10)

	summary, err := service.ReconcileWatchdogDueSidecars(t.Context())
	if err != nil {
		t.Fatalf("reconcile due sidecars: %v", err)
	}
	if summary.Checked != 2 {
		t.Fatalf("expected two checked sidecars, got %+v", summary)
	}
	calls := upstream.apiCallAuthIndexes()
	if len(calls) != 2 || !slices.Contains(calls, "idx-a1") || !slices.Contains(calls, "idx-b1") {
		t.Fatalf("expected one discovery probe per sidecar, got %v", calls)
	}
}

func TestWatchdogDueHoldBypassesStaleSnapshotForProbe(t *testing.T) {
	now := time.Date(2026, time.May, 11, 13, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setProbeResponse("idx-due", watchdogProbeTestResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"upstream"}`})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 73)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsStale(t, service, sidecar.ID, now)
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-due", "idx-due", now.Add(-time.Hour))

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile stale due hold: %v", err)
	}
	if result.SkipReason == watchdogActionSkippedStaleSnapshot {
		t.Fatalf("due probe must bypass stale snapshot skip, got %+v", result)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-due"}) {
		t.Fatalf("expected stale due hold probe, got %v", calls)
	}
	observations, err := service.store.ListWatchdogProbeObservations(t.Context(), sidecar.ID, 10)
	if err != nil || len(observations) != 1 {
		t.Fatalf("expected persisted due probe observation, observations=%+v err=%v", observations, err)
	}
	if got := countWatchdogActions(listWatchdogActions(t, service, sidecar.ID), watchdogActionSkippedStaleSnapshot); got != 0 {
		t.Fatalf("stale skip action must not be recorded when due probe runs, got %d", got)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 1 || holds[0].HoldUntil == nil || !holds[0].HoldUntil.After(now) {
		t.Fatalf("failed due hold should receive retry cooldown, holds=%+v err=%v", holds, err)
	}
}

func TestWatchdogRollingRefreshOrderingPrefersOldestEligibleAuth(t *testing.T) {
	now := time.Date(2026, time.May, 11, 13, 45, 0, 0, time.UTC)
	oldest := now.Add(-3 * time.Hour)
	older := now.Add(-2 * time.Hour)
	tie := now.Add(-90 * time.Minute)
	fresh := now.Add(-10 * time.Minute)
	policy := SidecarWatchdogPolicy{UsingPriority: 5, RollingRefreshEnabled: true, RollingRefreshAfterSeconds: 3600}
	snapshots := []SidecarAuthSnapshot{
		{AuthID: "auth-tie-b", AuthIndex: stringPtr("idx-tie-b"), Provider: stringPtr("codex"), Priority: intPtr(10)},
		{AuthID: "auth-low", AuthIndex: stringPtr("idx-low"), Provider: stringPtr("codex"), Priority: intPtr(1)},
		{AuthID: "auth-fresh", AuthIndex: stringPtr("idx-fresh"), Provider: stringPtr("codex"), Priority: intPtr(10)},
		{AuthID: "auth-never", AuthIndex: stringPtr("idx-never"), Provider: stringPtr("codex"), Priority: intPtr(10)},
		{AuthID: "auth-held", AuthIndex: stringPtr("idx-held"), Provider: stringPtr("codex"), Priority: intPtr(10)},
		{AuthID: "auth-oldest", AuthIndex: stringPtr("idx-oldest"), Provider: stringPtr("codex"), Priority: intPtr(10)},
		{AuthID: "auth-tie-a", AuthIndex: stringPtr("idx-tie-a"), Provider: stringPtr("codex"), Priority: intPtr(10)},
		{AuthID: "auth-older", AuthIndex: stringPtr("idx-older"), Provider: stringPtr("codex"), Priority: intPtr(10)},
	}
	quotaStates := map[string]SidecarAuthQuotaState{
		"auth-fresh":  {AuthID: "auth-fresh", LastProbedAt: &fresh},
		"auth-oldest": {AuthID: "auth-oldest", LastProbedAt: &oldest},
		"auth-older":  {AuthID: "auth-older", LastProbedAt: &older},
		"auth-tie-a":  {AuthID: "auth-tie-a", LastProbedAt: &tie},
		"auth-tie-b":  {AuthID: "auth-tie-b", LastProbedAt: &tie},
	}

	candidates := watchdogRollingRefreshProbeCandidates(policy, snapshots, map[string]struct{}{"auth-held": struct{}{}}, quotaStates, now)
	got := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		got = append(got, candidate.AuthID)
	}
	want := []string{"auth-never", "auth-oldest", "auth-older", "auth-tie-a", "auth-tie-b"}
	if !slices.Equal(got, want) {
		t.Fatalf("rolling refresh candidates = %v, want %v", got, want)
	}
}

func TestWatchdogBatchCompletionAdvancesOnlyAfterPersistedProbeAttempt(t *testing.T) {
	now := time.Date(2026, time.May, 11, 14, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 74)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported", "idx-unsupported", "gemini", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-missing-index", "", "codex", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-probe", "idx-probe", "codex", 10)

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("reconcile discovery batch: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-probe"}) {
		t.Fatalf("unsupported and missing-index snapshots must not consume budget, got %v", calls)
	}
	policy, err := service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecar.ID)
	if err != nil || policy.ProbeLastBatchCompletedAt == nil {
		t.Fatalf("persisted discovery probe should stamp hidden batch completion marker, policy=%+v err=%v", policy, err)
	}

	now = now.Add(time.Minute)
	upstream.setProbeResponse("idx-due-batch", watchdogProbeTestResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"upstream"}`})
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-due-batch", "idx-due-batch", now.Add(-time.Minute))
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("reconcile due-only batch: %v", err)
	}
	policy, err = service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecar.ID)
	if err != nil || policy.ProbeLastBatchCompletedAt == nil {
		t.Fatalf("due-hold-only run must still leave hidden batch completion recorded, policy=%+v err=%v", policy, err)
	}
}

func TestWatchdogProbeBatchCooldownGatesQuotaProbesOnly(t *testing.T) {
	now := time.Date(2026, time.May, 11, 14, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 75)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-first", "idx-first", "codex", 10)

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("reconcile initial probe batch: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-first"}) {
		t.Fatalf("initial batch should probe first auth once, got %v", calls)
	}
	policy, err := service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecar.ID)
	if err != nil || policy.ProbeLastBatchCompletedAt == nil {
		t.Fatalf("initial probe should stamp cooldown state, policy=%+v err=%v", policy, err)
	}
	firstBatchCompletedAt := *policy.ProbeLastBatchCompletedAt

	now = now.Add(10 * time.Second)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	upstream.setAuthFile("auth-due", "idx-due", "codex", DefaultQuotaExceededPriority)
	upstream.setAuthFile("auth-failure", "idx-failure", "codex", DefaultQuotaExceededPriority)
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-due", "idx-due", now.Add(-time.Minute))
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-second", "idx-second", "codex", 10)
	seedWatchdogFailureThresholdSnapshot(t, service, sidecar.ID, now, "auth-failure", "idx-failure", "codex", 10, DefaultFailureThreshold)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile within cooldown: %v", err)
	}
	if !result.Reconciled {
		t.Fatalf("failure-threshold work should still run inside probe cooldown, got %+v", result)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-first"}) {
		t.Fatalf("cooldown must suppress due-hold and discovery api probes, got %v", calls)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("already-deprioritized failure threshold should not patch priority, got %v", got)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || !watchdogTestHasActiveHold(holds, "auth-failure") {
		t.Fatalf("non-probe failure threshold should still create a hold, holds=%+v err=%v", holds, err)
	}
	policy, err = service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecar.ID)
	if err != nil || policy.ProbeLastBatchCompletedAt == nil || !policy.ProbeLastBatchCompletedAt.Equal(firstBatchCompletedAt) {
		t.Fatalf("cooldown-skipped tick must not advance batch completion, policy=%+v err=%v", policy, err)
	}

	now = now.Add(25 * time.Second)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("reconcile after cooldown: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-first", "idx-due"}) {
		t.Fatalf("due hold should probe first after cooldown reopens, got %v", calls)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{10}) {
		t.Fatalf("due hold restore should resume after cooldown reopens, got %v", got)
	}
}

func TestWatchdogFailureThresholdStillRunsWhenSnapshotsAreStaleOrPaused(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(*testing.T, *Service, int, time.Time)
	}{
		{name: "stale snapshots", prepare: func(t *testing.T, service *Service, sidecarID int, now time.Time) {
			markWatchdogSnapshotsStale(t, service, sidecarID, now)
		}},
		{name: "management auth paused", prepare: func(t *testing.T, service *Service, sidecarID int, now time.Time) {
			pauseUntil := now.Add(time.Hour)
			staleAfter := now.Add(2 * time.Hour)
			_, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecarID, LastSyncAt: now, LastSuccessfulSyncAt: &now, SnapshotStaleAfter: &staleAfter, ManagementAuthState: ManagementAuthStateValid, AuthFailurePauseUntil: &pauseUntil})
			if err != nil {
				t.Fatalf("mark watchdog snapshots paused: %v", err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, time.May, 11, 15, 0, 0, 0, time.UTC)
			upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: 20, FailureCount: DefaultFailureThreshold})
			defer upstream.Close()
			service := newWatchdogTestService(t, func() time.Time { return now })
			sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
			enableWatchdogPolicy(t, service, sidecar.ID)
			seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 20, FailureCount: DefaultFailureThreshold})
			tc.prepare(t, service, sidecar.ID, now)

			result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
			if err != nil {
				t.Fatalf("reconcile %s: %v", tc.name, err)
			}
			if result.Skipped {
				t.Fatalf("failure-threshold work should not be skipped for %s, got %+v", tc.name, result)
			}
			if !result.Reconciled {
				t.Fatalf("failure-threshold work should still reconcile for %s, got %+v actions=%+v", tc.name, result, listWatchdogActions(t, service, sidecar.ID))
			}
			holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
			if err != nil {
				t.Fatalf("list active holds for %s: %v", tc.name, err)
			}
			if !watchdogTestHasActiveHold(holds, watchdogAuthID) {
				t.Fatalf("expected failure-threshold hold for %s, holds=%+v", tc.name, holds)
			}
		})
	}
}

func TestWatchdogTimeoutUsesWorkerBudgetSafetyMargin(t *testing.T) {
	startedAt := time.Date(2026, time.May, 11, 15, 0, 0, 0, time.UTC)
	policy := SidecarWatchdogPolicy{ProbeBatchSize: 1, ProbeTimeoutSeconds: 20}
	timeout, ok := watchdogEffectiveProbeTimeout(policy, startedAt, startedAt)
	if !ok || timeout != 20*time.Second {
		t.Fatalf("initial effective timeout = %v ok=%v, want 20s true", timeout, ok)
	}
	timeout, ok = watchdogEffectiveProbeTimeout(policy, startedAt, startedAt.Add(10*time.Second))
	if !ok || timeout != 15*time.Second {
		t.Fatalf("remaining-budget effective timeout = %v ok=%v, want 15s true", timeout, ok)
	}
	if timeout, ok = watchdogEffectiveProbeTimeout(policy, startedAt, startedAt.Add(26*time.Second)); ok || timeout != 0 {
		t.Fatalf("expired worker budget timeout = %v ok=%v, want 0 false", timeout, ok)
	}
	if err := validateWatchdogProbeRuntimePolicy(SidecarWatchdogPolicy{ProbeBatchSize: 2, ProbeTimeoutSeconds: 13}); !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected oversized worker budget validation error, got %v", err)
	}
}

func TestWatchdogDueHoldFailureCooldownPreventsStarvation(t *testing.T) {
	now := time.Date(2026, time.May, 11, 16, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setProbeResponse("idx-starve-1", watchdogProbeTestResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"upstream"}`})
	upstream.setProbeResponse("idx-starve-2", watchdogProbeTestResponse{StatusCode: http.StatusInternalServerError, Body: `{"error":"upstream"}`})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 75)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-starve-1", "idx-starve-1", now.Add(-2*time.Hour))
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-starve-2", "idx-starve-2", now.Add(-time.Hour))

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("first starvation reconcile: %v", err)
	}
	now = now.Add(time.Duration(DefaultProbeBatchCooldownSeconds+1) * time.Second)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("second starvation reconcile: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-starve-1", "idx-starve-2"}) {
		t.Fatalf("failed first due hold should cool down so second can run after batch cooldown, got %v", calls)
	}
}

func TestManualQuotaScanIsAsyncResumableAndCancellable(t *testing.T) {
	now := time.Date(2026, time.May, 11, 17, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-a-low", "idx-low", "codex", 0)
	upstream.setAuthFile("auth-z-high", "idx-high", "codex", 0)
	upstream.setProbeResponse("idx-low", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	upstream.setProbeResponse("idx-high", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 76)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-a-low", "idx-low", "codex", 0)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-z-high", "idx-high", "codex", 0)
	snapshots, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("list auth snapshots: %v", err)
	}
	if err := service.materializeAuthQuotaStates(t.Context(), sidecar.ID, snapshots, now); err != nil {
		t.Fatalf("materialize quota states: %v", err)
	}
	stateStore, ok := service.store.(authQuotaStateStore)
	if !ok {
		t.Fatalf("store does not support quota state updates")
	}
	for _, snapshot := range []string{"auth-a-low", "auth-z-high"} {
		state, err := stateStore.UpsertAuthQuotaState(t.Context(), SidecarAuthQuotaStateInput{SidecarID: sidecar.ID, AuthID: snapshot, QuotaBand: quotaBandError, ReasonCode: stringPtr("healthy")})
		if err != nil || state.QuotaBand != quotaBandError || state.ReasonCode == nil || *state.ReasonCode != "healthy" {
			t.Fatalf("seed healthy quota state for %s: state=%+v err=%v", snapshot, state, err)
		}
	}
	scanRun, err := service.StartManualQuotaScan(t.Context(), sidecar.ID, nil, false)
	if err != nil {
		t.Fatalf("start manual quota scan: %v", err)
	}
	if scanRun.Status != quotaScanStatusQueued || scanRun.PlannedCount != 2 {
		t.Fatalf("manual quota scan not queued with planned inventory: %+v", scanRun)
	}
	if calls := upstream.apiCallAuthIndexes(); len(calls) != 0 {
		t.Fatalf("manual quota scan must not probe before reconcile, got %v", calls)
	}
	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("first manual quota reconcile: %v", err)
	}
	if !slices.Equal(upstream.apiCallAuthIndexes(), []string{"idx-low"}) {
		t.Fatalf("manual quota scan must include low priority auth first, got %v", upstream.apiCallAuthIndexes())
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("healthy manual scan must not reprioritize authfiles, got %v", got)
	}
	if result.Probed != 1 {
		t.Fatalf("expected one manual probe on first reconcile, got %+v", result)
	}
	reloadedRun, ok, err := service.store.(quotaScanRunPersistence).GetQuotaScanRun(t.Context(), sidecar.ID, scanRun.ID)
	if err != nil || !ok || reloadedRun.AttemptedCount != 1 || reloadedRun.Status != quotaScanStatusRunning {
		t.Fatalf("manual scan should stay resumable after one tick: run=%+v ok=%v err=%v", reloadedRun, ok, err)
	}
	cancelled, err := service.CancelQuotaScanRun(t.Context(), sidecar.ID, scanRun.ID)
	if err != nil {
		t.Fatalf("cancel manual quota scan: %v", err)
	}
	if cancelled.Status != quotaScanStatusCancelled || cancelled.CompletedAt == nil || cancelled.CancelRequestedAt == nil {
		t.Fatalf("manual quota scan cancellation not persisted: %+v", cancelled)
	}
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("post-cancel manual quota reconcile: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-low"}) {
		t.Fatalf("cancelled manual scan must stop future probe selection, got %v", calls)
	}
	if got := upstream.fieldPatchPriorities(); len(got) != 0 {
		t.Fatalf("cancelled manual scan must not reprioritize authfiles, got %v", got)
	}
}

func expectPendingPatchActionBeforeFieldPatch(upstream *watchdogProbeTestUpstream, service *Service, sidecarID int, actionType string, targetPriority int) <-chan error {
	pendingCheck := make(chan error, 1)
	upstream.setFieldPatchHook(func() {
		select {
		case pendingCheck <- pendingPatchActionVisible(service, sidecarID, actionType, targetPriority):
		default:
		}
	})
	return pendingCheck
}

func assertPendingPatchActionObserved(t *testing.T, pendingCheck <-chan error) {
	t.Helper()
	select {
	case err := <-pendingCheck:
		if err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("field patch happened without observing a pending watchdog decision")
	}
}

func createWatchdogPendingQueueRow(t *testing.T, service *Service, action SidecarWatchdogAction) {
	t.Helper()
	_, err := service.store.CreateWatchdogPendingAction(t.Context(), SidecarWatchdogPendingActionInput{SidecarID: action.SidecarID, HoldID: cloneIntPtr(action.HoldID), ActionHistoryCreatedAt: action.CreatedAt, ActionHistoryID: action.ID, AuthID: cloneStringPtr(action.AuthID), AuthName: cloneStringPtr(action.AuthName), AuthIndex: cloneStringPtr(action.AuthIndex), Provider: cloneStringPtr(action.Provider), ActionType: action.ActionType, Reason: cloneStringPtr(action.Reason), PreviousPriority: cloneIntPtr(action.PreviousPriority), TargetPriority: cloneIntPtr(action.TargetPriority), HoldUntil: cloneTimePtr(action.HoldUntil)})
	if err != nil {
		t.Fatalf("create pending watchdog queue row: %v", err)
	}
}

func pendingPatchActionVisible(service *Service, sidecarID int, actionType string, targetPriority int) error {
	lister, ok := service.store.(actionHistoryPersistence)
	if !ok {
		return errors.New("store does not support action history")
	}
	actions, err := lister.ListWatchdogActions(context.Background(), sidecarID)
	if err != nil {
		return err
	}
	for _, action := range actions {
		if action.ActionType != actionType || action.Status != watchdogActionStatusPending {
			continue
		}
		if action.CompletedAt != nil {
			return fmt.Errorf("pending watchdog action already completed: %+v", action)
		}
		if action.TargetPriority == nil || *action.TargetPriority != targetPriority {
			return fmt.Errorf("pending watchdog action target mismatch: %+v", action)
		}
		if strings.TrimSpace(stringValue(action.AuthName)) == "" {
			return fmt.Errorf("pending watchdog action missing durable auth name: %+v", action)
		}
		return nil
	}
	return fmt.Errorf("missing pending %s decision before priority patch in %+v", actionType, actions)
}

func enableWatchdogProbePolicy(t *testing.T, service *Service, sidecarID int, batchSize int, timeoutSeconds int) {
	t.Helper()
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecarID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, UsingPriority: DefaultUsingPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeBatchSize: batchSize, ProbeTimeoutSeconds: timeoutSeconds})
	if err != nil {
		t.Fatalf("enable probe watchdog policy: %v", err)
	}
}

func seedWatchdogProbeSnapshot(t *testing.T, service *Service, sidecarID int, observedAt time.Time, authID string, authIndex string, provider string, priority int) {
	t.Helper()
	snapshotJSON, err := json.Marshal(watchdogProbeAuthPayload(authID, authIndex, provider, priority))
	if err != nil {
		t.Fatalf("marshal probe snapshot: %v", err)
	}
	disabled := false
	unavailable := false
	_, err = service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecarID, AuthID: authID, AuthIndex: stringPtrFromNonEmpty(authIndex), Name: authID + ".json", Provider: stringPtrFromNonEmpty(provider), Label: stringPtrFromNonEmpty(authID), Status: stringPtrFromNonEmpty("active"), Disabled: &disabled, Unavailable: &unavailable, Priority: &priority, RecentRequestsJSON: json.RawMessage(`[]`), ModelStatesJSON: json.RawMessage(`{}`), SnapshotJSON: snapshotJSON, ObservedAt: observedAt})
	if err != nil {
		t.Fatalf("save probe snapshot: %v", err)
	}
}

func seedWatchdogFailureThresholdSnapshot(t *testing.T, service *Service, sidecarID int, observedAt time.Time, authID string, authIndex string, provider string, priority int, failureCount int) {
	t.Helper()
	recentRequests, err := json.Marshal([]map[string]any{{"window_start": observedAt.Add(-time.Minute).Format(time.RFC3339), "window_end": observedAt.Format(time.RFC3339), "failure_count": failureCount}})
	if err != nil {
		t.Fatalf("marshal failure threshold requests: %v", err)
	}
	snapshotJSON, err := json.Marshal(watchdogProbeAuthPayload(authID, authIndex, provider, priority))
	if err != nil {
		t.Fatalf("marshal failure threshold snapshot: %v", err)
	}
	disabled := false
	unavailable := false
	_, err = service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecarID, AuthID: authID, AuthIndex: stringPtrFromNonEmpty(authIndex), Name: authID + ".json", Provider: stringPtrFromNonEmpty(provider), Label: stringPtrFromNonEmpty(authID), Status: stringPtrFromNonEmpty("active"), Disabled: &disabled, Unavailable: &unavailable, Priority: &priority, FailedCount: &failureCount, RecentRequestsJSON: recentRequests, ModelStatesJSON: json.RawMessage(`{}`), SnapshotJSON: snapshotJSON, ObservedAt: observedAt})
	if err != nil {
		t.Fatalf("save failure threshold snapshot: %v", err)
	}
}

func createWatchdogProbeHold(t *testing.T, service *Service, sidecarID int, authID string, authIndex string, holdUntil time.Time) SidecarWatchdogHold {
	t.Helper()
	previousPriority := 10
	hold, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecarID, AuthID: authID, AuthIndex: stringPtrFromNonEmpty(authIndex), Provider: stringPtrFromNonEmpty("codex"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-" + authID, PreviousPriority: &previousPriority, TargetPriority: DefaultQuotaExceededPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
	if err != nil {
		t.Fatalf("create probe hold: %v", err)
	}
	return hold
}

func watchdogTestHasActiveHold(holds []SidecarWatchdogHold, authID string) bool {
	for _, hold := range holds {
		if hold.AuthID == authID && hold.Status == WatchdogHoldStatusActive {
			return true
		}
	}
	return false
}

func markWatchdogSnapshotsStale(t *testing.T, service *Service, sidecarID int, now time.Time) {
	t.Helper()
	staleAt := now.Add(-time.Hour)
	_, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecarID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAt, ManagementAuthState: ManagementAuthStateValid})
	if err != nil {
		t.Fatalf("mark watchdog snapshots stale: %v", err)
	}
}

func watchdogProbeAuthPayload(authID string, authIndex string, provider string, priority int) map[string]any {
	payload := map[string]any{"id": authID, "name": authID + ".json", "provider": provider, "label": authID, "status": "active", "disabled": false, "unavailable": false, "priority": priority, "recent_requests": []any{}}
	if authIndex != "" {
		payload["auth_index"] = authIndex
	}
	return payload
}

func watchdogHealthyUsageBody() string {
	return `{"rate_limit":{"allowed":true,"primary_window":{"used_percent":5,"limit_window_seconds":18000}}}`
}

func watchdogExhaustedUsageBody(resetAt time.Time) string {
	payload := map[string]any{"rate_limit": map[string]any{"allowed": false, "primary_window": map[string]any{"limit_reached": true, "limit_window_seconds": 18000, "reset_at": resetAt.Unix()}}}
	body, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(body)
}

func watchdogSensitiveUsageBody(t *testing.T, resetAt time.Time) string {
	t.Helper()
	payload := map[string]any{
		"account_id": "acct-probe-secret",
		"body":       "probe-body-secret",
		"email":      "person@example.test",
		"headers": map[string]any{
			"Authorization":      "Bearer probe-token-secret",
			"Chatgpt-Account-Id": "acct-probe-secret",
		},
		"rate_limit": map[string]any{"allowed": false, "primary_window": map[string]any{"limit_reached": true, "limit_window_seconds": 18000, "reset_at": resetAt.Unix()}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal sensitive usage body: %v", err)
	}
	return string(body)
}

func assertNoWatchdogSensitiveStrings(t *testing.T, value any, forbidden []string) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal leak assertion payload: %v", err)
	}
	text := string(payload)
	for _, secret := range forbidden {
		if strings.Contains(text, secret) {
			t.Fatalf("watchdog value leaked %q in %s", secret, text)
		}
	}
}

var errInjectedProbeDecisionFailure = errors.New("injected probe decision failure")

type failingProbeDecisionStore struct {
	persistence
	failCreateHoldDecisions int
	failUpdateHoldDecisions int
}

func (s *failingProbeDecisionStore) PersistWatchdogProbeDecision(ctx context.Context, decision SidecarWatchdogProbeDecision) (SidecarWatchdogProbeDecisionResult, error) {
	return s.PersistQuotaProbeDecision(ctx, decision)
}

func (s *failingProbeDecisionStore) PersistQuotaProbeDecision(ctx context.Context, decision SidecarQuotaPersistDecision) (SidecarQuotaPersistResult, error) {
	if decision.CreateHold != nil && s.failCreateHoldDecisions > 0 {
		s.failCreateHoldDecisions--
		return SidecarQuotaPersistResult{}, errInjectedProbeDecisionFailure
	}
	if decision.UpdateHold != nil && s.failUpdateHoldDecisions > 0 {
		s.failUpdateHoldDecisions--
		return SidecarQuotaPersistResult{}, errInjectedProbeDecisionFailure
	}
	return s.persistence.PersistQuotaProbeDecision(ctx, decision)
}

func (s *failingProbeDecisionStore) ListWatchdogActions(ctx context.Context, sidecarID int) ([]SidecarWatchdogAction, error) {
	return s.persistence.(actionHistoryPersistence).ListWatchdogActions(ctx, sidecarID)
}
