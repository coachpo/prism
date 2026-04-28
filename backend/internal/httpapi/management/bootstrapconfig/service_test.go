package bootstrapconfig

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

const (
	routeTestDatabasePassword         = "route-db-password"
	routeTestDatabaseQueryPassword    = "route-query-password"
	routeTestDatabaseSSLPassword      = "route-ssl-password"
	routeTestDatabaseURL              = "postgres://prism:" + routeTestDatabasePassword + "@db.route.internal:5432/prism?sslmode=disable&password=" + routeTestDatabaseQueryPassword + "&sslpassword=" + routeTestDatabaseSSLPassword
	routeTestNextDatabasePassword     = "route-next-db-password"
	routeTestNextDatabaseURL          = "postgres://prism:" + routeTestNextDatabasePassword + "@db.next.internal:5432/prism?sslmode=disable"
	routeTestReplacementJWTSecret     = "route-replacement-jwt-secret"
	routeTestRuntimeReplacementSecret = "route-runtime-replacement-secret"
)

var (
	routeTestCreatedAt = time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	routeTestUpdatedAt = time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC)
)

func TestBootstrapConfigRouteGetReturnsSafeMetadata(t *testing.T) {
	fixture := newBootstrapRouteFixture(t)

	response := fixture.do(t, http.MethodGet, "/api/config/bootstrap", nil)

	requireStatus(t, response, http.StatusOK)
	assertNoRouteSecrets(t, response.Body.Bytes(), fixture.settings)
	body := decodeBootstrapConfigResponse(t, response)
	if body.ConfigPath != fixture.path {
		t.Fatal("expected response to include the temp config path")
	}
	if body.FileRevision != fixture.snapshot.FileRevision || body.DocumentETag != fixture.snapshot.DocumentETag {
		t.Fatal("expected response to include current file revision and etag")
	}
	if body.LoadedRevision != fixture.snapshot.FileRevision {
		t.Fatal("expected response to include loaded revision metadata")
	}
	if body.LoadedDocumentETag != fixture.snapshot.DocumentETag {
		t.Fatal("expected response to include loaded document etag metadata")
	}
	if body.RestartRequired {
		t.Fatal("expected matching loaded metadata not to require restart")
	}
	if !body.Writable {
		t.Fatal("expected route fixture to report writable metadata")
	}
	databaseSecret := body.Secrets[config.BootstrapConfigSecretDatabaseURL]
	if !databaseSecret.Configured || !databaseSecret.Editable || databaseSecret.Masked == "" {
		t.Fatal("expected database secret metadata to be configured, editable, and masked")
	}
	runtimeSecret := body.Secrets[config.BootstrapConfigSecretRuntimeSecretEncryptionKey]
	if !runtimeSecret.Configured || runtimeSecret.Editable {
		t.Fatal("expected runtime secret metadata to be configured and read-only")
	}
}

