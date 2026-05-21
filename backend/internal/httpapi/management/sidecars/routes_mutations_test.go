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

func TestOperatorPriorityPatchSyncedWithoutRemovedSurfaceSideEffects(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"priority":42}`, http.StatusOK, &response)
	if response.Snapshot == nil || response.Snapshot.Priority == nil || *response.Snapshot.Priority != 42 {
		t.Fatalf("expected refreshed priority=42 snapshot, got %+v", response.Snapshot)
	}
	patches := upstream.fieldPatchPayloads()
	if len(patches) != 1 || intFromAny(patches[0]["priority"]) != 42 || patches[0]["disabled"] != nil {
		t.Fatalf("unexpected fields patch payloads: %+v", patches)
	}
	for _, removed := range []string{"prefix", "proxy_url", "note", "headers", "custom_headers"} {
		if _, ok := patches[0][removed]; ok {
			t.Fatalf("priority patch must not forward removed field %s: %+v", removed, patches[0])
		}
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/auth-files/fields", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, []string{"/v0/management/auth-files", "/v0/management/auth-files/fields"})
}

func TestPatchAuthFilePriorityZeroMeaningFrozen(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.zeroPriorityPatchClears = true
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"priority":0}`, http.StatusOK, &response)
	if response.Snapshot == nil {
		t.Fatalf("expected refreshed snapshot after priority=0 patch")
	}
	if response.Snapshot.Priority != nil {
		t.Fatalf("fields PATCH priority=0 must use upstream clear/remove sentinel, got snapshot priority %+v", response.Snapshot.Priority)
	}
	patches := upstream.fieldPatchPayloads()
	if len(patches) != 1 {
		t.Fatalf("expected one fields patch payload, got %+v", patches)
	}
	priority, prioritySent := patches[0]["priority"]
	if !prioritySent || intFromAny(priority) != 0 {
		t.Fatalf("expected Prism to forward the API-specific priority=0 sentinel, patches=%+v", patches)
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/auth-files/fields", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, []string{"/v0/management/auth-files", "/v0/management/auth-files/fields"})
}

