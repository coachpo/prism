package loadbalance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	loadbalancedomain "github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

var loadbalanceRoutePostgres struct {
	once     sync.Once
	hostPort string
	err      error
}

func TestLoadbalanceRoutesCRUDDefaultsCurrentStateAndProtections(t *testing.T) {
	ctx, conn, dsn := loadbalanceRouteMigratedDatabase(t, "loadbalance_routes_contract")
	now := time.Date(2026, time.June, 7, 15, 30, 0, 0, time.UTC)
	profileID := loadbalanceRouteInsertProfile(t, ctx, conn, "Loadbalance route profile", now)
	runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
	router := loadbalanceRouteRouter(t, ctx, dsn, now, runtimeState)

	createdResponse := loadbalanceRouteRequest(t, router, http.MethodPost, "/loadbalance/strategies", profileID, map[string]any{
		"name": " Primary Strategy ", "legacy_strategy_type": "ROUND-ROBIN", "failure_status_codes": []int{503, 429}, "ban_mode": "temporary", "cycle_retry_attempt_limit": 2, "ban_cumulative_retry_attempt_threshold": 4, "ban_duration_seconds": 60,
	})
	loadbalanceRouteRequireStatus(t, createdResponse, http.StatusCreated)
	var created loadbalanceStrategyResponse
	loadbalanceRouteDecode(t, createdResponse, &created)
	if created.Name != "Primary Strategy" || created.LegacyStrategyType != "round-robin" || !reflect.DeepEqual(created.FailureStatusCodes, []int{429, 503}) {
		t.Fatalf("unexpected created strategy response: %+v", created)
	}
	if created.BanMode != "temporary" || created.RetryBaseDelayMS != defaultRetryBaseDelayMS || created.RetryBackoffMultiplier != defaultRetryBackoffMultiplier || created.BanCumulativeRetryAttemptThreshold != 4 || created.BanDurationSeconds != 60 {
		t.Fatalf("unexpected canonical Ban Policy defaults: %+v", created)
	}

	listed := loadbalanceRouteRequest(t, router, http.MethodGet, "/loadbalance/strategies", profileID, nil)
	loadbalanceRouteRequireStatus(t, listed, http.StatusOK)
	var listItems []loadbalanceStrategyResponse
	loadbalanceRouteDecode(t, listed, &listItems)
	if len(listItems) != 1 || listItems[0].ID != created.ID || listItems[0].AttachedModelCount != 0 {
		t.Fatalf("unexpected list response: %+v", listItems)
	}

	updatedResponse := loadbalanceRouteRequest(t, router, http.MethodPut, fmt.Sprintf("/loadbalance/strategies/%d", created.ID), profileID, map[string]any{"name": "Primary Strategy Updated", "legacy_strategy_type": "fill-first"})
	loadbalanceRouteRequireStatus(t, updatedResponse, http.StatusOK)
	var updated loadbalanceStrategyResponse
	loadbalanceRouteDecode(t, updatedResponse, &updated)
	if updated.LegacyStrategyType != "fill-first" || updated.BanMode != "off" || updated.BanCumulativeRetryAttemptThreshold != 0 || !reflect.DeepEqual(updated.FailureStatusCodes, defaultFailureStatusCodes) {
		t.Fatalf("unexpected updated strategy response: %+v", updated)
	}

	duplicate := loadbalanceRouteRequest(t, router, http.MethodPost, "/loadbalance/strategies", profileID, map[string]any{"name": "Primary Strategy Updated", "legacy_strategy_type": "single"})
	loadbalanceRouteRequireStatus(t, duplicate, http.StatusConflict)
	loadbalanceRouteRequireDetail(t, duplicate, "Loadbalance strategy name already exists")

	defaults := loadbalanceRouteRequest(t, router, http.MethodPost, "/loadbalance/strategies/defaults", profileID, nil)
	loadbalanceRouteRequireStatus(t, defaults, http.StatusOK)
	var defaultsBody loadbalanceStrategyDefaultsResponse
	loadbalanceRouteDecode(t, defaults, &defaultsBody)
	if defaultsBody.CreatedCount != 3 || !reflect.DeepEqual(defaultsBody.CreatedNames, []string{"Default single routing", "Default fill-first routing", "Default round-robin routing"}) || len(defaultsBody.Items) != 4 {
		t.Fatalf("unexpected defaults creation response: %+v", defaultsBody)
	}

	defaultsAgain := loadbalanceRouteRequest(t, router, http.MethodPost, "/loadbalance/strategies/defaults", profileID, nil)
	loadbalanceRouteRequireStatus(t, defaultsAgain, http.StatusOK)
	var defaultsAgainBody loadbalanceStrategyDefaultsResponse
	loadbalanceRouteDecode(t, defaultsAgain, &defaultsAgainBody)
	if defaultsAgainBody.CreatedCount != 0 || !reflect.DeepEqual(defaultsAgainBody.ExistingNames, []string{"Default single routing", "Default fill-first routing", "Default round-robin routing"}) {
		t.Fatalf("unexpected idempotent defaults response: %+v", defaultsAgainBody)
	}

	deleteCandidate := loadbalanceRouteRequest(t, router, http.MethodPost, "/loadbalance/strategies", profileID, map[string]any{"name": "Delete candidate", "legacy_strategy_type": "single"})
	loadbalanceRouteRequireStatus(t, deleteCandidate, http.StatusCreated)
	var deleteStrategy loadbalanceStrategyResponse
	loadbalanceRouteDecode(t, deleteCandidate, &deleteStrategy)
	deleted := loadbalanceRouteRequest(t, router, http.MethodDelete, fmt.Sprintf("/loadbalance/strategies/%d", deleteStrategy.ID), profileID, nil)
	loadbalanceRouteRequireStatus(t, deleted, http.StatusOK)
	var deletedBody deletedResponse
	loadbalanceRouteDecode(t, deleted, &deletedBody)
	if !deletedBody.Deleted {
		t.Fatalf("expected deleted response, got %+v", deletedBody)
	}

	modelID, connectionID := loadbalanceRouteSeedRuntimeStateModel(t, ctx, conn, profileID, updated.ID, now)
	bannedUntil := now.Add(5 * time.Minute)
	runtimeState.SeedConnectionState(profileID, modelID, connectionID, loadbalancedomain.RuntimeConnectionState{ConnectionID: connectionID, BanMode: "temporary", BannedUntilAt: &bannedUntil, CumulativeRetryAttempts: 4, CycleRetryAttempts: 2, LastRetryDelayMS: 1200}, now, now)

	currentState := loadbalanceRouteRequest(t, router, http.MethodGet, fmt.Sprintf("/loadbalance/current-state?model_config_id=%d", modelID), profileID, nil)
	loadbalanceRouteRequireStatus(t, currentState, http.StatusOK)
	var currentStateBody loadbalancedomain.CurrentStateListResponse
	loadbalanceRouteDecode(t, currentState, &currentStateBody)
	if len(currentStateBody.Items) != 1 || currentStateBody.Items[0].ConnectionID != connectionID || currentStateBody.Items[0].State != "banned" || currentStateBody.Items[0].BanMode != "temporary" {
		t.Fatalf("unexpected current-state response: %+v", currentStateBody)
	}

	reset := loadbalanceRouteRequest(t, router, http.MethodPost, fmt.Sprintf("/loadbalance/current-state/%d/reset", connectionID), profileID, nil)
	loadbalanceRouteRequireStatus(t, reset, http.StatusOK)
	var resetBody loadbalancedomain.CurrentStateResetResponse
	loadbalanceRouteDecode(t, reset, &resetBody)
	if resetBody.ConnectionID != connectionID || !resetBody.Cleared {
		t.Fatalf("unexpected current-state reset response: %+v", resetBody)
	}

	blockedDelete := loadbalanceRouteRequest(t, router, http.MethodDelete, fmt.Sprintf("/loadbalance/strategies/%d", updated.ID), profileID, nil)
	loadbalanceRouteRequireStatus(t, blockedDelete, http.StatusConflict)
	var blocked map[string]any
	loadbalanceRouteDecode(t, blockedDelete, &blocked)
	detail := blocked["detail"].(map[string]any)
	if detail["message"] != "Cannot delete loadbalance strategy that is attached to models" || int(detail["attached_model_count"].(float64)) != 1 {
		t.Fatalf("unexpected delete protection detail: %+v", detail)
	}
}

