package sidecars

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

func TestManualSyncPersistsSnapshotsAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	requested := make([]string, 0)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		if r.Header.Get("X-Management-Key") != "sync-secret" {
			t.Errorf("expected management key header, got %q", r.Header.Get("X-Management-Key"))
		}
		switch r.URL.Path {
		case "/v0/management/auth-files":
			writeSyncJSON(w, syncAuthFixture())
		case "/v0/management/gemini-api-key":
			writeSyncJSON(w, `{"gemini-api-key":[{"api-key":"raw-provider-secret","priority":10,"prefix":"team-a/","auth-index":"auth_001","headers":{"Authorization":"Bearer raw-provider-token","X-API-Key":"raw-header-key"}}]}`)
		case "/v0/management/claude-api-key":
			writeSyncJSON(w, `{"claude-api-key":[{"api-key":"redacted-claude-key","priority":10,"auth-index":"auth_002"}]}`)
		case "/v0/management/codex-api-key":
			writeSyncJSON(w, `{"codex-api-key":[{"api-key":"redacted-codex-key","priority":10,"auth-index":"auth_003"}]}`)
		case "/v0/management/vertex-api-key":
			writeSyncJSON(w, `{"vertex-api-key":[{"api-key":"redacted-vertex-key","priority":10,"auth-index":"auth_004"}]}`)
		case "/v0/management/openai-compatibility":
			writeSyncJSON(w, `{"openai-compatibility":[{"name":"compat","priority":10,"api-key-entries":[{"api-key":"raw-openai-secret","auth-index":"auth_005"}]}]}`)
		case "/v0/management/usage-queue":
			t.Errorf("sync must not call destructive usage-queue endpoint")
			w.WriteHeader(http.StatusTeapot)
		default:
			t.Errorf("unexpected management path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 60)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response sidecarSyncResponse
	postSidecarSync(t, router, sidecar.ID, http.StatusOK, &response)
	if response.AuthSnapshotCount != 1 || response.ProviderSnapshotCount != 5 {
		t.Fatalf("unexpected sync counts: %+v", response)
	}
	stored, ok, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || !ok {
		t.Fatalf("load synced sidecar: ok=%v err=%v", ok, err)
	}
	if stored.LastSyncAt == nil || !stored.LastSyncAt.Equal(now) || stored.LastSuccessfulSyncAt == nil || !stored.LastSuccessfulSyncAt.Equal(now) {
		t.Fatalf("sync metadata not persisted: %+v", stored)
	}
	wantStaleAfter := now.Add(2 * time.Duration(stored.SyncIntervalSeconds) * time.Second)
	if stored.SnapshotStaleAfter == nil || !stored.SnapshotStaleAfter.Equal(wantStaleAfter) || stored.LastSyncError != nil || stored.ManagementAuthState != ManagementAuthStateValid {
		t.Fatalf("success metadata mismatch: %+v", stored)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 1 {
		t.Fatalf("list auth snapshots len=%d err=%v", len(auths), err)
	}
	firstAuthID := auths[0].AuthID
	if auths[0].Priority == nil || *auths[0].Priority != 0 || auths[0].QuotaExceeded == nil || !*auths[0].QuotaExceeded {
		t.Fatalf("auth priority/quota not normalized: %+v", auths[0])
	}
	if strings.Contains(string(auths[0].SnapshotJSON), "should-not-be-stored") {
		t.Fatalf("auth snapshot stored unsupported runtime metadata: %s", auths[0].SnapshotJSON)
	}
	providers, err := service.store.ListProviderSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(providers) != 5 {
		t.Fatalf("list provider snapshots len=%d err=%v", len(providers), err)
	}
	for _, provider := range providers {
		snapshotJSON := string(provider.SnapshotJSON)
		if strings.Contains(snapshotJSON, "raw-provider-secret") || strings.Contains(snapshotJSON, "raw-openai-secret") || strings.Contains(snapshotJSON, "raw-provider-token") || strings.Contains(snapshotJSON, "raw-header-key") {
			t.Fatalf("provider snapshot leaked raw secret: %s", provider.SnapshotJSON)
		}
	}
	assertSidecarSnapshotRoutes(t, router, sidecar.ID)
	now = now.Add(time.Minute)
	postSidecarSync(t, router, sidecar.ID, http.StatusOK, &response)
	auths, err = service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 1 {
		t.Fatalf("list repeated auth snapshots len=%d err=%v", len(auths), err)
	}
	if auths[0].AuthID != firstAuthID || !auths[0].ObservedAt.Equal(now) {
		t.Fatalf("expected idempotent auth identity with fresh observed_at, got %+v", auths[0])
	}
	providers, err = service.store.ListProviderSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(providers) != 5 {
		t.Fatalf("expected provider snapshots to upsert, len=%d err=%v", len(providers), err)
	}
	assertNoUsageQueueRequest(t, requested)
}

func TestLargeAuthInventorySyncFitsBodyCapAndUsesReadOnlyRequests(t *testing.T) {
	const largeAuthInventoryRowCount = 1381

	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	authPayload := largeSyncAuthFixture(t, largeAuthInventoryRowCount)
	payloadBytes := int64(len([]byte(authPayload)))
	if payloadBytes > defaultCLIProxyResponseBodyLimitBytes {
		t.Fatalf("large auth inventory payload bytes=%d exceeds response body cap=%d", payloadBytes, defaultCLIProxyResponseBodyLimitBytes)
	}

	requested := make([]string, 0, 1+len(sidecarProviderSyncEndpoints))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.Method+" "+r.URL.Path)
		if r.Method != http.MethodGet {
			t.Errorf("sync must perform read-only upstream inventory access, got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Management-Key") != "sync-secret" {
			t.Errorf("expected management key header, got %q", r.Header.Get("X-Management-Key"))
		}
		if r.URL.Path == "/v0/management/auth-files" {
			writeSyncJSON(w, authPayload)
			return
		}
		serveSyncFixturePath(t, w, r)
	}))
	defer server.Close()

	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 60)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response sidecarSyncResponse
	postSidecarSync(t, router, sidecar.ID, http.StatusOK, &response)
	if response.State != "succeeded" || response.AuthSnapshotCount != largeAuthInventoryRowCount || response.ProviderSnapshotCount != 5 || response.SyncStatus.LastSuccessfulSyncAt == nil {
		t.Fatalf("unexpected large inventory sync response: %+v", response)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != largeAuthInventoryRowCount {
		t.Fatalf("large inventory auth snapshots len=%d err=%v", len(auths), err)
	}
	providers, err := service.store.ListProviderSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(providers) != 5 {
		t.Fatalf("provider snapshots should still load independently, len=%d err=%v", len(providers), err)
	}

	var authList authSnapshotListResponse
	authRecorder := httptest.NewRecorder()
	router.ServeHTTP(authRecorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/auth-snapshots", nil))
	if authRecorder.Code != http.StatusOK {
		t.Fatalf("auth-snapshots status = %d body=%s", authRecorder.Code, authRecorder.Body.String())
	}
	if err := json.Unmarshal(authRecorder.Body.Bytes(), &authList); err != nil || len(authList.Items) != largeAuthInventoryRowCount {
		t.Fatalf("auth-snapshots route items=%d err=%v", len(authList.Items), err)
	}

	var providerList providerSnapshotListResponse
	providerRecorder := httptest.NewRecorder()
	router.ServeHTTP(providerRecorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/provider-snapshots", nil))
	if providerRecorder.Code != http.StatusOK {
		t.Fatalf("provider-snapshots status = %d body=%s", providerRecorder.Code, providerRecorder.Body.String())
	}
	if err := json.Unmarshal(providerRecorder.Body.Bytes(), &providerList); err != nil || len(providerList.Items) != 5 {
		t.Fatalf("provider-snapshots route items=%d err=%v", len(providerList.Items), err)
	}

	wantRequests := []string{
		"GET /v0/management/auth-files",
		"GET /v0/management/gemini-api-key",
		"GET /v0/management/claude-api-key",
		"GET /v0/management/codex-api-key",
		"GET /v0/management/vertex-api-key",
		"GET /v0/management/openai-compatibility",
	}
	if len(requested) != len(wantRequests) {
		t.Fatalf("unexpected upstream request log: got %v want %v", requested, wantRequests)
	}
	for i, want := range wantRequests {
		if requested[i] != want {
			t.Fatalf("unexpected upstream request %d: got %q want %q; full log=%v", i, requested[i], want, requested)
		}
	}
	t.Logf("large auth inventory sync rows=%d payload_bytes=%d body_cap_bytes=%d auth_snapshots=%d provider_snapshots=%d upstream_requests=%v", largeAuthInventoryRowCount, payloadBytes, defaultCLIProxyResponseBodyLimitBytes, len(auths), len(providers), requested)
}

func TestAuthFilesSyncRedactsSensitivePayloadFields(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			writeSyncJSON(w, sensitiveSyncAuthFixture())
			return
		}
		serveSyncFixturePath(t, w, r)
	}))
	defer server.Close()

	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 60)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)

	var response sidecarSyncResponse
	postSidecarSync(t, router, sidecar.ID, http.StatusOK, &response)
	if response.AuthSnapshotCount != 2 || response.ProviderSnapshotCount != 5 {
		t.Fatalf("unexpected sync counts for sensitive payload: %+v", response)
	}

	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 2 {
		t.Fatalf("list sensitive auth snapshots len=%d err=%v", len(auths), err)
	}
	redactionObserved := false
	for _, auth := range auths {
		for _, raw := range []json.RawMessage{auth.SnapshotJSON, auth.RecentRequestsJSON, auth.ModelStatesJSON} {
			text := string(raw)
			assertAuthPayloadSecretFree(t, text)
			if strings.Contains(text, "redacted-by-prism") {
				redactionObserved = true
			}
		}
	}
	if !redactionObserved {
		t.Fatalf("expected at least one sensitive auth value to be redacted in persisted snapshots: %+v", auths)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecar.ID)+"/auth-files", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("auth-files status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	assertAuthPayloadSecretFree(t, recorder.Body.String())
	if !strings.Contains(recorder.Body.String(), "redacted-by-prism") {
		t.Fatalf("auth-files response did not expose redaction marker: %s", recorder.Body.String())
	}
}