func TestAuthFieldsPayloadPriorityOnly(t *testing.T) {
	decode := func(tb testing.TB, body string) map[string]json.RawMessage {
		tb.Helper()
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			tb.Fatalf("decode mutation body: %v", err)
		}
		return raw
	}

	t.Run("priority-only payload preserves omitted removed fields", func(t *testing.T) {
		payload, fields, err := buildOperatorFieldsPayload(decode(t, `{"priority":7}`), retainedAuthName)
		if err != nil {
			t.Fatalf("build priority-only payload: %v", err)
		}
		if len(fields) != 1 || fields[0] != "priority" || intFromAny(payload["priority"]) != 7 || payload["name"] != retainedAuthName {
			t.Fatalf("unexpected priority-only payload fields=%v payload=%+v", fields, payload)
		}
		for _, removed := range []string{"prefix", "proxy_url", "note", "headers", "custom_headers"} {
			if _, ok := payload[removed]; ok {
				t.Fatalf("removed field %s must not be forwarded: %+v", removed, payload)
			}
		}
	})

	t.Run("priority zero remains explicit sentinel", func(t *testing.T) {
		payload, fields, err := buildOperatorFieldsPayload(decode(t, `{"priority":0}`), retainedAuthName)
		if err != nil {
			t.Fatalf("build priority=0 payload: %v", err)
		}
		if len(fields) != 1 || fields[0] != "priority" || intFromAny(payload["priority"]) != 0 {
			t.Fatalf("priority=0 must remain a forwarded sentinel fields=%v payload=%+v", fields, payload)
		}
	})

	rejections := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "prefix", body: `{"priority":7,"prefix":"team-a/"}`, wantErr: "unsupported fields: prefix"},
		{name: "proxy_url", body: `{"priority":7,"proxy_url":"https://proxy.example.test"}`, wantErr: "unsupported fields: proxy_url"},
		{name: "note", body: `{"priority":7,"note":"operator note"}`, wantErr: "unsupported fields: note"},
		{name: "headers", body: `{"priority":7,"headers":{"X-Trace-ID":"trace-1"}}`, wantErr: "unsupported fields: headers"},
		{name: "custom_headers", body: `{"priority":7,"custom_headers":{"X-Trace-ID":"trace-1"}}`, wantErr: "unsupported fields: custom_headers"},
	}
	for _, tt := range rejections {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := buildOperatorFieldsPayload(decode(t, tt.body), retainedAuthName)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestOperatorStatusPatchSyncedWithoutRemovedSurfaceSideEffects(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "status", `{"disabled":true}`, http.StatusOK, &response)
	if response.Snapshot == nil || response.Snapshot.Disabled == nil || !*response.Snapshot.Disabled {
		t.Fatalf("expected refreshed disabled snapshot, got %+v", response.Snapshot)
	}
	patches := upstream.statusPatchPayloads()
	if len(patches) != 1 || patches[0]["disabled"] != true || patches[0]["priority"] != nil {
		t.Fatalf("unexpected status patch payloads: %+v", patches)
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/auth-files/status", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, []string{"/v0/management/auth-files", "/v0/management/auth-files/status"})
}

func TestOperatorMutationStaleSnapshotRequiresForceLive(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	staleAt := now.Add(-3 * time.Hour)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, staleAt, retainedAuthFixture{Priority: 100})
	staleAfter := staleAt.Add(time.Minute)
	if _, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAfter, ManagementAuthState: ManagementAuthStateValid}); err != nil {
		t.Fatalf("mark snapshot stale: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"priority":50}`, http.StatusConflict, nil)
	if len(upstream.fieldPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 0 {
		t.Fatalf("stale mutation without force_live should not call upstream, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, nil)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields?force_live=true", `{"priority":55}`, http.StatusOK, &response)
	if response.Snapshot == nil || response.Snapshot.Priority == nil || *response.Snapshot.Priority != 55 {
		t.Fatalf("force_live mutation did not refresh snapshot: %+v", response.Snapshot)
	}
	if len(upstream.fieldPatchPayloads()) != 1 || upstream.getAuthFilesCount() < 2 {
		t.Fatalf("force_live should preflight and sync, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/auth-files/fields", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, []string{"/v0/management/auth-files", "/v0/management/auth-files/fields"})
}

func TestOperatorMutationParseFailuresSkipRemovedSurfaceActions(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"priority":`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"priority":50,"force_live":"yes"}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "status?legacy_control=true", `{"disabled":false}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "status", `{"disabled":false,"legacy_control":true}`, http.StatusBadRequest, nil)
	if len(upstream.fieldPatchPayloads()) != 0 || len(upstream.statusPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 0 {
		t.Fatalf("parse failures should not call upstream, fields=%v status=%v gets=%d", upstream.fieldPatchPayloads(), upstream.statusPatchPayloads(), upstream.getAuthFilesCount())
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, nil)
}

func TestOperatorMutationRejectsRemovedAuthFields(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	rejections := []struct {
		name string
		body string
	}{
		{name: "prefix", body: `{"priority":50,"prefix":"team-a/"}`},
		{name: "proxy_url", body: `{"priority":50,"proxy_url":"https://proxy.example.test"}`},
		{name: "note", body: `{"priority":50,"note":"operator note"}`},
		{name: "headers", body: `{"priority":50,"headers":{"X-Trace-ID":"trace-1"}}`},
		{name: "custom_headers", body: `{"priority":50,"custom_headers":{"X-Trace-ID":"trace-1"}}`},
	}
	for _, tt := range rejections {
		t.Run(tt.name, func(t *testing.T) {
			patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", tt.body, http.StatusBadRequest, nil)
		})
	}
	if len(upstream.fieldPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 0 {
		t.Fatalf("removed field rejections should not call upstream, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
	}
	upstream.assertAllowedManagementPaths(t, nil, nil)
}

func TestOperatorMutationForceLivePreflightFailureSkipsRemovedSurfaceActions(t *testing.T) {
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
	seedRetainedAuthSnapshot(t, service, sidecar.ID, staleAt, retainedAuthFixture{Priority: 100})
	if _, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAt, ManagementAuthState: ManagementAuthStateValid}); err != nil {
		t.Fatalf("mark stale snapshot: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields?force_live=true", `{"priority":50}`, http.StatusFailedDependency, nil)
	stored, _, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || stored.ManagementAuthState != ManagementAuthStateInvalid {
		t.Fatalf("expected invalid management auth state after preflight failure, sidecar=%+v err=%v", stored, err)
	}
}

func TestOperatorMutationRejectsSecretFields(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"api_key":"raw-secret"}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"headers":{"Authorization":"Bearer raw-secret"}}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"headers":{"Set-Cookie":"session=raw-secret"}}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"note":"bearer raw-secret"}`, http.StatusBadRequest, nil)
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"proxy_url":"https://user:raw-secret@example.com"}`, http.StatusBadRequest, nil)
	if len(upstream.fieldPatchPayloads()) != 0 || len(upstream.statusPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 0 {
		t.Fatalf("secret rejection should not call upstream, fields=%v status=%v gets=%d", upstream.fieldPatchPayloads(), upstream.statusPatchPayloads(), upstream.getAuthFilesCount())
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, nil)
}

func TestAuthMutationRejectsUnsafeIdentity(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)

	t.Run("duplicate live names", func(t *testing.T) {
		upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
		defer upstream.Close()
		duplicate := retainedAuthPayload(retainedAuthFixture{Priority: 90})
		duplicate["id"] = "auth-gemini-shadow"
		duplicate["auth_index"] = "auth_999"
		duplicate["name"] = retainedAuthName
		upstream.setAuthFiles(retainedAuthPayload(retainedAuthFixture{Priority: 100}), duplicate)
		service := newSyncTestService(t, func() time.Time { return now })
		sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
		seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
		router := chi.NewRouter()
		service.MountManagementRoutes(router)

		patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"priority":41}`, http.StatusConflict, nil)
		if len(upstream.fieldPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 1 {
			t.Fatalf("duplicate-name target must preflight once and refuse before PATCH, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
		}
	})

	t.Run("name derived degraded row", func(t *testing.T) {
		upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
		defer upstream.Close()
		degraded := map[string]any{"name": "gemini-disk-scan-only.json", "provider": "gemini", "priority": 5, "status": "active", "disabled": false}
		upstream.expectedPatchName = "gemini-disk-scan-only.json"
		upstream.setAuthFiles(degraded)
		service := newSyncTestService(t, func() time.Time { return now })
		sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
		markRetainedAuthSnapshotsFresh(t, service, sidecar.ID, now)
		_, err := service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: "gemini-disk-scan-only.json", Name: "gemini-disk-scan-only.json", Provider: stringPtr("gemini"), Priority: intPtr(5), SnapshotJSON: json.RawMessage(`{"condition":"condition_unobservable"}`), ObservedAt: now})
		if err != nil {
			t.Fatalf("seed degraded snapshot: %v", err)
		}
		router := chi.NewRouter()
		service.MountManagementRoutes(router)

		patchAuthMutation(t, router, sidecar.ID, "gemini-disk-scan-only.json", "fields", `{"priority":7}`, http.StatusConflict, nil)
		if len(upstream.fieldPatchPayloads()) != 0 || upstream.getAuthFilesCount() != 1 {
			t.Fatalf("name-derived target must preflight once and refuse before PATCH, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
		}
	})
}

func TestAuthMutationAllowsForceLiveRetry(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	staleAt := now.Add(-3 * time.Hour)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	renamed := retainedAuthPayload(retainedAuthFixture{Priority: 100})
	renamed["name"] = "gemini-renamed.json"
	upstream.expectedPatchName = "gemini-renamed.json"
	upstream.setAuthFiles(renamed)
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 60)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, staleAt, retainedAuthFixture{Priority: 100})
	if _, err := service.store.UpdateSidecarSyncMetadata(t.Context(), SidecarSyncMetadataInput{SidecarID: sidecar.ID, LastSyncAt: staleAt, LastSuccessfulSyncAt: &staleAt, SnapshotStaleAfter: &staleAt, ManagementAuthState: ManagementAuthStateValid}); err != nil {
		t.Fatalf("mark stale snapshot: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields?force_live=true", `{"priority":66}`, http.StatusOK, &response)
	if response.State != "succeeded" || response.Snapshot == nil || response.Snapshot.Name != "gemini-renamed.json" || response.Snapshot.Priority == nil || *response.Snapshot.Priority != 66 {
		t.Fatalf("force-live retry did not use current live identity and refresh snapshot: %+v", response)
	}
	patches := upstream.fieldPatchPayloads()
	if len(patches) != 1 || patches[0]["name"] != "gemini-renamed.json" || intFromAny(patches[0]["priority"]) != 66 || upstream.getAuthFilesCount() < 2 {
		t.Fatalf("force-live retry should preflight, patch live name, and resync; patches=%v gets=%d", patches, upstream.getAuthFilesCount())
	}
}

func TestAuthMutationResponseMarksSyncFailure(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.authFilesFailureAfterPatch = true
	upstream.authFilesFailureStatus = http.StatusInternalServerError
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	patchAuthMutation(t, router, sidecar.ID, retainedAuthID, "fields", `{"priority":44}`, http.StatusOK, &response)
	if response.State != "succeeded_sync_failed" || response.SyncError == nil || strings.TrimSpace(*response.SyncError) == "" {
		t.Fatalf("expected succeeded_sync_failed with sync_error, got %+v", response)
	}
	if response.Snapshot == nil || response.Snapshot.Priority == nil || *response.Snapshot.Priority != 100 {
		t.Fatalf("sync-failed response should not present stale mutation as refreshed truth: %+v", response.Snapshot)
	}
	if len(upstream.fieldPatchPayloads()) != 1 || upstream.getAuthFilesCount() < 2 {
		t.Fatalf("expected one successful patch and failed resync, patches=%v gets=%d", upstream.fieldPatchPayloads(), upstream.getAuthFilesCount())
	}
}

func TestOperatorDeleteAuthFileUsesLiveNameAndResyncs(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	survivor := retainedAuthPayload(retainedAuthFixture{Priority: 75, Provider: "claude"})
	survivor["id"] = "auth-claude-survivor"
	survivor["auth_index"] = "auth_002"
	survivor["name"] = "claude-survivor.json"
	survivor["provider"] = "claude"
	upstream.setAuthFiles(retainedAuthPayload(retainedAuthFixture{Priority: 100}), survivor)
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"`+retainedAuthName+`"}`, http.StatusOK, &response)
	if response.State != "succeeded" {
		t.Fatalf("expected succeeded delete response, got %+v", response)
	}
	if response.Snapshot != nil {
		t.Fatalf("deleted auth file should be absent after successful resync, got %+v", response.Snapshot)
	}
	deletes := upstream.deletePayloads()
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0] != retainedAuthName {
		t.Fatalf("expected single upstream delete for live name, got %+v", deletes)
	}
	if upstream.getAuthFilesCount() < 2 {
		t.Fatalf("delete should preflight and resync auth files, gets=%d", upstream.getAuthFilesCount())
	}
	upstream.assertAllowedManagementPaths(t, []string{"/v0/management/auth-files", "/v0/management/gemini-api-key", "/v0/management/claude-api-key", "/v0/management/codex-api-key", "/v0/management/vertex-api-key", "/v0/management/openai-compatibility"}, []string{"/v0/management/auth-files"})
}

