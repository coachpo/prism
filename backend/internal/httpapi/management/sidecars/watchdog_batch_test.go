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
	apiCallHook      func(string)
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

func (u *watchdogProbeTestUpstream) setAPICallHook(hook func(string)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.apiCallHook = hook
}

func (u *watchdogProbeTestUpstream) setAuthFile(authID string, authIndex string, provider string, priority int) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authFiles[authID] = watchdogProbeAuthPayload(authID, authIndex, provider, watchdogTestCanonicalPriority(priority))
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
	hook := u.apiCallHook
	u.mu.Unlock()
	if hook != nil {
		hook(request.AuthIndex)
	}
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
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultQuotaExceededPriority}) {
		t.Fatalf("expected one deprioritize patch, got %v", got)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 1 {
		t.Fatalf("expected active quota hold, holds=%+v err=%v", holds, err)
	}
	if holds[0].PreviousPriority == nil || *holds[0].PreviousPriority != DefaultWorkingPriority || holds[0].HoldUntil == nil || !holds[0].HoldUntil.Equal(resetAt) {
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
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultQuotaExceededPriority}) {
		t.Fatalf("expected one deprioritize patch, got %v", got)
	}
	assertWatchdogAction(t, listWatchdogActions(t, service, sidecar.ID), watchdogActionDeprioritize, watchdogActionStatusSucceeded)
}

func TestWatchdogRestorePersistsPendingDecisionBeforePriorityPatch(t *testing.T) {
	now := time.Date(2026, time.May, 11, 11, 20, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-pending-restore", "idx-pending-restore", "codex", DefaultQuotaExceededPriority)
	upstream.setProbeResponse("idx-pending-restore", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 82)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-pending-restore", "idx-pending-restore", now.Add(-time.Minute))
	pendingCheck := expectPendingPatchActionBeforeFieldPatch(upstream, service, sidecar.ID, watchdogActionRestore, DefaultWorkingPriority)

	result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("reconcile healthy restore probe: %v", err)
	}
	if !result.Reconciled || result.Restored != 1 {
		t.Fatalf("expected healthy probe to restore, got %+v", result)
	}
	assertPendingPatchActionObserved(t, pendingCheck)
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority}) {
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
		{name: "replays unapplied pending patch", livePriority: 10, wantPatches: []int{DefaultQuotaExceededPriority}},
		{name: "finalizes already applied pending patch", livePriority: DefaultQuotaExceededPriority, wantPatches: nil},
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
			previousPriority := DefaultWorkingPriority
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
	previousPriority := DefaultWorkingPriority
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
		{name: "replays unapplied pending patch", livePriority: DefaultQuotaExceededPriority, wantPatches: []int{DefaultWorkingPriority}},
		{name: "finalizes already applied pending patch", livePriority: DefaultWorkingPriority, wantPatches: nil},
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
			restorePriority := DefaultWorkingPriority
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
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultQuotaExceededPriority}) {
		t.Fatalf("first run should patch once before final DB failure, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 0 {
		t.Fatalf("failed final hold write should not leave an active hold, holds=%+v err=%v", holds, err)
	}

	now = now.Add(time.Minute)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("repair reconcile: %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultQuotaExceededPriority}) {
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
	upstream.setAuthFile("auth-restore-repair", "idx-restore-repair", "codex", DefaultQuotaExceededPriority)
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
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority}) {
		t.Fatalf("first restore should patch once before final DB failure, got %v", got)
	}
	if holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID); err != nil || len(holds) != 1 {
		t.Fatalf("failed final release write should leave the active hold for repair, holds=%+v err=%v", holds, err)
	}

	now = now.Add(time.Minute)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("restore repair reconcile: %v", err)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority}) {
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
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultQuotaExceededPriority}) {
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
	upstream := newWatchdogUpstream(t, watchdogUpstreamAuth{Priority: DefaultQuotaExceededPriority, Provider: "gemini"})
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 88)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	previousPriority := DefaultWorkingPriority
	holdUntil := now.Add(-time.Minute)
	_, err := service.store.CreateWatchdogHold(t.Context(), SidecarWatchdogHoldInput{SidecarID: sidecar.ID, AuthID: watchdogAuthID, AuthIndex: stringPtr(watchdogAuthIndex), Provider: stringPtr("gemini"), Reason: watchdogReasonQuotaExceeded, ConditionHash: "hash-unsupported-flood", PreviousPriority: &previousPriority, TargetPriority: DefaultQuotaExceededPriority, HoldUntil: &holdUntil, Status: WatchdogHoldStatusActive})
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
	setWatchdogSweepInterval(t, service, sidecar.ID, 1)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported-a", "idx-unsupported-a", "gemini", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported-b", "idx-unsupported-b", "gemini", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported-c", "idx-unsupported-c", "openai", 10)

	first, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil || first.UnsupportedSkipped != 3 || first.ActionCount != 2 || first.Reconciled {
		t.Fatalf("expected provider-grouped unsupported discovery skips, result=%+v err=%v", first, err)
	}
	now = now.Add(time.Second)
	second, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
	if err != nil || second.UnsupportedSkipped != 3 || second.ActionCount != 0 || second.Reconciled {
		t.Fatalf("expected next sweep unsupported skips to dedupe action history, result=%+v err=%v", second, err)
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

func TestWatchdogDueHoldWaveFlushesInHoldOrder(t *testing.T) {
	now := time.Date(2026, time.May, 11, 13, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setAuthFile("auth-due-wave-a", "idx-due-wave-a", "codex", DefaultQuotaExceededPriority)
	upstream.setAuthFile("auth-due-wave-b", "idx-due-wave-b", "codex", DefaultQuotaExceededPriority)
	upstream.setProbeResponse("idx-due-wave-a", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody(), Delay: 150 * time.Millisecond})
	upstream.setProbeResponse("idx-due-wave-b", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	started := make(chan string, 2)
	release := make(chan struct{})
	released := false
	releaseProbes := func() {
		if !released {
			close(release)
			released = true
		}
	}
	defer releaseProbes()
	upstream.setAPICallHook(func(authIndex string) {
		started <- authIndex
		<-release
	})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 78)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 2, 2)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-due-wave-a", "idx-due-wave-a", "codex", DefaultQuotaExceededPriority)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-due-wave-b", "idx-due-wave-b", "codex", DefaultQuotaExceededPriority)
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-due-wave-a", "idx-due-wave-a", now.Add(-2*time.Hour))
	createWatchdogProbeHold(t, service, sidecar.ID, "auth-due-wave-b", "idx-due-wave-b", now.Add(-time.Hour))

	type reconcileResult struct {
		result SidecarWatchdogResult
		err    error
	}
	done := make(chan reconcileResult, 1)
	go func() {
		result, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
		done <- reconcileResult{result: result, err: err}
	}()
	waitForStarted := func() string {
		t.Helper()
		select {
		case authIndex := <-started:
			return authIndex
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for due-hold probe to start")
			return ""
		}
	}
	startedAuthIndexes := []string{waitForStarted(), waitForStarted()}
	if !slices.Contains(startedAuthIndexes, "idx-due-wave-a") || !slices.Contains(startedAuthIndexes, "idx-due-wave-b") {
		t.Fatalf("expected both due holds to start before release, got %v", startedAuthIndexes)
	}
	select {
	case result := <-done:
		t.Fatalf("due-hold wave completed before blocked probes were released: %+v", result)
	default:
	}
	releaseProbes()

	var reconcile reconcileResult
	select {
	case reconcile = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for due-hold wave reconcile")
	}
	if reconcile.err != nil {
		t.Fatalf("reconcile due-hold wave: %v", reconcile.err)
	}
	if reconcile.result.Probed != 2 || reconcile.result.Restored != 2 || reconcile.result.ActionCount != 2 || !reconcile.result.Reconciled {
		t.Fatalf("expected two due holds restored from one wave, got %+v", reconcile.result)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority, DefaultWorkingPriority}) {
		t.Fatalf("expected two sequential restore patches, got %v", got)
	}
	observations, err := service.store.ListWatchdogProbeObservations(t.Context(), sidecar.ID, 10)
	if err != nil {
		t.Fatalf("list probe observations: %v", err)
	}
	observationIDs := map[string]int{}
	for _, observation := range observations {
		observationIDs[observation.AuthID] = observation.ID
	}
	if observationIDs["auth-due-wave-a"] == 0 || observationIDs["auth-due-wave-b"] == 0 || observationIDs["auth-due-wave-a"] >= observationIDs["auth-due-wave-b"] {
		t.Fatalf("due-hold observations should persist in hold order despite probe completion order, ids=%+v observations=%+v", observationIDs, observations)
	}
	restoreActionIDs := map[string]int{}
	for _, action := range listWatchdogActions(t, service, sidecar.ID) {
		if action.ActionType == watchdogActionRestore && action.Status == watchdogActionStatusSucceeded {
			restoreActionIDs[stringValue(action.AuthID)] = action.ID
		}
	}
	if restoreActionIDs["auth-due-wave-a"] == 0 || restoreActionIDs["auth-due-wave-b"] == 0 || restoreActionIDs["auth-due-wave-a"] >= restoreActionIDs["auth-due-wave-b"] {
		t.Fatalf("due-hold restore actions should apply in hold order, ids=%+v", restoreActionIDs)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 0 {
		t.Fatalf("expected both due holds released, holds=%+v err=%v", holds, err)
	}
}