func TestSyncFailureMarksStaleAndPreservesSnapshots(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	failAuthFiles := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" && failAuthFiles {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"temporary"}`))
			return
		}
		serveSyncFixturePath(t, w, r)
	}))
	defer server.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 30)
	if _, err := service.SyncSidecar(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 1 {
		t.Fatalf("initial auth snapshots len=%d err=%v", len(auths), err)
	}
	firstSnapshotID := auths[0].ID
	firstSuccessAt := now
	now = now.Add(2 * time.Minute)
	failAuthFiles = true
	result, err := service.SyncSidecar(t.Context(), sidecar.ID)
	if err == nil || result.ErrorDetail == "" {
		t.Fatalf("expected failed sync result, result=%+v err=%v", result, err)
	}
	stored, ok, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || !ok {
		t.Fatalf("load failed sidecar ok=%v err=%v", ok, err)
	}
	if stored.LastSuccessfulSyncAt == nil || !stored.LastSuccessfulSyncAt.Equal(firstSuccessAt) || stored.LastSyncError == nil {
		t.Fatalf("failure metadata did not preserve last success and store error: %+v", stored)
	}
	status := service.sidecarSyncStatus(stored)
	if !status.Stale || stored.SnapshotStaleAfter == nil || !stored.SnapshotStaleAfter.Equal(now) {
		t.Fatalf("failed sync should mark snapshots stale now, status=%+v stored=%+v", status, stored)
	}
	auths, err = service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 1 || auths[0].ID != firstSnapshotID {
		t.Fatalf("failed sync should preserve previous snapshots, got %+v err=%v", auths, err)
	}
}

