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

	failures := make([]string, 0)
	for path, methods := range expectedSidecarRouteSurface() {
		for _, method := range methods {
			if !mounted[path][method] {
				failures = append(failures, method+" "+path+" missing from mounted management router")
			}
			if _, ok := openAPI[path][strings.ToLower(method)]; !ok {
				failures = append(failures, method+" "+path+" missing from OpenAPI paths")
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
	for path, operations := range openAPI {
		if !strings.HasPrefix(path, "/api/sidecars") {
			continue
		}
		for operation := range operations {
			method := strings.ToUpper(operation)
			if !slices.Contains(expectedSidecarRouteSurface()[path], method) {
				failures = append(failures, method+" "+path+" unexpectedly documented in OpenAPI paths")
			}
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("sidecar route/OpenAPI surface mismatch:\n%s", strings.Join(failures, "\n"))
	}
}

func TestSidecarOpenAPIComponentsMatchCurrentSurface(t *testing.T) {
	_, openAPI := loadSidecarOpenAPIArtifact(t)
	assertSidecarOpenAPISchemaSet(t, openAPI.Components.Schemas)
	assertSidecarOpenAPIProperties(t, openAPI.Components.Schemas, "SidecarCreateRequest", []string{"allow_insecure_http", "allow_private_network", "base_url", "enabled", "environment_label", "management_password", "name", "request_timeout_seconds", "skip_tls_verify", "sync_interval_seconds"})
	assertSidecarOpenAPIProperties(t, openAPI.Components.Schemas, "SidecarUpdateRequest", []string{"allow_insecure_http", "allow_private_network", "base_url", "enabled", "environment_label", "management_password", "name", "request_timeout_seconds", "skip_tls_verify", "sync_interval_seconds"})
	assertSidecarOpenAPIProperties(t, openAPI.Components.Schemas, "SidecarInstance", []string{"allow_insecure_http", "allow_private_network", "base_url", "base_url_canonical", "created_at", "credential_state", "enabled", "environment_label", "id", "last_successful_sync_at", "last_sync_at", "last_sync_error", "management_auth_state", "name", "pause_metadata", "request_timeout_seconds", "skip_tls_verify", "snapshot_stale_after", "sync_interval_seconds", "updated_at"})
	assertSidecarOpenAPIProperties(t, openAPI.Components.Schemas, "SidecarAuthModel", []string{"display_name", "id", "owned_by", "type"})
	assertSidecarOpenAPIProperties(t, openAPI.Components.Schemas, "SidecarAuthModelsResponse", []string{"models"})
	assertSidecarMutationOpenAPIContract(t, openAPI)
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

type sidecarOpenAPIDocument struct {
	Paths      map[string]map[string]any `json:"paths"`
	Components struct {
		Schemas map[string]any `json:"schemas"`
	} `json:"components"`
}

func loadSidecarOpenAPIArtifact(t *testing.T) ([]byte, sidecarOpenAPIDocument) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "docs", "openapi.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAPI artifact %s: %v", path, err)
	}
	var doc sidecarOpenAPIDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode OpenAPI artifact: %v", err)
	}
	return raw, doc
}

func loadSidecarOpenAPIPaths(t *testing.T) map[string]map[string]any {
	t.Helper()
	_, doc := loadSidecarOpenAPIArtifact(t)
	return doc.Paths
}

func assertSidecarOpenAPISchemaSet(t *testing.T, schemas map[string]any) {
	t.Helper()
	expected := map[string]bool{
		"SidecarAuthModel":                    true,
		"SidecarAuthModelsResponse":           true,
		"SidecarAuthMutationResponse":         true,
		"SidecarAuthSnapshot":                 true,
		"SidecarAuthSnapshotListResponse":     true,
		"SidecarCreateRequest":                true,
		"SidecarCredentialState":              true,
		"SidecarInstance":                     true,
		"SidecarListResponse":                 true,
		"SidecarPauseMetadata":                true,
		"SidecarProviderSnapshot":             true,
		"SidecarProviderSnapshotListResponse": true,
		"SidecarSyncResponse":                 true,
		"SidecarSyncStatusResponse":           true,
		"SidecarTestConnectionResponse":       true,
		"SidecarUpdateRequest":                true,
	}
	failures := []string{}
	for name := range expected {
		if _, ok := schemas[name]; !ok {
			failures = append(failures, name+" missing from OpenAPI schemas")
		}
	}
	for name := range schemas {
		if strings.HasPrefix(name, "Sidecar") && !expected[name] {
			failures = append(failures, name+" unexpectedly present in OpenAPI schemas")
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		t.Fatalf("sidecar OpenAPI schema mismatch:\n%s", strings.Join(failures, "\n"))
	}
}

func assertSidecarOpenAPIProperties(t *testing.T, schemas map[string]any, schemaName string, expected []string) {
	t.Helper()
	schema, ok := schemas[schemaName].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI schema %s missing or malformed", schemaName)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI schema %s properties missing or malformed", schemaName)
	}
	got := make([]string, 0, len(properties))
	for name := range properties {
		got = append(got, name)
	}
	sort.Strings(got)
	want := append([]string(nil), expected...)
	sort.Strings(want)
	if !slices.Equal(got, want) {
		t.Fatalf("OpenAPI schema %s properties = %v want %v", schemaName, got, want)
	}
}

