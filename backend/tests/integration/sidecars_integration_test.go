package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
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
	"raw-codex-secret",
	"raw-codex-token",
	"proxy-secret.invalid",
	"mutation-secret",
	"mutation-token",
	"mutation-management-secret",
	"chatgpt-account-secret",
	"mutation-response-body",
	"mutation-user@example.test",
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
	legacyAuthCountField := "auth_" + "snapshot_count"
	if _, exists := syncPayload[legacyAuthCountField]; exists {
		t.Fatalf("sync response must not expose auth snapshot counts: %+v", syncPayload)
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
	}, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, mutationBody)
	upstream.assertFieldPatch(t, 42)
	sidecarIntegrationAssertCurrentSchema(t, conn)

	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func TestSidecarIntegrationRouteSurfaceMatchesCurrentSet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, _, router := newSidecarIntegrationServiceRouter(t, ctx)

	sidecarIntegrationAssertRouteSurface(t, router)
	sidecarIntegrationAssertCurrentSchema(t, conn)
}

func TestSidecarIntegrationLiveAuthFilesAndRetainedProvidersUseCurrentSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, router := newSidecarIntegrationRouter(t, ctx)
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "Snapshots")

	authBody, authPayload := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, authBody)
	authItems := sidecarIntegrationItems(t, authPayload, "items")
	if len(authItems) != 1 {
		t.Fatalf("expected one live auth file, got %+v", authPayload)
	}
	authFile, ok := authItems[0].(map[string]any)
	if !ok || authFile["auth_id"] != "auth-gemini-primary" || sidecarIntegrationInt(t, authFile["priority"], "auth priority") != 10 {
		t.Fatalf("unexpected live auth file: %+v", authItems[0])
	}

	providerBody, providerPayload := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/providers", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, providerBody)
	providerItems := sidecarIntegrationItems(t, providerPayload, "items")
	if len(providerItems) != 2 {
		t.Fatalf("expected two retained provider snapshots, got %+v", providerPayload)
	}
	sidecarIntegrationAssertCurrentSchema(t, conn)
}

func TestSidecarIntegrationDirectAuthFileMutationReadsLiveAuthFilesWithCurrentSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, router := newSidecarIntegrationRouter(t, ctx)
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "Direct Mutation")

	mutationBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodPatch, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files/auth-gemini-primary/fields", map[string]any{
		"priority": 77,
	}, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, mutationBody)
	upstream.assertFieldPatch(t, 77)

	authBody, authPayload := sidecarIntegrationRequestJSON(t, router, http.MethodGet, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, authBody)
	authItems := sidecarIntegrationItems(t, authPayload, "items")
	if len(authItems) != 1 {
		t.Fatalf("expected one live auth file after direct mutation, got %+v", authPayload)
	}
	authFile, ok := authItems[0].(map[string]any)
	if !ok || sidecarIntegrationInt(t, authFile["priority"], "mutated auth priority") != 77 {
		t.Fatalf("direct auth-file mutation did not update live auth file: %+v", authItems[0])
	}
	sidecarIntegrationAssertCurrentSchema(t, conn)
}

func TestSidecarAuthMutationIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	conn, router := newSidecarIntegrationRouter(t, ctx)
	upstream := newSidecarIntegrationUpstream(t)
	defer upstream.Close()
	sidecarID := sidecarIntegrationCreateAndSyncSidecar(t, conn, router, upstream, "Safe Mutation")

	mutationBody, mutationPayload := sidecarIntegrationRequestJSON(t, router, http.MethodPatch, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files/auth-gemini-primary/fields", map[string]any{
		"priority": 88,
	}, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, mutationBody)
	if mutationPayload["state"] != "succeeded" {
		t.Fatalf("expected succeeded mutation state, got %+v", mutationPayload)
	}
	upstream.assertFieldPatch(t, 88)

	removedFieldRejections := []struct {
		field string
		body  map[string]any
	}{
		{field: "prefix", body: map[string]any{"priority": 89, "prefix": "team-a/"}},
		{field: "proxy_url", body: map[string]any{"priority": 89, "proxy_url": "https://proxy.example.test"}},
		{field: "note", body: map[string]any{"priority": 89, "note": "operator note"}},
		{field: "headers", body: map[string]any{"priority": 89, "headers": map[string]any{"X-Trace-ID": "trace-123"}}},
		{field: "custom_headers", body: map[string]any{"priority": 89, "custom_headers": map[string]any{"X-Trace-ID": "trace-123"}}},
	}
	for _, tt := range removedFieldRejections {
		rejectionBody := sidecarIntegrationRequestStatus(t, router, http.MethodPatch, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files/auth-gemini-primary/fields", tt.body, http.StatusBadRequest)
		sidecarIntegrationAssertNoLeaks(t, rejectionBody)
		if !strings.Contains(rejectionBody, "unsupported fields") || !strings.Contains(rejectionBody, tt.field) {
			t.Fatalf("expected unsupported-field rejection for %s, got %s", tt.field, rejectionBody)
		}
	}
	upstream.assertFieldPatchPriorities(t, []int{88})

	primary := sidecarIntegrationAuthFixtureWith("auth-gemini-primary", "auth_001", "gemini", 88)
	shadow := sidecarIntegrationAuthFixtureWith("auth-gemini-shadow", "auth_999", "gemini", 77)
	shadow["name"] = primary["name"]
	upstream.setAuthFiles(primary, shadow)
	duplicateNameBody, _ := sidecarIntegrationRequestJSON(t, router, http.MethodPatch, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files/auth-gemini-primary/fields", map[string]any{
		"priority": 99,
	}, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, duplicateNameBody)
	upstream.assertFieldPatchPriorities(t, []int{88, 99})

	upstream.setAuthFiles(sidecarIntegrationAuthFixtureWith("auth-gemini-primary", "auth_001", "gemini", 88))
	upstream.failAuthFilesAfterNextFieldPatch()
	degradedBody, degradedPayload := sidecarIntegrationRequestJSON(t, router, http.MethodPatch, "/sidecars/"+strconv.Itoa(sidecarID)+"/auth-files/auth-gemini-primary/fields", map[string]any{
		"priority": 90,
	}, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, degradedBody)
	if degradedPayload["state"] != "succeeded_sync_failed" || strings.TrimSpace(fmt.Sprint(degradedPayload["sync_error"])) == "" {
		t.Fatalf("expected succeeded_sync_failed mutation state with sync_error, got %+v", degradedPayload)
	}
	degradedAuthFile, ok := degradedPayload["snapshot"].(map[string]any)
	if !ok || sidecarIntegrationInt(t, degradedAuthFile["priority"], "degraded auth priority") != 88 {
		t.Fatalf("degraded mutation response should retain pre-patch live auth file, got %+v", degradedPayload["snapshot"])
	}
	upstream.assertFieldPatchPriorities(t, []int{88, 99, 90})
	sidecarIntegrationAssertCurrentSchema(t, conn)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
}

func newSidecarIntegrationRouter(t *testing.T, ctx context.Context) (*pgx.Conn, http.Handler) {
	t.Helper()
	conn, _, router := newSidecarIntegrationServiceRouter(t, ctx)
	return conn, router
}

func newSidecarIntegrationServiceRouter(t *testing.T, ctx context.Context) (*pgx.Conn, *managementsidecars.Service, http.Handler) {
	t.Helper()
	return newSidecarIntegrationServiceRouterWithNow(t, ctx, func() time.Time {
		return time.Date(2026, time.May, 10, 12, 0, 0, 0, time.UTC)
	})
}

func newSidecarIntegrationServiceRouterWithNow(t *testing.T, ctx context.Context, now func() time.Time) (*pgx.Conn, *managementsidecars.Service, http.Handler) {
	t.Helper()
	conn, _, service, router := newSidecarIntegrationServiceRouterPoolWithNow(t, ctx, now)
	return conn, service, router
}