func TestAuthFilesSyncRejectsMalformedRawPayloadsAndPreservesPreviousGeneration(t *testing.T) {
	tests := []struct {
		name       string
		payload    string
		wantCode   string
		wantDetail string
	}{
		{name: "legacy auth_files", payload: syncAuthFixtureWithEnvelopeKey(t, "auth_files"), wantCode: "sync_contract", wantDetail: "files must be present"},
		{name: "missing files key", payload: `{"metadata":{"row_count":1},"provider_inventory":{"gemini-api-key":[{"api-key":"redacted-provider-key"}]}}`, wantCode: "sync_contract", wantDetail: "files must be present"},
		{name: "files null", payload: `{"files":null,"metadata":{"row_count":0}}`, wantCode: "sync_contract", wantDetail: "files must be an array"},
		{name: "malformed row", payload: `{"files":[{"provider":"gemini","priority":10,"api_key":"raw-malformed-row-secret"}],"metadata":{"row_count":1}}`, wantCode: "sync_failed", wantDetail: "auth snapshot requires id or name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
			authPayload := syncAuthFixture()
			countProviderRequests := false
			providerRequests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v0/management/auth-files" {
					writeSyncJSON(w, authPayload)
					return
				}
				if countProviderRequests {
					providerRequests++
				}
				serveSyncFixturePath(t, w, r)
			}))
			defer server.Close()

			service := newSyncTestService(t, func() time.Time { return now })
			sidecar := createSyncTestSidecar(t, service, server.URL, true, 60)
			if _, err := service.SyncSidecar(t.Context(), sidecar.ID); err != nil {
				t.Fatalf("initial sync: %v", err)
			}
			previous, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
			if err != nil || len(previous) != 1 {
				t.Fatalf("initial auth snapshots len=%d err=%v", len(previous), err)
			}
			previousSnapshotID := previous[0].ID
			previousSnapshotJSON := string(previous[0].SnapshotJSON)
			firstSuccessAt := now

			now = now.Add(time.Minute)
			authPayload = tt.payload
			countProviderRequests = true
			result, err := service.SyncSidecar(t.Context(), sidecar.ID)
			if err == nil || result.ErrorCode != tt.wantCode || !strings.Contains(result.ErrorDetail, tt.wantDetail) {
				t.Fatalf("expected failed sync code=%s detail containing %q, result=%+v err=%v", tt.wantCode, tt.wantDetail, result, err)
			}
			assertAuthPayloadSecretFree(t, result.ErrorDetail)
			if providerRequests != 0 {
				t.Fatalf("provider inventory must not be used as fallback after /auth-files failure, provider requests=%d", providerRequests)
			}

			stored, ok, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
			if err != nil || !ok {
				t.Fatalf("load failed sidecar ok=%v err=%v", ok, err)
			}
			if stored.LastSuccessfulSyncAt == nil || !stored.LastSuccessfulSyncAt.Equal(firstSuccessAt) || stored.LastSyncError == nil || !strings.Contains(*stored.LastSyncError, tt.wantDetail) {
				t.Fatalf("failure metadata did not preserve success or record expected error: %+v", stored)
			}
			assertAuthPayloadSecretFree(t, *stored.LastSyncError)
			auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
			if err != nil || len(auths) != 1 || auths[0].ID != previousSnapshotID || string(auths[0].SnapshotJSON) != previousSnapshotJSON {
				t.Fatalf("failed generation should preserve previous auth snapshot, got %+v err=%v", auths, err)
			}
		})
	}
}