func TestLoadbalanceIncidentsRouteReturnsActiveBansAndRecentEvents(t *testing.T) {
	ctx, conn, dsn := loadbalanceRouteMigratedDatabase(t, "loadbalance_incidents_route")
	now := time.Date(2026, time.June, 7, 15, 30, 0, 0, time.UTC)
	profileID := loadbalanceRouteInsertProfile(t, ctx, conn, "Loadbalance incidents profile", now)
	runtimeState := loadbalancedomain.NewLocalRuntimeStateStore()
	router := loadbalanceRouteRouter(t, ctx, dsn, now, runtimeState)

	strategyResponse := loadbalanceRouteRequest(t, router, http.MethodPost, "/loadbalance/strategies", profileID, map[string]any{
		"name": "Incident Strategy", "legacy_strategy_type": "round-robin", "failure_status_codes": []int{503}, "ban_mode": "temporary", "cycle_retry_attempt_limit": 2, "ban_cumulative_retry_attempt_threshold": 2, "ban_duration_seconds": 60,
	})
	loadbalanceRouteRequireStatus(t, strategyResponse, http.StatusCreated)
	var strategy loadbalanceStrategyResponse
	loadbalanceRouteDecode(t, strategyResponse, &strategy)
	modelID, connectionID := loadbalanceRouteSeedRuntimeStateModel(t, ctx, conn, profileID, strategy.ID, now)
	loadbalanceRouteEnsureLoadbalancePartition(t, ctx, conn, now)
	loadbalanceRouteInsertLoadbalanceEvent(t, ctx, conn, profileID, connectionID, "banned", now.Add(-time.Hour), "loadbalance-contract-model")
	loadbalanceRouteInsertLoadbalanceEvent(t, ctx, conn, profileID, connectionID, "recovered", now.Add(-30*time.Minute), "loadbalance-contract-model")
	loadbalanceRouteInsertLoadbalanceEvent(t, ctx, conn, profileID, connectionID, "retry_scheduled", now.Add(-10*time.Minute), "loadbalance-contract-model")

	bannedUntil := now.Add(15 * time.Minute)
	runtimeState.SeedConnectionState(profileID, modelID, connectionID, loadbalancedomain.RuntimeConnectionState{ConnectionID: connectionID, BanMode: "temporary", BannedUntilAt: &bannedUntil, CumulativeRetryAttempts: 7, CycleRetryAttempts: 2}, now.Add(-time.Hour), now)

	response := loadbalanceRouteRequest(t, router, http.MethodGet, "/loadbalance/incidents?since_hours=24&limit=10", profileID, nil)
	loadbalanceRouteRequireStatus(t, response, http.StatusOK)
	var body map[string]any
	loadbalanceRouteDecode(t, response, &body)
	activeBans := body["active_bans"].([]any)
	if len(activeBans) != 1 || jsonIntFromAny(t, activeBans[0].(map[string]any)["connection_id"]) != connectionID {
		t.Fatalf("unexpected active bans: %+v", activeBans)
	}
	recentEvents := body["recent_events"].([]any)
	if len(recentEvents) != 2 {
		t.Fatalf("expected only incident event types, got %+v", recentEvents)
	}
	eventTypes := []string{recentEvents[0].(map[string]any)["event_type"].(string), recentEvents[1].(map[string]any)["event_type"].(string)}
	if !reflect.DeepEqual(eventTypes, []string{"recovered", "banned"}) {
		t.Fatalf("unexpected incident event order/types: %+v", eventTypes)
	}
	if body["generated_at"] == "" {
		t.Fatalf("expected generated_at in incidents response: %+v", body)
	}
}