func TestBuildWatchdogSweepSnapshotOrdering(t *testing.T) {
	now := time.Date(2026, time.May, 15, 14, 0, 0, 0, time.UTC)
	policy := SidecarWatchdogPolicy{Enabled: true, QuotaInventoryEnabled: true, InitialScanEnabled: true, RollingRefreshEnabled: true, RollingRefreshAfterSeconds: 3600, WorkingPriority: DefaultWorkingPriority, EmptyQuotaPriority: DefaultEmptyQuotaPriority, InitialPriority: DefaultInitialPriority, ErrorPriority: DefaultErrorPriority}

	t.Run("initial inventory and rolling refresh", func(t *testing.T) {
		service := newWatchdogTestService(t, func() time.Time { return now })
		sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 60)
		policy.SidecarID = sidecar.ID
		lastProbedOld := now.Add(-3 * time.Hour)
		lastProbedNewer := now.Add(-2 * time.Hour)
		stateStore := service.store.(authQuotaStateStore)
		for _, state := range []SidecarAuthQuotaStateInput{
			{SidecarID: sidecar.ID, AuthID: "auth-roll-working-old", AuthIndex: stringPtr("idx-roll-working-old"), AuthName: stringPtr("auth-roll-working-old.json"), Provider: stringPtr("codex"), SnapshotObservedAt: &now, QuotaBand: quotaBandUsing, LastProbedAt: &lastProbedOld},
			{SidecarID: sidecar.ID, AuthID: "auth-roll-working-newer", AuthIndex: stringPtr("idx-roll-working-newer"), AuthName: stringPtr("auth-roll-working-newer.json"), Provider: stringPtr("codex"), SnapshotObservedAt: &now, QuotaBand: quotaBandUsing, LastProbedAt: &lastProbedNewer},
			{SidecarID: sidecar.ID, AuthID: "auth-roll-empty", AuthIndex: stringPtr("idx-roll-empty"), AuthName: stringPtr("auth-roll-empty.json"), Provider: stringPtr("chatgpt"), SnapshotObservedAt: &now, QuotaBand: quotaBandQuotaExceeded, LastProbedAt: &lastProbedOld},
		} {
			if _, err := stateStore.UpsertAuthQuotaState(t.Context(), state); err != nil {
				t.Fatalf("seed quota state %s: %v", state.AuthID, err)
			}
		}
		dueAt := now.Add(-time.Hour)
		dueOlder := now.Add(-2 * time.Hour)
		dueHolds := []SidecarWatchdogHold{
			{ID: 1, SidecarID: sidecar.ID, AuthID: "auth-due-working-newer", AuthIndex: stringPtr("idx-due-working-newer"), Provider: stringPtr("codex"), Reason: watchdogReasonQuotaExceeded, PreviousPriority: intPtr(DefaultWorkingPriority), TargetPriority: DefaultEmptyQuotaPriority, HoldUntil: &dueAt, Status: WatchdogHoldStatusActive},
			{ID: 2, SidecarID: sidecar.ID, AuthID: "auth-due-empty-b", AuthIndex: stringPtr("idx-due-empty-b"), Provider: stringPtr("chatgpt"), Reason: watchdogReasonQuotaExceeded, PreviousPriority: intPtr(DefaultEmptyQuotaPriority), TargetPriority: DefaultErrorPriority, HoldUntil: &dueOlder, Status: WatchdogHoldStatusActive},
			{ID: 3, SidecarID: sidecar.ID, AuthID: "auth-due-empty-a", AuthIndex: stringPtr("idx-due-empty-a"), Provider: stringPtr("codex"), Reason: watchdogReasonQuotaExceeded, PreviousPriority: intPtr(DefaultEmptyQuotaPriority), TargetPriority: DefaultErrorPriority, HoldUntil: &dueOlder, Status: WatchdogHoldStatusActive},
		}
		freshSnapshots := []SidecarAuthSnapshot{
			{SidecarID: sidecar.ID, AuthID: "auth-initial-low-chatgpt", AuthIndex: stringPtr("idx-initial-low-chatgpt"), Provider: stringPtr("chatgpt"), Priority: intPtr(DefaultInitialPriority), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-roll-empty", AuthIndex: stringPtr("idx-roll-empty"), Provider: stringPtr("chatgpt"), Priority: intPtr(DefaultEmptyQuotaPriority), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-initial-tie-b", AuthIndex: stringPtr("idx-initial-tie-b"), Provider: stringPtr("codex"), Priority: intPtr(DefaultInitialPriority + 10), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-roll-working-newer", AuthIndex: stringPtr("idx-roll-working-newer"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-initial-high-codex", AuthIndex: stringPtr("idx-initial-high-codex"), Provider: stringPtr("codex"), Priority: intPtr(DefaultInitialPriority + 20), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-roll-working-old", AuthIndex: stringPtr("idx-roll-working-old"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-initial-tie-a", AuthIndex: stringPtr("idx-initial-tie-a"), Provider: stringPtr("chatgpt"), Priority: intPtr(DefaultInitialPriority + 10), ObservedAt: now},
		}

		items, outcome, err := service.buildWatchdogSweepSnapshot(t.Context(), sidecar, policy, dueHolds, freshSnapshots, map[string]struct{}{"auth-due-working-newer": {}, "auth-due-empty-b": {}, "auth-due-empty-a": {}}, false, now)
		if err != nil {
			t.Fatalf("build sweep snapshot: %v", err)
		}
		if outcome.ActionCount != 0 || outcome.UnsupportedSkipped != 0 {
			t.Fatalf("unexpected snapshot side effects: %+v", outcome)
		}
		got := watchdogSweepSourceAuthIDs(items)
		want := []string{
			watchdogSweepSourceDueHoldProbe + ":auth-due-working-newer",
			watchdogSweepSourceDueHoldProbe + ":auth-due-empty-a",
			watchdogSweepSourceDueHoldProbe + ":auth-due-empty-b",
			watchdogSweepSourceInitialInventoryProbe + ":auth-initial-high-codex",
			watchdogSweepSourceInitialInventoryProbe + ":auth-initial-tie-a",
			watchdogSweepSourceInitialInventoryProbe + ":auth-initial-tie-b",
			watchdogSweepSourceInitialInventoryProbe + ":auth-initial-low-chatgpt",
			watchdogSweepSourceRollingRefreshProbe + ":auth-roll-working-old",
			watchdogSweepSourceRollingRefreshProbe + ":auth-roll-working-newer",
			watchdogSweepSourceRollingRefreshProbe + ":auth-roll-empty",
		}
		if !slices.Equal(got, want) {
			t.Fatalf("sweep snapshot ordering = %v, want %v", got, want)
		}
		revision, err := service.store.(watchdogPolicyRevisionLifecyclePersistence).EnsureActiveWatchdogPolicyRevision(t.Context(), policy)
		if err != nil {
			t.Fatalf("ensure ordering revision: %v", err)
		}
		sweep, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-ordering-facts", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: marshalWatchdogSweepItems(t, items), StartedAt: now})
		if err != nil {
			t.Fatalf("seed ordering sweep: %v", err)
		}
		materializeWatchdogSweepTestItems(t, service, sweep, items)
		childItems, err := service.store.(watchdogSweepItemPersistence).ListWatchdogSweepItems(t.Context(), sweep.SweepID)
		if err != nil {
			t.Fatalf("list materialized ordering items: %v", err)
		}
		if len(childItems) != len(items) || childItems[0].SourceRank != watchdogSweepSourceRank(watchdogSweepSourceDueHoldProbe) || childItems[0].Priority != DefaultWorkingPriority || childItems[0].DueAt == nil || !childItems[0].DueAt.Equal(dueAt) {
			t.Fatalf("due-hold materialized facts were not frozen: %+v", childItems)
		}
		if childItems[3].SourceRank != watchdogSweepSourceRank(watchdogSweepSourceInitialInventoryProbe) || childItems[3].Priority != DefaultInitialPriority+20 || childItems[7].SourceRank != watchdogSweepSourceRank(watchdogSweepSourceRollingRefreshProbe) || childItems[7].DueAt == nil || !childItems[7].DueAt.Equal(lastProbedOld) {
			t.Fatalf("inventory/rolling materialized facts were not frozen: %+v", childItems)
		}
	})

	t.Run("initial inventory source", func(t *testing.T) {
		service := newWatchdogTestService(t, func() time.Time { return now })
		sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 60)
		policy.SidecarID = sidecar.ID
		dueAt := now.Add(-time.Hour)
		dueHold := SidecarWatchdogHold{ID: 2, SidecarID: sidecar.ID, AuthID: "auth-manual-due", AuthIndex: stringPtr("idx-manual-due"), Provider: stringPtr("codex"), Reason: watchdogReasonQuotaExceeded, PreviousPriority: intPtr(DefaultWorkingPriority), TargetPriority: DefaultEmptyQuotaPriority, HoldUntil: &dueAt, Status: WatchdogHoldStatusActive}
		freshSnapshots := []SidecarAuthSnapshot{
			{SidecarID: sidecar.ID, AuthID: "auth-manual-empty", AuthIndex: stringPtr("idx-manual-empty"), Provider: stringPtr("chatgpt"), Priority: intPtr(DefaultEmptyQuotaPriority), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-manual-b", AuthIndex: stringPtr("idx-manual-b"), Provider: stringPtr("chatgpt"), Priority: intPtr(DefaultWorkingPriority), ObservedAt: now},
			{SidecarID: sidecar.ID, AuthID: "auth-manual-a", AuthIndex: stringPtr("idx-manual-a"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority), ObservedAt: now},
		}

		items, _, err := service.buildWatchdogSweepSnapshot(t.Context(), sidecar, policy, []SidecarWatchdogHold{dueHold}, freshSnapshots, map[string]struct{}{"auth-manual-due": {}}, false, now)
		if err != nil {
			t.Fatalf("build manual sweep snapshot: %v", err)
		}
		got := watchdogSweepSourceAuthIDs(items)
		want := []string{
			watchdogSweepSourceDueHoldProbe + ":auth-manual-due",
			watchdogSweepSourceInitialInventoryProbe + ":auth-manual-a",
			watchdogSweepSourceInitialInventoryProbe + ":auth-manual-b",
			watchdogSweepSourceInitialInventoryProbe + ":auth-manual-empty",
		}
		if !slices.Equal(got, want) {
			t.Fatalf("inventory sweep snapshot ordering = %v, want %v", got, want)
		}
	})
}