func TestAuthFilesSyncAcceptsEmptyFilesArrayAsSuccessfulGeneration(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	authPayload := syncAuthFixture()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/management/auth-files" {
			writeSyncJSON(w, authPayload)
			return
		}
		serveSyncFixturePath(t, w, r)
	}))
	defer server.Close()

	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 60)
	if _, err := service.SyncSidecar(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("initial sync: %v", err)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 1 {
		t.Fatalf("initial auth snapshots len=%d err=%v", len(auths), err)
	}

	now = now.Add(time.Minute)
	authPayload = `{"files":[],"metadata":{"row_count":0,"generated_at":"2026-05-10T12:01:00Z"}}`
	result, err := service.SyncSidecar(t.Context(), sidecar.ID)
	if err != nil {
		t.Fatalf("empty files sync should succeed: result=%+v err=%v", result, err)
	}
	if result.AuthSnapshotCount != 0 || result.ProviderSnapshotCount != 5 || result.ErrorDetail != "" {
		t.Fatalf("unexpected empty generation result: %+v", result)
	}
	auths, err = service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 0 {
		t.Fatalf("empty files generation should replace previous auth rows, got %+v err=%v", auths, err)
	}
	stored, ok, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || !ok || stored.LastSuccessfulSyncAt == nil || !stored.LastSuccessfulSyncAt.Equal(now) || stored.LastSyncError != nil {
		t.Fatalf("empty generation should be a successful sync, stored=%+v ok=%v err=%v", stored, ok, err)
	}
}