func TestBootstrapConfigRouteValidateDoesNotRewriteFile(t *testing.T) {
	fixture := newBootstrapRouteFixture(t)
	before := mustReadFile(t, fixture.path)
	request := bootstrapRouteRequestForSnapshot(t, fixture.snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(1200)

	response := fixture.doJSON(t, http.MethodPost, "/api/config/bootstrap/validate", request)

	requireStatus(t, response, http.StatusOK)
	assertFileUnchanged(t, fixture.path, before)
	assertNoRouteSecrets(t, response.Body.Bytes(), fixture.settings)
	body := decodeBootstrapConfigResponse(t, response)
	if body.FileRevision != fixture.snapshot.FileRevision+1 {
		t.Fatal("expected validation response to show the prepared revision")
	}
	if !body.RestartRequired {
		t.Fatal("expected changed validation response to require restart against loaded metadata")
	}
}

func TestBootstrapConfigRoutePutWritesAndReturnsRestartRequired(t *testing.T) {
	fixture := newBootstrapRouteFixture(t)
	originalSettings := fixture.settings
	before := mustReadFile(t, fixture.path)
	request := bootstrapRouteRequestForSnapshot(t, fixture.snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(1800)
	request.SecretUpdates[config.BootstrapConfigSecretDatabaseURL] = config.BootstrapConfigSecretUpdate{
		Action: config.BootstrapConfigSecretActionReplace,
		Value:  routeStringPtr(routeTestNextDatabaseURL),
	}
	request.SecretUpdates[config.BootstrapConfigSecretAuthJWTSigningKey] = config.BootstrapConfigSecretUpdate{
		Action: config.BootstrapConfigSecretActionReplace,
		Value:  routeStringPtr(routeTestReplacementJWTSecret),
	}
	request.Confirmations = []string{
		config.BootstrapConfigConfirmationDatabaseURLChange,
		config.BootstrapConfigConfirmationAuthJWTSigningKeyChange,
	}

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusOK)
	assertNoRouteSecrets(t, response.Body.Bytes(), fixture.settings,
		routeSecret{label: "replacement database password", value: routeTestNextDatabasePassword},
		routeSecret{label: "replacement JWT secret", value: routeTestReplacementJWTSecret},
	)
	body := decodeBootstrapConfigResponse(t, response)
	if body.FileRevision != fixture.snapshot.FileRevision+1 {
		t.Fatal("expected successful write to increment revision")
	}
	if !body.RestartRequired {
		t.Fatal("expected write response to require restart when current file differs from loaded metadata")
	}
	after := mustReadFile(t, fixture.path)
	if bytes.Equal(before, after) {
		t.Fatal("expected successful write to update the bootstrap file")
	}
	_, writtenSettings, err := fixture.manager.LoadBootstrapConfigDocument(fixture.path)
	if err != nil {
		t.Fatalf("load written bootstrap config: %v", err)
	}
	if writtenSettings.DatabaseURL != routeTestNextDatabaseURL {
		t.Fatal("expected database secret replacement to persist")
	}
	if writtenSettings.AuthJWTSecret != routeTestReplacementJWTSecret {
		t.Fatal("expected auth JWT secret replacement to persist")
	}
	if writtenSettings.SecretEncryptionKey != originalSettings.SecretEncryptionKey {
		t.Fatal("expected runtime secret preserve action to keep the original value")
	}
	if writtenSettings.ConfigBundleEncryptionKey != originalSettings.ConfigBundleEncryptionKey {
		t.Fatal("expected bundle secret preserve action to keep the original value")
	}
}

func TestBootstrapConfigRoutesRejectStaleOrMissingExpectations(t *testing.T) {
	operations := []struct {
		name   string
		method string
		path   string
	}{
		{name: "put", method: http.MethodPut, path: "/api/config/bootstrap"},
		{name: "validate", method: http.MethodPost, path: "/api/config/bootstrap/validate"},
	}
	tests := []struct {
		name string
		body func(*testing.T, config.BootstrapConfigSnapshot) []byte
	}{
		{
			name: "stale revision",
			body: func(t *testing.T, snapshot config.BootstrapConfigSnapshot) []byte {
				request := bootstrapRouteRequestForSnapshot(t, snapshot)
				request.ExpectedRevision = snapshot.FileRevision + 1
				return mustMarshalBootstrapRouteJSON(t, request)
			},
		},
		{
			name: "stale etag",
			body: func(t *testing.T, snapshot config.BootstrapConfigSnapshot) []byte {
				request := bootstrapRouteRequestForSnapshot(t, snapshot)
				request.ExpectedETag = "sha256:stale"
				return mustMarshalBootstrapRouteJSON(t, request)
			},
		},
		{
			name: "zero expected revision",
			body: func(t *testing.T, snapshot config.BootstrapConfigSnapshot) []byte {
				request := bootstrapRouteRequestForSnapshot(t, snapshot)
				request.ExpectedRevision = 0
				return mustMarshalBootstrapRouteJSON(t, request)
			},
		},
		{
			name: "missing expected revision",
			body: func(t *testing.T, snapshot config.BootstrapConfigSnapshot) []byte {
				return bootstrapRouteRequestPayloadWithoutFields(t, snapshot, "expected_revision")
			},
		},
		{
			name: "empty expected etag",
			body: func(t *testing.T, snapshot config.BootstrapConfigSnapshot) []byte {
				request := bootstrapRouteRequestForSnapshot(t, snapshot)
				request.ExpectedETag = ""
				return mustMarshalBootstrapRouteJSON(t, request)
			},
		},
		{
			name: "missing expected etag",
			body: func(t *testing.T, snapshot config.BootstrapConfigSnapshot) []byte {
				return bootstrapRouteRequestPayloadWithoutFields(t, snapshot, "expected_etag")
			},
		},
	}

	for _, operation := range operations {
		for _, testCase := range tests {
			t.Run(operation.name+" "+testCase.name, func(t *testing.T) {
				fixture := newBootstrapRouteFixture(t)
				before := mustReadFile(t, fixture.path)

				response := fixture.do(t, operation.method, operation.path, testCase.body(t, fixture.snapshot))

				requireStatus(t, response, http.StatusConflict)
				assertFileUnchanged(t, fixture.path, before)
				assertNoRouteSecrets(t, response.Body.Bytes(), fixture.settings)
				detail := decodeErrorDetailMap(t, response)
				if detail["message"] != "bootstrap config has changed since it was loaded" {
					t.Fatal("expected conflict response detail message")
				}
				if detail["current_revision"] != float64(fixture.snapshot.FileRevision) {
					t.Fatal("expected conflict response to include current revision")
				}
				if detail["current_etag"] != fixture.snapshot.DocumentETag {
					t.Fatal("expected conflict response to include current etag")
				}
			})
		}
	}
}

func TestBootstrapConfigRoutePutRejectsConfirmationsAndUnsupportedSecretActions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*config.BootstrapConfigUpdateRequest, config.BootstrapConfigSnapshot)
		assert func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "missing server port confirmation",
			mutate: func(request *config.BootstrapConfigUpdateRequest, snapshot config.BootstrapConfigSnapshot) {
				values := *request.Values
				values.Server.Port = routeIntPtr(*snapshot.Values.Server.Port + 1)
				request.Values = &values
			},
			assert: func(t *testing.T, response *httptest.ResponseRecorder) {
				detail := decodeErrorDetailMap(t, response)
				if !containsStringValue(detail["required_confirmations"], config.BootstrapConfigConfirmationServerPortChange) {
					t.Fatal("expected missing confirmation response to name server port confirmation")
				}
			},
		},
		{
			name: "unsupported secret action",
			mutate: func(request *config.BootstrapConfigUpdateRequest, _ config.BootstrapConfigSnapshot) {
				request.SecretUpdates[config.BootstrapConfigSecretAuthJWTSigningKey] = config.BootstrapConfigSecretUpdate{
					Action: config.BootstrapConfigSecretAction("rotate"),
				}
			},
			assert: func(t *testing.T, response *httptest.ResponseRecorder) {
				detail := decodeErrorDetailMap(t, response)
				if detail["field"] != config.BootstrapConfigSecretAuthJWTSigningKey || detail["action"] != "rotate" {
					t.Fatal("expected unsupported action response to identify the secret field and action")
				}
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newBootstrapRouteFixture(t)
			before := mustReadFile(t, fixture.path)
			request := bootstrapRouteRequestForSnapshot(t, fixture.snapshot)
			testCase.mutate(&request, fixture.snapshot)

			response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

			requireStatus(t, response, http.StatusUnprocessableEntity)
			assertFileUnchanged(t, fixture.path, before)
			assertNoRouteSecrets(t, response.Body.Bytes(), fixture.settings)
			testCase.assert(t, response)
		})
	}
}

