package sidecars

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestOperatorPriorityPatchAuditedAndSynced(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, watchdogUpstreamAuth{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"priority":42,"headers":{"X-Trace-ID":"trace-1"}}`, http.StatusOK, &response)
	if response.Snapshot == nil || response.Snapshot.Priority == nil || *response.Snapshot.Priority != 42 {
		t.Fatalf("expected refreshed priority=42 snapshot, got %+v", response.Snapshot)
	}
	patches := upstream.fieldPatchPayloads()
	if len(patches) != 1 || intFromAny(patches[0]["priority"]) != 42 || patches[0]["disabled"] != nil {
		t.Fatalf("unexpected fields patch payloads: %+v", patches)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 1 || holds[0].ManualPauseUntil == nil {
		t.Fatalf("expected default manual watchdog pause, holds=%+v err=%v", holds, err)
	}
	action := requireOperatorPatchAction(t, service, sidecar.ID, watchdogActionStatusSucceeded)
	if action.HoldID == nil || action.Provider == nil || *action.Provider != "gemini" {
		t.Fatalf("operator action missing hold/provider identifiers: %+v", action)
	}
	if action.Reason == nil || !strings.Contains(*action.Reason, "priority") || !strings.Contains(*action.Reason, "headers.X-Trace-ID") {
		t.Fatalf("operator action reason missing requested field summary: %+v", action)
	}
	assertNoMutationSecretLeak(t, *action.Reason)
	assertActionHistoryRedacted(t, router, sidecar.ID)
}

func TestOperatorStatusPatchHonorsAllowWatchdog(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, watchdogUpstreamAuth{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "status", `{"disabled":true,"allow_watchdog":true}`, http.StatusOK, &response)
	if response.Snapshot == nil || response.Snapshot.Disabled == nil || !*response.Snapshot.Disabled {
		t.Fatalf("expected refreshed disabled snapshot, got %+v", response.Snapshot)
	}
	patches := upstream.statusPatchPayloads()
	if len(patches) != 1 || patches[0]["disabled"] != true || patches[0]["priority"] != nil {
		t.Fatalf("unexpected status patch payloads: %+v", patches)
	}
	holds, err := service.store.ListActiveWatchdogHolds(t.Context(), sidecar.ID)
	if err != nil || len(holds) != 0 {
		t.Fatalf("allow_watchdog=true should skip manual hold, holds=%+v err=%v", holds, err)
	}
	action := requireOperatorPatchAction(t, service, sidecar.ID, watchdogActionStatusSucceeded)
	if action.HoldID != nil || action.AuthID == nil || *action.AuthID != watchdogAuthID {
		t.Fatalf("unexpected status action identifiers: %+v", action)
	}
	if action.Reason == nil || !strings.Contains(*action.Reason, "disabled") {
		t.Fatalf("status action missing disabled summary: %+v", action)
	}
}

func TestOperatorMutationStaleSnapshotRequiresForceLive(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	staleAt := now.Add(-3 * time.Hour)
	upstream := newOperatorMutationUpstream(t, watchdogUpstreamAuth{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	seedWatchdogSnapshot(t, service, sidecar.ID, staleAt, watchdogUpstreamAuth{Priority: 100})
	staleAfter := staleAt.Add(time.Minute)
	if _, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAfter, ManagementAuthState: ManagementAuthStateValid}); err != nil {
		t.Fatalf("mark snapshot stale: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"priority":50}`, http.StatusConflict, nil)
	if len(upstream.fieldPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 0 {
		t.Fatalf("stale mutation without force_live should not call upstream, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
	}
	rejected := requireOperatorPatchAction(t, service, sidecar.ID, watchdogActionStatusFailed)
	if rejected.Reason == nil || !strings.Contains(*rejected.Reason, "stale_snapshot") {
		t.Fatalf("expected stale_snapshot audit reason, got %+v", rejected)
	}

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields?force_live=true&allow_watchdog=true", `{"priority":55}`, http.StatusOK, &response)
	if response.Snapshot == nil || response.Snapshot.Priority == nil || *response.Snapshot.Priority != 55 {
		t.Fatalf("force_live mutation did not refresh snapshot: %+v", response.Snapshot)
	}
	if len(upstream.fieldPatchPayloads()) != 1 || upstream.getAuthFilesCount() < 2 {
		t.Fatalf("force_live should preflight and sync, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
	}
}

func TestOperatorMutationParseFailuresAreAudited(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, watchdogUpstreamAuth{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"priority":`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"priority":50,"force_live":"yes"}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "status", `{"disabled":false,"allow_watchdog":"yes"}`, http.StatusBadRequest, nil)
	if len(upstream.fieldPatchPayloads()) != 0 || len(upstream.statusPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 0 {
		t.Fatalf("parse failures should not call upstream, fields=%v status=%v gets=%d", upstream.fieldPatchPayloads(), upstream.statusPatchPayloads(), upstream.getAuthFilesCount())
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	if len(actions) != 3 {
		t.Fatalf("expected audited parse/control failures, got %+v", actions)
	}
	for _, action := range actions {
		if action.ActionType != watchdogActionOperatorPatch || action.Status != watchdogActionStatusFailed {
			t.Fatalf("unexpected parse failure audit action: %+v", action)
		}
	}
}

func TestOperatorMutationForceLivePreflightFailureAudited(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		t.Fatalf("force_live preflight failure should not continue to %s", r.URL.Path)
	}))
	defer server.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 60)
	staleAt := now.Add(-3 * time.Hour)
	seedWatchdogSnapshot(t, service, sidecar.ID, staleAt, watchdogUpstreamAuth{Priority: 100})
	if _, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAt, ManagementAuthState: ManagementAuthStateValid}); err != nil {
		t.Fatalf("mark stale snapshot: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields?force_live=true", `{"priority":50}`, http.StatusFailedDependency, nil)
	action := requireOperatorPatchAction(t, service, sidecar.ID, watchdogActionStatusFailed)
	if action.Reason == nil || !strings.Contains(*action.Reason, string(CLIProxyErrorInvalidManagementAuth)) {
		t.Fatalf("expected audited invalid auth preflight failure, got %+v", action)
	}
	stored, _, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || stored.ManagementAuthState != ManagementAuthStateInvalid {
		t.Fatalf("expected invalid management auth state after preflight failure, sidecar=%+v err=%v", stored, err)
	}
}

func TestOperatorMutationUsesWatchdogLock(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, watchdogUpstreamAuth{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	if !service.tryAcquireWatchdogRun(sidecar.ID) {
		t.Fatalf("expected test to acquire watchdog lock")
	}
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"priority":50}`, http.StatusConflict, nil)
	service.releaseWatchdogRun(sidecar.ID)
	if len(upstream.fieldPatchPayloads()) != 0 {
		t.Fatalf("locked mutation should not patch upstream: %+v", upstream.fieldPatchPayloads())
	}
	action := requireOperatorPatchAction(t, service, sidecar.ID, watchdogActionStatusFailed)
	if action.Reason == nil || !strings.Contains(*action.Reason, "already running") {
		t.Fatalf("expected lock failure audit action, got %+v", action)
	}
}

func TestOperatorMutationRejectsSecretFields(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, watchdogUpstreamAuth{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedWatchdogSnapshot(t, service, sidecar.ID, now, watchdogUpstreamAuth{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"api_key":"raw-secret"}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"headers":{"Authorization":"Bearer raw-secret"}}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"headers":{"Set-Cookie":"session=raw-secret"}}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"note":"bearer raw-secret"}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, watchdogAuthID, "fields", `{"proxy_url":"https://user:raw-secret@example.com"}`, http.StatusBadRequest, nil)
	if len(upstream.fieldPatchPayloads()) != 0 || len(upstream.statusPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 0 {
		t.Fatalf("secret rejection should not call upstream, fields=%v status=%v gets=%d", upstream.fieldPatchPayloads(), upstream.statusPatchPayloads(), upstream.getAuthFilesCount())
	}
	actions := listWatchdogActions(t, service, sidecar.ID)
	if len(actions) != 5 {
		t.Fatalf("expected failed audit rows for rejected mutations, got %+v", actions)
	}
	for _, action := range actions {
		if action.ActionType != watchdogActionOperatorPatch || action.Status != watchdogActionStatusFailed {
			t.Fatalf("unexpected rejection audit action: %+v", action)
		}
		if action.Reason != nil {
			assertNoMutationSecretLeak(t, *action.Reason)
		}
	}
}

type operatorMutationUpstream struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	auth   watchdogUpstreamAuth

	fieldPatches      []map[string]any
	statusPatches     []map[string]any
	getAuthFilesCalls int
}

