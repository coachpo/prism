package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementsidecars "github.com/coachpo/prism/backend/internal/httpapi/management/sidecars"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

const sidecarIntegrationManagementSecret = "integration-management-secret"

var sidecarIntegrationSecrets = []string{
	sidecarIntegrationManagementSecret,
	"raw-auth-secret",
	"raw-auth-token",
	"raw-provider-secret",
	"raw-provider-token",
	"raw-header-key",
	"raw-openai-secret",
	"raw-openai-token",
	"proxy-secret.invalid",
	"mutation-secret",
	"mutation-token",
	"mutation-management-secret",
}

func TestSidecarDBBackedSyncMutationsAndRedaction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, router := newSidecarIntegrationRouter(t, ctx)
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()

	createBody, created := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars", map[string]any{
		"name":                    "Integration Sidecar",
		"base_url":                upstream.URL(),
		"management_password":     sidecarIntegrationManagementSecret,
		"allow_private_network":   true,
		"allow_insecure_http":     true,
		"sync_interval_seconds":   60,
		"request_timeout_seconds": 5,
	}, http.StatusCreated)
	sidecarIntegrationAssertNoLeaks(t, createBody)
	sidecarID := sidecarIntegrationInt(t, created["id"], "id")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	syncBody, syncPayload := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars/"+strconv.Itoa(sidecarID)+"/sync", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, syncBody)
	if sidecarIntegrationInt(t, syncPayload["auth_snapshot_count"], "auth_snapshot_count") != 1 {
		t.Fatalf("expected one auth snapshot after sync, got %+v", syncPayload)
	}
	if sidecarIntegrationInt(t, syncPayload["provider_snapshot_count"], "provider_snapshot_count") != 2 {
		t.Fatalf("expected two provider snapshots after sync, got %+v", syncPayload)
	}
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)

	for _, path := range []string{
		"/sidecars",
		"/sidecars/" + strconv.Itoa(sidecarID),
		"/sidecars/" + strconv.Itoa(sidecarID) + "/auth-files",
		"/sidecars/" + strconv.Itoa(sidecarID) + "/providers",
		"/sidecars/" + strconv.Itoa(sidecarID) + "/sync-status",
	} {
		body, _ := sidecarIntegrationRequestJSON(t, router, http.MethodGet, path, nil, http.StatusOK)
		sidecarIntegrationAssertNoLeaks(t, body)
	}

	mutationBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodPatch, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files/auth-gemini-primary/fields", map[string]any{
		"priority": 42,
		"headers":  map[string]any{"X-Trace-ID": "trace-123"},
	}, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, mutationBody)
	upstream.assertFieldPatch(t, 42)
	assertSidecarIntegrationManualPause(t, conn, sidecarID)

	actionsBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/actions", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, actionsBody)
	if !strings.Contains(actionsBody, "operator_patch") || !strings.Contains(actionsBody, "redacted-by-prism") {
		t.Fatalf("expected redacted operator patch action history, got %s", actionsBody)
	}
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func newSidecarIntegrationRouter(t *testing.T, ctx context.Context) (*pgx.Conn, http.Handler) {
	t.Helper()
	harness := newPostgresHarness(t)
	databaseName := "sidecars_integration_" + randomSuffix(t)
	conn := harness.openDatabase(t, ctx, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })
	if _, err := newRunner(t).Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for sidecar integration: %v", err)
	}
	pool, err := pgxpool.New(ctx, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("open sidecar integration pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := managementsidecars.NewService(config.Settings{SecretEncryptionKey: "sidecar-integration-secret"}, managementsidecars.Options{Pool: pool, Now: func() time.Time {
		return time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	}})
	if err != nil {
		t.Fatalf("build sidecar integration service: %v", err)
	}
	t.Cleanup(service.Close)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return conn, router
}

func sidecarIntegrationRequestJSON(t *testing.T, handler http.Handler, method string, path string, body any, wantStatus int) (string, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal sidecar integration request: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	responseBody := strings.TrimSpace(recorder.Body.String())
	if recorder.Code != wantStatus {
		t.Fatalf("%s %s status = %d want %d body=%s", method, path, recorder.Code, wantStatus, responseBody)
	}
	payload := map[string]any{}
	if responseBody != "" {
		if err := json.Unmarshal([]byte(responseBody), &payload); err != nil {
			t.Fatalf("decode sidecar integration response %s: %v", responseBody, err)
		}
	}
	return responseBody, payload
}

func sidecarIntegrationAssertDBNoLeaks(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	var combined string
	if err := conn.QueryRow(context.Background(), `SELECT concat_ws(' ',
		COALESCE((SELECT string_agg(management_password, ' ') FROM sidecar_instances WHERE id = $1), ''),
		COALESCE((SELECT string_agg(snapshot_json::text, ' ') FROM sidecar_auth_snapshots WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(snapshot_json::text, ' ') FROM sidecar_provider_snapshots WHERE sidecar_id = $1), ''),
		COALESCE((SELECT string_agg(concat_ws(' ', reason, error_message), ' ') FROM sidecar_watchdog_actions WHERE sidecar_id = $1), '')
	)`, sidecarID).Scan(&combined); err != nil {
		t.Fatalf("collect sidecar persisted strings: %v", err)
	}
	sidecarIntegrationAssertNoLeaks(t, combined)
	if !strings.Contains(combined, "enc:") {
		t.Fatalf("expected persisted sidecar management password to be encrypted, got %s", combined)
	}
}