func TestOperatorDeleteAuthFileCanCleanlyDeleteLastAuthFile(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"`+retainedAuthName+`"}`, http.StatusOK, &response)
	if response.State != "succeeded" {
		t.Fatalf("expected succeeded delete response for last auth file, got %+v", response)
	}
	if response.Snapshot != nil {
		t.Fatalf("last deleted auth file should be absent after clean refresh, got %+v", response.Snapshot)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 0 {
		t.Fatalf("last delete should clear local auth snapshots, got %+v err=%v", auths, err)
	}
	stored, ok, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || !ok || stored.LastSyncError != nil {
		t.Fatalf("last delete should finish with clean sync metadata, ok=%v stored=%+v err=%v", ok, stored, err)
	}
	deletes := upstream.deletePayloads()
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0] != retainedAuthName {
		t.Fatalf("expected single upstream delete for live name, got %+v", deletes)
	}
	if upstream.getAuthFilesCount() != 2 {
		t.Fatalf("last delete should preflight and refresh auth files exactly once each, gets=%d", upstream.getAuthFilesCount())
	}
}

func TestOperatorDeleteAuthFileRejectsUnknownCapabilityBeforeDelete(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.deleteCapabilityHeaders = false
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"`+retainedAuthName+`"}`, http.StatusFailedDependency, nil)
	if len(upstream.deletePayloads()) != 0 || upstream.getAuthFilesCount() != 1 {
		t.Fatalf("unknown delete capability should preflight once and not delete, deletes=%v gets=%d", upstream.deletePayloads(), upstream.getAuthFilesCount())
	}
}