func newSidecarIntegrationServiceRouterPoolWithNow(t *testing.T, ctx context.Context, now func() time.Time) (*pgx.Conn, *pgxpool.Pool, *managementsidecars.Service, http.Handler) {
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
	service, err := managementsidecars.NewService(config.Settings{SecretEncryptionKey: "sidecar-integration-secret"}, managementsidecars.Options{Pool: pool, Now: now})
	if err != nil {
		t.Fatalf("build sidecar integration service: %v", err)
	}
	t.Cleanup(service.Close)
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return conn, pool, service, router
}

func sidecarIntegrationRequestJSON(t *testing.T, handler http.Handler, method string, path string, body any, wantStatus int) (string, map[string]any) {
	t.Helper()
	responseBody := sidecarIntegrationRequestStatus(t, handler, method, path, body, wantStatus)
	payload := map[string]any{}
	if responseBody != "" {
		if err := json.Unmarshal([]byte(responseBody), &payload); err != nil {
			t.Fatalf("decode sidecar integration response %s: %v", responseBody, err)
		}
	}
	return responseBody, payload
}

func sidecarIntegrationRequestStatus(t *testing.T, handler http.Handler, method string, path string, body any, wantStatus int) string {
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
	return responseBody
}

func sidecarIntegrationAssertDBNoLeaks(t *testing.T, conn *pgx.Conn, sidecarID int) {
	t.Helper()
	var combined string
	if err := conn.QueryRow(context.Background(), `SELECT concat_ws(' ',
		COALESCE((SELECT string_agg(management_password, ' ') FROM sidecar_instances WHERE id = $1), ''),
		COALESCE((SELECT string_agg(snapshot_json::text, ' ') FROM sidecar_provider_snapshots WHERE sidecar_id = $1), '')
	)`, sidecarID).Scan(&combined); err != nil {
		t.Fatalf("collect sidecar persisted strings: %v", err)
	}
	sidecarIntegrationAssertNoLeaks(t, combined)
	if !strings.Contains(combined, "enc:") {
		t.Fatalf("expected persisted sidecar management password to be encrypted, got %s", combined)
	}
}

func sidecarIntegrationAssertCurrentSchema(t *testing.T, conn *pgx.Conn) {
	t.Helper()
	assertSidecarSchemaContract(t, context.Background(), conn)
}

func sidecarIntegrationAssertRouteSurface(t *testing.T, handler http.Handler) {
	t.Helper()
	routes, ok := handler.(chi.Routes)
	if !ok {
		t.Fatalf("sidecar integration router does not expose chi routes")
	}
	mounted := map[string]map[string]bool{}
	if err := chi.Walk(routes, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if mounted[route] == nil {
			mounted[route] = map[string]bool{}
		}
		mounted[route][method] = true
		return nil
	}); err != nil {
		t.Fatalf("walk sidecar integration routes: %v", err)
	}
	assertSidecarRouteSet(t, mounted, sidecarIntegrationRouteSurface())
}

func sidecarIntegrationRouteSurface() map[string][]string {
	return map[string][]string{
		"/sidecars":                                          {http.MethodGet, http.MethodPost},
		"/sidecars/{sidecar_id}":                             {http.MethodGet, http.MethodPatch, http.MethodDelete},
		"/sidecars/{sidecar_id}/test-connection":             {http.MethodPost},
		"/sidecars/{sidecar_id}/sync":                        {http.MethodPost},
		"/sidecars/{sidecar_id}/auth-files":                  {http.MethodGet},
		"/sidecars/{sidecar_id}/auth-files/models":           {http.MethodGet},
		"/sidecars/{sidecar_id}/auth-files/{auth_id}":        {http.MethodDelete},
		"/sidecars/{sidecar_id}/auth-files/{auth_id}/status": {http.MethodPatch},
		"/sidecars/{sidecar_id}/auth-files/{auth_id}/fields": {http.MethodPatch},
		"/sidecars/{sidecar_id}/providers":                   {http.MethodGet},
		"/sidecars/{sidecar_id}/provider-snapshots":          {http.MethodGet},
		"/sidecars/{sidecar_id}/sync-status":                 {http.MethodGet},
	}
}

