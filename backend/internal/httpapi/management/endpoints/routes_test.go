package endpoints

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

var endpointsRoutePostgres struct {
	once     sync.Once
	hostPort string
	err      error
}

func TestEndpointRoutesCRUDMaskedKeyOrderingAndUsageProtections(t *testing.T) {
	ctx, conn, dsn := endpointsRouteMigratedDatabase(t, "endpoints_routes_contract")
	now := time.Date(2026, time.June, 7, 15, 20, 0, 0, time.UTC)
	profileID := endpointsRouteInsertProfile(t, ctx, conn, "Endpoints route profile", now)
	router := endpointsRouteRouter(t, ctx, dsn, now)

	first := endpointsRouteRequest(t, router, http.MethodPost, "/endpoints", profileID, map[string]any{"name": " Primary Endpoint ", "base_url": "https://api.example.com/v1///", "api_key": "secret-one"})
	endpointsRouteRequireStatus(t, first, http.StatusCreated)
	var created endpointResponse
	endpointsRouteDecode(t, first, &created)
	if created.Name != "Primary Endpoint" || created.BaseURL != "https://api.example.com/v1" || created.Position != 0 {
		t.Fatalf("unexpected created endpoint response: %+v", created)
	}
	if !created.HasAPIKey || created.MaskedAPIKey == nil || *created.MaskedAPIKey != "********" || strings.Contains(first.Body.String(), "secret-one") {
		t.Fatalf("expected masked api key without plaintext, got body=%s", first.Body.String())
	}

	second := endpointsRouteRequest(t, router, http.MethodPost, "/endpoints", profileID, map[string]any{"name": "Secondary Endpoint", "base_url": "https://secondary.example", "api_key": ""})
	endpointsRouteRequireStatus(t, second, http.StatusCreated)
	var secondary endpointResponse
	endpointsRouteDecode(t, second, &secondary)
	if secondary.HasAPIKey || secondary.MaskedAPIKey != nil || secondary.Position != 1 {
		t.Fatalf("unexpected secondary endpoint response: %+v", secondary)
	}

	duplicateName := endpointsRouteRequest(t, router, http.MethodPost, "/endpoints", profileID, map[string]any{"name": "Primary Endpoint", "base_url": "https://other.example", "api_key": "other"})
	endpointsRouteRequireStatus(t, duplicateName, http.StatusConflict)
	endpointsRouteRequireDetail(t, duplicateName, "Endpoint name 'Primary Endpoint' already exists")

	moved := endpointsRouteRequest(t, router, http.MethodPatch, fmt.Sprintf("/endpoints/%d/position", created.ID), profileID, map[string]any{"to_index": 1})
	endpointsRouteRequireStatus(t, moved, http.StatusOK)
	var movedItems []endpointResponse
	endpointsRouteDecode(t, moved, &movedItems)
	if len(movedItems) != 2 || movedItems[0].ID != secondary.ID || movedItems[0].Position != 0 || movedItems[1].ID != created.ID || movedItems[1].Position != 1 {
		t.Fatalf("expected normalized moved order, got %+v", movedItems)
	}

	duplicated := endpointsRouteRequest(t, router, http.MethodPost, fmt.Sprintf("/endpoints/%d/duplicate", created.ID), profileID, nil)
	endpointsRouteRequireStatus(t, duplicated, http.StatusCreated)
	var duplicate endpointResponse
	endpointsRouteDecode(t, duplicated, &duplicate)
	if duplicate.Name != "Primary Endpoint copy" || duplicate.Position != 2 || !duplicate.HasAPIKey || duplicate.MaskedAPIKey == nil {
		t.Fatalf("unexpected duplicate response: %+v", duplicate)
	}

	updated := endpointsRouteRequest(t, router, http.MethodPut, fmt.Sprintf("/endpoints/%d", created.ID), profileID, map[string]any{"name": "Primary Renamed", "api_key": ""})
	endpointsRouteRequireStatus(t, updated, http.StatusOK)
	var updatedEndpoint endpointResponse
	endpointsRouteDecode(t, updated, &updatedEndpoint)
	if updatedEndpoint.Name != "Primary Renamed" || !updatedEndpoint.HasAPIKey || updatedEndpoint.MaskedAPIKey == nil || strings.Contains(updated.Body.String(), "secret-one") {
		t.Fatalf("expected empty api_key update to preserve masked secret, got %+v body=%s", updatedEndpoint, updated.Body.String())
	}

	modelID, connectionID := endpointsRouteSeedConnectionUsage(t, ctx, conn, profileID, created.ID, now)
	usage := endpointsRouteRequest(t, router, http.MethodGet, "/endpoints/connections", profileID, nil)
	endpointsRouteRequireStatus(t, usage, http.StatusOK)
	var dropdown connectionDropdownResponse
	endpointsRouteDecode(t, usage, &dropdown)
	if len(dropdown.Items) != 1 || dropdown.Items[0].ID != connectionID || dropdown.Items[0].EndpointID != created.ID || dropdown.Items[0].Name == nil || *dropdown.Items[0].Name != "primary-terminal" {
		t.Fatalf("unexpected endpoint connection dropdown response: %+v", dropdown)
	}

	blockedDelete := endpointsRouteRequest(t, router, http.MethodDelete, fmt.Sprintf("/endpoints/%d", created.ID), profileID, nil)
	endpointsRouteRequireStatus(t, blockedDelete, http.StatusConflict)
	var blocked map[string]any
	endpointsRouteDecode(t, blockedDelete, &blocked)
	detail := blocked["detail"].(map[string]any)
	if detail["message"] != "Cannot delete endpoint that is referenced by connections" {
		t.Fatalf("unexpected delete protection message: %+v", detail)
	}
	connections := detail["connections"].([]any)
	if len(connections) != 1 || int(connections[0].(map[string]any)["connection_id"].(float64)) != connectionID || int(connections[0].(map[string]any)["model_config_id"].(float64)) != modelID {
		t.Fatalf("unexpected delete protection connections: %+v", connections)
	}

	deleted := endpointsRouteRequest(t, router, http.MethodDelete, fmt.Sprintf("/endpoints/%d", secondary.ID), profileID, nil)
	endpointsRouteRequireStatus(t, deleted, http.StatusOK)
	var deletedBody deletedResponse
	endpointsRouteDecode(t, deleted, &deletedBody)
	if !deletedBody.Deleted {
		t.Fatalf("expected deleted response, got %+v", deletedBody)
	}
}

