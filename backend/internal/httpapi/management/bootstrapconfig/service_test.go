package bootstrapconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
)

const (
	routeTestDatabasePassword                      = "route-db-password"
	routeTestDatabaseQueryPassword                 = "route-query-password"
	routeTestDatabaseSSLPassword                   = "route-ssl-password"
	routeTestDatabaseURL                           = "postgres://prism:" + routeTestDatabasePassword + "@db.route.internal:5432/prism?sslmode=disable&password=" + routeTestDatabaseQueryPassword + "&sslpassword=" + routeTestDatabaseSSLPassword
	routeTestNextDatabasePassword                  = "route-next-db-password"
	routeTestNextDatabaseURL                       = "postgres://prism:" + routeTestNextDatabasePassword + "@db.next.internal:5432/prism?sslmode=disable"
	routeTestReplacementJWTSecret                  = "route-replacement-jwt-secret"
	routeTestRuntimeReplacementSecret              = "route-runtime-replacement-secret"
	routeTestRuntimeSideEffectsAttemptTimeoutField = "runtime.side_effects.attempt_timeout"
)

var (
	routeTestCreatedAt = time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	routeTestUpdatedAt = time.Date(2026, 4, 28, 11, 0, 0, 0, time.UTC)
	errFakeHotApply    = errors.New("fake hot apply failure")
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
	if len(body.ApplyCapabilities) != len(config.BootstrapConfigApplyCapabilities()) {
		t.Fatal("expected response to include apply capabilities")
	}
	if body.PlannedChanges != nil || body.ApplyResult != nil {
		t.Fatal("expected GET response to omit planned changes and apply result")
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
	if body.RestartRequired {
		t.Fatal("expected hot-only validation response not to require restart")
	}
	if body.PlannedChanges == nil || body.PlannedChanges.RestartRequired || len(body.PlannedChanges.ChangedFields) != 1 {
		t.Fatalf("expected hot-only planned changes, got %+v", body.PlannedChanges)
	}
	if body.PlannedChanges.ChangedFields[0].Field != "auth.access_token_ttl_seconds" || body.PlannedChanges.ChangedFields[0].Mode != config.BootstrapConfigApplyModeHotApply {
		t.Fatalf("expected planned auth TTL hot-apply change, got %+v", body.PlannedChanges.ChangedFields)
	}
	if body.ApplyResult != nil {
		t.Fatal("expected validation response to omit apply result")
	}
}

func TestBootstrapConfigRouteValidateReportsRestartOnlyPlannedChanges(t *testing.T) {
	fixture := newBootstrapRouteFixture(t)
	before := mustReadFile(t, fixture.path)
	request := bootstrapRouteRequestForSnapshot(t, fixture.snapshot)
	request.Values.Server.Port = routeIntPtr(*fixture.snapshot.Values.Server.Port + 1)
	request.Confirmations = []string{config.BootstrapConfigConfirmationServerPortChange}

	response := fixture.doJSON(t, http.MethodPost, "/api/config/bootstrap/validate", request)

	requireStatus(t, response, http.StatusOK)
	assertFileUnchanged(t, fixture.path, before)
	body := decodeBootstrapConfigResponse(t, response)
	if !body.RestartRequired || body.PlannedChanges == nil || !body.PlannedChanges.RestartRequired {
		t.Fatalf("expected restart-only validation planning, got restart=%v planned=%+v", body.RestartRequired, body.PlannedChanges)
	}
	assertFieldChangesEqual(t, body.PlannedChanges.ChangedFields, []config.BootstrapConfigFieldChange{{Field: "server.port", Mode: config.BootstrapConfigApplyModeRestartRequired}})
}

func TestBootstrapConfigRouteValidateReportsRuntimeSideEffectsAttemptTimeoutRestartRequired(t *testing.T) {
	fixture := newBootstrapRouteFixture(t)
	before := mustReadFile(t, fixture.path)
	request := bootstrapRouteRequestForSnapshot(t, fixture.snapshot)
	request.Values.Runtime.SideEffects.AttemptTimeout = routeStringPtr("15s")

	response := fixture.doJSON(t, http.MethodPost, "/api/config/bootstrap/validate", request)

	requireStatus(t, response, http.StatusOK)
	assertFileUnchanged(t, fixture.path, before)
	body := decodeBootstrapConfigResponse(t, response)
	capability, ok := body.ApplyCapabilities[routeTestRuntimeSideEffectsAttemptTimeoutField]
	if !ok {
		t.Fatal("expected response capabilities to include runtime side-effects attempt timeout")
	}
	if capability.Mode != config.BootstrapConfigApplyModeRestartRequired || capability.ConfirmationToken != "" {
		t.Fatalf("expected side-effects attempt timeout to be restart-required without confirmation, got %+v", capability)
	}
	if !body.RestartRequired || body.PlannedChanges == nil || !body.PlannedChanges.RestartRequired {
		t.Fatalf("expected side-effects attempt timeout validation to require restart, got restart=%v planned=%+v", body.RestartRequired, body.PlannedChanges)
	}
	assertFieldChangesEqual(t, body.PlannedChanges.ChangedFields, []config.BootstrapConfigFieldChange{{Field: routeTestRuntimeSideEffectsAttemptTimeoutField, Mode: config.BootstrapConfigApplyModeRestartRequired}})
	if body.ApplyResult != nil {
		t.Fatal("expected validation response to omit apply result")
	}
}

func TestBootstrapConfigRouteValidateDoesNotTouchHotApplyRuntime(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := &fakeBootstrapHotApplyRuntime{}
	fixture := newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag, hotRuntime)
	before := mustReadFile(t, path)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(2400)

	response := fixture.doJSON(t, http.MethodPost, "/api/config/bootstrap/validate", request)

	requireStatus(t, response, http.StatusOK)
	assertFileUnchanged(t, path, before)
	if hotRuntime.validateCalls != 0 || hotRuntime.publishCalls != 0 {
		t.Fatalf("expected validate route not to touch hot runtime, got validate=%d publish=%d", hotRuntime.validateCalls, hotRuntime.publishCalls)
	}
	body := decodeBootstrapConfigResponse(t, response)
	if body.PlannedChanges == nil || body.PlannedChanges.RestartRequired {
		t.Fatalf("expected hot-only planned changes without restart, got %+v", body.PlannedChanges)
	}
	assertFieldChangesEqual(t, body.PlannedChanges.ChangedFields, []config.BootstrapConfigFieldChange{{Field: "auth.access_token_ttl_seconds", Mode: config.BootstrapConfigApplyModeHotApply}})
	if body.ApplyResult != nil {
		t.Fatal("expected validation response to omit apply result")
	}
}