func watchdogSweepSourceAuthIDs(items []watchdogSweepSnapshotItem) []string {
	ids := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Source+":"+item.AuthID)
	}
	return ids
}

func TestWatchdogRollingRefreshOrderingPrefersOldestEligibleAuth(t *testing.T) {
	now := time.Date(2026, time.May, 11, 13, 45, 0, 0, time.UTC)
	oldest := now.Add(-3 * time.Hour)
	older := now.Add(-2 * time.Hour)
	tie := now.Add(-90 * time.Minute)
	fresh := now.Add(-10 * time.Minute)
	policy := SidecarWatchdogPolicy{WorkingPriority: DefaultWorkingPriority, EmptyQuotaPriority: DefaultEmptyQuotaPriority, InitialPriority: DefaultInitialPriority, ErrorPriority: DefaultErrorPriority, RollingRefreshEnabled: true, RollingRefreshAfterSeconds: 3600}
	snapshots := []SidecarAuthSnapshot{
		{AuthID: "auth-tie-b", AuthIndex: stringPtr("idx-tie-b"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
		{AuthID: "auth-low", AuthIndex: stringPtr("idx-low"), Provider: stringPtr("codex"), Priority: intPtr(1)},
		{AuthID: "auth-fresh", AuthIndex: stringPtr("idx-fresh"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
		{AuthID: "auth-never", AuthIndex: stringPtr("idx-never"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
		{AuthID: "auth-held", AuthIndex: stringPtr("idx-held"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
		{AuthID: "auth-oldest", AuthIndex: stringPtr("idx-oldest"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
		{AuthID: "auth-tie-a", AuthIndex: stringPtr("idx-tie-a"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
		{AuthID: "auth-older", AuthIndex: stringPtr("idx-older"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
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

func TestWatchdogInitialInventoryIncludesMissingQuotaState(t *testing.T) {
	now := time.Date(2026, time.May, 15, 13, 0, 0, 0, time.UTC)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, "http://127.0.0.1:18080", true, 60)
	policy := SidecarWatchdogPolicy{SidecarID: sidecar.ID, QuotaInventoryEnabled: true, InitialScanEnabled: true, WorkingPriority: DefaultWorkingPriority, EmptyQuotaPriority: DefaultEmptyQuotaPriority, InitialPriority: DefaultInitialPriority, ErrorPriority: DefaultErrorPriority}
	snapshots := []SidecarAuthSnapshot{{SidecarID: sidecar.ID, AuthID: "auth-missing-quota-state", AuthIndex: stringPtr("idx-missing-quota-state"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority), ObservedAt: now}}
	quotaStates := map[string]SidecarAuthQuotaState{}

	scanRun, active, err := service.ensureActiveQuotaScanRun(t.Context(), sidecar.ID, policy, snapshots, map[string]struct{}{}, quotaStates)
	if err != nil {
		t.Fatalf("ensure initial quota scan run: %v", err)
	}
	if !active || scanRun.ScanType != quotaScanTypeInitial || scanRun.PlannedCount != 1 {
		t.Fatalf("missing quota state should create one initial inventory probe, active=%v run=%+v", active, scanRun)
	}
	candidates := watchdogQuotaScanProbeCandidates(policy, scanRun, snapshots, map[string]struct{}{}, quotaStates)
	if len(candidates) != 1 || candidates[0].AuthID != "auth-missing-quota-state" {
		t.Fatalf("missing quota state initial candidates = %+v", candidates)
	}
}

func TestWatchdogInitialInventoryIncludesInitialPriorityState(t *testing.T) {
	policy := SidecarWatchdogPolicy{QuotaInventoryEnabled: true, InitialScanEnabled: true, WorkingPriority: DefaultWorkingPriority, EmptyQuotaPriority: DefaultEmptyQuotaPriority, InitialPriority: DefaultInitialPriority, ErrorPriority: DefaultErrorPriority}
	scanRun := SidecarQuotaScanRun{ScanType: quotaScanTypeInitial}
	snapshots := []SidecarAuthSnapshot{
		{AuthID: "auth-initial", AuthIndex: stringPtr("idx-initial"), Provider: stringPtr("codex"), Priority: intPtr(DefaultInitialPriority)},
		{AuthID: "auth-working", AuthIndex: stringPtr("idx-working"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
	}
	quotaStates := map[string]SidecarAuthQuotaState{
		"auth-initial": {AuthID: "auth-initial", QuotaBand: quotaBandUsing},
		"auth-working": {AuthID: "auth-working", QuotaBand: quotaBandUsing},
	}

	candidates := watchdogQuotaScanProbeCandidates(policy, scanRun, snapshots, map[string]struct{}{}, quotaStates)
	if len(candidates) != 1 || candidates[0].AuthID != "auth-initial" {
		t.Fatalf("initial priority-state candidates = %+v", candidates)
	}
}

func TestWatchdogRollingRefreshIncludesEmptyQuota(t *testing.T) {
	now := time.Date(2026, time.May, 15, 13, 15, 0, 0, time.UTC)
	lastProbedAt := now.Add(-2 * time.Hour)
	policy := SidecarWatchdogPolicy{WorkingPriority: DefaultWorkingPriority, EmptyQuotaPriority: DefaultEmptyQuotaPriority, InitialPriority: DefaultInitialPriority, ErrorPriority: DefaultErrorPriority, RollingRefreshEnabled: true, RollingRefreshAfterSeconds: 3600}
	snapshots := []SidecarAuthSnapshot{
		{AuthID: "auth-working", AuthIndex: stringPtr("idx-working"), Provider: stringPtr("codex"), Priority: intPtr(DefaultWorkingPriority)},
		{AuthID: "auth-empty-quota", AuthIndex: stringPtr("idx-empty-quota"), Provider: stringPtr("codex"), Priority: intPtr(DefaultEmptyQuotaPriority)},
		{AuthID: "auth-initial", AuthIndex: stringPtr("idx-initial"), Provider: stringPtr("codex"), Priority: intPtr(DefaultInitialPriority)},
		{AuthID: "auth-error", AuthIndex: stringPtr("idx-error"), Provider: stringPtr("codex"), Priority: intPtr(DefaultErrorPriority)},
	}
	quotaStates := map[string]SidecarAuthQuotaState{
		"auth-working":     {AuthID: "auth-working", LastProbedAt: &lastProbedAt},
		"auth-empty-quota": {AuthID: "auth-empty-quota", LastProbedAt: &lastProbedAt},
		"auth-initial":     {AuthID: "auth-initial", LastProbedAt: &lastProbedAt},
		"auth-error":       {AuthID: "auth-error", LastProbedAt: &lastProbedAt},
	}

	candidates := watchdogRollingRefreshProbeCandidates(policy, snapshots, map[string]struct{}{}, quotaStates, now)
	got := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		got = append(got, candidate.AuthID)
	}
	want := []string{"auth-working", "auth-empty-quota"}
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

func TestWatchdogSweepIntervalGatesNewQuotaProbeWork(t *testing.T) {
	now := time.Date(2026, time.May, 11, 14, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 75)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	setWatchdogSweepInterval(t, service, sidecar.ID, 60)
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
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("reconcile after cooldown but before sweep interval: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-first"}) {
		t.Fatalf("new quota probe work must wait for the next sweep interval, got %v", calls)
	}

	now = now.Add(25 * time.Second)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("reconcile after sweep interval: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); !slices.Equal(calls, []string{"idx-first", "idx-due"}) {
		t.Fatalf("due hold should probe when the next sweep opens, got %v", calls)
	}
	if got := upstream.fieldPatchPriorities(); !slices.Equal(got, []int{DefaultWorkingPriority}) {
		t.Fatalf("due hold restore should resume when the next sweep opens, got %v", got)
	}
}

func TestWatchdogBatchCooldownBetweenSweepBatches(t *testing.T) {
	now := time.Date(2026, time.May, 12, 13, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 80)
	cooldownSeconds := 10
	zeroJitter := 0
	rollingRefresh := true
	quotaInventory := true
	initialScan := true
	rollingAfter := 60
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, UsingPriority: DefaultUsingPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeConcurrency: 2, ProbeTimeoutSeconds: 5, ProbeBatchCooldownSeconds: &cooldownSeconds, ProbeJitterMinMS: &zeroJitter, ProbeJitterMaxMS: &zeroJitter, QuotaInventoryEnabled: &quotaInventory, InitialScanEnabled: &initialScan, RollingRefreshEnabled: &rollingRefresh, RollingRefreshAfterSeconds: &rollingAfter})
	if err != nil {
		t.Fatalf("enable cooldown sweep policy: %v", err)
	}
	policy, err := service.store.GetOrCreateWatchdogPolicy(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("load cooldown sweep policy: %v", err)
	}
	revision, err := service.store.(watchdogPolicyRevisionLifecyclePersistence).EnsureActiveWatchdogPolicyRevision(t.Context(), policy)
	if err != nil {
		t.Fatalf("ensure cooldown sweep revision: %v", err)
	}
	items := []watchdogSweepSnapshotItem{
		{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-cool-a", AuthIndex: "idx-cool-a", Provider: "codex"},
		{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-cool-b", AuthIndex: "idx-cool-b", Provider: "codex"},
		{Source: watchdogSweepSourceRollingRefreshProbe, AuthID: "auth-cool-c", AuthIndex: "idx-cool-c", Provider: "codex"},
	}
	lifecycle := service.store.(watchdogSweepLifecyclePersistence)
	sweep, err := lifecycle.UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-cooldown", SidecarID: sidecar.ID, PolicyRevisionID: revision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: marshalWatchdogSweepItems(t, items), StartedAt: now.Add(-time.Second)})
	if err != nil {
		t.Fatalf("seed cooldown sweep: %v", err)
	}
	if err := service.materializeWatchdogSweepItems(t.Context(), sweep, items); err != nil {
		t.Fatalf("materialize cooldown sweep items: %v", err)
	}

	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("first cooldown batch reconcile: %v", err)
	}
	calls := upstream.apiCallAuthIndexes()
	if len(calls) != 2 || !slices.Contains(calls, "idx-cool-a") || !slices.Contains(calls, "idx-cool-b") || slices.Contains(calls, "idx-cool-c") {
		t.Fatalf("first sweep batch should launch only first cooldown window, calls=%v", calls)
	}
	active, found, err := lifecycle.GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || !found || active.Status != string(SidecarWatchdogSweepStatusPaused) || active.NextItemIndex != 2 || active.BatchIndex != 1 {
		t.Fatalf("first batch should pause sweep at next batch checkpoint: active=%+v found=%v err=%v", active, found, err)
	}
	expectedNextBatchAfter := now.Add(time.Duration(cooldownSeconds) * time.Second)
	if active.NextBatchAfter == nil || !active.NextBatchAfter.Equal(expectedNextBatchAfter) || stringValue(active.PauseReason) != watchdogSweepPauseReasonBatchCooldown {
		t.Fatalf("first batch did not persist cooldown checkpoint: active=%+v want next=%v", active, expectedNextBatchAfter)
	}

	now = now.Add(time.Duration(cooldownSeconds-1) * time.Second)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("cooldown-held sweep reconcile: %v", err)
	}
	if calls := upstream.apiCallAuthIndexes(); len(calls) != 2 {
		t.Fatalf("batch cooldown should prevent the next sweep batch, calls=%v", calls)
	}

	now = now.Add(time.Second)
	if _, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("cooldown-open sweep reconcile: %v", err)
	}
	calls = upstream.apiCallAuthIndexes()
	if len(calls) != 3 || !slices.Contains(calls, "idx-cool-c") {
		t.Fatalf("next sweep batch should launch after cooldown, calls=%v", calls)
	}
	active, found, err = lifecycle.GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || found {
		t.Fatalf("final cooldown batch should complete the sweep: active=%+v found=%v err=%v", active, found, err)
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
	policy := SidecarWatchdogPolicy{ProbeConcurrency: 1, ProbeTimeoutSeconds: 20}
	run := newWatchdogProbeRun(policy, startedAt)
	timeout, ok := run.nextLaunchTimeout(startedAt)
	if !ok || timeout != 20*time.Second {
		t.Fatalf("initial launch timeout = %v ok=%v, want 20s true", timeout, ok)
	}
	timeout, ok = run.nextLaunchTimeout(startedAt.Add(100 * time.Second))
	if !ok || timeout != 20*time.Second {
		t.Fatalf("last safe launch timeout = %v ok=%v, want 20s true", timeout, ok)
	}
	if timeout, ok = run.nextLaunchTimeout(startedAt.Add(101 * time.Second)); ok || timeout != 0 {
		t.Fatalf("unsafe launch budget timeout = %v ok=%v, want 0 false", timeout, ok)
	}
	maxBudgetSeconds := watchdogProbeConcurrencyBudgetMaxSeconds()
	if maxBudgetSeconds != 120 {
		t.Fatalf("watchdog probe timeout budget = %ds, want 120s", maxBudgetSeconds)
	}
	if err := validateWatchdogProbeRuntimePolicy(SidecarWatchdogPolicy{ProbeConcurrency: MaxProbeConcurrency, ProbeTimeoutSeconds: maxBudgetSeconds}); err != nil {
		t.Fatalf("expected max concurrency with max per-probe timeout to be valid, got %v", err)
	}
	if err := validateWatchdogProbeRuntimePolicy(SidecarWatchdogPolicy{ProbeConcurrency: 1, ProbeTimeoutSeconds: maxBudgetSeconds + 1}); !IsStoreError(err, StoreErrorInvalidInput) {
		t.Fatalf("expected oversized per-probe timeout validation error, got %v", err)
	}
}

func TestWatchdogProbeConcurrencyContractValidation(t *testing.T) {
	if DefaultProbeConcurrency != 3 {
		t.Fatalf("DefaultProbeConcurrency = %d, want 3", DefaultProbeConcurrency)
	}
	if MaxProbeConcurrency != 8 {
		t.Fatalf("MaxProbeConcurrency = %d, want 8", MaxProbeConcurrency)
	}
	if got := normalizedProbeConcurrency(SidecarWatchdogPolicy{}); got != DefaultProbeConcurrency {
		t.Fatalf("normalized default concurrency = %d, want %d", got, DefaultProbeConcurrency)
	}

	cases := []struct {
		name        string
		policy      SidecarWatchdogPolicy
		wantInvalid bool
	}{
		{name: "valid single concurrency", policy: SidecarWatchdogPolicy{ProbeConcurrency: 1, ProbeTimeoutSeconds: 1}},
		{name: "valid default concurrency", policy: SidecarWatchdogPolicy{ProbeConcurrency: DefaultProbeConcurrency, ProbeTimeoutSeconds: 1}},
		{name: "valid max concurrency", policy: SidecarWatchdogPolicy{ProbeConcurrency: MaxProbeConcurrency, ProbeTimeoutSeconds: 1}},
		{name: "valid max concurrency with max per-probe timeout", policy: SidecarWatchdogPolicy{ProbeConcurrency: MaxProbeConcurrency, ProbeTimeoutSeconds: watchdogProbeConcurrencyBudgetMaxSeconds()}},
		{name: "invalid zero concurrency", policy: SidecarWatchdogPolicy{ProbeConcurrency: 0, ProbeTimeoutSeconds: 1}, wantInvalid: true},
		{name: "invalid nine concurrency", policy: SidecarWatchdogPolicy{ProbeConcurrency: 9, ProbeTimeoutSeconds: 1}, wantInvalid: true},
		{name: "invalid timeout above per-probe budget", policy: SidecarWatchdogPolicy{ProbeConcurrency: 1, ProbeTimeoutSeconds: watchdogProbeConcurrencyBudgetMaxSeconds() + 1}, wantInvalid: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateWatchdogProbeRuntimePolicy(tc.policy)
			if tc.wantInvalid && !IsStoreError(err, StoreErrorInvalidInput) {
				t.Fatalf("expected invalid input error, got %v", err)
			}
			if !tc.wantInvalid && err != nil {
				t.Fatalf("expected valid policy, got %v", err)
			}
		})
	}
}

func TestWatchdogConcurrentBatchOverlap(t *testing.T) {
	now := time.Date(2026, time.May, 11, 15, 30, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	delay := 200 * time.Millisecond
	upstream.setProbeResponse("idx-wave-a", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody(), Delay: delay})
	upstream.setProbeResponse("idx-wave-b", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody(), Delay: delay})
	started := make(chan string, 2)
	release := make(chan struct{})
	upstream.setAPICallHook(func(authIndex string) {
		started <- authIndex
		<-release
	})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 76)
	policy := SidecarWatchdogPolicy{ProbeConcurrency: 2, ProbeTimeoutSeconds: 2, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds}
	run := newWatchdogProbeRun(policy, time.Now().UTC())
	candidates := []watchdogProbeCandidate{
		{AuthID: "auth-wave-a", AuthIndex: "idx-wave-a", Provider: "codex"},
		{AuthID: "auth-wave-b", AuthIndex: "idx-wave-b", Provider: "codex"},
	}
	type waveResult struct {
		results []watchdogProbeWaveResult
		err     error
	}
	done := make(chan waveResult, 1)
	startedAt := time.Now()
	go func() {
		results, err := service.executeWatchdogProbeWave(t.Context(), sidecar, policy, candidates, &run, now)
		done <- waveResult{results: results, err: err}
	}()

	seen := map[string]struct{}{}
	for i := 0; i < len(candidates); i++ {
		select {
		case authIndex := <-started:
			seen[authIndex] = struct{}{}
		case <-time.After(750 * time.Millisecond):
			close(release)
			t.Fatalf("timed out waiting for two concurrent probe starts, saw %v", seen)
		}
	}
	for _, authIndex := range []string{"idx-wave-a", "idx-wave-b"} {
		if _, ok := seen[authIndex]; !ok {
			close(release)
			t.Fatalf("missing concurrent start for %s, saw %v", authIndex, seen)
		}
	}
	close(release)

	select {
	case wave := <-done:
		elapsed := time.Since(startedAt)
		if wave.err != nil {
			t.Fatalf("execute concurrent wave: %v", wave.err)
		}
		if len(wave.results) != len(candidates) {
			t.Fatalf("wave results = %d, want %d", len(wave.results), len(candidates))
		}
		if elapsed >= 2*delay+200*time.Millisecond {
			t.Fatalf("slow probes did not overlap enough, elapsed=%v delay=%v", elapsed, delay)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for concurrent wave to finish")
	}
}

func TestWatchdogBatchJitterDoesNotSerialize(t *testing.T) {
	now := time.Date(2026, time.May, 11, 15, 32, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	startTimes := make(chan time.Time, 2)
	release := make(chan struct{})
	upstream.setAPICallHook(func(authIndex string) {
		startTimes <- time.Now()
		<-release
	})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 79)
	jitterMS := 80
	policy := SidecarWatchdogPolicy{ProbeConcurrency: 2, ProbeTimeoutSeconds: 2, ProbeJitterMinMS: jitterMS, ProbeJitterMaxMS: jitterMS, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds}
	run := newWatchdogProbeRun(policy, time.Now().UTC())
	candidates := []watchdogProbeCandidate{
		{AuthID: "auth-jitter-a", AuthIndex: "idx-jitter-a", Provider: "codex"},
		{AuthID: "auth-jitter-b", AuthIndex: "idx-jitter-b", Provider: "codex"},
	}
	type waveResult struct {
		results []watchdogProbeWaveResult
		err     error
	}
	done := make(chan waveResult, 1)
	go func() {
		results, err := service.executeWatchdogProbeWave(t.Context(), sidecar, policy, candidates, &run, now)
		done <- waveResult{results: results, err: err}
	}()

	var firstStarted time.Time
	select {
	case firstStarted = <-startTimes:
	case <-time.After(500 * time.Millisecond):
		close(release)
		t.Fatal("jittered batch did not launch the first probe")
	}
	var secondStarted time.Time
	select {
	case secondStarted = <-startTimes:
	case <-time.After(500 * time.Millisecond):
		close(release)
		t.Fatal("jittered batch did not launch the second probe while the first was still running")
	}
	if secondStarted.Sub(firstStarted) < 40*time.Millisecond {
		close(release)
		t.Fatalf("expected jitter to stagger launches, delta=%v", secondStarted.Sub(firstStarted))
	}
	select {
	case wave := <-done:
		close(release)
		t.Fatalf("jittered batch completed before blocked probes were released: %+v", wave)
	default:
	}
	close(release)

	select {
	case wave := <-done:
		if wave.err != nil {
			t.Fatalf("execute jittered batch: %v", wave.err)
		}
		if len(wave.results) != len(candidates) {
			t.Fatalf("jittered results = %d, want %d", len(wave.results), len(candidates))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for jittered batch to finish")
	}
}

func TestWatchdogConcurrentWavePreservesInputOrderAndBudget(t *testing.T) {
	now := time.Date(2026, time.May, 11, 15, 35, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setProbeResponse("idx-wave-slow", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody(), Delay: 200 * time.Millisecond})
	upstream.setProbeResponse("idx-wave-fast", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	upstream.setProbeResponse("idx-wave-extra", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 77)
	policy := SidecarWatchdogPolicy{ProbeConcurrency: 2, ProbeTimeoutSeconds: 2, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds}
	run := newWatchdogProbeRun(policy, time.Now().UTC())
	candidates := []watchdogProbeCandidate{
		{AuthID: "auth-wave-slow", AuthIndex: "idx-wave-slow", Provider: "codex"},
		{AuthID: "auth-wave-fast", AuthIndex: "idx-wave-fast", Provider: "codex"},
		{AuthID: "auth-wave-extra", AuthIndex: "idx-wave-extra", Provider: "codex"},
	}

	results, err := service.executeWatchdogProbeWave(t.Context(), sidecar, policy, candidates, &run, now)
	if err != nil {
		t.Fatalf("execute ordered wave: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("wave results = %d, want 2", len(results))
	}
	for i, wantAuthID := range []string{"auth-wave-slow", "auth-wave-fast"} {
		if results[i].Candidate.AuthID != wantAuthID || results[i].Observation.AuthID != wantAuthID {
			t.Fatalf("result %d = candidate %q observation %q, want %q", i, results[i].Candidate.AuthID, results[i].Observation.AuthID, wantAuthID)
		}
		if results[i].Classification.Status != watchdogProbeStatusSucceeded {
			t.Fatalf("result %d status = %q, want %q", i, results[i].Classification.Status, watchdogProbeStatusSucceeded)
		}
	}
	if run.remaining != 0 {
		t.Fatalf("remaining budget = %d, want 0", run.remaining)
	}
	calls := upstream.apiCallAuthIndexes()
	if len(calls) != 2 || slices.Contains(calls, "idx-wave-extra") {
		t.Fatalf("wave should only launch budgeted probes, calls=%v", calls)
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

func TestManualQuotaScanActiveSweepFailureDoesNotCreateProjection(t *testing.T) {
	now := time.Date(2026, time.May, 11, 16, 45, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 76)
	enableWatchdogProbePolicy(t, service, sidecar.ID, 1, 5)
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-manual-conflict", "idx-manual-conflict", "codex", 0)
	policyState, err := service.getWatchdogPolicyRevisionState(t.Context(), sidecar.ID)
	if err != nil || policyState.ActiveRevision == nil {
		t.Fatalf("load active watchdog revision: state=%+v err=%v", policyState, err)
	}
	if _, err := service.store.(watchdogSweepLifecyclePersistence).UpsertWatchdogSweep(t.Context(), SidecarWatchdogSweepInput{SweepID: "sweep-manual-conflict", SidecarID: sidecar.ID, PolicyRevisionID: policyState.ActiveRevision.ID, Status: string(SidecarWatchdogSweepStatusPaused), SnapshotJSON: json.RawMessage(`[]`), StartedAt: now}); err != nil {
		t.Fatalf("seed active sweep: %v", err)
	}

	_, err = service.StartManualQuotaScan(t.Context(), sidecar.ID, nil, false)
	if err == nil || !strings.Contains(err.Error(), "active watchdog sweep already exists") {
		t.Fatalf("expected active sweep conflict, got %v", err)
	}
	runs, err := service.store.ListQuotaScanRuns(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("list quota scan projections: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("failed manual scan queued misleading projection history: %+v", runs)
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
	if scanRun.Status != quotaScanStatusCompleted || scanRun.PlannedCount != 2 {
		t.Fatalf("manual quota scan projection not recorded with planned inventory: %+v", scanRun)
	}
	if calls := upstream.apiCallAuthIndexes(); len(calls) != 0 {
		t.Fatalf("manual quota scan must not probe before reconcile, got %v", calls)
	}
	activeSweep, found, err := service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || !found || activeSweep.Status != string(SidecarWatchdogSweepStatusPaused) {
		t.Fatalf("manual quota scan did not queue a paused parent sweep: sweep=%+v found=%v err=%v", activeSweep, found, err)
	}
	items, err := service.store.(watchdogSweepItemPersistence).ListWatchdogSweepItems(t.Context(), activeSweep.SweepID)
	if err != nil || len(items) != 2 || items[0].Source != watchdogSweepSourceManualScanProbe {
		t.Fatalf("manual quota scan did not materialize child items: items=%+v err=%v", items, err)
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
}

func TestWatchdogSweepUsesOrderedConcurrentWaveAndMaterializedItems(t *testing.T) {
	now := time.Date(2026, time.May, 11, 17, 15, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setProbeResponse("idx-a", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	upstream.setProbeResponse("idx-b", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	upstream.setProbeResponse("idx-c", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	firstStarted := make(chan struct{}, 1)
	secondStarted := make(chan struct{}, 1)
	releaseFirst := make(chan struct{})
	upstream.setAPICallHook(func(authIndex string) {
		switch authIndex {
		case "idx-a":
			select {
			case firstStarted <- struct{}{}:
			default:
			}
			<-releaseFirst
		case "idx-b":
			select {
			case secondStarted <- struct{}{}:
			default:
			}
		}
	})
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 77)
	policy := SidecarWatchdogPolicy{SidecarID: sidecar.ID, Enabled: true, ProbeConcurrency: 3, ProbeTimeoutSeconds: 5, ProbeBatchCooldownSeconds: 5, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaInventoryEnabled: true, InitialScanEnabled: true, WorkingPriority: DefaultWorkingPriority, EmptyQuotaPriority: DefaultEmptyQuotaPriority, InitialPriority: DefaultInitialPriority, ErrorPriority: DefaultErrorPriority}
	revision, err := service.store.(watchdogPolicyRevisionLifecyclePersistence).EnsureActiveWatchdogPolicyRevision(t.Context(), policy)
	if err != nil {
		t.Fatalf("ensure active revision: %v", err)
	}
	snapshots := []SidecarAuthSnapshot{
		{SidecarID: sidecar.ID, AuthID: "auth-a", AuthIndex: stringPtr("idx-a"), Name: "auth-a.json", Provider: stringPtr("codex"), Disabled: boolPtr(false), Priority: intPtr(DefaultWorkingPriority)},
		{SidecarID: sidecar.ID, AuthID: "auth-b", AuthIndex: stringPtr("idx-b"), Name: "auth-b.json", Provider: stringPtr("codex"), Disabled: boolPtr(false), Priority: intPtr(DefaultWorkingPriority)},
		{SidecarID: sidecar.ID, AuthID: "auth-c", AuthIndex: stringPtr("idx-c"), Name: "auth-c.json", Provider: stringPtr("codex"), Disabled: boolPtr(false), Priority: intPtr(DefaultWorkingPriority)},
	}
	items := make([]watchdogSweepSnapshotItem, 0, 2)
	quotaStates := map[string]SidecarAuthQuotaState{}
	for _, candidate := range watchdogQuotaScanProbeCandidates(policy, SidecarQuotaScanRun{ScanType: quotaScanTypeInitial, PlannedCount: 2}, snapshots[:2], map[string]struct{}{}, quotaStates) {
		items = append(items, watchdogSweepItemFromCandidate(watchdogSweepSourceInitialInventoryProbe, candidate, nil, nil, watchdogQuotaScanCandidateLastProbedAt(candidate, quotaStates)))
	}
	resultCh := make(chan struct {
		outcome watchdogProbeBatchOutcome
		err     error
	}, 1)
	go func() {
		outcome, err := service.startWatchdogSweep(t.Context(), service.store.(watchdogSweepLifecyclePersistence), sidecar, policy, revision, items, watchdogProbeBatchOutcome{}, map[string]SidecarAuthSnapshot{}, nil, now)
		resultCh <- struct {
			outcome watchdogProbeBatchOutcome
			err     error
		}{outcome: outcome, err: err}
	}()
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first sweep probe to start")
	}
	select {
	case <-secondStarted:
		close(releaseFirst)
	case <-time.After(time.Second):
		close(releaseFirst)
		t.Fatal("sweep did not launch the second probe concurrently")
	}
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("run sweep batch: %v", result.err)
	}
	if calls := upstream.apiCallAuthIndexes(); len(calls) != 2 || !slices.Contains(calls, "idx-a") || !slices.Contains(calls, "idx-b") || slices.Contains(calls, "idx-c") {
		t.Fatalf("sweep should launch only materialized auths, got %v", calls)
	}
	if result.outcome.Attempted != 2 || result.outcome.ProbeFailed != 0 {
		t.Fatalf("unexpected sweep outcome: %+v", result.outcome)
	}
	active, found, err := service.store.(watchdogSweepLifecyclePersistence).GetActiveWatchdogSweep(t.Context(), sidecar.ID)
	if err != nil || found || active.SweepID != "" {
		t.Fatalf("sweep should complete after materialized items: sweep=%+v found=%v err=%v", active, found, err)
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

func enableWatchdogProbePolicy(t *testing.T, service *Service, sidecarID int, probeConcurrency int, timeoutSeconds int) {
	t.Helper()
	zeroJitter := 0
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecarID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, UsingPriority: DefaultUsingPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeConcurrency: probeConcurrency, ProbeTimeoutSeconds: timeoutSeconds, ProbeJitterMinMS: &zeroJitter, ProbeJitterMaxMS: &zeroJitter})
	if err != nil {
		t.Fatalf("enable probe watchdog policy: %v", err)
	}
}

func seedWatchdogProbeSnapshot(t *testing.T, service *Service, sidecarID int, observedAt time.Time, authID string, authIndex string, provider string, priority int) {
	t.Helper()
	priority = watchdogTestCanonicalPriority(priority)
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
	priority = watchdogTestCanonicalPriority(priority)
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
	previousPriority := DefaultWorkingPriority
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
func TestWatchdogDiscoveryProbeBatchUsesConcurrentWaveAndFlushesSequentially(t *testing.T) {
	now := time.Date(2026, time.May, 11, 18, 0, 0, 0, time.UTC)
	upstream := newWatchdogProbeTestUpstream(t)
	defer upstream.Close()
	upstream.setProbeResponse("idx-discovery-a", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody(), Delay: 200 * time.Millisecond})
	upstream.setProbeResponse("idx-discovery-b", watchdogProbeTestResponse{StatusCode: http.StatusOK, Body: watchdogHealthyUsageBody()})
	started := make(chan string, 2)
	release := make(chan struct{})
	skipCheck := make(chan error, 1)
	service := newWatchdogTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 90)
	_, err := service.store.UpsertWatchdogPolicy(t.Context(), SidecarWatchdogPolicyInput{SidecarID: sidecar.ID, Enabled: true, FailureThreshold: DefaultFailureThreshold, FailureWindowSeconds: DefaultFailureWindowSeconds, FallbackCooldownSeconds: DefaultFallbackCooldownSeconds, QuotaExceededPriority: DefaultQuotaExceededPriority, UsingPriority: DefaultUsingPriority, ManualOverridePauseSeconds: DefaultManualOverridePauseSeconds, ProbeConcurrency: 2, ProbeTimeoutSeconds: 2, RollingRefreshEnabled: boolPtr(true), RollingRefreshAfterSeconds: intPtr(3600)})
	if err != nil {
		t.Fatalf("enable discovery watchdog policy: %v", err)
	}
	markWatchdogSnapshotsFresh(t, service, sidecar.ID, now)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-discovery-a", "idx-discovery-a", "codex", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-discovery-b", "idx-discovery-b", "codex", 10)
	seedWatchdogProbeSnapshot(t, service, sidecar.ID, now, "auth-unsupported", "idx-unsupported", "gemini", 10)
	lister := service.store.(actionHistoryPersistence)
	var checkOnce sync.Once
	upstream.setAPICallHook(func(authIndex string) {
		checkOnce.Do(func() {
			actions, err := lister.ListWatchdogActions(context.Background(), sidecar.ID)
			if err != nil {
				skipCheck <- fmt.Errorf("list watchdog actions before discovery wave: %w", err)
				return
			}
			if got := countWatchdogActions(actions, watchdogProbeStatusSkippedUnsupportedProvider); got != 1 {
				skipCheck <- fmt.Errorf("expected unsupported discovery skip before first probe, got %d actions=%+v", got, actions)
				return
			}
			skipCheck <- nil
		})
		started <- authIndex
		<-release
	})
	resultCh := make(chan struct {
		outcome SidecarWatchdogResult
		err     error
	}, 1)
	go func() {
		outcome, err := service.ReconcileSidecarWatchdog(t.Context(), sidecar.ID)
		resultCh <- struct {
			outcome SidecarWatchdogResult
			err     error
		}{outcome: outcome, err: err}
	}()
	startedAuths := map[string]struct{}{}
	for len(startedAuths) < 2 {
		select {
		case authIndex := <-started:
			startedAuths[authIndex] = struct{}{}
			if len(startedAuths) == 1 {
				select {
				case err := <-skipCheck:
					if err != nil {
						t.Fatal(err)
					}
				case <-time.After(time.Second):
					t.Fatal("timed out waiting for unsupported skip to record before the discovery wave")
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for concurrent discovery probes, started=%v", startedAuths)
		}
	}
	close(release)
	result := <-resultCh
	if result.err != nil {
		t.Fatalf("reconcile discovery batch: %v", result.err)
	}
	if result.outcome.Probed != 2 || result.outcome.UnsupportedSkipped != 1 || result.outcome.ActionCount != 1 {
		t.Fatalf("unexpected discovery reconcile result: %+v", result.outcome)
	}
	observations, err := service.store.ListWatchdogProbeObservations(t.Context(), sidecar.ID, 10)
	if err != nil {
		t.Fatalf("list discovery observations: %v", err)
	}
	if got := []string{observations[0].AuthID, observations[1].AuthID}; !slices.Equal(got, []string{"auth-discovery-b", "auth-discovery-a"}) {
		t.Fatalf("expected ordered sequential flush, observations=%+v", observations)
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	if got := countWatchdogActions(actions, watchdogProbeStatusSkippedUnsupportedProvider); got != 1 {
		t.Fatalf("unsupported discovery skips should record once, got %d actions=%+v", got, actions)
	}
}