func TestEndpointUpdateDoesNotOwnDependentRoutingPricingOrHealthState(t *testing.T) {
	ctx, conn, dsn := endpointsRouteMigratedDatabase(t, "endpoints_boundary_update")
	now := time.Date(2026, time.June, 7, 15, 40, 0, 0, time.UTC)
	profileID := endpointsRouteInsertProfile(t, ctx, conn, "Endpoints boundary profile", now)
	router := endpointsRouteRouter(t, ctx, dsn, now)

	createdResponse := endpointsRouteRequest(t, router, http.MethodPost, "/endpoints", profileID, map[string]any{"name": "Boundary Endpoint", "base_url": "https://boundary.example/v1", "api_key": "boundary-secret"})
	endpointsRouteRequireStatus(t, createdResponse, http.StatusCreated)
	var created endpointResponse
	endpointsRouteDecode(t, createdResponse, &created)

	modelID, connectionID := endpointsRouteSeedRichConnectionUsage(t, ctx, conn, profileID, created.ID, now)
	before := endpointsRouteLoadConnectionBoundaryState(t, ctx, conn, modelID, connectionID)

	updatedResponse := endpointsRouteRequest(t, router, http.MethodPut, fmt.Sprintf("/endpoints/%d", created.ID), profileID, map[string]any{"base_url": "https://transport.changed/v2///", "api_key": "rotated-secret"})
	endpointsRouteRequireStatus(t, updatedResponse, http.StatusOK)
	var updated endpointResponse
	endpointsRouteDecode(t, updatedResponse, &updated)
	if updated.BaseURL != "https://transport.changed/v2" || !updated.HasAPIKey || updated.MaskedAPIKey == nil || strings.Contains(updatedResponse.Body.String(), "rotated-secret") {
		t.Fatalf("expected endpoint update to stay on transport/credential response shape, got %+v body=%s", updated, updatedResponse.Body.String())
	}

	after := endpointsRouteLoadConnectionBoundaryState(t, ctx, conn, modelID, connectionID)
	if before != after {
		t.Fatalf("endpoint update changed dependent routing/pricing/health state\nbefore=%+v\nafter=%+v", before, after)
	}
}