func TestOperatorDeleteAuthFileAllowsCompatibleBuildAfterNonMutatingProbe(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.deleteCapabilityCommit = "ffffffffffffffffffffffffffffffffffffffff"
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"`+retainedAuthName+`"}`, http.StatusOK, &response)
	if response.State != "succeeded" {
		t.Fatalf("expected compatible build delete to succeed after probe, got %+v", response)
	}
	deletes := upstream.deletePayloads()
	if len(deletes) != 2 || len(deletes[0]) != 0 || len(deletes[1]) != 1 || deletes[1][0] != retainedAuthName {
		t.Fatalf("expected empty capability probe before single delete, got %+v", deletes)
	}
}

func TestSyncMarksDeleteSupportedForCompatibleBuildAfterNonMutatingProbe(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.deleteCapabilityCommit = "ffffffffffffffffffffffffffffffffffffffff"
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)

	if _, err := service.SyncSidecar(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("sync compatible delete capability: %v", err)
	}
	deletes := upstream.deletePayloads()
	if len(deletes) != 1 || len(deletes[0]) != 0 {
		t.Fatalf("expected one empty capability probe during sync, got %+v", deletes)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 1 {
		t.Fatalf("load synced auth snapshots len=%d err=%v", len(auths), err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(auths[0].SnapshotJSON, &snapshot); err != nil {
		t.Fatalf("decode auth snapshot json: %v", err)
	}
	if snapshot["delete_supported"] != true {
		t.Fatalf("compatible probed build should surface delete_supported=true, snapshot=%s", auths[0].SnapshotJSON)
	}
}

func TestOperatorDeleteAuthFileRejectsStaleConfirmation(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"wrong.json"}`, http.StatusConflict, nil)
	if len(upstream.deletePayloads()) != 0 || upstream.getAuthFilesCount() != 1 {
		t.Fatalf("stale confirmation should preflight once and not delete, deletes=%v gets=%d", upstream.deletePayloads(), upstream.getAuthFilesCount())
	}
}