func newOperatorMutationUpstream(t *testing.T, auth watchdogUpstreamAuth) *operatorMutationUpstream {
	t.Helper()
	upstream := &operatorMutationUpstream{t: t, auth: auth}
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.serveHTTP))
	return upstream
}

func (u *operatorMutationUpstream) Close() { u.server.Close() }

func (u *operatorMutationUpstream) URL() string { return u.server.URL }

func (u *operatorMutationUpstream) fieldPatchPayloads() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return cloneMutationPayloads(u.fieldPatches)
}

func (u *operatorMutationUpstream) statusPatchPayloads() []map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	return cloneMutationPayloads(u.statusPatches)
}

func (u *operatorMutationUpstream) getAuthFilesCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.getAuthFilesCalls
}

func (u *operatorMutationUpstream) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Management-Key") != "sync-secret" {
		u.t.Errorf("expected management key header, got %q", r.Header.Get("X-Management-Key"))
	}
	switch r.URL.Path {
	case "/v0/management/auth-files":
		u.serveAuthFiles(w)
	case "/v0/management/auth-files/fields":
		u.serveFieldsPatch(w, r)
	case "/v0/management/auth-files/status":
		u.serveStatusPatch(w, r)
	case "/v0/management/gemini-api-key":
		writeSyncJSON(w, `{"gemini-api-key":[{"api-key":"redacted-gemini-key","priority":10,"auth-index":"auth_001"}]}`)
	case "/v0/management/claude-api-key":
		writeSyncJSON(w, `{"claude-api-key":[{"api-key":"redacted-claude-key","priority":10,"auth-index":"auth_002"}]}`)
	case "/v0/management/codex-api-key":
		writeSyncJSON(w, `{"codex-api-key":[{"api-key":"redacted-codex-key","priority":10,"auth-index":"auth_003"}]}`)
	case "/v0/management/vertex-api-key":
		writeSyncJSON(w, `{"vertex-api-key":[{"api-key":"redacted-vertex-key","priority":10,"auth-index":"auth_004"}]}`)
	case "/v0/management/openai-compatibility":
		writeSyncJSON(w, `{"openai-compatibility":[{"name":"compat","priority":10,"api-key-entries":[{"api-key":"redacted-openai-key","auth-index":"auth_005"}]}]}`)
	default:
		u.t.Errorf("unexpected management path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *operatorMutationUpstream) serveAuthFiles(w http.ResponseWriter) {
	u.mu.Lock()
	u.getAuthFilesCalls++
	auth := u.auth
	u.mu.Unlock()
	payload := watchdogAuthPayload(auth)
	payload["api_key"] = "raw-sync-secret"
	writeWatchdogJSON(w, map[string]any{"auth_files": []any{payload}})
}

func (u *operatorMutationUpstream) serveFieldsPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	patch := decodeMutationPatchPayload(u.t, r)
	if patch["name"] != watchdogAuthName {
		u.t.Errorf("unexpected fields patch target: %+v", patch)
	}
	u.mu.Lock()
	u.fieldPatches = append(u.fieldPatches, cloneMutationPayload(patch))
	if priority, ok := patch["priority"]; ok {
		u.auth.Priority = intFromAny(priority)
	}
	u.mu.Unlock()
	writeWatchdogJSON(w, map[string]any{"status": "ok", "api_key": "raw-response-secret", "headers": map[string]any{"Authorization": "Bearer response-secret"}})
}