func TestInvalidManagementAuthPausesPeriodicSyncUntilManualSync(t *testing.T) {
	now := time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	allowAuth := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		serveSyncFixturePath(t, w, r)
	}))
	defer server.Close()
	service := newSyncTestService(t, func() time.Time { return now })
	sidecar := createSyncTestSidecar(t, service, server.URL, true, 300)
	result, err := service.SyncSidecar(t.Context(), sidecar.ID)
	if err == nil || result.ErrorCode != string(CLIProxyErrorInvalidManagementAuth) {
		t.Fatalf("expected invalid auth failure, result=%+v err=%v", result, err)
	}
	stored, ok, err := service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if err != nil || !ok {
		t.Fatalf("load invalid-auth sidecar ok=%v err=%v", ok, err)
	}
	if stored.ManagementAuthState != ManagementAuthStateInvalid || stored.AuthFailurePauseUntil == nil || !stored.AuthFailurePauseUntil.Equal(now.Add(300*time.Second)) {
		t.Fatalf("invalid auth pause metadata mismatch: %+v", stored)
	}
	now = now.Add(time.Minute)
	allowAuth = true
	summary, err := service.SyncDueSidecars(t.Context())
	if err != nil {
		t.Fatalf("periodic sync while paused: %v", err)
	}
	if summary.Synced != 0 || summary.Skipped != 1 {
		t.Fatalf("expected paused sidecar to be skipped, got %+v", summary)
	}
	auths, err := service.store.ListAuthSnapshots(t.Context(), sidecar.ID)
	if err != nil || len(auths) != 0 {
		t.Fatalf("paused periodic sync should not write snapshots, got %+v err=%v", auths, err)
	}
	if _, err := service.SyncSidecar(t.Context(), sidecar.ID); err != nil {
		t.Fatalf("manual sync should bypass pause after credential becomes valid: %v", err)
	}
	stored, _, _ = service.store.GetSidecarInstance(t.Context(), sidecar.ID)
	if stored.ManagementAuthState != ManagementAuthStateValid || stored.AuthFailurePauseUntil != nil {
		t.Fatalf("manual success should clear invalid auth pause: %+v", stored)
	}
}

