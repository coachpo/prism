package models

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/config"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

var modelsStorePostgres struct {
	once          sync.Once
	containerName string
	hostPort      string
	err           error
}

type modelRouteErrorResponse struct {
	Detail            string                       `json:"detail"`
	RoutingPlanIssues []routingPlanValidationIssue `json:"routing_plan_issues"`
}

type modelsPostgresHarness struct {
	containerName string
	hostPort      string
}

type promotionModelSeed struct {
	ModelID   string
	APIFamily string
	IsEnabled bool
}

func mustInsertModelRecord(t *testing.T, ctx context.Context, tx pgx.Tx, record modelRecord) modelRecord {
	t.Helper()
	created, err := insertModel(ctx, tx, record)
	if err != nil {
		t.Fatalf("insert model %q: %v", record.ModelID, err)
	}
	return created
}

func createPromotionTargetTestDatabase(t *testing.T, name string) (context.Context, *pgx.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)

	harness := modelsStoreHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, modelsRandomSuffix(t))
	dsn := harness.connectionString(databaseName)
	conn := harness.openDatabase(t, ctx, databaseName)

	runner, err := migrate.New(migrate.Options{})
	if err != nil {
		t.Fatalf("build migration runner: %v", err)
	}
	if _, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("run migrations for %s: %v", databaseName, err)
	}
	t.Cleanup(func() {
		_ = conn.Close(ctx)
	})
	return ctx, conn, dsn
}

func newPromotionTargetRouter(t *testing.T, ctx context.Context, dsn string, now time.Time) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)

	service, err := NewService(config.Settings{}, Options{Pool: pool, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build model service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func performPromotionTargetModelRequest(t *testing.T, handler http.Handler, method string, path string, profileID int, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		rawBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(rawBody)
	} else {
		reader = bytes.NewReader(nil)
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

func decodePromotionTargetJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody=%s", err, response.Body.String())
	}
}

func findListedModelByID(t *testing.T, items []modelConfigListResponse, modelConfigID int) modelConfigListResponse {
	t.Helper()
	for _, item := range items {
		if item.ID == modelConfigID {
			return item
		}
	}
	t.Fatalf("expected model %d in listed response, got %+v", modelConfigID, items)
	return modelConfigListResponse{}
}

func seedPromotionTerminalModel(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, strategyID int, now time.Time, seed promotionModelSeed, _ int, _ float64) (modelRecord, int) {
	t.Helper()
	model := insertPromotionModel(t, ctx, tx, profileID, strategyID, now, seed)
	endpointID := insertPromotionEndpoint(t, ctx, tx, profileID, seed.ModelID+"-endpoint", now)
	connectionID := insertPromotionConnection(t, ctx, tx, profileID, endpointID, seed.APIFamily, seed.ModelID+"-connection", now)
	insertPromotionConnectionTarget(t, ctx, tx, profileID, model.ID, connectionID, 0, true, now)
	return model, connectionID
}

func insertPromotionProfile(t *testing.T, ctx context.Context, tx pgx.Tx, name string, now time.Time) int {
	t.Helper()
	return insertPromotionProfileWithActive(t, ctx, tx, name, true, now)
}

func insertPromotionProfileWithActive(t *testing.T, ctx context.Context, tx pgx.Tx, name string, isActive bool, now time.Time) int {
	t.Helper()
	var profileID int
	if err := tx.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, $2, $3, FALSE, TRUE, 1, $4, $4) RETURNING id`, name, nil, isActive, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func insertPromotionStrategy(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, name string, now time.Time) int {
	t.Helper()
	var strategyID int
	if err := tx.QueryRow(ctx, `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, 'single', ARRAY[429], 'off', 60000, 2.0, 0.2, 900000, 3, 0, 0, $3, $3) RETURNING id`, profileID, name, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert loadbalance strategy %q: %v", name, err)
	}
	return strategyID
}

func insertPromotionModel(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, strategyID int, now time.Time, seed promotionModelSeed) modelRecord {
	t.Helper()
	record := modelRecord{
		ProfileID:             profileID,
		APIFamily:             seed.APIFamily,
		ModelID:               seed.ModelID,
		DisplayName:           stringPtr(seed.ModelID),
		LoadbalanceStrategyID: intPtr(strategyID),
		IsEnabled:             seed.IsEnabled,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
	if seed.APIFamily == "openai" {
		record.OpenAIAcceptedFormat = stringPtr(openAIAcceptedFormatDualNative)
	}
	return mustInsertModelRecord(t, ctx, tx, record)
}

func insertPromotionEndpoint(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, name string, now time.Time) int {
	t.Helper()
	var endpointID int
	if err := tx.QueryRow(ctx, `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, 0, $5, $5) RETURNING id`, profileID, name, fmt.Sprintf("https://%s.example", name), "test-api-key", now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint %q: %v", name, err)
	}
	return endpointID
}

func insertPromotionConnection(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, endpointID int, apiFamily string, name string, now time.Time) int {
	t.Helper()
	var connectionID int
	var openAITextCapability any
	if apiFamily == "openai" {
		openAITextCapability = "responses_only"
	}
	if err := tx.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, NULL, NULL, NULL, $4, TRUE, 0, $5, NULL, NULL, 'healthy', NULL, NULL, $6, $6) RETURNING id`, profileID, apiFamily, endpointID, openAITextCapability, name, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection %q: %v", name, err)
	}
	return connectionID
}