func (u *operatorMutationUpstream) serveStatusPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	patch := decodeMutationPatchPayload(u.t, r)
	if patch["name"] != watchdogAuthName {
		u.t.Errorf("unexpected status patch target: %+v", patch)
	}
	u.mu.Lock()
	u.statusPatches = append(u.statusPatches, cloneMutationPayload(patch))
	if disabled, ok := patch["disabled"].(bool); ok {
		u.auth.Disabled = disabled
	}
	u.mu.Unlock()
	writeWatchdogJSON(w, map[string]any{"status": "ok", "disabled": patch["disabled"], "api_key": "raw-status-response-secret"})
}

func patchAuthMutation(t *testing.T, router http.Handler, sidecarID int, authID string, route string, body string, wantStatus int, target any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	path := "/sidecars/" + strconv.Itoa(sidecarID) + "/auth-files/" + authID + "/" + route
	req := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(body))
	router.ServeHTTP(recorder, req)
	if recorder.Code != wantStatus {
		t.Fatalf("mutation status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertNoMutationSecretLeak(t, recorder.Body.String())
	if target != nil {
		if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
			t.Fatalf("decode mutation response: %v body=%s", err, recorder.Body.String())
		}
	}
}

func decodeMutationPatchPayload(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	defer r.Body.Close()
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode mutation patch payload: %v", err)
	}
	return payload
}

func requireOperatorPatchAction(t *testing.T, service *Service, sidecarID int, status string) SidecarWatchdogAction {
	t.Helper()
	for _, action := range listWatchdogActions(t, service, sidecarID) {
		if action.ActionType == watchdogActionOperatorPatch && action.Status == status {
			return action
		}
	}
	t.Fatalf("missing operator_patch action with status %s", status)
	return SidecarWatchdogAction{}
}

func assertActionHistoryRedacted(t *testing.T, router http.Handler, sidecarID int) {
	t.Helper()
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/actions", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("actions status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertNoMutationSecretLeak(t, recorder.Body.String())
	if !strings.Contains(recorder.Body.String(), "operator_patch") || !strings.Contains(recorder.Body.String(), "provider") {
		t.Fatalf("action history response missing operator/provider fields: %s", recorder.Body.String())
	}
}

func assertNoMutationSecretLeak(t *testing.T, value string) {
	t.Helper()
	for _, secret := range []string{"raw-secret", "raw-response-secret", "response-secret", "raw-status-response-secret", "raw-sync-secret"} {
		if strings.Contains(value, secret) {
			t.Fatalf("mutation output leaked secret %q in %s", secret, value)
		}
	}
}

func cloneMutationPayloads(items []map[string]any) []map[string]any {
	copies := make([]map[string]any, 0, len(items))
	for _, item := range items {
		copies = append(copies, cloneMutationPayload(item))
	}
	return copies
}

func cloneMutationPayload(item map[string]any) map[string]any {
	copy := make(map[string]any, len(item))
	for key, value := range item {
		copy[key] = value
	}
	return copy
}