func TestBootstrapConfigRoutePutRejectsUnsafeSecretUpdates(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(*config.BootstrapConfigUpdateRequest, config.BootstrapConfigSnapshot)
		extraSecrets []routeSecret
		assert       func(*testing.T, *httptest.ResponseRecorder)
	}{
		{
			name: "redacted placeholder replacement",
			mutate: func(request *config.BootstrapConfigUpdateRequest, snapshot config.BootstrapConfigSnapshot) {
				request.SecretUpdates[config.BootstrapConfigSecretAuthJWTSigningKey] = config.BootstrapConfigSecretUpdate{
					Action: config.BootstrapConfigSecretActionReplace,
					Value:  routeStringPtr(snapshot.Secrets[config.BootstrapConfigSecretAuthJWTSigningKey].Masked),
				}
			},
			assert: func(t *testing.T, response *httptest.ResponseRecorder) {
				detail := decodeErrorDetailMap(t, response)
				if detail["field"] != config.BootstrapConfigSecretAuthJWTSigningKey || detail["reason"] != "replacement value must not be a redacted placeholder" {
					t.Fatal("expected placeholder response to identify the rejected secret field")
				}
			},
		},
		{
			name: "runtime secret replacement",
			mutate: func(request *config.BootstrapConfigUpdateRequest, _ config.BootstrapConfigSnapshot) {
				request.SecretUpdates[config.BootstrapConfigSecretRuntimeSecretEncryptionKey] = config.BootstrapConfigSecretUpdate{
					Action: config.BootstrapConfigSecretActionReplace,
					Value:  routeStringPtr(routeTestRuntimeReplacementSecret),
				}
			},
			extraSecrets: []routeSecret{{label: "runtime replacement secret", value: routeTestRuntimeReplacementSecret}},
			assert: func(t *testing.T, response *httptest.ResponseRecorder) {
				detail := decodeErrorDetailMap(t, response)
				if detail["field"] != config.BootstrapConfigSecretRuntimeSecretEncryptionKey || detail["action"] != string(config.BootstrapConfigSecretActionReplace) {
					t.Fatal("expected runtime secret response to identify the rejected secret field")
				}
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newBootstrapRouteFixture(t)
			before := mustReadFile(t, fixture.path)
			request := bootstrapRouteRequestForSnapshot(t, fixture.snapshot)
			testCase.mutate(&request, fixture.snapshot)

			response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

			requireStatus(t, response, http.StatusUnprocessableEntity)
			assertFileUnchanged(t, fixture.path, before)
			assertNoRouteSecrets(t, response.Body.Bytes(), fixture.settings, testCase.extraSecrets...)
			testCase.assert(t, response)
		})
	}
}