func TestBootstrapConfigRoutePutUnchangedReturnsEmptyApplyResult(t *testing.T) {
	fixture := newBootstrapRouteFixture(t)
	before := mustReadFile(t, fixture.path)
	request := bootstrapRouteRequestForSnapshot(t, fixture.snapshot)

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusOK)
	assertFileUnchanged(t, fixture.path, before)
	body := decodeBootstrapConfigResponse(t, response)
	if body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected unchanged PUT apply result without restart, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	if len(body.ApplyResult.PendingHotApplyFields) != 0 || len(body.ApplyResult.RestartRequiredFields) != 0 || len(body.ApplyResult.AppliedNowFields) != 0 {
		t.Fatalf("expected empty changed apply result fields, got %+v", body.ApplyResult)
	}
	if len(body.ApplyResult.UnchangedFields) != len(config.BootstrapConfigApplyCapabilityFields()) {
		t.Fatalf("expected unchanged fields to include all capabilities, got %d", len(body.ApplyResult.UnchangedFields))
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
		t.Fatal("expected write response to require restart for restart-required fields")
	}
	if body.ApplyResult == nil {
		t.Fatal("expected PUT response to include apply result")
	}
	assertStringSetEqual(t, body.ApplyResult.PendingHotApplyFields, []string{"auth.access_token_ttl_seconds"})
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{config.BootstrapConfigSecretDatabaseURL, config.BootstrapConfigSecretAuthJWTSigningKey})
	if len(body.ApplyResult.AppliedNowFields) != 0 || len(body.ApplyResult.FailedHotApplyFields) != 0 {
		t.Fatalf("expected no immediate or failed hot-apply fields, got %+v", body.ApplyResult)
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

func TestBootstrapConfigRoutePutRestartOnlyDoesNotPublishHotRuntime(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := &fakeBootstrapHotApplyRuntime{}
	fixture := newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag, hotRuntime)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Server.Port = routeIntPtr(*snapshot.Values.Server.Port + 1)
	request.Confirmations = []string{config.BootstrapConfigConfirmationServerPortChange}

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusOK)
	if hotRuntime.validateCalls != 0 || hotRuntime.publishCalls != 0 {
		t.Fatalf("expected restart-only PUT not to touch hot runtime, got validate=%d publish=%d", hotRuntime.validateCalls, hotRuntime.publishCalls)
	}
	body := decodeBootstrapConfigResponse(t, response)
	if !body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected restart-only apply result, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.AppliedNowFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{"server.port"})
	assertStringSetEqual(t, body.ApplyResult.PendingHotApplyFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.FailedHotApplyFields, []string{})

	getResponse := fixture.do(t, http.MethodGet, "/api/config/bootstrap", nil)
	requireStatus(t, getResponse, http.StatusOK)
	getBody := decodeBootstrapConfigResponse(t, getResponse)
	if !getBody.RestartRequired || getBody.ApplyResult == nil {
		t.Fatalf("expected GET to report restart-only drift, got restart=%v result=%+v", getBody.RestartRequired, getBody.ApplyResult)
	}
	assertStringSetEqual(t, getBody.ApplyResult.RestartRequiredFields, []string{"server.port"})
	assertStringSetEqual(t, getBody.ApplyResult.PendingHotApplyFields, []string{})
	assertStringSetEqual(t, getBody.ApplyResult.FailedHotApplyFields, []string{})
}