func assertSidecarRouteSet(t *testing.T, mounted map[string]map[string]bool, expected map[string][]string) {
	t.Helper()
	failures := []string{}
	for route, methods := range expected {
		for _, method := range methods {
			if !mounted[route][method] {
				failures = append(failures, method+" "+route+" missing from sidecar router")
			}
		}
	}
	for route, methods := range mounted {
		for method := range methods {
			if !sidecarIntegrationRouteMethodAllowed(expected, route, method) {
				failures = append(failures, method+" "+route+" unexpectedly present in sidecar router")
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("sidecar route surface mismatch:\n%s", strings.Join(failures, "\n"))
	}
}

func sidecarIntegrationRouteMethodAllowed(surface map[string][]string, route string, method string) bool {
	return slices.Contains(surface[route], method)
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

func sidecarIntegrationItems(t *testing.T, payload map[string]any, field string) []any {
	t.Helper()
	items, ok := payload[field].([]any)
	if !ok {
		t.Fatalf("expected array field %s, got %T %v", field, payload[field], payload[field])
	}
	return items
}

func sidecarIntegrationCreateOnlySidecar(t *testing.T, conn *pgx.Conn, router http.Handler, upstream *sidecarIntegrationUpstream, suffix string) int {
	t.Helper()
	body, created := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars", map[string]any{
		"name":                    "Integration " + suffix,
		"base_url":                upstream.URL(),
		"management_password":     sidecarIntegrationManagementSecret,
		"allow_private_network":   true,
		"allow_insecure_http":     true,
		"sync_interval_seconds":   60,
		"request_timeout_seconds": 5,
	}, http.StatusCreated)
	sidecarIntegrationAssertNoLeaks(t, body)
	sidecarID := sidecarIntegrationInt(t, created["id"], "id")
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
	return sidecarID
}

func sidecarIntegrationCreateAndSyncSidecar(t *testing.T, conn *pgx.Conn, router http.Handler, upstream *sidecarIntegrationUpstream, suffix string) int {
	t.Helper()
	sidecarID := sidecarIntegrationCreateOnlySidecar(t, conn, router, upstream, suffix)
	body, _ := sidecarIntegrationRequestJSON(t, router, http.MethodPost, "/sidecars/"+strconv.Itoa(sidecarID)+"/sync", nil, http.StatusOK)
	sidecarIntegrationAssertNoLeaks(t, body)
	sidecarIntegrationAssertDBNoLeaks(t, conn, sidecarID)
	return sidecarID
}

type sidecarIntegrationUpstream struct {
	server                          *httptest.Server
	mu                              sync.Mutex
	fieldPatches                    []map[string]any
	authFiles                       []map[string]any
	authFilesFailureAfterPatchCount int
}

func newSidecarIntegrationUpstream(t *testing.T) *sidecarIntegrationUpstream {
	t.Helper()
	upstream := &sidecarIntegrationUpstream{}
	upstream.setAuthFiles(sidecarIntegrationAuthFixture())
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.handle(t, w, r)
	}))
	return upstream
}

func (u *sidecarIntegrationUpstream) URL() string { return u.server.URL }

func (u *sidecarIntegrationUpstream) Close() { u.server.Close() }

func (u *sidecarIntegrationUpstream) setAuthFiles(files ...map[string]any) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authFiles = make([]map[string]any, 0, len(files))
	for _, file := range files {
		u.authFiles = append(u.authFiles, sidecarIntegrationCloneMap(file))
	}
}

func (u *sidecarIntegrationUpstream) failAuthFilesAfterNextFieldPatch() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.authFilesFailureAfterPatchCount = len(u.fieldPatches) + 1
}