func assertSidecarIntegrationManualPause(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT count(*) FROM sidecar_watchdog_holds WHERE sidecar_id = $1 AND status = 'paused' AND manual_pause_until IS NOT NULL`, sidecarID).Scan(&count); err != nil {
		t.Fatalf("count manual pause holds: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one paused manual watchdog hold after operator mutation, got %d", count)
	}
}

func sidecarIntegrationAssertNoLeaks(t *testing.T, value string) {
	t.Helper()
	for _, secret := range sidecarIntegrationSecrets {
		if strings.Contains(value, secret) {
			t.Fatalf("sidecar integration value leaked %q in %s", secret, value)
		}
	}
}

func sidecarIntegrationInt(t *testing.T, value any, field string) int {
	t.Helper()
	number, ok := value.(float64)
	if !ok {
		t.Fatalf("expected numeric %s, got %T %v", field, value, value)
	}
	return int(number)
}

type sidecarIntegrationUpstream struct {
	server       *httptest.Server
	mu           sync.Mutex
	fieldPatches []map[string]any
}

func newSidecarIntegrationUpstream(t *testing.T) *sidecarIntegrationUpstream {
	t.Helper()
	upstream := &sidecarIntegrationUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.handle(t, w, r)
	}))
	return upstream
}

func (u *sidecarIntegrationUpstream) URL() string { return u.server.URL }

func (u *sidecarIntegrationUpstream) Close() { u.server.Close() }

func (u *sidecarIntegrationUpstream) handle(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Management-Key"); got != sidecarIntegrationManagementSecret {
		t.Errorf("expected management key header %q, got %q", sidecarIntegrationManagementSecret, got)
	}
	switch r.URL.Path {
	case "/v0/management/auth-files":
		sidecarIntegrationWriteJSON(w, map[string]any{"files": []any{sidecarIntegrationAuthFixture()}})
	case "/v0/management/gemini-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"gemini-api-key": []any{sidecarIntegrationGeminiProviderFixture()}})
	case "/v0/management/openai-compatibility":
		sidecarIntegrationWriteJSON(w, map[string]any{"openai-compatibility": []any{sidecarIntegrationOpenAIProviderFixture()}})
	case "/v0/management/claude-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"claude-api-key": []any{}})
	case "/v0/management/codex-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"codex-api-key": []any{}})
	case "/v0/management/vertex-api-key":
		sidecarIntegrationWriteJSON(w, map[string]any{"vertex-api-key": []any{}})
	case "/v0/management/auth-files/fields":
		u.recordFieldPatch(t, r)
		sidecarIntegrationWriteJSON(w, map[string]any{"ok": true, "api_key": "mutation-secret", "management_password": "mutation-management-secret", "headers": map[string]any{"Authorization": "Bearer mutation-token"}})
	default:
		t.Errorf("unexpected sidecar integration upstream path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *sidecarIntegrationUpstream) recordFieldPatch(t *testing.T, r *http.Request) {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatalf("decode field patch payload: %v", err)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.fieldPatches = append(u.fieldPatches, payload)
}

func (u *sidecarIntegrationUpstream) assertFieldPatch(t *testing.T, wantPriority int) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.fieldPatches) != 1 {
		t.Fatalf("expected one fields mutation patch, got %+v", u.fieldPatches)
	}
	if got := sidecarIntegrationInt(t, u.fieldPatches[0]["priority"], "priority"); got != wantPriority {
		t.Fatalf("fields mutation priority = %d want %d", got, wantPriority)
	}
}

func sidecarIntegrationWriteJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func sidecarIntegrationAuthFixture() map[string]any {
	return map[string]any{
		"id": "auth-gemini-primary", "auth_index": "auth_001", "name": "gemini-primary.json", "provider": "gemini", "label": "Gemini primary", "status": "active", "disabled": false, "priority": 10,
		"quota": map[string]any{"exceeded": false, "reason": "", "next_recover_at": nil}, "recent_requests": []any{map[string]any{"success": 4, "failed": 0}}, "model_states": map[string]any{"gemini-pro": map[string]any{"status": "active", "api_key": "raw-auth-token"}},
		"api_key": "raw-auth-secret",
	}
}

func sidecarIntegrationGeminiProviderFixture() map[string]any {
	return map[string]any{"api-key": "raw-provider-secret", "priority": 10, "prefix": "team-a/", "auth-index": "auth_001", "proxy-url": "http://proxy-secret.invalid", "headers": map[string]any{"Authorization": "Bearer raw-provider-token", "X-API-Key": "raw-header-key"}}
}

func sidecarIntegrationOpenAIProviderFixture() map[string]any {
	return map[string]any{"name": "compat", "priority": 5, "api-key-entries": []any{map[string]any{"api-key": "raw-openai-secret", "auth-index": "auth_openai", "headers": map[string]any{"Authorization": "Bearer raw-openai-token"}}}}
}
