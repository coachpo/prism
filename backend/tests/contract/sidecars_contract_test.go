package contract_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementsidecars "github.com/coachpo/prism/backend/internal/httpapi/management/sidecars"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
)

const sidecarContractManagementPassword = "sidecar-contract-management-secret"

func TestSidecarManagementRoutesRequireAuthAndRedactSecrets(t *testing.T) {
	authHarness := newContractHarness(t)
	seedVerifiedAuthSettings(t, authHarness, "sidecar-admin", "sidecar-password-123", "sidecar@example.com")
	sidecarHarness := newSidecarContractHarness(t, authHarness)

	unauthenticatedClient := sidecarHarness.newClient(t)
	unauthenticated := sidecarHarness.requestJSON(t, unauthenticatedClient, http.MethodGet, "/api/sidecars", nil, nil)
	assertErrorResponse(t, unauthenticated, http.StatusUnauthorized, "Authentication required")

	login := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/auth/login", map[string]any{
		"username":         "sidecar-admin",
		"password":         "sidecar-password-123",
		"session_duration": "7_days",
	}, nil)
	assertStatus(t, login, http.StatusOK)

	create := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/sidecars", map[string]any{
		"name":                    "Contract Sidecar",
		"base_url":                "http://127.0.0.1:19090",
		"management_password":     sidecarContractManagementPassword,
		"allow_private_network":   true,
		"allow_insecure_http":     true,
		"sync_interval_seconds":   60,
		"request_timeout_seconds": 5,
	}, nil)
	assertStatus(t, create, http.StatusCreated)
	createBody := readResponseBody(t, create)
	assertSidecarContractNoSecretLeak(t, createBody, sidecarContractManagementPassword)

	var created map[string]any
	decodeJSONResponse(t, create, &created)
	credential, ok := created["credential_state"].(map[string]any)
	if !ok || credential["management_password_configured"] != true || credential["management_password"] != "********" {
		t.Fatalf("expected masked management credential state, got %+v", created["credential_state"])
	}
	sidecarID, ok := created["id"].(float64)
	if !ok || sidecarID <= 0 {
		t.Fatalf("expected numeric sidecar id, got %+v", created["id"])
	}

	list := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodGet, "/api/sidecars", nil, nil)
	assertStatus(t, list, http.StatusOK)
	assertSidecarContractNoSecretLeak(t, readResponseBody(t, list), sidecarContractManagementPassword)

	var storedPassword string
	if err := authHarness.conn.QueryRow(context.Background(), `SELECT management_password FROM sidecar_instances WHERE id = $1`, int(sidecarID)).Scan(&storedPassword); err != nil {
		t.Fatalf("load stored sidecar management password: %v", err)
	}
	if storedPassword == sidecarContractManagementPassword || strings.Contains(storedPassword, sidecarContractManagementPassword) || !strings.HasPrefix(storedPassword, "enc:") {
		t.Fatalf("stored management password was not encrypted/redacted: %q", storedPassword)
	}
}

