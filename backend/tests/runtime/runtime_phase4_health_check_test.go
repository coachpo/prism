package runtime_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	managementconnections "github.com/coachpo/prism/backend/internal/httpapi/management/connections"
	managementprofiles "github.com/coachpo/prism/backend/internal/httpapi/management/profiles"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type phase4HealthCheckHarness struct {
	base   *runtimeHarness
	client *http.Client
	url    string
}

type phase4HealthCheckHarnessOptions struct {
	UpstreamClient *http.Client
	Now            func() time.Time
	MutatePool     func(*pgxpool.Config)
}

type phase4ConnectionHealthSnapshot struct {
	HealthStatus    string
	HealthDetail    *string
	LastHealthCheck *time.Time
	UpdatedAt       time.Time
}

func TestConnectionHealthCheckDoesNotHoldTransactionDuringProbe(t *testing.T) {
	blockingUpstream := newBlockingScriptedUpstream(t, 1, http.StatusOK, map[string]any{"ok": true})
	harness := newPhase4HealthCheckHarness(t, phase4HealthCheckHarnessOptions{
		UpstreamClient: blockingUpstream.server.Client(),
		MutatePool: func(cfg *pgxpool.Config) {
			cfg.MaxConns = 1
			cfg.MinIdleConns = 0
		},
	})
	profileID := harness.base.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.base.seedLegacyStrategy(t, profileID, "phase4-short-tx-"+suffix, "round-robin")
	modelConfigID := harness.base.seedModel(t, profileID, "openai", "phase4-short-tx-model-"+suffix, "native", &strategyID)
	endpointID := harness.base.seedEndpoint(t, profileID, "phase4-short-tx-endpoint-"+suffix, blockingUpstream.baseURL("/phase4/short-tx"), "phase4-short-tx-key", 0)
	connectionID := harness.base.seedConnection(t, profileID, modelConfigID, endpointID, "phase4-short-tx-connection-"+suffix, nil, nil, 0)

	resultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+fmt.Sprintf("/api/models/%d/connections/%d/health", modelConfigID, connectionID), nil, runtimeModelHeader(profileID))
	blockingUpstream.waitUntilReady(t, 5*time.Second)

	activeProfileResponse := performPriorityRequest(t, harness.client, time.Second, http.MethodGet, harness.url+"/api/profiles/active", nil, nil)
	assertStatus(t, activeProfileResponse, http.StatusOK)
	var activeProfile map[string]any
	decodeJSONResponse(t, activeProfileResponse, &activeProfile)
	if got := jsonInt(t, activeProfile["id"]); got != profileID {
		t.Fatalf("expected concurrent management request to resolve active profile %d, got %+v", profileID, activeProfile)
	}

	blockingUpstream.releaseRequests()
	result := awaitConcurrentRuntimeResult(t, resultCh, 5*time.Second)
	if result.Err != nil {
		t.Fatalf("expected persisted health check to complete after upstream release: %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected persisted health check status 200, got %d with body %s", result.StatusCode, result.Body)
	}
}