func assertSidecarMutationOpenAPIContract(t *testing.T, doc sidecarOpenAPIDocument) {
	t.Helper()
	mutationSchema := sidecarContractMap(t, doc.Components.Schemas, "SidecarAuthMutationResponse")
	mutationProps := sidecarContractMap(t, mutationSchema, "properties")
	state := sidecarContractMap(t, mutationProps, "state")
	stateEnum, ok := state["enum"].([]any)
	if !ok || len(stateEnum) != 2 || stateEnum[0] != "succeeded" || stateEnum[1] != "succeeded_sync_failed" {
		t.Fatalf("SidecarAuthMutationResponse.state enum = %v", state["enum"])
	}
	if syncError := sidecarContractMap(t, mutationProps, "sync_error"); syncError["nullable"] == true {
		t.Fatalf("SidecarAuthMutationResponse.sync_error must be optional string, not nullable")
	}

	fieldsSchema := sidecarMutationRequestSchema(t, doc, "/api/sidecars/{sidecar_id}/auth-files/{auth_id}/fields", "patch")
	if fieldsSchema["type"] != "object" || fieldsSchema["additionalProperties"] != false {
		t.Fatalf("fields mutation request must be a closed object schema, got %v", fieldsSchema)
	}
	fieldsProps := sidecarContractMap(t, fieldsSchema, "properties")
	fieldNames := make([]string, 0, len(fieldsProps))
	for name := range fieldsProps {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)
	wantFields := []string{"force_live", "priority"}
	if !slices.Equal(fieldNames, wantFields) {
		t.Fatalf("fields mutation request properties = %v want %v", fieldNames, wantFields)
	}
	priority := sidecarContractMap(t, fieldsProps, "priority")
	if priority["type"] != "integer" || priority["minimum"] != float64(0) || priority["nullable"] == true {
		t.Fatalf("fields mutation priority must be a non-null non-negative integer, got %v", priority)
	}
	forceLive := sidecarContractMap(t, fieldsProps, "force_live")
	if forceLive["type"] != "boolean" || forceLive["nullable"] == true {
		t.Fatalf("fields mutation force_live must be a non-null boolean, got %v", forceLive)
	}

	deleteSchema := sidecarMutationRequestSchema(t, doc, "/api/sidecars/{sidecar_id}/auth-files/{auth_id}", "delete")
	deleteProps := sidecarContractMap(t, deleteSchema, "properties")
	confirmName := sidecarContractMap(t, deleteProps, "confirm_name")
	if confirmName["nullable"] == true || confirmName["minLength"] != float64(1) {
		t.Fatalf("delete confirm_name must be required non-null non-empty string, got %v", confirmName)
	}
}

func sidecarMutationRequestSchema(t *testing.T, doc sidecarOpenAPIDocument, path string, method string) map[string]any {
	t.Helper()
	operation := sidecarContractMap(t, doc.Paths[path], method)
	requestBody := sidecarContractMap(t, operation, "requestBody")
	content := sidecarContractMap(t, requestBody, "content")
	jsonContent := sidecarContractMap(t, content, "application/json")
	return sidecarContractMap(t, jsonContent, "schema")
}

func sidecarContractMap(t *testing.T, source map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := source[key].(map[string]any)
	if !ok {
		t.Fatalf("OpenAPI key %s missing or malformed in %v", key, source)
	}
	return value
}

func expectedSidecarRouteSurface() map[string][]string {
	return map[string][]string{
		"/api/sidecars":                                           {http.MethodGet, http.MethodPost},
		"/api/sidecars/{sidecar_id}":                              {http.MethodGet, http.MethodPatch, http.MethodDelete},
		"/api/sidecars/{sidecar_id}/test-connection":              {http.MethodPost},
		"/api/sidecars/{sidecar_id}/sync":                         {http.MethodPost},
		"/api/sidecars/{sidecar_id}/auth-files":                   {http.MethodGet},
		"/api/sidecars/{sidecar_id}/auth-files/models":            {http.MethodGet},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}":         {http.MethodDelete},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}/status":  {http.MethodPatch},
		"/api/sidecars/{sidecar_id}/auth-files/{auth_id}/fields":  {http.MethodPatch},
		"/api/sidecars/{sidecar_id}/auth-snapshots":               {http.MethodGet},
		"/api/sidecars/{sidecar_id}/auth-snapshots/{snapshot_id}": {http.MethodGet},
		"/api/sidecars/{sidecar_id}/providers":                    {http.MethodGet},
		"/api/sidecars/{sidecar_id}/provider-snapshots":           {http.MethodGet},
		"/api/sidecars/{sidecar_id}/sync-status":                  {http.MethodGet},
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