func loadbalanceRouteRouter(t *testing.T, ctx context.Context, dsn string, now time.Time, runtimeState *loadbalancedomain.LocalRuntimeStateStore) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(config.Settings{}, Options{Pool: pool, Now: func() time.Time { return now }, RuntimeState: runtimeState})
	if err != nil {
		t.Fatalf("build loadbalance service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func loadbalanceRouteRequest(t *testing.T, handler http.Handler, method string, path string, profileID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	reader := bytes.NewReader(nil)
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(raw)
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set(profiledomain.ProfileIDHeader, fmt.Sprintf("%d", profileID))
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func loadbalanceRouteDecode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody=%s", err, response.Body.String())
	}
}

func loadbalanceRouteRequireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, response.Code, response.Body.String())
	}
}

func loadbalanceRouteRequireDetail(t *testing.T, response *httptest.ResponseRecorder, detail string) {
	t.Helper()
	var body map[string]any
	loadbalanceRouteDecode(t, response, &body)
	if body["detail"] != detail {
		t.Fatalf("expected detail %q, got %+v", detail, body)
	}
}

func loadbalanceRouteSeedRuntimeStateModel(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, strategyID int, now time.Time) (int, int) {
	t.Helper()
	var modelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, openai_accepted_format, created_at, updated_at) VALUES ($1, 'openai', 'loadbalance-contract-model', 'Loadbalance Contract Model', $2, TRUE, 'dual_native', $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&modelID); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	var endpointID int
	if err := conn.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, 'Loadbalance Endpoint', 'https://loadbalance.example', 'plain-key', 0, $2, $2) RETURNING id`, profileID, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	var connectionID int
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, 'responses_only', TRUE, 0, 'loadbalance-terminal', NULL, NULL, 'healthy', NULL, NULL, $3, $3) RETURNING id`, profileID, endpointID, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, modelID, connectionID, now); err != nil {
		t.Fatalf("insert access target: %v", err)
	}
	return modelID, connectionID
}