func TestBootstrapConfigRoutePutPublishesHotApplyRuntimeAfterWrite(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := &fakeBootstrapHotApplyRuntime{}
	hotRuntime.publishHook = func(config.Settings) {
		_, writtenSettings, err := manager.LoadBootstrapConfigDocument(path)
		if err != nil {
			t.Fatalf("load bootstrap config during hot publish: %v", err)
		}
		if writtenSettings.AuthAccessTokenTTLSeconds != 2400 {
			t.Fatalf("expected canonical file write before publish, got auth TTL %d", writtenSettings.AuthAccessTokenTTLSeconds)
		}
	}
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     snapshot.FileRevision,
		LoadedDocumentETag: snapshot.DocumentETag,
		HotApplyRuntime:    hotRuntime,
		Manager:            manager,
		Writable:           func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("create bootstrap config route service: %v", err)
	}
	router := chi.NewRouter()
	router.Route("/api", service.MountManagementRoutes)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(2400)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config/bootstrap", bytes.NewReader(mustMarshalBootstrapRouteJSON(t, request))))

	requireStatus(t, response, http.StatusOK)
	if hotRuntime.validateCalls != 1 || hotRuntime.publishCalls != 1 || hotRuntime.retired.closeCalls != 1 {
		t.Fatalf("expected validate/publish/retire once, got validate=%d publish=%d close=%d", hotRuntime.validateCalls, hotRuntime.publishCalls, hotRuntime.retired.closeCalls)
	}
	if hotRuntime.published.AuthAccessTokenTTLSeconds != 2400 {
		t.Fatalf("expected published hot auth TTL, got %d", hotRuntime.published.AuthAccessTokenTTLSeconds)
	}
	body := decodeBootstrapConfigResponse(t, response)
	if body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected hot-only successful apply without restart, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.AppliedNowFields, []string{"auth.access_token_ttl_seconds"})
	assertStringSetEqual(t, body.ApplyResult.PendingHotApplyFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.FailedHotApplyFields, []string{})
}

func TestBootstrapConfigRoutePutValidatesHotApplyBeforeWrite(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	before := mustReadFile(t, path)
	hotRuntime := &fakeBootstrapHotApplyRuntime{validateErr: errFakeHotApply}
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     snapshot.FileRevision,
		LoadedDocumentETag: snapshot.DocumentETag,
		HotApplyRuntime:    hotRuntime,
		Manager:            manager,
		Writable:           func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("create bootstrap config route service: %v", err)
	}
	router := chi.NewRouter()
	router.Route("/api", service.MountManagementRoutes)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(2400)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config/bootstrap", bytes.NewReader(mustMarshalBootstrapRouteJSON(t, request))))

	requireStatus(t, response, http.StatusBadRequest)
	assertFileUnchanged(t, path, before)
	if hotRuntime.validateCalls != 1 || hotRuntime.publishCalls != 0 {
		t.Fatalf("expected validate only, got validate=%d publish=%d", hotRuntime.validateCalls, hotRuntime.publishCalls)
	}
}