func TestOperatorDeleteAuthFileRejectsUnsafeRows(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "runtime only", mutate: func(row map[string]any) { row["runtime_only"] = true }},
		{name: "memory source", mutate: func(row map[string]any) { row["source"] = "memory" }},
		{name: "missing path", mutate: func(row map[string]any) { delete(row, "path") }},
		{name: "path like name", mutate: func(row map[string]any) { row["name"] = "nested/" + retainedAuthName }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
			defer upstream.Close()
			row := retainedAuthPayload(retainedAuthFixture{Priority: 100})
			tt.mutate(row)
			upstream.setAuthFiles(row)
			service := newSyncTestService(t, func() time.Time { return now })
			sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
			seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
			router := chi.NewRouter()
			service.MountManagementRoutes(router)

			deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"`+retainedAuthName+`"}`, http.StatusConflict, nil)
			if len(upstream.deletePayloads()) != 0 || upstream.getAuthFilesCount() != 1 {
				t.Fatalf("unsafe row should preflight once and not delete, deletes=%v gets=%d", upstream.deletePayloads(), upstream.getAuthFilesCount())
			}
		})
	}
}

func TestOperatorDeleteAuthFileRejectsNameDerivedIdentity(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	name := "gemini-disk-scan-only.json"
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.setAuthFiles(map[string]any{"name": name, "provider": "gemini", "status": "active", "disabled": false, "runtime_only": false, "source": "file", "path": "/mock/cliproxy/auth/" + name, "priority": 5})
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	markRetainedAuthSnapshotsFresh(t, service, sidecar.ID, now)
	_, err := service.store.SaveAuthSnapshot(t.Context(), SidecarAuthSnapshotInput{SidecarID: sidecar.ID, AuthID: name, Name: name, Provider: stringPtr("gemini"), Priority: intPtr(5), SnapshotJSON: json.RawMessage(`{"condition":"condition_unobservable"}`), ObservedAt: now})
	if err != nil {
		t.Fatalf("seed name-derived delete snapshot: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	deleteAuthMutation(t, router, sidecar.ID, name, `{"confirm_name":"`+name+`"}`, http.StatusConflict, nil)
	if len(upstream.deletePayloads()) != 0 || upstream.getAuthFilesCount() != 1 {
		t.Fatalf("name-derived delete target should preflight once and not delete, deletes=%v gets=%d", upstream.deletePayloads(), upstream.getAuthFilesCount())
	}
}

func TestOperatorDeleteAuthFileResponseMarksSyncFailure(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.authFilesFailureAfterDelete = true
	upstream.authFilesFailureStatus = http.StatusInternalServerError
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response authMutationResponse
	deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"`+retainedAuthName+`"}`, http.StatusOK, &response)
	if response.State != "succeeded_sync_failed" || response.SyncError == nil || strings.TrimSpace(*response.SyncError) == "" {
		t.Fatalf("expected succeeded_sync_failed delete response, got %+v", response)
	}
	if response.Snapshot == nil || response.Snapshot.Name != retainedAuthName {
		t.Fatalf("sync-failed delete response should retain pre-delete snapshot, got %+v", response.Snapshot)
	}
	if len(upstream.deletePayloads()) != 1 || upstream.getAuthFilesCount() < 2 {
		t.Fatalf("expected one delete and failed resync, deletes=%v gets=%d", upstream.deletePayloads(), upstream.getAuthFilesCount())
	}
}

