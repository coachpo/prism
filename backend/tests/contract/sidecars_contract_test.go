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
	activeRevision := sidecarContractObject(t, payload, "active_revision")
	activeRevisionID := sidecarContractNumber(t, activeRevision, "id")

	updated := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPut, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/watchdog-policy", map[string]any{
		"expected_revision_id":          activeRevisionID,
		"enabled":                       true,
		"failure_threshold":             4,
		"failure_window_seconds":        120,
		"fallback_cooldown_seconds":     600,
		"quota_exceeded_priority":       90,
		"using_priority":                99,
		"working_priority":              99,
		"empty_quota_priority":          90,
		"initial_priority":              50,
		"error_priority":                10,
		"manual_override_pause_seconds": 900,
		"probe_concurrency":             2,
		"probe_timeout_seconds":         10,
		"probe_jitter_min_ms":           100,
		"probe_jitter_max_ms":           1000,
		"cooldown_jitter_percent":       20,
		"probe_batch_cooldown_seconds":  45,
		"quota_inventory_enabled":       true,
		"initial_scan_enabled":          true,
		"rolling_refresh_enabled":       false,
		"rolling_refresh_after_seconds": 7200,
	}, nil)
	assertStatus(t, updated, http.StatusOK)
	updatedBody := readResponseBody(t, updated)
	assertSidecarContractNoSecretLeak(t, updatedBody, sidecarContractManagementPassword)
	var updatedPayload map[string]any
	decodeJSONResponse(t, updated, &updatedPayload)
	sidecarContractRequirePublicPolicyFields(t, updatedPayload)
	if updatedPayload["has_pending_changes"] != true {
		t.Fatalf("watchdog policy save should create pending changes: %+v", updatedPayload)
	}
	pendingRevision := sidecarContractObject(t, updatedPayload, "pending_revision")
	if sidecarContractNumber(t, pendingRevision, "failure_threshold") != 4 || sidecarContractNumber(t, pendingRevision, "failure_window_seconds") != 120 || sidecarContractNumber(t, pendingRevision, "fallback_cooldown_seconds") != 600 || sidecarContractNumber(t, pendingRevision, "quota_exceeded_priority") != 90 || sidecarContractNumber(t, pendingRevision, "using_priority") != 99 || sidecarContractNumber(t, pendingRevision, "working_priority") != 99 || sidecarContractNumber(t, pendingRevision, "empty_quota_priority") != 90 || sidecarContractNumber(t, pendingRevision, "initial_priority") != 50 || sidecarContractNumber(t, pendingRevision, "error_priority") != 10 || sidecarContractNumber(t, pendingRevision, "manual_override_pause_seconds") != 900 || sidecarContractNumber(t, pendingRevision, "probe_concurrency") != 2 || sidecarContractNumber(t, pendingRevision, "probe_timeout_seconds") != 10 || sidecarContractNumber(t, pendingRevision, "probe_jitter_min_ms") != 100 || sidecarContractNumber(t, pendingRevision, "probe_jitter_max_ms") != 1000 || sidecarContractNumber(t, pendingRevision, "cooldown_jitter_percent") != 20 || sidecarContractNumber(t, pendingRevision, "probe_batch_cooldown_seconds") != 45 || sidecarContractNumber(t, pendingRevision, "rolling_refresh_after_seconds") != 7200 || pendingRevision["quota_inventory_enabled"] != true || pendingRevision["initial_scan_enabled"] != true || pendingRevision["rolling_refresh_enabled"] != false {
		t.Fatalf("watchdog policy update did not round-trip pending fields: %+v", updatedPayload)
	}
	pendingRevisionID := sidecarContractNumber(t, pendingRevision, "id")
	applied := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/watchdog-policy/apply", map[string]any{
		"target_revision_id":   pendingRevisionID,
		"expected_revision_id": activeRevisionID,
	}, nil)
	assertStatus(t, applied, http.StatusOK)
	var appliedPayload map[string]any
	decodeJSONResponse(t, applied, &appliedPayload)
	sidecarContractRequirePublicPolicyFields(t, appliedPayload)
	if appliedPayload["has_pending_changes"] != false || appliedPayload["pending_revision"] != nil || sidecarContractNumber(t, appliedPayload, "probe_concurrency") != 2 || sidecarContractNumber(t, sidecarContractObject(t, appliedPayload, "active_revision"), "id") != pendingRevisionID {
		t.Fatalf("watchdog policy apply did not activate pending revision: %+v", appliedPayload)
	}
}