func TestBootstrapConfigRoutePutPublishFailureReturnsApplyResultAndPendingRetry(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := &fakeBootstrapHotApplyRuntime{publishErr: errFakeHotApply}
	fixture := newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag, hotRuntime)
	before := mustReadFile(t, path)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(2400)

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusInternalServerError)
	assertNoRouteSecrets(t, response.Body.Bytes(), settings)
	if hotRuntime.validateCalls != 1 || hotRuntime.publishCalls != 1 || hotRuntime.retired.closeCalls != 0 {
		t.Fatalf("expected validate and failed publish only, got validate=%d publish=%d close=%d", hotRuntime.validateCalls, hotRuntime.publishCalls, hotRuntime.retired.closeCalls)
	}
	body := decodeBootstrapConfigResponse(t, response)
	if body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected structured hot-apply failure without restart, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.AppliedNowFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.PendingHotApplyFields, []string{"auth.access_token_ttl_seconds"})
	assertStringSetEqual(t, body.ApplyResult.FailedHotApplyFields, []string{"auth.access_token_ttl_seconds"})
	detail := decodeErrorDetailMap(t, response)
	if detail["message"] != "Failed to apply bootstrap config" || !containsStringValue(detail["failed_hot_apply_fields"], "auth.access_token_ttl_seconds") {
		t.Fatalf("expected structured apply failure detail, got %+v", detail)
	}
	after := mustReadFile(t, path)
	if bytes.Equal(before, after) {
		t.Fatal("expected publish failure to leave the canonical file written")
	}
	writtenSnapshot, writtenSettings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load written bootstrap config after failed publish: %v", err)
	}
	if writtenSettings.AuthAccessTokenTTLSeconds != 2400 {
		t.Fatalf("expected failed publish to keep written auth TTL, got %d", writtenSettings.AuthAccessTokenTTLSeconds)
	}

	getResponse := fixture.do(t, http.MethodGet, "/api/config/bootstrap", nil)
	requireStatus(t, getResponse, http.StatusOK)
	getBody := decodeBootstrapConfigResponse(t, getResponse)
	if getBody.RestartRequired || getBody.ApplyResult == nil {
		t.Fatalf("expected GET to report pending hot apply without restart, got restart=%v result=%+v", getBody.RestartRequired, getBody.ApplyResult)
	}
	assertStringSetEqual(t, getBody.ApplyResult.PendingHotApplyFields, []string{"auth.access_token_ttl_seconds"})
	assertStringSetEqual(t, getBody.ApplyResult.FailedHotApplyFields, []string{})

	hotRuntime.publishErr = nil
	retryRequest := bootstrapRouteRequestForSnapshot(t, writtenSnapshot)
	retryResponse := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", retryRequest)

	requireStatus(t, retryResponse, http.StatusOK)
	if hotRuntime.validateCalls != 2 || hotRuntime.publishCalls != 2 || hotRuntime.retired.closeCalls != 1 {
		t.Fatalf("expected retry to validate, publish, and retire once, got validate=%d publish=%d close=%d", hotRuntime.validateCalls, hotRuntime.publishCalls, hotRuntime.retired.closeCalls)
	}
	retryBody := decodeBootstrapConfigResponse(t, retryResponse)
	if retryBody.RestartRequired || retryBody.ApplyResult == nil {
		t.Fatalf("expected retry to hot-apply without restart, got restart=%v result=%+v", retryBody.RestartRequired, retryBody.ApplyResult)
	}
	assertStringSetEqual(t, retryBody.ApplyResult.AppliedNowFields, []string{"auth.access_token_ttl_seconds"})
	assertStringSetEqual(t, retryBody.ApplyResult.PendingHotApplyFields, []string{})
	assertStringSetEqual(t, retryBody.ApplyResult.FailedHotApplyFields, []string{})

	finalGetResponse := fixture.do(t, http.MethodGet, "/api/config/bootstrap", nil)
	requireStatus(t, finalGetResponse, http.StatusOK)
	finalGetBody := decodeBootstrapConfigResponse(t, finalGetResponse)
	if finalGetBody.RestartRequired || finalGetBody.ApplyResult != nil {
		t.Fatalf("expected retry to clear pending GET effects, got restart=%v result=%+v", finalGetBody.RestartRequired, finalGetBody.ApplyResult)
	}
}