func TestBootstrapConfigRoutesRejectMalformedAndUnknownBodies(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   []byte
	}{
		{name: "malformed validate body", method: http.MethodPost, path: "/api/config/bootstrap/validate", body: []byte(`{"expected_revision":`)},
		{name: "unknown put body field", method: http.MethodPut, path: "/api/config/bootstrap", body: []byte(`{"unexpected":true}`)},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newBootstrapRouteFixture(t)
			before := mustReadFile(t, fixture.path)

			response := fixture.do(t, testCase.method, testCase.path, testCase.body)

			requireStatus(t, response, http.StatusBadRequest)
			assertFileUnchanged(t, fixture.path, before)
			assertNoRouteSecrets(t, response.Body.Bytes(), fixture.settings)
			if detail := decodeErrorDetailString(t, response); detail != "Invalid request body" {
				t.Fatal("expected invalid body response detail")
			}
		})
	}
}

func TestBootstrapConfigRoutePutMapsFileWriteFailure(t *testing.T) {
	sourcePath, _, _, sourceSettings := seedBootstrapRouteConfig(t)
	sourcePayload := mustReadFile(t, sourcePath)
	blockerPath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockerPath, []byte("blocked"), 0o600); err != nil {
		t.Fatalf("create write failure blocker: %v", err)
	}
	targetPath := filepath.Join(blockerPath, "bootstrap-config.json")
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{
		ReadFile: func(string) ([]byte, error) {
			return os.ReadFile(sourcePath)
		},
		TimeNow: func() time.Time { return routeTestUpdatedAt },
	})
	snapshot, settings, err := manager.LoadBootstrapConfigDocument(targetPath)
	if err != nil {
		t.Fatalf("load synthetic write failure bootstrap snapshot: %v", err)
	}
	fixture := newBootstrapRouteFixtureFromSnapshot(t, targetPath, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(2400)

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusInternalServerError)
	assertNoRouteSecrets(t, response.Body.Bytes(), sourceSettings)
	if detail := decodeErrorDetailString(t, response); detail != "Failed to write bootstrap config" {
		t.Fatal("expected file write failure response detail")
	}
	assertFileUnchanged(t, sourcePath, sourcePayload)
}