func TestAuthFilesEnvelopeRejectsLegacyKey(t *testing.T) {
	tests := []struct {
		name     string
		envelope map[string]json.RawMessage
		wantErr  string
	}{
		{name: "missing files key", envelope: map[string]json.RawMessage{}, wantErr: "files must be present"},
		{name: "legacy auth_files only", envelope: map[string]json.RawMessage{"auth_files": json.RawMessage(`[]`)}, wantErr: "files must be present"},
		{name: "files null", envelope: map[string]json.RawMessage{"files": json.RawMessage(`null`)}, wantErr: "files must be an array"},
		{name: "files not array", envelope: map[string]json.RawMessage{"files": json.RawMessage(`{}`)}, wantErr: "files must be an array"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeSidecarAuthFileRows(tt.envelope)
			var contractErr *sidecarSyncContractError
			if !errors.As(err, &contractErr) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected sync-contract %q error, got %T %v", tt.wantErr, err, err)
			}
		})
	}
}

func TestAuthFilesEnvelopeAcceptsEmptyFilesArray(t *testing.T) {
	files, err := decodeSidecarAuthFileRows(map[string]json.RawMessage{"files": json.RawMessage(`[]`)})
	if err != nil {
		t.Fatalf("expected empty files array to succeed, got %v", err)
	}
	if len(files) != 0 {
		t.Fatalf("expected zero auth file rows, got %+v", files)
	}
}

func newSyncTestService(t *testing.T, now func() time.Time) *Service {
	t.Helper()
	service, err := NewService(config.Settings{SecretEncryptionKey: "unit-test-secret"}, Options{Now: now})
	if err != nil {
		t.Fatalf("new sidecar service: %v", err)
	}
	return service
}

func createSyncTestSidecar(t *testing.T, service *Service, baseURL string, enabled bool, syncInterval int) SidecarInstance {
	t.Helper()
	sidecar, err := service.store.CreateSidecarInstance(t.Context(), SidecarInstanceInput{
		Name:                  "Sync sidecar " + strconv.Itoa(syncInterval),
		BaseURL:               baseURL,
		BaseURLCanonical:      baseURL,
		ManagementPassword:    "sync-secret",
		Enabled:               &enabled,
		SyncIntervalSeconds:   syncInterval,
		RequestTimeoutSeconds: 2,
		AllowPrivateNetwork:   true,
		AllowInsecureHTTP:     true,
		ManagementAuthState:   ManagementAuthStateUnknown,
	})
	if err != nil {
		t.Fatalf("create sync sidecar: %v", err)
	}
	return sidecar
}