func TestBootstrapConfigRoutePutPublishesOnlyHotProjectedSettings(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := &fakeBootstrapHotApplyRuntime{}
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     snapshot.FileRevision,
		LoadedDocumentETag: snapshot.DocumentETag,
		HotApplyRuntime:    hotRuntime,
		Manager:            manager,
		Writable:           func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("create bootstrap config route service: %v", err)
	}
	router := chi.NewRouter()
	router.Route("/api", service.MountManagementRoutes)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(2400)
	request.SecretUpdates[config.BootstrapConfigSecretDatabaseURL] = config.BootstrapConfigSecretUpdate{Action: config.BootstrapConfigSecretActionReplace, Value: routeStringPtr(routeTestNextDatabaseURL)}
	request.Confirmations = []string{config.BootstrapConfigConfirmationDatabaseURLChange}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config/bootstrap", bytes.NewReader(mustMarshalBootstrapRouteJSON(t, request))))

	requireStatus(t, response, http.StatusOK)
	if hotRuntime.published.AuthAccessTokenTTLSeconds != 2400 {
		t.Fatalf("expected hot auth TTL to publish, got %d", hotRuntime.published.AuthAccessTokenTTLSeconds)
	}
	if hotRuntime.published.DatabaseURL != settings.DatabaseURL {
		t.Fatalf("expected restart-only database URL to stay live baseline, got %q", hotRuntime.published.DatabaseURL)
	}
	body := decodeBootstrapConfigResponse(t, response)
	if !body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected mixed apply result with restart required, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.AppliedNowFields, []string{"auth.access_token_ttl_seconds"})
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{config.BootstrapConfigSecretDatabaseURL})
	assertStringSetEqual(t, body.ApplyResult.PendingHotApplyFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.FailedHotApplyFields, []string{})
	_, writtenSettings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load written bootstrap config: %v", err)
	}
	if writtenSettings.DatabaseURL != routeTestNextDatabaseURL {
		t.Fatal("expected restart-only database URL to persist to file")
	}
}

func TestBootstrapConfigRoutePutHotOnlyCORSAppliesNewOriginWithoutRestart(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	settings.CORSAllowedOrigins = "https://old.example.test"
	corsProvider := newMutableRouteCORSProvider("https://old.example.test")
	hotRuntime := &fakeBootstrapHotApplyRuntime{publishHook: func(settings config.Settings) {
		corsProvider.publish(settings.CORSAllowedOriginsList()...)
	}}
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     snapshot.FileRevision,
		LoadedDocumentETag: snapshot.DocumentETag,
		CORSOriginProvider: corsProvider,
		HotApplyRuntime:    hotRuntime,
		Manager:            manager,
		Writable:           func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("create bootstrap config route service: %v", err)
	}
	router := chi.NewRouter()
	router.Route("/api", service.MountManagementRoutes)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.HTTP.CORSAllowedOrigins = &[]string{"https://new.example.test"}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config/bootstrap", bytes.NewReader(mustMarshalBootstrapRouteJSON(t, request))))

	requireStatus(t, response, http.StatusOK)
	body := decodeBootstrapConfigResponse(t, response)
	if body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected hot-only CORS PUT without restart, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.AppliedNowFields, []string{"http.cors_allowed_origins"})
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{})
	assertBootstrapErrorCORSOrigin(t, router, "https://new.example.test", "https://new.example.test")
	assertBootstrapErrorCORSOrigin(t, router, "https://old.example.test", "")
}

func TestBootstrapConfigRoutePutRestartOnlyServerPortDoesNotMutateLiveState(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := &fakeBootstrapHotApplyRuntime{}
	fixture := newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag, hotRuntime)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Server.Port = routeIntPtr(*snapshot.Values.Server.Port + 1)
	request.Confirmations = []string{config.BootstrapConfigConfirmationServerPortChange}

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusOK)
	if hotRuntime.validateCalls != 0 || hotRuntime.publishCalls != 0 {
		t.Fatalf("expected server port PUT not to touch hot runtime, got validate=%d publish=%d", hotRuntime.validateCalls, hotRuntime.publishCalls)
	}
	body := decodeBootstrapConfigResponse(t, response)
	if !body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected restart-required server port PUT, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{"server.port"})

	getResponse := fixture.do(t, http.MethodGet, "/api/config/bootstrap", nil)
	requireStatus(t, getResponse, http.StatusOK)
	getBody := decodeBootstrapConfigResponse(t, getResponse)
	if !getBody.RestartRequired || getBody.ApplyResult == nil {
		t.Fatalf("expected live baseline to keep server port pending restart, got restart=%v result=%+v", getBody.RestartRequired, getBody.ApplyResult)
	}
	assertStringSetEqual(t, getBody.ApplyResult.RestartRequiredFields, []string{"server.port"})
}