type routeFixture struct {
	path     string
	manager  config.BootstrapConfigManager
	snapshot config.BootstrapConfigSnapshot
	settings config.Settings
	router   http.Handler
}

type routeSecret struct {
	label string
	value string
}

func newBootstrapRouteFixture(t *testing.T) routeFixture {
	t.Helper()
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	return newBootstrapRouteFixtureFromSnapshot(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag)
}

func newBootstrapRouteFixtureFromSnapshot(
	t *testing.T,
	path string,
	manager config.BootstrapConfigManager,
	snapshot config.BootstrapConfigSnapshot,
	settings config.Settings,
	loadedRevision int,
	loadedDocumentETag string,
) routeFixture {
	t.Helper()
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     loadedRevision,
		LoadedDocumentETag: loadedDocumentETag,
		Manager:            manager,
		Writable:           func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("create bootstrap config route service: %v", err)
	}
	router := chi.NewRouter()
	router.Route("/api", service.MountManagementRoutes)
	return routeFixture{
		path:     path,
		manager:  manager,
		snapshot: snapshot,
		settings: settings,
		router:   router,
	}
}

func seedBootstrapRouteConfig(t *testing.T) (string, config.BootstrapConfigManager, config.BootstrapConfigSnapshot, config.Settings) {
	t.Helper()
	t.Setenv("DATABASE_URL", routeTestDatabaseURL)
	configPath := filepath.Join(t.TempDir(), "bootstrap-config.json")
	currentTime := routeTestCreatedAt
	manager := config.NewBootstrapConfigManager(config.BootstrapConfigManagerOptions{
		TimeNow: func() time.Time { return currentTime },
	})
	if _, err := manager.LoadOrSeed(configPath); err != nil {
		t.Fatalf("seed temp bootstrap config: %v", err)
	}
	currentTime = routeTestUpdatedAt
	snapshot, settings, err := manager.LoadBootstrapConfigDocument(configPath)
	if err != nil {
		t.Fatalf("load temp bootstrap config snapshot: %v", err)
	}
	return configPath, manager, snapshot, settings
}

func (fixture routeFixture) do(t *testing.T, method string, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, bytes.NewReader(body))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	return response
}

func (fixture routeFixture) doJSON(t *testing.T, method string, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	return fixture.do(t, method, path, mustMarshalBootstrapRouteJSON(t, value))
}

func mustMarshalBootstrapRouteJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal bootstrap route request: %v", err)
	}
	return payload
}

func bootstrapRouteRequestForSnapshot(t *testing.T, snapshot config.BootstrapConfigSnapshot) config.BootstrapConfigUpdateRequest {
	t.Helper()
	values := cloneBootstrapRouteValues(t, snapshot.Values)
	return config.BootstrapConfigUpdateRequest{
		ExpectedRevision: snapshot.FileRevision,
		ExpectedETag:     snapshot.DocumentETag,
		Values:           &values,
		SecretUpdates:    preserveBootstrapRouteSecrets(),
	}
}