func postSidecarSync(t *testing.T, router http.Handler, sidecarID int, wantStatus int, target any) {
	t.Helper()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/sidecars/"+strconv.Itoa(sidecarID)+"/sync", nil)
	router.ServeHTTP(recorder, req)
	if recorder.Code != wantStatus {
		t.Fatalf("sync status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if target != nil {
		if err := json.Unmarshal(recorder.Body.Bytes(), target); err != nil {
			t.Fatalf("decode sync response: %v body=%s", err, recorder.Body.String())
		}
	}
}

func serveSyncFixturePath(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	switch r.URL.Path {
	case "/v0/management/auth-files":
		writeSyncJSON(w, syncAuthFixture())
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
		t.Errorf("unexpected management path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func syncAuthFixture() string {
	return `{"files":[{"id":"auth-gemini-primary","auth_index":"auth_001","name":"gemini-primary.json","provider":"gemini","label":"Gemini primary","status":"active","status_message":"","disabled":false,"unavailable":false,"priority":0,"quota":{"exceeded":true,"reason":"rate_limit","next_recover_at":"2026-05-10T12:30:00Z"},"success":4,"failed":1,"recent_requests":[{"time":"12:00-12:10","success":4,"failed":1}],"model_states":{"gemini-pro":{"status":"active"}},"path":"/tmp/should-not-be-stored","api_key":"redacted-auth-key"}]}`
}

func largeSyncAuthFixture(t *testing.T, rowCount int) string {
	t.Helper()
	files := make([]map[string]any, 0, rowCount)
	providers := []string{"gemini", "claude", "codex", "vertex"}
	for i := range rowCount {
		rowNumber := i + 1
		provider := providers[i%len(providers)]
		files = append(files, map[string]any{
			"id":             "auth-large-" + strconv.Itoa(rowNumber),
			"auth_index":     "auth_" + strconv.Itoa(rowNumber),
			"name":           "large-auth-" + strconv.Itoa(rowNumber) + ".json",
			"provider":       provider,
			"label":          "Large inventory auth " + strconv.Itoa(rowNumber),
			"status":         "active",
			"status_message": "ready",
			"disabled":       false,
			"unavailable":    false,
			"priority":       rowNumber % 50,
			"quota":          map[string]any{"exceeded": rowNumber%17 == 0, "reason": "healthy", "next_recover_at": "2026-05-10T12:30:00Z"},
			"success":        rowNumber % 100,
			"failed":         rowNumber % 3,
			"recent_requests": []map[string]any{
				{"time": "12:00-12:10", "success": rowNumber % 7, "failed": rowNumber % 2},
				{"time": "12:10-12:20", "success": rowNumber % 5, "failed": 0},
			},
			"model_states": map[string]any{
				provider + "-primary":  map[string]any{"status": "active"},
				provider + "-fallback": map[string]any{"status": "standby"},
			},
			"note": "large inventory operational fixture",
		})
	}
	encoded, err := json.Marshal(map[string]any{"files": files, "metadata": map[string]any{"row_count": rowCount, "generated_at": "2026-05-10T12:00:00Z"}})
	if err != nil {
		t.Fatalf("encode large auth fixture: %v", err)
	}
	return string(encoded)
}

func syncAuthFixtureWithEnvelopeKey(t *testing.T, key string) string {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(syncAuthFixture()), &payload); err != nil {
		t.Fatalf("decode sync auth fixture: %v", err)
	}
	files, ok := payload["files"]
	if !ok {
		t.Fatalf("sync auth fixture missing files key")
	}
	encoded, err := json.Marshal(map[string]json.RawMessage{
		key:        files,
		"metadata": json.RawMessage(`{"row_count":1,"generated_at":"2026-05-10T12:00:00Z"}`),
	})
	if err != nil {
		t.Fatalf("encode sync auth fixture with %s key: %v", key, err)
	}
	return string(encoded)
}

func sensitiveSyncAuthFixture() string {
	return `{"files":[{"id":"auth-gemini-sensitive","auth_index":"auth_001","name":"gemini-sensitive.json","type":"gemini","provider":"gemini","label":"Sensitive Gemini","status":"active","status_message":"","disabled":false,"unavailable":false,"priority":20,"quota":{"exceeded":false,"reason":"healthy","management_key":"raw-quota-management-key"},"success":9,"failed":0,"recent_requests":[{"time":"12:00-12:10","success":9,"failed":0,"headers":{"Authorization":"Bearer raw-authorization-token","X-API-Key":"raw-header-api-key","Cookie":"session=raw-cookie-secret"}}],"model_states":{"gemini-pro":{"status":"active","token":"raw-model-state-token"}},"path":"/etc/cliproxy/auth/gemini-sensitive.json","api_key":"raw-auth-api-key","management_password":"raw-management-password","headers":{"Authorization":"Bearer raw-top-level-auth-token"},"note":"safe operator metadata"},{"id":"auth-claude-sensitive","auth_index":"auth_002","name":"claude-sensitive.json","type":"claude","provider":"claude","label":"Sensitive Claude","status":"active","disabled":false,"unavailable":false,"priority":5,"quota":{"exceeded":true,"reason":"rate_limit","next_recover_at":"2026-05-10T13:00:00Z"},"success":1,"failed":2,"recent_requests":[{"time":"12:10-12:20","success":1,"failed":2}],"model_states":{"claude-sonnet":{"status":"limited"}},"api-key":"raw-kebab-api-key"}],"metadata":{"row_count":2,"generated_at":"2026-05-10T12:00:00Z"}}`
}

func assertAuthPayloadSecretFree(t *testing.T, value string) {
	t.Helper()
	for _, secret := range []string{"raw-auth-api-key", "raw-management-password", "raw-quota-management-key", "raw-authorization-token", "raw-header-api-key", "raw-cookie-secret", "raw-model-state-token", "raw-top-level-auth-token", "raw-kebab-api-key", "raw-malformed-row-secret"} {
		if strings.Contains(value, secret) {
			t.Fatalf("auth payload leaked secret %q in %s", secret, value)
		}
	}
}

func writeSyncJSON(w http.ResponseWriter, payload string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(payload))
}

func assertNoUsageQueueRequest(t *testing.T, paths []string) {
	t.Helper()
	for _, path := range paths {
		if strings.Contains(path, "usage-queue") {
			t.Fatalf("sync called forbidden usage queue path: %v", paths)
		}
	}
}

func assertSidecarSnapshotRoutes(t *testing.T, router http.Handler, sidecarID int) {
	t.Helper()
	var authList authSnapshotListResponse
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("auth-files status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &authList); err != nil {
		t.Fatalf("decode auth-files route response: %v", err)
	}
	if len(authList.Items) != 1 || authList.Items[0].QuotaExceeded == nil || !*authList.Items[0].QuotaExceeded {
		t.Fatalf("auth-files route did not return normalized auth snapshot: %+v", authList.Items)
	}

	var providerList providerSnapshotListResponse
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/providers", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("providers status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &providerList); err != nil {
		t.Fatalf("decode providers route response: %v", err)
	}
	if len(providerList.Items) != 5 {
		t.Fatalf("providers route returned %d items, want 5", len(providerList.Items))
	}
	for _, item := range providerList.Items {
		if strings.Contains(string(item.Snapshot), "raw-provider-token") || strings.Contains(string(item.Snapshot), "raw-header-key") {
			t.Fatalf("providers route leaked raw header secret: %s", item.Snapshot)
		}
	}

	var status sidecarSyncStatusResponse
	recorder = httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/sync-status", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("sync-status status = %d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode sync-status response: %v", err)
	}
	if status.SidecarID != sidecarID || status.ManagementAuthState != ManagementAuthStateValid || status.Stale || status.LastSuccessfulSyncAt == nil {
		t.Fatalf("sync-status response mismatch: %+v", status)
	}
}