func TestConnectionHealthCheckPreviewDoesNotPersist(t *testing.T) {
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"ok": true})
	harness := newPhase4HealthCheckHarness(t, phase4HealthCheckHarnessOptions{UpstreamClient: upstream.server.Client()})
	profileID := harness.base.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.base.seedLegacyStrategy(t, profileID, "phase4-preview-"+suffix, "round-robin")
	modelConfigID := harness.base.seedModel(t, profileID, "openai", "phase4-preview-model-"+suffix, "native", &strategyID)
	existingEndpointID := harness.base.seedEndpoint(t, profileID, "phase4-preview-existing-endpoint-"+suffix, "https://phase4-preview-existing.invalid", "phase4-preview-existing-key", 0)
	connectionID := harness.base.seedConnection(t, profileID, modelConfigID, existingEndpointID, "phase4-preview-existing-connection-"+suffix, nil, nil, 0)
	oldCheckedAt := time.Date(2026, time.April, 26, 9, 30, 0, 0, time.UTC)
	updatePhase4ConnectionHealthSnapshot(t, harness.base.conn, connectionID, "healthy", phase4StringPtr("existing preview health state"), &oldCheckedAt)
	endpointsBefore := countPhase4ProfileEndpoints(t, harness.base.conn, profileID)

	legacyPreviewResponse := performPriorityRequest(
		t,
		harness.client,
		5*time.Second,
		http.MethodPost,
		harness.url+fmt.Sprintf("/api/models/%d/connections/health-check-preview", modelConfigID),
		map[string]any{
			"endpoint_create": map[string]any{"name": "Phase 4 Preview Inline Endpoint", "base_url": upstream.server.URL, "api_key": "phase4-preview-inline-key"},
			"custom_headers":  map[string]string{"X-Allow-Preview": "preview-ok"},
		},
		runtimeModelHeader(profileID),
	)
	assertStatus(t, legacyPreviewResponse, http.StatusNotFound)
	if got := len(upstream.requestsSnapshot()); got != 0 {
		t.Fatalf("expected removed preview route to avoid upstream probes, got %d requests", got)
	}

	endpointsAfter := countPhase4ProfileEndpoints(t, harness.base.conn, profileID)
	if endpointsAfter != endpointsBefore {
		t.Fatalf("expected removed preview route to avoid persisting inline endpoints, got before=%d after=%d", endpointsBefore, endpointsAfter)
	}
	snapshot := loadPhase4ConnectionHealthSnapshot(t, harness.base.conn, connectionID)
	if snapshot.HealthStatus != "healthy" || snapshot.HealthDetail == nil || *snapshot.HealthDetail != "existing preview health state" {
		t.Fatalf("expected removed preview route to leave persisted health state untouched, got %+v", snapshot)
	}
	if snapshot.LastHealthCheck == nil || !snapshot.LastHealthCheck.Equal(oldCheckedAt) {
		t.Fatalf("expected removed preview route to preserve last_health_check %s, got %+v", oldCheckedAt, snapshot)
	}
}

func TestConnectionHealthCheckWritebackSkipsOnVersionChange(t *testing.T) {
	blockingUpstream := newBlockingScriptedUpstream(t, 1, http.StatusUnauthorized, map[string]any{"error": map[string]any{"message": "stale probe auth failure"}})
	harness := newPhase4HealthCheckHarness(t, phase4HealthCheckHarnessOptions{UpstreamClient: blockingUpstream.server.Client()})
	profileID := harness.base.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.base.seedLegacyStrategy(t, profileID, "phase4-writeback-"+suffix, "round-robin")
	modelConfigID := harness.base.seedModel(t, profileID, "openai", "phase4-writeback-model-"+suffix, "native", &strategyID)
	endpointID := harness.base.seedEndpoint(t, profileID, "phase4-writeback-endpoint-"+suffix, blockingUpstream.baseURL("/phase4/writeback"), "phase4-writeback-key", 0)
	connectionID := harness.base.seedConnection(t, profileID, modelConfigID, endpointID, "phase4-writeback-connection-"+suffix, nil, nil, 0)

	resultCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+fmt.Sprintf("/api/models/%d/connections/%d/health", modelConfigID, connectionID), nil, runtimeModelHeader(profileID))
	blockingUpstream.waitUntilReady(t, 5*time.Second)
	newerCheckedAt := time.Date(2026, time.April, 26, 15, 4, 5, 0, time.UTC)
	updatePhase4ConnectionHealthSnapshot(t, harness.base.conn, connectionID, "healthy", phase4StringPtr("newer persisted state"), &newerCheckedAt)
	blockingUpstream.releaseRequests()

	result := awaitConcurrentRuntimeResult(t, resultCh, 5*time.Second)
	if result.Err != nil {
		t.Fatalf("expected persisted health check to finish after version change: %v", result.Err)
	}
	if result.StatusCode != http.StatusOK {
		t.Fatalf("expected persisted health check status 200, got %d with body %s", result.StatusCode, result.Body)
	}
	snapshot := loadPhase4ConnectionHealthSnapshot(t, harness.base.conn, connectionID)
	if snapshot.HealthStatus != "healthy" || snapshot.HealthDetail == nil || *snapshot.HealthDetail != "newer persisted state" {
		t.Fatalf("expected optimistic writeback to preserve newer persisted state, got %+v", snapshot)
	}
	if snapshot.LastHealthCheck == nil || !snapshot.LastHealthCheck.Equal(newerCheckedAt) {
		t.Fatalf("expected optimistic writeback skip to preserve last_health_check %s, got %+v", newerCheckedAt, snapshot)
	}
	if got := len(blockingUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected stale persisted probe to perform one upstream request, got %d", got)
	}
}