func TestBootstrapConfigRoutePutRuntimeSideEffectsAttemptTimeoutRestartOnlyDoesNotPublishHotRuntime(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := &fakeBootstrapHotApplyRuntime{}
	fixture := newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag, hotRuntime)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Runtime.SideEffects.AttemptTimeout = routeStringPtr("15s")

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusOK)
	if hotRuntime.validateCalls != 0 || hotRuntime.publishCalls != 0 {
		t.Fatalf("expected side-effects attempt timeout PUT not to touch hot runtime, got validate=%d publish=%d", hotRuntime.validateCalls, hotRuntime.publishCalls)
	}
	body := decodeBootstrapConfigResponse(t, response)
	if !body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected restart-required side-effects attempt timeout PUT, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.AppliedNowFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{routeTestRuntimeSideEffectsAttemptTimeoutField})
	assertStringSetEqual(t, body.ApplyResult.PendingHotApplyFields, []string{})
	assertStringSetEqual(t, body.ApplyResult.FailedHotApplyFields, []string{})
	_, writtenSettings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load written bootstrap config: %v", err)
	}
	if got := writtenSettings.RuntimeSideEffects().AttemptTimeout; got != 15*time.Second {
		t.Fatalf("expected written side-effects attempt timeout 15s, got %s", got)
	}

	getResponse := fixture.do(t, http.MethodGet, "/api/config/bootstrap", nil)
	requireStatus(t, getResponse, http.StatusOK)
	getBody := decodeBootstrapConfigResponse(t, getResponse)
	if !getBody.RestartRequired || getBody.ApplyResult == nil {
		t.Fatalf("expected GET to report side-effects attempt timeout pending restart, got restart=%v result=%+v", getBody.RestartRequired, getBody.ApplyResult)
	}
	assertStringSetEqual(t, getBody.ApplyResult.AppliedNowFields, []string{})
	assertStringSetEqual(t, getBody.ApplyResult.RestartRequiredFields, []string{routeTestRuntimeSideEffectsAttemptTimeoutField})
	assertStringSetEqual(t, getBody.ApplyResult.PendingHotApplyFields, []string{})
	assertStringSetEqual(t, getBody.ApplyResult.FailedHotApplyFields, []string{})
}

func TestBootstrapConfigRoutePutMixedCORSAndDatabaseURLAppliesOnlyCORS(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	settings.CORSAllowedOrigins = "https://old.example.test"
	corsProvider := newMutableRouteCORSProvider("https://old.example.test")
	hotRuntime := &fakeBootstrapHotApplyRuntime{publishHook: func(settings config.Settings) {
		corsProvider.publish(settings.CORSAllowedOriginsList()...)
	}}
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     snapshot.FileRevision,
		LoadedDocumentETag: snapshot.DocumentETag,
		CORSOriginProvider: corsProvider,
		HotApplyRuntime:    hotRuntime,
		Manager:            manager,
		Writable:           func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("create bootstrap config route service: %v", err)
	}
	router := chi.NewRouter()
	router.Route("/api", service.MountManagementRoutes)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.HTTP.CORSAllowedOrigins = &[]string{"https://new.example.test"}
	request.SecretUpdates[config.BootstrapConfigSecretDatabaseURL] = config.BootstrapConfigSecretUpdate{Action: config.BootstrapConfigSecretActionReplace, Value: routeStringPtr(routeTestNextDatabaseURL)}
	request.Confirmations = []string{config.BootstrapConfigConfirmationDatabaseURLChange}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPut, "/api/config/bootstrap", bytes.NewReader(mustMarshalBootstrapRouteJSON(t, request))))

	requireStatus(t, response, http.StatusOK)
	body := decodeBootstrapConfigResponse(t, response)
	if !body.RestartRequired || body.ApplyResult == nil {
		t.Fatalf("expected mixed CORS/database PUT to require restart, got restart=%v result=%+v", body.RestartRequired, body.ApplyResult)
	}
	assertStringSetEqual(t, body.ApplyResult.AppliedNowFields, []string{"http.cors_allowed_origins"})
	assertStringSetEqual(t, body.ApplyResult.RestartRequiredFields, []string{config.BootstrapConfigSecretDatabaseURL})
	assertStringSetEqual(t, body.ApplyResult.PendingHotApplyFields, []string{})
	assertBootstrapErrorCORSOrigin(t, router, "https://new.example.test", "https://new.example.test")
	assertBootstrapErrorCORSOrigin(t, router, "https://old.example.test", "")
	_, writtenSettings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load written bootstrap config: %v", err)
	}
	if writtenSettings.DatabaseURL != routeTestNextDatabaseURL {
		t.Fatal("expected mixed PUT to persist restart-required database URL")
	}
}