func TestMemorySidecarStorePreservesEncryptedPasswordOnUpdate(t *testing.T) {
	service := newSyncTestService(t, time.Now)
	enabled := true
	created, err := service.store.CreateSidecarInstance(t.Context(), SidecarInstanceInput{
		Name:                  "Memory Preserve Secret",
		BaseURL:               "http://127.0.0.1:19090",
		BaseURLCanonical:      "http://127.0.0.1:19090",
		ManagementPassword:    "original-secret",
		Enabled:               &enabled,
		AllowPrivateNetwork:   true,
		AllowInsecureHTTP:     true,
		ManagementAuthState:   ManagementAuthStateUnknown,
		SyncIntervalSeconds:   300,
		RequestTimeoutSeconds: 2,
	})
	if err != nil {
		t.Fatalf("create memory sidecar: %v", err)
	}
	input := instanceToInput(created)
	input.Name = "Memory Preserve Secret Updated"
	updated, err := service.store.UpdateSidecarInstance(t.Context(), created.ID, input)
	if err != nil {
		t.Fatalf("update memory sidecar: %v", err)
	}
	decrypted, err := service.decryptManagementPassword(updated.EncryptedManagementPassword)
	if err != nil {
		t.Fatalf("decrypt updated memory password: %v", err)
	}
	if decrypted != "original-secret" {
		t.Fatalf("memory store double-encrypted preserved password, decrypted=%q", decrypted)
	}
}