func TestConnectionHealthCheckDuplicateProbeSuppression(t *testing.T) {
	blockingUpstream := newBlockingScriptedUpstream(t, 1, http.StatusOK, map[string]any{"ok": true})
	harness := newPhase4HealthCheckHarness(t, phase4HealthCheckHarnessOptions{UpstreamClient: blockingUpstream.server.Client()})
	profileID := harness.base.activeProfileID(t)
	suffix := randomSuffix()
	strategyID := harness.base.seedLegacyStrategy(t, profileID, "phase4-duplicate-"+suffix, "round-robin")
	modelConfigID := harness.base.seedModel(t, profileID, "openai", "phase4-duplicate-model-"+suffix, "native", &strategyID)
	endpointID := harness.base.seedEndpoint(t, profileID, "phase4-duplicate-endpoint-"+suffix, blockingUpstream.baseURL("/phase4/duplicate"), "phase4-duplicate-key", 0)
	connectionID := harness.base.seedConnection(t, profileID, modelConfigID, endpointID, "phase4-duplicate-connection-"+suffix, nil, nil, 0)

	resultOneCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+fmt.Sprintf("/api/models/%d/connections/%d/health", modelConfigID, connectionID), nil, runtimeModelHeader(profileID))
	blockingUpstream.waitUntilReady(t, 5*time.Second)
	resultTwoCh := startAsyncPriorityRequest(t, harness.client, http.MethodPost, harness.url+fmt.Sprintf("/api/models/%d/connections/%d/health", modelConfigID, connectionID), nil, runtimeModelHeader(profileID))
	time.Sleep(100 * time.Millisecond)
	if got := len(blockingUpstream.requestsSnapshot()); got != 1 {
		t.Fatalf("expected duplicate persisted probe suppression to keep one inflight upstream request, got %d", got)
	}
	blockingUpstream.releaseRequests()

	resultOne := awaitConcurrentRuntimeResult(t, resultOneCh, 5*time.Second)
	if resultOne.Err != nil {
		t.Fatalf("expected first persisted health check to succeed: %v", resultOne.Err)
	}
	if resultOne.StatusCode != http.StatusOK {
		t.Fatalf("expected first persisted health check status 200, got %d with body %s", resultOne.StatusCode, resultOne.Body)
	}
	resultTwo := awaitConcurrentRuntimeResult(t, resultTwoCh, 5*time.Second)
	if resultTwo.Err != nil {
		t.Fatalf("expected duplicate persisted health check to share result: %v", resultTwo.Err)
	}
	if resultTwo.StatusCode != http.StatusOK {
		t.Fatalf("expected duplicate persisted health check status 200, got %d with body %s", resultTwo.StatusCode, resultTwo.Body)
	}
	if got := len(blockingUpstream.requestsSnapshot()); got != 2 {
		t.Fatalf("expected duplicate persisted probes to share one two-step upstream check, got %d requests", got)
	}
}