func TestOperatorDeleteAuthFileUpstreamFailureLeavesSnapshotIntact(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	upstream := newOperatorMutationUpstream(t, retainedAuthFixture{Priority: 100})
	upstream.deleteFailureStatus = http.StatusFailedDependency
	upstream.deleteFailureBody = `{"error":"upstream delete refused"}`
	defer upstream.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, upstream.URL(), true, 3600)
	seedRetainedAuthSnapshot(t, service, sidecar.ID, now, retainedAuthFixture{Priority: 100})
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	deleteAuthMutation(t, router, sidecar.ID, retainedAuthID, `{"confirm_name":"`+retainedAuthName+`"}`, http.StatusBadGateway, nil)
	deletes := upstream.deletePayloads()
	if len(deletes) != 1 || len(deletes[0]) != 1 || deletes[0][0] != retainedAuthName {
		t.Fatalf("expected one named upstream delete attempt, got %+v", deletes)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 1 || auths[0].AuthID != retainedAuthID {
		t.Fatalf("upstream delete failure should leave local snapshot intact, got %+v err=%v", auths, err)
	}
	if upstream.getAuthFilesCount() != 1 {
		t.Fatalf("upstream delete failure should not run post-delete refresh, gets=%d", upstream.getAuthFilesCount())
	}
}

type operatorMutationUpstream struct {
	t      *testing.T
	server *httptest.Server
	mu     sync.Mutex
	auth   retainedAuthFixture

	paths                       []string
	fieldPatches                []map[string]any
	statusPatches               []map[string]any
	deleteRequests              [][]string
	authFiles                   []map[string]any
	getAuthFilesCalls           int
	expectedPatchName           string
	authFilesFailureOnCall      int
	authFilesFailureAfterPatch  bool
	authFilesFailureAfterDelete bool
	authFilesFailureStatus      int
	authFilesFailureBody        string
	deleteFailureStatus         int
	deleteFailureBody           string
	zeroPriorityPatchClears     bool
	deleteCapabilityHeaders     bool
	deleteCapabilityCommit      string
}

func newOperatorMutationUpstream(t *testing.T, auth retainedAuthFixture) *operatorMutationUpstream {
	t.Helper()
	upstream := &operatorMutationUpstream{t: t, auth: auth, expectedPatchName: retainedAuthName, deleteCapabilityHeaders: true}
	upstream.setAuthFiles(retainedAuthPayload(auth))
	upstream.server = httptest.NewServer(http.HandlerFunc(upstream.serveHTTP))
	return upstream
}