func endpointsRouteRouter(t *testing.T, ctx context.Context, dsn string, now time.Time) http.Handler {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	service, err := NewService(config.Settings{SecretEncryptionKey: "test-secret-key"}, Options{Pool: pool, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatalf("build endpoint service: %v", err)
	}
	router := chi.NewRouter()
	service.MountManagementRoutes(router)
	return router
}

func endpointsRouteRequest(t *testing.T, handler http.Handler, method string, path string, profileID int, body any) *httptest.ResponseRecorder {
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

func endpointsRouteDecode(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode JSON response: %v\nbody=%s", err, response.Body.String())
	}
}

func endpointsRouteRequireStatus(t *testing.T, response *httptest.ResponseRecorder, status int) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("expected status %d, got %d body=%s", status, response.Code, response.Body.String())
	}
}

func endpointsRouteRequireDetail(t *testing.T, response *httptest.ResponseRecorder, detail string) {
	t.Helper()
	var body map[string]any
	endpointsRouteDecode(t, response, &body)
	if body["detail"] != detail {
		t.Fatalf("expected detail %q, got %+v", detail, body)
	}
}

func endpointsRouteSeedConnectionUsage(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, endpointID int, now time.Time) (int, int) {
	t.Helper()
	var modelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'endpoint-contract-model', 'Endpoint Contract Model', NULL, TRUE, $2, $2) RETURNING id`, profileID, now).Scan(&modelID); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	var connectionID int
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, NULL, NULL, NULL, NULL, NULL, 'responses_only', TRUE, 0, 'primary-terminal', NULL, NULL, 'healthy', NULL, NULL, $3, $3) RETURNING id`, profileID, endpointID, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, modelID, connectionID, now); err != nil {
		t.Fatalf("insert model access target: %v", err)
	}
	return modelID, connectionID
}

type endpointsRouteConnectionBoundaryState struct {
	EndpointID         int
	PricingTemplateID  int
	QPSLimit           int
	MaxNonStream       int
	MaxStream          int
	Priority           int
	HealthStatus       string
	HealthDetail       string
	LastHealthCheck    time.Time
	TargetType         string
	TargetConnectionID int
	TargetPosition     int
	TargetEnabled      bool
}

func endpointsRouteSeedRichConnectionUsage(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, endpointID int, now time.Time) (int, int) {
	t.Helper()
	pricingID := endpointsRouteInsertPricingTemplate(t, ctx, conn, profileID, "Endpoint Boundary Pricing", now)
	var modelID int
	if err := conn.QueryRow(ctx, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'endpoint-boundary-model', 'Endpoint Boundary Model', NULL, TRUE, $2, $2) RETURNING id`, profileID, now).Scan(&modelID); err != nil {
		t.Fatalf("insert boundary model: %v", err)
	}
	var connectionID int
	lastHealthCheck := now.Add(-time.Hour)
	if err := conn.QueryRow(ctx, `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, $3, 7, 3, 2, 'responses_minimal', 'responses_only', TRUE, 42, 'boundary-terminal', 'bearer', '{"X-Boundary":"kept"}', 'degraded', 'sticky health detail', $4, $5, $5) RETURNING id`, profileID, endpointID, pricingID, lastHealthCheck, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert boundary connection: %v", err)
	}
	if _, err := conn.Exec(ctx, `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 5, FALSE, $4, $4)`, profileID, modelID, connectionID, now); err != nil {
		t.Fatalf("insert boundary access target: %v", err)
	}
	return modelID, connectionID
}

func endpointsRouteInsertPricingTemplate(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, name string, now time.Time) int {
	t.Helper()
	var pricingID int
	if err := conn.QueryRow(ctx, `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, NULL, 'PER_1M', 'USD', '1', '2', '0', '0', '0', 1, $3, $3) RETURNING id`, profileID, name, now).Scan(&pricingID); err != nil {
		t.Fatalf("insert pricing template: %v", err)
	}
	return pricingID
}

func endpointsRouteLoadConnectionBoundaryState(t *testing.T, ctx context.Context, conn *pgx.Conn, modelID int, connectionID int) endpointsRouteConnectionBoundaryState {
	t.Helper()
	var state endpointsRouteConnectionBoundaryState
	if err := conn.QueryRow(ctx, `SELECT connections.endpoint_id, connections.pricing_template_id, connections.qps_limit, connections.max_in_flight_non_stream, connections.max_in_flight_stream, connections.priority, connections.health_status, connections.health_detail, connections.last_health_check, model_access_targets.target_type, model_access_targets.target_connection_id, model_access_targets.position, model_access_targets.is_enabled
		FROM connections
		JOIN model_access_targets ON model_access_targets.profile_id = connections.profile_id AND model_access_targets.target_connection_id = connections.id
		WHERE connections.id = $1 AND model_access_targets.source_model_config_id = $2`, connectionID, modelID).Scan(&state.EndpointID, &state.PricingTemplateID, &state.QPSLimit, &state.MaxNonStream, &state.MaxStream, &state.Priority, &state.HealthStatus, &state.HealthDetail, &state.LastHealthCheck, &state.TargetType, &state.TargetConnectionID, &state.TargetPosition, &state.TargetEnabled); err != nil {
		t.Fatalf("load connection boundary state: %v", err)
	}
	return state
}

func endpointsRouteInsertProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, name string, now time.Time) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, created_at, updated_at) VALUES ($1, NULL, TRUE, TRUE, TRUE, 1, $2, $2) RETURNING id`, name, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile: %v", err)
	}
	return profileID
}