func TestBootstrapConfigRoutePutInvalidEnabledSMTPLeavesFileAndLiveStateUnchanged(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	before := mustReadFile(t, path)
	hotRuntime := &fakeBootstrapHotApplyRuntime{}
	fixture := newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag, hotRuntime)
	request := bootstrapRouteRequestForSnapshot(t, snapshot)
	request.Values.Mail = &config.BootstrapConfigMailValues{
		Enabled: routeBoolPtr(true),
		From:    routeStringPtr("Prism <noreply@example.com>"),
		SMTP: &config.BootstrapConfigMailSMTPValues{
			Host: routeStringPtr("smtp.example.com"),
			Port: routeIntPtr(587),
			Mode: routeStringPtr(string(config.MailSMTPModeStartTLSRequired)),
			Auth: routeStringPtr(string(config.MailSMTPAuthPlain)),
		},
	}

	response := fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", request)

	requireStatus(t, response, http.StatusBadRequest)
	assertFileUnchanged(t, path, before)
	if hotRuntime.validateCalls != 0 || hotRuntime.publishCalls != 0 {
		t.Fatalf("expected invalid SMTP PUT not to touch live runtime, got validate=%d publish=%d", hotRuntime.validateCalls, hotRuntime.publishCalls)
	}
}

func TestBootstrapConfigRouteConcurrentPUTsSerializeAndKeepReadableConfig(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	hotRuntime := newBlockingHotApplyRuntime()
	fixture := newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, snapshot.FileRevision, snapshot.DocumentETag, hotRuntime)
	firstRequest := bootstrapRouteRequestForSnapshot(t, snapshot)
	firstRequest.Values.Auth.AccessTokenTTLSeconds = routeIntPtr(2100)

	firstDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		firstDone <- fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", firstRequest)
	}()

	<-hotRuntime.firstPublishStarted
	writtenSnapshot, _, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load config after first serialized write: %v", err)
	}
	secondRequest := bootstrapRouteRequestForSnapshot(t, writtenSnapshot)
	secondRequest.Values.Auth.RefreshTokenTTLSeconds = routeIntPtr(2200)
	secondDone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		secondDone <- fixture.doJSON(t, http.MethodPut, "/api/config/bootstrap", secondRequest)
	}()

	close(hotRuntime.releaseFirstPublish)
	firstResponse := <-firstDone
	secondResponse := <-secondDone

	requireStatus(t, firstResponse, http.StatusOK)
	requireStatus(t, secondResponse, http.StatusOK)
	firstBody := decodeBootstrapConfigResponse(t, firstResponse)
	secondBody := decodeBootstrapConfigResponse(t, secondResponse)
	assertStringSetEqual(t, firstBody.ApplyResult.AppliedNowFields, []string{"auth.access_token_ttl_seconds"})
	assertStringSetEqual(t, secondBody.ApplyResult.AppliedNowFields, []string{"auth.refresh_token_ttl_seconds"})
	if hotRuntime.maxConcurrentPublishes != 1 {
		t.Fatalf("expected serialized hot publishes, saw %d concurrent", hotRuntime.maxConcurrentPublishes)
	}
	_, finalSettings, err := manager.LoadBootstrapConfigDocument(path)
	if err != nil {
		t.Fatalf("load final bootstrap config after concurrent PUTs: %v", err)
	}
	if finalSettings.AuthAccessTokenTTLSeconds != 2100 || finalSettings.AuthRefreshTokenTTLSeconds != 2200 {
		t.Fatalf("expected final config to include both serialized updates, got access=%d refresh=%d", finalSettings.AuthAccessTokenTTLSeconds, finalSettings.AuthRefreshTokenTTLSeconds)
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
	return newBootstrapRouteFixtureFromSnapshotWithHotRuntime(t, path, manager, snapshot, settings, loadedRevision, loadedDocumentETag, nil)
}

func newBootstrapRouteFixtureFromSnapshotWithHotRuntime(
	t *testing.T,
	path string,
	manager config.BootstrapConfigManager,
	snapshot config.BootstrapConfigSnapshot,
	settings config.Settings,
	loadedRevision int,
	loadedDocumentETag string,
	hotRuntime config.BootstrapConfigHotApplyRuntime,
) routeFixture {
	t.Helper()
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     loadedRevision,
		LoadedDocumentETag: loadedDocumentETag,
		HotApplyRuntime:    hotRuntime,
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

func assertStringSetEqual(t *testing.T, got []string, want []string) {
	t.Helper()
	sortedGot := append([]string(nil), got...)
	sortedWant := append([]string(nil), want...)
	slices.Sort(sortedGot)
	slices.Sort(sortedWant)
	if !slices.Equal(sortedGot, sortedWant) {
		t.Fatalf("unexpected string set\n got: %v\nwant: %v", got, want)
	}
}

func assertFieldChangesEqual(t *testing.T, got []config.BootstrapConfigFieldChange, want []config.BootstrapConfigFieldChange) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected field changes\n got: %+v\nwant: %+v", got, want)
	}
}