func insertPromotionModelTarget(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, sourceModelConfigID int, targetModelConfigID int, position int, isEnabled bool, now time.Time) {
	t.Helper()
	if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, targetModelConfigID, position, isEnabled, now); err != nil {
		t.Fatalf("insert model target: %v", err)
	}
}

func insertPromotionConnectionTarget(t *testing.T, ctx context.Context, tx pgx.Tx, profileID int, sourceModelConfigID int, connectionID int, position int, isEnabled bool, now time.Time) {
	t.Helper()
	if _, err := tx.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, $4, $5, $6, $6)`, profileID, sourceModelConfigID, connectionID, position, isEnabled, now); err != nil {
		t.Fatalf("insert connection target: %v", err)
	}
}

func modelsStoreHarness(t *testing.T) modelsPostgresHarness {
	t.Helper()
	modelsStorePostgres.once.Do(func() {
		containerName := "prism-models-" + modelsRandomSuffix(t)
		if _, err := runModelsDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			modelsStorePostgres.err = err
			return
		}
		modelsStorePostgres.containerName = containerName
		hostPort, err := modelsDockerPort(containerName)
		if err != nil {
			modelsStorePostgres.err = err
			return
		}
		if err := waitForModelsPostgres(hostPort); err != nil {
			modelsStorePostgres.err = err
			return
		}
		modelsStorePostgres.hostPort = hostPort
	})

	if modelsStorePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", modelsStorePostgres.err)
	}
	return modelsPostgresHarness{containerName: modelsStorePostgres.containerName, hostPort: modelsStorePostgres.hostPort}
}

func (h modelsPostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := modelsConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+modelsQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+modelsQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return modelsConnect(t, ctx, h.connectionString(databaseName))
}

func (h modelsPostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func modelsConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func modelsDockerPort(containerName string) (string, error) {
	output, err := runModelsDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	firstLine := strings.TrimSpace(strings.Split(output, "\n")[0])
	_, port, err := net.SplitHostPort(firstLine)
	if err != nil {
		return "", fmt.Errorf("parse docker port output %q: %w", firstLine, err)
	}
	return port, nil
}

func waitForModelsPostgres(hostPort string) error {
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

func runModelsDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s failed: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func modelsQuoteIdentifier(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func modelsRandomSuffix(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 4)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(buffer)
}