func loadbalanceRouteEnsureLoadbalancePartition(t *testing.T, ctx context.Context, conn *pgx.Conn, createdAt time.Time) {
	t.Helper()
	start := time.Date(createdAt.UTC().Year(), createdAt.UTC().Month(), createdAt.UTC().Day(), 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 0, 1)
	name := fmt.Sprintf("loadbalance_events_%s", start.Format("20060102"))
	_, err := conn.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s PARTITION OF loadbalance_events FOR VALUES FROM ('%s') TO ('%s')`, loadbalanceRouteQuoteIdentifier(name), start.Format(time.RFC3339), end.Format(time.RFC3339)))
	if err != nil {
		t.Fatalf("create loadbalance_events partition: %v", err)
	}
}

func loadbalanceRouteInsertLoadbalanceEvent(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, connectionID int, eventType string, createdAt time.Time, modelID string) {
	t.Helper()
	if _, err := conn.Exec(ctx, `INSERT INTO loadbalance_events (profile_id, connection_id, event_type, failure_kind, cycle_retry_attempts, cumulative_retry_attempts, next_retry_at, last_retry_delay_ms, model_id, endpoint_id, ban_mode, policy_cycle_retry_attempt_limit, policy_ban_cumulative_retry_attempt_threshold, created_at) VALUES ($1, $2, $3, 'transient_http', 2, 7, NULL, 0, $4, 1, 'temporary', 2, 2, $5)`, profileID, connectionID, eventType, modelID, createdAt.UTC()); err != nil {
		t.Fatalf("insert loadbalance event %s: %v", eventType, err)
	}
}

func loadbalanceRouteInsertProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, now time.Time) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, NULL, TRUE, TRUE, TRUE, 1, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return profileID
}

func loadbalanceRouteMigratedDatabase(t *testing.T, name string) (context.Context, *pgx.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := loadbalanceRouteHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, loadbalanceRouteRandomSuffix(t))
	dsn := harness.connectionString(databaseName)
	conn := harness.openDatabase(t, ctx, databaseName)
	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx) })
	return ctx, conn, dsn
}

type loadbalanceRoutePostgresHarness struct{ hostPort string }

func loadbalanceRouteHarness(t *testing.T) loadbalanceRoutePostgresHarness {
	t.Helper()
	loadbalanceRoutePostgres.once.Do(func() {
		containerName := "prism-loadbalance-" + loadbalanceRouteRandomSuffix(t)
		if _, err := loadbalanceRouteDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			loadbalanceRoutePostgres.err = err
			return
		}
		hostPort, err := loadbalanceRouteDockerPort(containerName)
		if err != nil {
			loadbalanceRoutePostgres.err = err
			return
		}
		if err := loadbalanceRouteWaitForPostgres(hostPort); err != nil {
			loadbalanceRoutePostgres.err = err
			return
		}
		loadbalanceRoutePostgres.hostPort = hostPort
	})
	if loadbalanceRoutePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", loadbalanceRoutePostgres.err)
	}
	return loadbalanceRoutePostgresHarness{hostPort: loadbalanceRoutePostgres.hostPort}
}

func (h loadbalanceRoutePostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := loadbalanceRouteConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+loadbalanceRouteQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+loadbalanceRouteQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return loadbalanceRouteConnect(t, ctx, h.connectionString(databaseName))
}

func (h loadbalanceRoutePostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func loadbalanceRouteConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func loadbalanceRouteDockerPort(containerName string) (string, error) {
	output, err := loadbalanceRouteDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(strings.Split(output, "\n")[0]))
	return port, err
}

func loadbalanceRouteWaitForPostgres(hostPort string) error {
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/postgres?sslmode=disable", hostPort))
		if err == nil {
			_ = conn.Close(ctx)
			cancel()
			return nil
		}
		cancel()
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("postgres container on port %s did not become ready in time", hostPort)
}

func loadbalanceRouteDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func loadbalanceRouteRandomSuffix(t *testing.T) string {
	t.Helper()
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(raw[:])
}

func loadbalanceRouteQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

func jsonIntFromAny(t *testing.T, value any) int {
	t.Helper()
	asFloat, ok := value.(float64)
	if !ok {
		t.Fatalf("expected JSON number, got %T %[1]v", value)
	}
	return int(asFloat)
}