func (u *operatorMutationUpstream) Close() { u.server.Close() }

func (u *operatorMutationUpstream) URL() string { return u.server.URL }

func (u *operatorMutationUpstream) setAuthFiles(files ...map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authFiles = make([]map[string]any, 0, len(files))
	for _, file := range files {
		u.authFiles = append(u.authFiles, cloneMutationPayload(file))
	}
}

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

func (u *operatorMutationUpstream) deletePayloads() [][]string {
	u.mu.Lock()
	defer u.mu.Unlock()
	copies := make([][]string, 0, len(u.deleteRequests))
	for _, payload := range u.deleteRequests {
		copies = append(copies, append([]string(nil), payload...))
	}
	return copies
}

func (u *operatorMutationUpstream) getAuthFilesCount() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.getAuthFilesCalls
}

func (u *operatorMutationUpstream) assertAllowedManagementPaths(t *testing.T, allowed []string, required []string) {
	t.Helper()
	u.mu.Lock()
	paths := append([]string(nil), u.paths...)
	u.mu.Unlock()
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, path := range allowed {
		allowedSet[path] = struct{}{}
	}
	for _, path := range paths {
		if _, ok := allowedSet[path]; !ok {
			t.Fatalf("unexpected management path %s; paths=%v allowed=%v", path, paths, allowed)
		}
	}
	for _, path := range required {
		found := false
		for _, observed := range paths {
			if observed == path {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing required management path %s; paths=%v required=%v", path, paths, required)
		}
	}
}

func (u *operatorMutationUpstream) serveHTTP(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	u.paths = append(u.paths, r.URL.Path)
	u.mu.Unlock()
	if r.Header.Get("X-Management-Key") != "sync-secret" {
		u.t.Errorf("expected management key header, got %q", r.Header.Get("X-Management-Key"))
	}
	switch r.URL.Path {
	case "/v0/management/auth-files":
		switch r.Method {
		case http.MethodGet:
			u.serveAuthFiles(w)
		case http.MethodDelete:
			u.serveAuthFilesDelete(w, r)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
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
	callNumber := u.getAuthFilesCalls
	zeroPriorityPatchClears := u.zeroPriorityPatchClears
	failureOnCall := u.authFilesFailureOnCall
	failureAfterMutation := (u.authFilesFailureAfterPatch && (len(u.fieldPatches) > 0 || len(u.statusPatches) > 0)) || (u.authFilesFailureAfterDelete && len(u.deleteRequests) > 0)
	failureStatus := u.authFilesFailureStatus
	failureBody := u.authFilesFailureBody
	deleteCapabilityHeaders := u.deleteCapabilityHeaders
	deleteCapabilityCommit := u.deleteCapabilityCommit
	files := make([]map[string]any, 0, len(u.authFiles))
	for _, file := range u.authFiles {
		files = append(files, cloneMutationPayload(file))
	}
	u.mu.Unlock()
	if failureAfterMutation || (failureOnCall > 0 && callNumber == failureOnCall) {
		if failureStatus == 0 {
			failureStatus = http.StatusInternalServerError
		}
		if failureBody == "" {
			failureBody = `{"error":"sync failed"}`
		}
		w.WriteHeader(failureStatus)
		_, _ = w.Write([]byte(failureBody))
		return
	}
	if deleteCapabilityHeaders {
		if strings.TrimSpace(deleteCapabilityCommit) == "" {
			deleteCapabilityCommit = cliProxyAuthFileDeleteBaselineCommit
		}
		w.Header().Set("X-CPA-COMMIT", deleteCapabilityCommit)
		w.Header().Set("X-CPA-VERSION", "test-delete-capable")
		w.Header().Set("X-CPA-BUILD-DATE", "2026-05-21T00:00:00Z")
	}
	payloads := make([]any, 0, len(files))
	for _, payload := range files {
		if zeroPriorityPatchClears && intFromAny(payload["priority"]) == 0 {
			delete(payload, "priority")
		}
		payload["api_key"] = "raw-sync-secret"
		payloads = append(payloads, payload)
	}
	writeAuthFixtureJSON(w, map[string]any{"files": payloads})
}

func (u *operatorMutationUpstream) serveAuthFilesDelete(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Names []string `json:"names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		u.t.Fatalf("decode auth delete payload: %v", err)
	}
	u.mu.Lock()
	u.deleteRequests = append(u.deleteRequests, append([]string(nil), payload.Names...))
	deleteFailureStatus := u.deleteFailureStatus
	deleteFailureBody := u.deleteFailureBody
	if len(payload.Names) == 0 {
		u.mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid name"}`))
		return
	}
	if deleteFailureStatus != 0 {
		u.mu.Unlock()
		if deleteFailureBody == "" {
			deleteFailureBody = `{"error":"delete failed"}`
		}
		w.WriteHeader(deleteFailureStatus)
		_, _ = w.Write([]byte(deleteFailureBody))
		return
	}
	deleteSet := make(map[string]struct{}, len(payload.Names))
	for _, name := range payload.Names {
		deleteSet[name] = struct{}{}
	}
	remaining := make([]map[string]any, 0, len(u.authFiles))
	for _, file := range u.authFiles {
		name, _ := file["name"].(string)
		if _, ok := deleteSet[name]; ok {
			continue
		}
		remaining = append(remaining, file)
	}
	u.authFiles = remaining
	u.mu.Unlock()
	writeAuthFixtureJSON(w, map[string]any{"status": "ok", "deleted": len(payload.Names), "api_key": "raw-delete-response-secret"})
}