func TestSidecarQuotaRoutesRedactInternalState(t *testing.T) {
	authHarness := newContractHarness(t)
	seedVerifiedAuthSettings(t, authHarness, "sidecar-quota-admin", "sidecar-quota-password-123", "sidecar-quota@example.com")
	sidecarHarness := newSidecarContractHarness(t, authHarness)
	login := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/auth/login", map[string]any{
		"username":         "sidecar-quota-admin",
		"password":         "sidecar-quota-password-123",
		"session_duration": "7_days",
	}, nil)
	assertStatus(t, login, http.StatusOK)
	create := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/sidecars", map[string]any{
		"name":                    "Quota Contract Sidecar",
		"base_url":                "http://127.0.0.1:19092",
		"management_password":     sidecarContractManagementPassword,
		"allow_private_network":   true,
		"allow_insecure_http":     true,
		"sync_interval_seconds":   60,
		"request_timeout_seconds": 5,
	}, nil)
	assertStatus(t, create, http.StatusCreated)
	var created map[string]any
	decodeJSONResponse(t, create, &created)
	sidecarID := sidecarContractNumber(t, created, "id")

	hiddenAuthIndex := "contract-hidden-auth-index"
	hiddenCursor := "contract-hidden-scan-cursor"
	hiddenSnapshotSecret := "contract-hidden-snapshot-secret"
	now := time.Date(2026, time.May, 11, 20, 0, 0, 0, time.UTC)
	var observationID int
	if err := authHarness.conn.QueryRow(context.Background(), `INSERT INTO sidecar_quota_probe_observations (
sidecar_id, auth_id, auth_index, provider, probed_at, probe_status, upstream_status_code,
quota_exceeded, reason_code, windows_json)
VALUES ($1, 'contract-auth', $2, 'codex', $3, 'probe_succeeded', 200, false, 'healthy', '[]'::jsonb)
RETURNING id`, sidecarID, hiddenAuthIndex, now).Scan(&observationID); err != nil {
		t.Fatalf("seed quota probe observation: %v", err)
	}
	if _, err := authHarness.conn.Exec(context.Background(), `INSERT INTO sidecar_auth_snapshots (
sidecar_id, auth_id, auth_index, name, provider, status, disabled, priority,
recent_requests_json, model_states_json, snapshot_json, observed_at)
VALUES ($1, 'contract-auth', $2, 'contract-auth.json', 'codex', 'active', false, 7,
'[]'::jsonb, $3::jsonb, $4::jsonb, $5)`, sidecarID, hiddenAuthIndex, `{"codex":{"api_key":"`+hiddenSnapshotSecret+`"}}`, `{"api_key":"`+hiddenSnapshotSecret+`"}`, now); err != nil {
		t.Fatalf("seed quota auth snapshot: %v", err)
	}
	if _, err := authHarness.conn.Exec(context.Background(), `INSERT INTO sidecar_auth_quota_states (
sidecar_id, auth_id, auth_index, auth_name, provider, snapshot_observed_at, quota_band,
probe_status, quota_exceeded, reason_code, last_observation_id, last_probed_at)
VALUES ($1, 'contract-auth', $2, 'contract-auth.json', 'codex', $3, 'using', 'probe_succeeded', false, 'healthy', $4, $3)`, sidecarID, hiddenAuthIndex, now, observationID); err != nil {
		t.Fatalf("seed quota auth state: %v", err)
	}
	var scanID int
	if err := authHarness.conn.QueryRow(context.Background(), `INSERT INTO sidecar_quota_scan_runs (
sidecar_id, scan_type, status, requested_by, cursor_auth_id, planned_count, attempted_count, started_at)
VALUES ($1, 'manual', 'running', 'contract-operator', $2, 2, 1, $3)
RETURNING id`, sidecarID, hiddenCursor, now).Scan(&scanID); err != nil {
		t.Fatalf("seed quota scan run: %v", err)
	}

	states := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodGet, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/quota-states", nil, nil)
	assertStatus(t, states, http.StatusOK)
	stateBody := readResponseBody(t, states)
	assertSidecarContractNoSecretLeak(t, stateBody, sidecarContractManagementPassword, hiddenAuthIndex, hiddenCursor, hiddenSnapshotSecret)
	assertSidecarContractNoInternalQuotaLeak(t, stateBody)
	if !strings.Contains(stateBody, `"auth_index_present":true`) || !strings.Contains(stateBody, `"current_priority":7`) || !strings.Contains(stateBody, `"quota_band":"using"`) || !strings.Contains(stateBody, `"reason_code":"healthy"`) || !strings.Contains(stateBody, `"last_snapshot_at":`) || !strings.Contains(stateBody, `"active_hold":false`) {
		t.Fatalf("quota-state response did not expose the public shape: %s", stateBody)
	}

	current := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodGet, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans/current", nil, nil)
	assertStatus(t, current, http.StatusOK)
	currentBody := readResponseBody(t, current)
	assertSidecarContractNoSecretLeak(t, currentBody, hiddenCursor, hiddenAuthIndex, hiddenSnapshotSecret)
	assertSidecarContractNoInternalQuotaLeak(t, currentBody)
	if !strings.Contains(currentBody, `"status":"running"`) || !strings.Contains(currentBody, `"attempted_count":1`) {
		t.Fatalf("quota-scan current response did not expose safe scan state: %s", currentBody)
	}

	cancel := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodPost, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans/"+strconv.Itoa(scanID)+"/cancel", nil, nil)
	assertStatus(t, cancel, http.StatusAccepted)
	cancelBody := readResponseBody(t, cancel)
	assertSidecarContractNoSecretLeak(t, cancelBody, hiddenCursor, hiddenAuthIndex, hiddenSnapshotSecret)
	assertSidecarContractNoInternalQuotaLeak(t, cancelBody)
	if !strings.Contains(cancelBody, `"status":"cancelled"`) {
		t.Fatalf("quota-scan cancel response did not expose cancelled status: %s", cancelBody)
	}

	currentAfterCancel := sidecarHarness.requestJSON(t, sidecarHarness.client, http.MethodGet, "/api/sidecars/"+strconv.Itoa(sidecarID)+"/quota-scans/current", nil, nil)
	assertStatus(t, currentAfterCancel, http.StatusNoContent)
	if body := readResponseBody(t, currentAfterCancel); body != "" {
		t.Fatalf("quota-scan current response should be empty after cancel, got %q", body)
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
		"/api/sidecars/{sidecar_id}/watchdog-policy/apply":        {http.MethodPost},
		"/api/sidecars/{sidecar_id}/actions":                      {http.MethodGet},
	}
}