func endpointsRouteMigratedDatabase(t *testing.T, name string) (context.Context, *pgx.Conn, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := endpointsRouteHarness(t)
	databaseName := fmt.Sprintf("%s_%s", name, endpointsRouteRandomSuffix(t))
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

type endpointsRoutePostgresHarness struct{ hostPort string }

func endpointsRouteHarness(t *testing.T) endpointsRoutePostgresHarness {
	t.Helper()
	endpointsRoutePostgres.once.Do(func() {
		containerName := "prism-endpoints-" + endpointsRouteRandomSuffix(t)
		if _, err := endpointsRouteDockerCommand(context.Background(), "run", "--rm", "-d", "--name", containerName, "-e", "POSTGRES_DB=postgres", "-e", "POSTGRES_USER=prism", "-e", "POSTGRES_PASSWORD=prism", "-P", "postgres:16-alpine"); err != nil {
			endpointsRoutePostgres.err = err
			return
		}
		hostPort, err := endpointsRouteDockerPort(containerName)
		if err != nil {
			endpointsRoutePostgres.err = err
			return
		}
		if err := endpointsRouteWaitForPostgres(hostPort); err != nil {
			endpointsRoutePostgres.err = err
			return
		}
		endpointsRoutePostgres.hostPort = hostPort
	})
	if endpointsRoutePostgres.err != nil {
		t.Fatalf("start postgres harness: %v", endpointsRoutePostgres.err)
	}
	return endpointsRoutePostgresHarness{hostPort: endpointsRoutePostgres.hostPort}
}

func (h endpointsRoutePostgresHarness) openDatabase(t *testing.T, ctx context.Context, databaseName string) *pgx.Conn {
	t.Helper()
	adminConn := endpointsRouteConnect(t, ctx, h.connectionString("postgres"))
	defer func() { _ = adminConn.Close(ctx) }()
	if _, err := adminConn.Exec(ctx, `DROP DATABASE IF EXISTS `+endpointsRouteQuoteIdentifier(databaseName)+` WITH (FORCE)`); err != nil {
		t.Fatalf("drop database %s: %v", databaseName, err)
	}
	if _, err := adminConn.Exec(ctx, `CREATE DATABASE `+endpointsRouteQuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create database %s: %v", databaseName, err)
	}
	return endpointsRouteConnect(t, ctx, h.connectionString(databaseName))
}

func (h endpointsRoutePostgresHarness) connectionString(databaseName string) string {
	return fmt.Sprintf("postgres://prism:prism@127.0.0.1:%s/%s?sslmode=disable", h.hostPort, databaseName)
}

func endpointsRouteConnect(t *testing.T, ctx context.Context, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to postgres %s: %v", dsn, err)
	}
	return conn
}

func endpointsRouteDockerPort(containerName string) (string, error) {
	output, err := endpointsRouteDockerCommand(context.Background(), "port", containerName, "5432/tcp")
	if err != nil {
		return "", err
	}
	_, port, err := net.SplitHostPort(strings.TrimSpace(strings.Split(output, "\n")[0]))
	return port, err
}

func endpointsRouteWaitForPostgres(hostPort string) error {
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

func endpointsRouteDockerCommand(ctx context.Context, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "docker", args...)
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func endpointsRouteRandomSuffix(t *testing.T) string {
	t.Helper()
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		t.Fatalf("generate random suffix: %v", err)
	}
	return hex.EncodeToString(raw[:])
}

func endpointsRouteQuoteIdentifier(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}
