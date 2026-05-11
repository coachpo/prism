package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
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

func TestSidecarRouteSurfaceMatchesOpenAPI(t *testing.T) {
	mounted := collectMountedSidecarRoutes(t)
	openAPI := loadSidecarOpenAPIPaths(t)

	missing := make([]string, 0)
	for path, methods := range expectedSidecarRouteSurface() {
		for _, method := range methods {
			if !mounted[path][method] {
				missing = append(missing, method+" "+path+" missing from mounted management router")
			}
			if _, ok := openAPI[path][strings.ToLower(method)]; !ok {
				missing = append(missing, method+" "+path+" missing from OpenAPI paths")
			}
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		t.Fatalf("sidecar route/OpenAPI surface mismatch:\n%s", strings.Join(missing, "\n"))
	}
}

func TestSidecarAPIAllowlistAndWatchdogPolicyContract(t *testing.T) {
	expectedPaths := []string{"/auth-files", "/auth-files/status", "/auth-files/fields", "/gemini-api-key", "/claude-api-key", "/codex-api-key", "/vertex-api-key", "/openai-compatibility", "/api-call"}
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

	authHarness := newContractHarness(t)
	seedVerifiedAuthSettings(t, authHarness, "sidecar-policy-admin", "sidecar-policy-password-123", "sidecar-policy@example.com")
	sidecarHarness := newSidecarContractHarness(t, authHarness)
	login := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/auth/login", map[string]any{
		"username":         "sidecar-policy-admin",
		"password":         "sidecar-policy-password-123",
		"session_duration": "7_days",
	}, nil)
	assertStatus(t, login, http.StatusOK)
	create := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/sidecars", map[string]any{
		"name":                    "Policy Contract Sidecar",
		"base_url":                "http://127.0.0.1:19091",
		"management_password":     sidecarContractManagementPassword,
		"allow_private_network":   true,
		"allow_insecure_http":     true,
		"sync_interval_seconds":   60,
		"request_timeout_seconds": 5,
	}, nil)
	assertStatus(t, create, http.StatusCreated)
	assertSidecarContractNoSecretLeak(t, readResponseBody(t, create), sidecarContractManagementPassword)
	var created map[string]any
	decodeJSONResponse(t, create, &created)
	sidecarID := sidecarContractNumber(t, created, "id")

	policy := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodGet, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/watchdog-policy", nil, nil)
	assertStatus(t, policy, http.StatusOK)
	policyBody := readResponseBody(t, policy)
	assertSidecarContractNoSecretLeak(t, policyBody, sidecarContractManagementPassword)
	var payload map[string]any
	decodeJSONResponse(t, policy, &payload)
	sidecarContractRequirePublicPolicyFields(t, payload)

	updated := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPut, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/watchdog-policy", map[string]any{
		"enabled":                       true,
		"failure_threshold":             4,
		"failure_window_seconds":        120,
		"fallback_cooldown_seconds":     600,
		"deprioritized_priority":        0,
		"prioritized_priority":          2,
		"manual_override_pause_seconds": 900,
		"probe_batch_size":              2,
		"probe_timeout_seconds":         10,
	}, nil)
	assertStatus(t, updated, http.StatusOK)
	updatedBody := readResponseBody(t, updated)
	assertSidecarContractNoSecretLeak(t, updatedBody, sidecarContractManagementPassword)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updated, &updatedPayload)
	sidecarContractRequirePublicPolicyFields(t, updatedPayload)
	if sidecarContractNumber(t, updatedPayload, "failure_threshold") != 4 || sidecarContractNumber(t, updatedPayload, "failure_window_seconds") != 120 || sidecarContractNumber(t, updatedPayload, "fallback_cooldown_seconds") != 600 || sidecarContractNumber(t, updatedPayload, "prioritized_priority") != 2 || sidecarContractNumber(t, updatedPayload, "manual_override_pause_seconds") != 900 || sidecarContractNumber(t, updatedPayload, "probe_batch_size") != 2 || sidecarContractNumber(t, updatedPayload, "probe_timeout_seconds") != 10 {
		t.Fatalf("watchdog policy update did not round-trip public fields: %+v", updatedPayload)
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

func loadSidecarOpenAPIPaths(t *testing.T) map[string]map[string]any {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "openapi.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAPI artifact %s: %v", path, err)
	}
	var doc struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode OpenAPI artifact: %v", err)
	}
	return doc.Paths
}

func expectedSidecarRouteSurface() map[string][]string {
	return map[string][]string{
		"/api/sidecars":                                           {http.MethodGet, http.MethodPost},
		"/api/sidecars/{sidecar_id}":                              {http.MethodGet, http.MethodPatch, http.MethodDelete},
		"/api/sidecars/{sidecar_id}/test-connection":              {http.MethodPost},
		"/api/sidecars/{sidecar_id}/sync":                         {http.MethodPost},
		"/api/sidecars/{sidecar_id}/auth-files":                   {http.MethodGet},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}/status":  {http.MethodPatch},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}/fields":  {http.MethodPatch},
		"/api/sidecars/{sidecar_id}/auth-snapshots":               {http.MethodGet},
		"/api/sidecars/{sidecar_id}/auth-snapshots/{snapshot_id}": {http.MethodGet},
		"/api/sidecars/{sidecar_id}/providers":                    {http.MethodGet},
		"/api/sidecars/{sidecar_id}/provider-snapshots":           {http.MethodGet},
		"/api/sidecars/{sidecar_id}/sync-status":                  {http.MethodGet},
		"/api/sidecars/{sidecar_id}/watchdog-policy":              {http.MethodGet, http.MethodPut, http.MethodPatch},
		"/api/sidecars/{sidecar_id}/actions":                      {http.MethodGet},
	}
}

func sidecarContractRequirePublicPolicyFields(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, field := range []string{"id", "sidecar_id", "enabled", "failure_threshold", "failure_window_seconds", "fallback_cooldown_seconds", "deprioritized_priority", "prioritized_priority", "manual_override_pause_seconds", "probe_batch_size", "probe_timeout_seconds", "created_at", "updated_at"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("watchdog policy payload missing public field %q: %+v", field, payload)
		}
	}
	for _, internal := range []string{"probe_cursor_auth_id"} {
		if _, ok := payload[internal]; ok {
			t.Fatalf("watchdog policy payload exposed internal field %q: %+v", internal, payload)
		}
	}
	if _, ok := payload["enabled"].(bool); !ok {
		t.Fatalf("watchdog policy enabled field must be boolean: %+v", payload)
	}
	for _, field := range []string{"id", "sidecar_id", "failure_threshold", "failure_window_seconds", "fallback_cooldown_seconds", "deprioritized_priority", "prioritized_priority", "manual_override_pause_seconds", "probe_batch_size", "probe_timeout_seconds"} {
		sidecarContractNumber(t, payload, field)
	}
}

func sidecarContractNumber(t *testing.T, payload map[string]any, field string) int {
	t.Helper()
	number, ok := payload[field].(float64)
	if !ok {
		t.Fatalf("expected numeric field %q in %+v", field, payload)
	}
	return int(number)
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