func bootstrapRouteRequestPayloadWithoutFields(t *testing.T, snapshot config.BootstrapConfigSnapshot, fields ...string) []byte {
	t.Helper()
	payload := mustMarshalBootstrapRouteJSON(t, bootstrapRouteRequestForSnapshot(t, snapshot))
	var request map[string]any
	if err := json.Unmarshal(payload, &request); err != nil {
		t.Fatalf("unmarshal bootstrap route request map: %v", err)
	}
	for _, field := range fields {
		delete(request, field)
	}
	return mustMarshalBootstrapRouteJSON(t, request)
}

func preserveBootstrapRouteSecrets() map[string]config.BootstrapConfigSecretUpdate {
	return map[string]config.BootstrapConfigSecretUpdate{
		config.BootstrapConfigSecretDatabaseURL:                {Action: config.BootstrapConfigSecretActionPreserve},
		config.BootstrapConfigSecretRuntimeSecretEncryptionKey: {Action: config.BootstrapConfigSecretActionPreserve},
		config.BootstrapConfigSecretAuthJWTSigningKey:          {Action: config.BootstrapConfigSecretActionPreserve},
		config.BootstrapConfigSecretStateTransferBundleKey:     {Action: config.BootstrapConfigSecretActionPreserve},
	}
}

func cloneBootstrapRouteValues(t *testing.T, values config.BootstrapConfigValues) config.BootstrapConfigValues {
	t.Helper()
	payload, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal bootstrap route values clone: %v", err)
	}
	var clone config.BootstrapConfigValues
	if err := json.Unmarshal(payload, &clone); err != nil {
		t.Fatalf("unmarshal bootstrap route values clone: %v", err)
	}
	return clone
}

func decodeBootstrapConfigResponse(t *testing.T, response *httptest.ResponseRecorder) config.BootstrapConfigResponse {
	t.Helper()
	var body config.BootstrapConfigResponse
	decodeJSONResponse(t, response, &body)
	return body
}

func decodeErrorDetailMap(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	decodeJSONResponse(t, response, &body)
	detail, ok := body["detail"].(map[string]any)
	if !ok {
		t.Fatal("expected error response detail object")
	}
	return detail
}

func decodeErrorDetailString(t *testing.T, response *httptest.ResponseRecorder) string {
	t.Helper()
	var body map[string]any
	decodeJSONResponse(t, response, &body)
	detail, ok := body["detail"].(string)
	if !ok {
		t.Fatal("expected error response detail string")
	}
	return detail
}

func decodeJSONResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(response.Body.Bytes()))
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode JSON response: %v", err)
	}
}

func requireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected HTTP status %d, got %d", status, response.Code)
	}
}

func assertNoRouteSecrets(t *testing.T, body []byte, settings config.Settings, extraSecrets ...routeSecret) {
	t.Helper()
	secrets := []routeSecret{
		{label: "database password", value: routeTestDatabasePassword},
		{label: "database query password", value: routeTestDatabaseQueryPassword},
		{label: "database SSL password", value: routeTestDatabaseSSLPassword},
		{label: "runtime secret", value: settings.SecretEncryptionKey},
		{label: "auth JWT secret", value: settings.AuthJWTSecret},
		{label: "bundle secret", value: settings.ConfigBundleEncryptionKey},
	}
	secrets = append(secrets, extraSecrets...)
	for _, secret := range secrets {
		assertNoRawSecret(t, body, secret)
	}
}

func assertNoRawSecret(t *testing.T, body []byte, secret routeSecret) {
	t.Helper()
	if secret.value == "" {
		return
	}
	if bytes.Contains(body, []byte(secret.value)) {
		t.Fatalf("response exposed %s", secret.label)
	}
}

func assertFileUnchanged(t *testing.T, path string, before []byte) {
	t.Helper()
	after := mustReadFile(t, path)
	if !bytes.Equal(before, after) {
		t.Fatal("expected bootstrap config file to remain unchanged")
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp bootstrap config: %v", err)
	}
	return payload
}

func containsStringValue(values any, expected string) bool {
	items, ok := values.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		if item == expected {
			return true
		}
	}
	return false
}

func routeIntPtr(value int) *int {
	return &value
}

func routeStringPtr(value string) *string {
	return &value
}