func routeIntPtr(value int) *int {
	return &value
}

func routeStringPtr(value string) *string {
	return &value
}

func routeBoolPtr(value bool) *bool {
	return &value
}

func TestBootstrapConfigRouteErrorsUsePublishedCORSProvider(t *testing.T) {
	path, manager, snapshot, settings := seedBootstrapRouteConfig(t)
	corsProvider := newMutableRouteCORSProvider("https://old.example.test")
	service, err := NewService(settings, Options{
		ConfigPath:         path,
		LoadedRevision:     snapshot.FileRevision,
		LoadedDocumentETag: snapshot.DocumentETag,
		CORSOriginProvider: corsProvider,
		Manager:            manager,
		Writable:           func(string) bool { return true },
	})
	if err != nil {
		t.Fatalf("create bootstrap config route service: %v", err)
	}
	router := chi.NewRouter()
	router.Route("/api", service.MountManagementRoutes)

	assertBootstrapErrorCORSOrigin(t, router, "https://old.example.test", "https://old.example.test")
	corsProvider.publish("https://new.example.test")

	assertBootstrapErrorCORSOrigin(t, router, "https://new.example.test", "https://new.example.test")
	assertBootstrapErrorCORSOrigin(t, router, "https://old.example.test", "")
}

func assertBootstrapErrorCORSOrigin(t *testing.T, router http.Handler, origin string, want string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/config/bootstrap/validate", bytes.NewReader([]byte(`{"expected_revision":`)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid body status 400, got %d", response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != want {
		t.Fatalf("Access-Control-Allow-Origin for %q = %q, want %q", origin, got, want)
	}
}

type fakeBootstrapHotApplyRuntime struct {
	validateCalls int
	publishCalls  int
	validated     config.Settings
	published     config.Settings
	validateErr   error
	publishErr    error
	validateHook  func(config.Settings)
	publishHook   func(config.Settings)
	retired       fakeBootstrapHotApplyRetiredResources
}

type fakeBootstrapHotApplyRetiredResources struct {
	closeCalls int
}

type blockingHotApplyRuntime struct {
	mu                     sync.Mutex
	activePublishes        int
	maxConcurrentPublishes int
	firstPublishStarted    chan struct{}
	releaseFirstPublish    chan struct{}
	firstPublishSignalOnce sync.Once
	retired                fakeBootstrapHotApplyRetiredResources
}

func newBlockingHotApplyRuntime() *blockingHotApplyRuntime {
	return &blockingHotApplyRuntime{
		firstPublishStarted: make(chan struct{}),
		releaseFirstPublish: make(chan struct{}),
	}
}

func (r *blockingHotApplyRuntime) Validate(config.Settings) error {
	return nil
}

func (r *blockingHotApplyRuntime) Publish(config.Settings) (config.BootstrapConfigHotApplyRetiredResources, error) {
	r.mu.Lock()
	r.activePublishes++
	if r.activePublishes > r.maxConcurrentPublishes {
		r.maxConcurrentPublishes = r.activePublishes
	}
	r.mu.Unlock()

	r.firstPublishSignalOnce.Do(func() {
		close(r.firstPublishStarted)
		<-r.releaseFirstPublish
	})

	r.mu.Lock()
	r.activePublishes--
	r.mu.Unlock()
	return &r.retired, nil
}

func (r *fakeBootstrapHotApplyRuntime) Validate(settings config.Settings) error {
	r.validateCalls++
	r.validated = settings
	if r.validateHook != nil {
		r.validateHook(settings)
	}
	return r.validateErr
}

func (r *fakeBootstrapHotApplyRuntime) Publish(settings config.Settings) (config.BootstrapConfigHotApplyRetiredResources, error) {
	r.publishCalls++
	r.published = settings
	if r.publishHook != nil {
		r.publishHook(settings)
	}
	if r.publishErr != nil {
		return nil, r.publishErr
	}
	return &r.retired, nil
}

func (r *fakeBootstrapHotApplyRetiredResources) CloseIdleConnections() {
	r.closeCalls++
}

type mutableRouteCORSProvider struct {
	mu       sync.RWMutex
	snapshot platformcors.Snapshot
}

func newMutableRouteCORSProvider(origins ...string) *mutableRouteCORSProvider {
	return &mutableRouteCORSProvider{snapshot: platformcors.NewSnapshot(origins)}
}

func (p *mutableRouteCORSProvider) CORSSnapshot() platformcors.Snapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.snapshot
}

func (p *mutableRouteCORSProvider) publish(origins ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.snapshot = platformcors.NewSnapshot(origins)
}