func newPhase4HealthCheckHarness(t *testing.T, options phase4HealthCheckHarnessOptions) *phase4HealthCheckHarness {
	t.Helper()
	base := newRuntimeHarness(t)
	settings := phase4HealthCheckSettings(base.databaseName)
	parsedConfig, err := pgxpool.ParseConfig(settings.DatabaseURL)
	if err != nil {
		t.Fatalf("parse phase-4 health check pool config: %v", err)
	}
	if options.MutatePool != nil {
		options.MutatePool(parsedConfig)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), parsedConfig)
	if err != nil {
		t.Fatalf("create phase-4 health check pool: %v", err)
	}
	t.Cleanup(pool.Close)
	profilesService, err := managementprofiles.NewService(settings, managementprofiles.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build phase-4 profiles service: %v", err)
	}
	t.Cleanup(profilesService.Close)
	connectionOptions := managementconnections.Options{Pool: pool}
	if options.Now != nil {
		connectionOptions.Now = options.Now
	}
	if options.UpstreamClient != nil {
		connectionOptions.HTTPClient = options.UpstreamClient
	}
	connectionsService, err := managementconnections.NewService(settings, connectionOptions)
	if err != nil {
		t.Fatalf("build phase-4 connections service: %v", err)
	}
	t.Cleanup(connectionsService.Close)
	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{
		Version:            "runtime-phase4-health-test",
		ProfilesService:    profilesService,
		ConnectionsService: connectionsService,
	})
	if err != nil {
		t.Fatalf("build phase-4 health check handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &phase4HealthCheckHarness{base: base, client: server.Client(), url: server.URL}
}

func phase4HealthCheckSettings(databaseName string) config.Settings {
	return config.Settings{
		Host:                "127.0.0.1",
		Port:                8000,
		AppEnv:              config.EnvironmentProduction,
		DatabaseURL:         sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey: "runtime-secret",
		CORSAllowedOrigins:  "http://localhost:5173,http://127.0.0.1:5173",
	}
}

func awaitConcurrentRuntimeResult(t *testing.T, resultCh <-chan concurrentRuntimeRequestResult, timeout time.Duration) concurrentRuntimeRequestResult {
	t.Helper()
	select {
	case result := <-resultCh:
		return result
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for concurrent runtime result after %s", timeout)
		return concurrentRuntimeRequestResult{}
	}
}

func countPhase4ProfileEndpoints(t *testing.T, conn *pgx.Conn, profileID int) int {
	t.Helper()
	var count int
	if err := conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM endpoints WHERE profile_id = $1`, profileID).Scan(&count); err != nil {
		t.Fatalf("count phase-4 endpoints for profile %d: %v", profileID, err)
	}
	return count
}

func loadPhase4ConnectionHealthSnapshot(t *testing.T, conn *pgx.Conn, connectionID int) phase4ConnectionHealthSnapshot {
	t.Helper()
	var detail sql.NullString
	var checkedAt sql.NullTime
	snapshot := phase4ConnectionHealthSnapshot{}
	if err := conn.QueryRow(
		context.Background(),
		`SELECT health_status, health_detail, last_health_check, updated_at FROM connections WHERE id = $1`,
		connectionID,
	).Scan(&snapshot.HealthStatus, &detail, &checkedAt, &snapshot.UpdatedAt); err != nil {
		t.Fatalf("load phase-4 connection health snapshot for %d: %v", connectionID, err)
	}
	snapshot.HealthDetail = phase4NullableStringValue(detail)
	snapshot.LastHealthCheck = phase4NullableTime(checkedAt)
	snapshot.UpdatedAt = snapshot.UpdatedAt.UTC()
	return snapshot
}

func updatePhase4ConnectionHealthSnapshot(t *testing.T, conn *pgx.Conn, connectionID int, healthStatus string, healthDetail *string, lastHealthCheck *time.Time) {
	t.Helper()
	var detailValue any
	if healthDetail != nil {
		detailValue = *healthDetail
	}
	var checkedAtValue any
	if lastHealthCheck != nil {
		checkedAtValue = *lastHealthCheck
	}
	if _, err := conn.Exec(
		context.Background(),
		`UPDATE connections SET health_status = $2, health_detail = $3, last_health_check = $4, updated_at = $5 WHERE id = $1`,
		connectionID,
		healthStatus,
		detailValue,
		checkedAtValue,
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("update phase-4 connection %d health snapshot: %v", connectionID, err)
	}
}

func phase4StringPtr(value string) *string {
	resolved := value
	return &resolved
}

func phase4NullableStringValue(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	resolved := value.String
	return &resolved
}

func phase4NullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	resolved := value.Time.UTC()
	return &resolved
}