func TestSidecarRouteSurfaceMatchesMountedManagementRouter(t *testing.T) {
	mounted := collectMountedSidecarRoutes(t)

	failures := make([]string, 0)
	for path, methods := range expectedSidecarRouteSurface() {
		for _, method := range methods {
			if !mounted[path][method] {
				failures = append(failures, method+" "+path+" missing from mounted management router")
			}
		}
	}
	for path, methods := range mounted {
		for method := range methods {
			if !slices.Contains(expectedSidecarRouteSurface()[path], method) {
				failures = append(failures, method+" "+path+" unexpectedly mounted in management router")
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("sidecar route surface mismatch:\n%s", strings.Join(failures, "\n"))
	}
}

func TestSidecarAPIAllowlistContract(t *testing.T) {
	expectedPaths := []string{"/auth-files", "/auth-files/models", "/auth-files/status", "/auth-files/fields", "/gemini-api-key", "/claude-api-key", "/codex-api-key", "/vertex-api-key", "/openai-compatibility"}
	paths := managementsidecars.SupportedCLIProxyManagementPaths()
	if !slices.Equal(paths, expectedPaths) {
		t.Fatalf("CLIProxyAPI management allowlist changed: got %v want %v", paths, expectedPaths)
	}
	if sidecarContractStringIn(paths, "/usage-queue") {
		t.Fatalf("destructive /usage-queue must stay outside the management allowlist: %v", paths)
	}
	paths[0] = "/mutated"
	if managementsidecars.SupportedCLIProxyManagementPaths()[0] == "/mutated" {
		t.Fatalf("SupportedCLIProxyManagementPaths must return a defensive copy")
	}
}

func newSidecarContractHarness(t *testing.T, authHarness *contractHarness) *contractHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	settings := contractAuthSettings()
	settings.DatabaseURL = authHarness.dsn
	pool, err := pgxpool.New(ctx, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create sidecar contract pool: %v", err)
	}
	t.Cleanup(pool.Close)
	sidecarService, err := managementsidecars.NewService(settings, managementsidecars.Options{CORSOriginProvider: authHarness.hotRuntime, Pool: pool})
	if err != nil {
		t.Fatalf("build sidecar service: %v", err)
	}
	t.Cleanup(sidecarService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:                   "sidecar-contract-test",
		AuthService:               authHarness.service,
		RuntimeAuthService:        authHarness.service,
		RuntimeCache:              authHarness.runtimeCache,
		HotBootstrapConfigRuntime: authHarness.hotRuntime,
		SidecarsService:           sidecarService,
	})
	if err != nil {
		t.Fatalf("build sidecar contract handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &contractHarness{client: client, conn: authHarness.conn, dsn: authHarness.dsn, mailer: authHarness.mailer, server: server, service: authHarness.service, runtimeCache: authHarness.runtimeCache, hotRuntime: authHarness.hotRuntime, url: server.URL}
}

func collectMountedSidecarRoutes(t *testing.T) map[string]map[string]bool {
	t.Helper()
	service, err := managementsidecars.NewService(config.Settings{SecretEncryptionKey: "sidecar-route-contract-secret"}, managementsidecars.Options{})
	if err != nil {
		t.Fatalf("build sidecar route service: %v", err)
	}
	router := platformhttp.NewManagementRouter(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, service, nil, nil)
	routes, ok := router.(chi.Routes)
	if !ok {
		t.Fatalf("management router does not expose chi routes")
	}
	mounted := map[string]map[string]bool{}
	if err := chi.Walk(routes, func(method string, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/sidecars") {
			return nil
		}
		path := "/api" + route
		if mounted[path] == nil {
			mounted[path] = map[string]bool{}
		}
		mounted[path][method] = true
		return nil
	}); err != nil {
		t.Fatalf("walk sidecar management routes: %v", err)
	}
	return mounted
}

func expectedSidecarRouteSurface() map[string][]string {
	return map[string][]string{
		"/api/sidecars":                                          {http.MethodGet, http.MethodPost},
		"/api/sidecars/{sidecar_id}":                             {http.MethodGet, http.MethodPatch, http.MethodDelete},
		"/api/sidecars/{sidecar_id}/test-connection":             {http.MethodPost},
		"/api/sidecars/{sidecar_id}/sync":                        {http.MethodPost},
		"/api/sidecars/{sidecar_id}/auth-files":                  {http.MethodGet},
		"/api/sidecars/{sidecar_id}/auth-files/models":           {http.MethodGet},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}":        {http.MethodDelete},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}/status": {http.MethodPatch},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}/fields": {http.MethodPatch},
		"/api/sidecars/{sidecar_id}/providers":                   {http.MethodGet},
		"/api/sidecars/{sidecar_id}/provider-snapshots":          {http.MethodGet},
		"/api/sidecars/{sidecar_id}/sync-status":                 {http.MethodGet},
	}
}

func sidecarContractStringIn(values []string, want string) bool {
	return slices.Contains(values, want)
}

func assertSidecarContractNoSecretLeak(t *testing.T, value string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			t.Fatalf("sidecar contract payload leaked secret %q in %s", secret, value)
		}
	}
}