func (u *operatorMutationUpstream) serveFieldsPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	patch := decodeMutationPatchPayload(u.t, r)
	u.mu.Lock()
	expectedName := u.expectedPatchName
	u.fieldPatches = append(u.fieldPatches, cloneMutationPayload(patch))
	if priority, ok := patch["priority"]; ok {
		for index := range u.authFiles {
			if u.authFiles[index]["name"] == patch["name"] {
				u.authFiles[index]["priority"] = intFromAny(priority)
			}
		}
	}
	u.mu.Unlock()
	if patch["name"] != expectedName {
		u.t.Errorf("unexpected fields patch target: %+v", patch)
	}
	writeAuthFixtureJSON(w, map[string]any{"status": "ok", "api_key": "raw-response-secret", "headers": map[string]any{"Authorization": "Bearer response-secret"}})
}

func (u *operatorMutationUpstream) serveStatusPatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPatch {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	patch := decodeMutationPatchPayload(u.t, r)
	u.mu.Lock()
	expectedName := u.expectedPatchName
	u.statusPatches = append(u.statusPatches, cloneMutationPayload(patch))
	if disabled, ok := patch["disabled"].(bool); ok {
		for index := range u.authFiles {
			if u.authFiles[index]["name"] == patch["name"] {
				u.authFiles[index]["disabled"] = disabled
			}
		}
	}
	u.mu.Unlock()
	if patch["name"] != expectedName {
		u.t.Errorf("unexpected status patch target: %+v", patch)
	}
	writeAuthFixtureJSON(w, map[string]any{"status": "ok", "disabled": patch["disabled"], "api_key": "raw-status-response-secret"})
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

func deleteAuthMutation(t *testing.T, router http.Handler, sidecarID int, authID string, body string, wantStatus int, target any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	path := "/sidecars/" + strconv.Itoa(sidecarID) + "/auth-files/" + authID
	req := httptest.NewRequest(http.MethodDelete, path, strings.NewReader(body))
	router.ServeHTTP(recorder, req)
	if recorder.Code != wantStatus {
		t.Fatalf("delete mutation status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertNoMutationSecretLeak(t, recorder.Body.String())
	if target != nil {
		if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
			t.Fatalf("decode delete mutation response: %v body=%s", err, recorder.Body.String())
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

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	default:
		return 0
	}
}

func assertNoMutationSecretLeak(t *testing.T, value string) {
	t.Helper()
	for _, secret := range []string{"raw-secret", "raw-response-secret", "response-secret", "raw-status-response-secret", "raw-delete-response-secret", "raw-sync-secret"} {
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