func sidecarContractRequirePublicPolicyFields(t *testing.T, payload map[string]any) {
	t.Helper()
	for _, field := range []string{"id", "sidecar_id", "active_revision_id", "pending_revision_id", "has_pending_changes", "active_revision", "pending_revision", "active_sweep", "enabled", "watchdog_sweep_interval_seconds", "failure_threshold", "failure_window_seconds", "fallback_cooldown_seconds", "quota_exceeded_priority", "using_priority", "working_priority", "empty_quota_priority", "initial_priority", "error_priority", "manual_override_pause_seconds", "probe_concurrency", "probe_timeout_seconds", "probe_jitter_min_ms", "probe_jitter_max_ms", "cooldown_jitter_percent", "probe_batch_cooldown_seconds", "quota_inventory_enabled", "initial_scan_enabled", "rolling_refresh_enabled", "rolling_refresh_after_seconds", "created_at", "updated_at"} {
		if _, ok := payload[field]; !ok {
			t.Fatalf("watchdog policy payload missing public field %q: %+v", field, payload)
		}
	}
	for _, internal := range []string{"probe_cursor_auth_id", "probe_last_batch_completed_at"} {
		if _, ok := payload[internal]; ok {
			t.Fatalf("watchdog policy payload exposed internal field %q: %+v", internal, payload)
		}
	}
	if _, ok := payload["has_pending_changes"].(bool); !ok {
		t.Fatalf("watchdog policy has_pending_changes field must be boolean: %+v", payload)
	}
	for _, field := range []string{"enabled", "quota_inventory_enabled", "initial_scan_enabled", "rolling_refresh_enabled"} {
		if _, ok := payload[field].(bool); !ok {
			t.Fatalf("watchdog policy %s field must be boolean: %+v", field, payload)
		}
	}
	for _, field := range []string{"id", "sidecar_id", "active_revision_id", "failure_threshold", "failure_window_seconds", "fallback_cooldown_seconds", "quota_exceeded_priority", "using_priority", "working_priority", "empty_quota_priority", "initial_priority", "error_priority", "manual_override_pause_seconds", "probe_concurrency", "probe_timeout_seconds", "probe_jitter_min_ms", "probe_jitter_max_ms", "cooldown_jitter_percent", "probe_batch_cooldown_seconds", "rolling_refresh_after_seconds", "watchdog_sweep_interval_seconds"} {
		sidecarContractNumber(t, payload, field)
	}
	sidecarContractObject(t, payload, "active_revision")
}

func sidecarContractNumber(t *testing.T, payload map[string]any, field string) int {
	t.Helper()
	number, ok := payload[field].(float64)
	if !ok {
		t.Fatalf("expected numeric field %q in %+v", field, payload)
	}
	return int(number)
}

func sidecarContractObject(t *testing.T, payload map[string]any, field string) map[string]any {
	t.Helper()
	object, ok := payload[field].(map[string]any)
	if !ok {
		t.Fatalf("expected object field %q in %+v", field, payload)
	}
	return object
}

func sidecarContractStringIn(values []string, want string) bool {
	return slices.Contains(values, want)
}

func assertSidecarContractNoInternalQuotaLeak(t *testing.T, value string) {
	t.Helper()
	for _, marker := range []string{`"auth_index":`, `"cursor_auth_id":`, `"last_observation_id":`} {
		if strings.Contains(value, marker) {
			t.Fatalf("sidecar quota contract payload leaked internal marker %q in %s", marker, value)
		}
	}
}

func assertSidecarContractNoSecretLeak(t *testing.T, value string, secrets ...string) {
	t.Helper()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(value, secret) {
			t.Fatalf("sidecar contract payload leaked secret %q in %s", secret, value)
		}
	}
}