func (u *sidecarIntegrationUpstream) handle(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("X-Management-Key"); got != sidecarIntegrationManagementSecret {
		t.Errorf("expected management key header %q, got %q", sidecarIntegrationManagementSecret, got)
	}
	switch r.URL.Path {
	case "/v0/management/auth-files":
		u.handleAuthFiles(w)
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
		sidecarIntegrationWriteJSON(w, map[string]any{"ok": true, "api_key": "mutation-secret", "management_password": "mutation-management-secret", "headers": map[string]any{"Authorization": "Bearer mutation-token"}, "body": "mutation-response-body", "account_id": "chatgpt-account-secret", "email": "mutation-user@example.test"})
	default:
		t.Errorf("unexpected sidecar integration upstream path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}
}

func (u *sidecarIntegrationUpstream) handleAuthFiles(w http.ResponseWriter) {
	u.mu.Lock()
	failAfterPatchCount := u.authFilesFailureAfterPatchCount
	patchCount := len(u.fieldPatches)
	files := make([]any, 0, len(u.authFiles))
	for _, file := range u.authFiles {
		files = append(files, sidecarIntegrationCloneMap(file))
	}
	u.mu.Unlock()
	if failAfterPatchCount > 0 && patchCount >= failAfterPatchCount {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"auth live refresh failed"}`))
		return
	}
	w.Header().Set("X-CPA-COMMIT", "21fad9dbb447a2ab70d51d0ac3e3d032525a6054")
	w.Header().Set("X-CPA-VERSION", "integration-delete-capable")
	sidecarIntegrationWriteJSON(w, map[string]any{"files": files})
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
	name, _ := payload["name"].(string)
	if name == "" || payload["priority"] == nil {
		return
	}
	priority := sidecarIntegrationInt(t, payload["priority"], "priority")
	for _, file := range u.authFiles {
		if file["id"] == name || file["name"] == name {
			file["priority"] = priority
			return
		}
	}
}

func (u *sidecarIntegrationUpstream) assertFieldPatch(t *testing.T, wantPriority int) {
	t.Helper()
	u.assertFieldPatchPriorities(t, []int{wantPriority})
}

func (u *sidecarIntegrationUpstream) assertFieldPatchPriorities(t *testing.T, want []int) {
	t.Helper()
	u.mu.Lock()
	defer u.mu.Unlock()
	if len(u.fieldPatches) != len(want) {
		t.Fatalf("field patch priorities count = %d want %d patches=%+v", len(u.fieldPatches), len(want), u.fieldPatches)
	}
	for i, wantPriority := range want {
		if got := sidecarIntegrationInt(t, u.fieldPatches[i]["priority"], "priority"); got != wantPriority {
			t.Fatalf("field patch[%d] priority = %d want %d patches=%+v", i, got, wantPriority, u.fieldPatches)
		}
	}
}

func sidecarIntegrationWriteJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func sidecarIntegrationCloneMap(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		panic(err)
	}
	return cloned
}

func sidecarIntegrationAuthFixture() map[string]any {
	return sidecarIntegrationAuthFixtureWith("auth-gemini-primary", "auth_001", "gemini", 10)
}

func sidecarIntegrationAuthFixtureWith(authID string, authIndex string, provider string, priority int) map[string]any {
	return map[string]any{
		"id": authID, "auth_index": authIndex, "name": authID + ".json", "provider": provider, "label": authID, "status": "active", "disabled": false, "runtime_only": false, "source": "file", "path": "/mock/cliproxy/auth/" + authID + ".json", "priority": priority,
		"quota": map[string]any{"exceeded": false, "reason": "", "next_recover_at": nil}, "recent_requests": []any{map[string]any{"success": 4, "failed": 0}}, "model_states": map[string]any{"default": map[string]any{"status": "active", "api_key": "raw-auth-token"}},
		"api_key": "raw-auth-secret",
	}
}

func sidecarIntegrationGeminiProviderFixture() map[string]any {
	return map[string]any{"api-key": "raw-provider-secret", "priority": 10, "prefix": "team-a/", "auth-index": "auth_001", "proxy-url": "http://proxy-secret.invalid", "headers": map[string]any{"Authorization": "Bearer raw-provider-token", "X-API-Key": "raw-header-key"}}
}

func sidecarIntegrationOpenAIProviderFixture() map[string]any {
	return map[string]any{"name": "compat", "priority": 5, "api-key-entries": []any{map[string]any{"api-key": "raw-openai-secret", "auth-index": "auth_openai", "headers": map[string]any{"Authorization": "Bearer raw-openai-token"}}}}
}
