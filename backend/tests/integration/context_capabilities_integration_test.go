package integration_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

const integrationConfigBundleSecretKey = "integration-configbundle-secret"
const integrationConfigBundlePreviewTokenKey = "integration-configbundle-bundle-key"

var integrationConfigBundleTime = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

func TestMigrations(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openDatabase(t, testContext, "context_capability_migrations")
	defer func() { _ = conn.Close(testContext) }()

	result, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("run baseline for context capability migration contract: %v", err)
	}
	if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected migration baseline to apply, got %q", result.Outcome)
	}
	assertContextCapabilityColumnContracts(t, testContext, conn)
}

func TestConfigBundle(t *testing.T) {
	harness := newConfigBundleIntegrationHarness(t)
	profileID := loadIntegrationDefaultProfileID(t, harness.conn)
	seedIntegrationConfigBundleGraph(t, harness.conn, profileID, "Default single", "single")

	exported := requestJSONMap(t, harness.client, http.MethodGet, harness.server.URL+"/api/config/profile/export", nil, profileHeader(profileID))
	if integrationJSONInt(t, exported["version"]) != 3 {
		t.Fatalf("expected profile export version 3, got %+v", exported)
	}
	if _, ok := exported["runtime"]; ok {
		t.Fatalf("expected profile export to stay on the existing bundle contract without bootstrap runtime rollout controls, got %+v", exported)
	}
	connection := asMap(t, exported["connections"].([]any)[0])
	if integrationJSONInt(t, connection["context_window_tokens"]) != 200000 || integrationJSONInt(t, connection["default_output_token_reserve"]) != 4096 || integrationJSONFloat(t, connection["max_context_utilization"]) != 0.9 {
		t.Fatalf("expected explicit connection capability export, got %+v", connection)
	}
	if connection["openai_probe_endpoint_variant"] != "responses_minimal" {
		t.Fatalf("expected config bundle export to keep openai_probe_endpoint_variant=responses_minimal, got %+v", connection)
	}

	mutated := cloneJSONMap(t, exported)
	mutatedConnection := asMap(t, mutated["connections"].([]any)[0])
	delete(mutatedConnection, "default_output_token_reserve")
	delete(mutatedConnection, "max_context_utilization")

	preview := requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import/preview", mutated, profileHeader(profileID))
	if preview["ready"] != true || strings.TrimSpace(preview["preview_token"].(string)) == "" {
		t.Fatalf("expected ready preview token for config bundle import, got %+v", preview)
	}

	importHeaders := profileHeader(profileID)
	importHeaders["X-Prism-Preview-Token"] = preview["preview_token"].(string)
	imported := requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import", mutated, importHeaders)
	if integrationJSONInt(t, imported["models_imported"]) != 1 || integrationJSONInt(t, imported["connections_imported"]) != 1 {
		t.Fatalf("expected single model/connection import counts, got %+v", imported)
	}

	reExported := requestJSONMap(t, harness.client, http.MethodGet, harness.server.URL+"/api/config/profile/export", nil, profileHeader(profileID))
	reExportedConnection := asMap(t, reExported["connections"].([]any)[0])
	if integrationJSONInt(t, reExportedConnection["default_output_token_reserve"]) != 4096 || integrationJSONFloat(t, reExportedConnection["max_context_utilization"]) != 0.9 {
		t.Fatalf("expected re-exported connection defaults after legacy omission import, got %+v", reExportedConnection)
	}
	if reExportedConnection["openai_probe_endpoint_variant"] != "responses_minimal" {
		t.Fatalf("expected re-exported connection to keep openai_probe_endpoint_variant=responses_minimal, got %+v", reExportedConnection)
	}

	assertIntegrationStoredConnectionCapabilities(t, harness.conn, "Primary OpenAI connection", intPtr(200000), 4096, 0.9)
	assertIntegrationStoredConnectionProbeVariant(t, harness.conn, "Primary OpenAI connection", "responses_minimal")
}

func TestConfigBundleRoundTripsRoundRobinStrategy(t *testing.T) {
	harness := newConfigBundleIntegrationHarness(t)
	profileID := loadIntegrationDefaultProfileID(t, harness.conn)
	seedIntegrationConfigBundleGraph(t, harness.conn, profileID, "Default round-robin routing", "round-robin")

	exported := requestJSONMap(t, harness.client, http.MethodGet, harness.server.URL+"/api/config/profile/export", nil, profileHeader(profileID))
	strategies := exported["loadbalance_strategies"].([]any)
	if len(strategies) != 1 {
		t.Fatalf("expected one exported loadbalance strategy, got %+v", strategies)
	}
	strategy := asMap(t, strategies[0])
	if strategy["legacy_strategy_type"] != "round-robin" || strategy["name"] != "Default round-robin routing" {
		t.Fatalf("expected round-robin export, got %+v", strategy)
	}

	preview := requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import/preview", exported, profileHeader(profileID))
	if preview["ready"] != true || strings.TrimSpace(preview["preview_token"].(string)) == "" {
		t.Fatalf("expected ready preview token for round-robin import, got %+v", preview)
	}

	importHeaders := profileHeader(profileID)
	importHeaders["X-Prism-Preview-Token"] = preview["preview_token"].(string)
	imported := requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import", exported, importHeaders)
	if integrationJSONInt(t, imported["strategies_imported"]) != 1 {
		t.Fatalf("expected one imported round-robin strategy, got %+v", imported)
	}

	reExported := requestJSONMap(t, harness.client, http.MethodGet, harness.server.URL+"/api/config/profile/export", nil, profileHeader(profileID))
	reExportedStrategy := asMap(t, reExported["loadbalance_strategies"].([]any)[0])
	if reExportedStrategy["legacy_strategy_type"] != "round-robin" || reExportedStrategy["name"] != "Default round-robin routing" {
		t.Fatalf("expected round-robin re-export, got %+v", reExportedStrategy)
	}

	var storedStrategyType string
	if err := harness.conn.QueryRow(context.Background(), `SELECT legacy_strategy_type FROM loadbalance_strategies WHERE profile_id = $1 AND name = $2 LIMIT 1`, profileID, "Default round-robin routing").Scan(&storedStrategyType); err != nil {
		t.Fatalf("load stored round-robin strategy: %v", err)
	}
	if storedStrategyType != "round-robin" {
		t.Fatalf("expected stored round-robin strategy, got %q", storedStrategyType)
	}
}

type configBundleIntegrationHarness struct {
	conn   *pgx.Conn
	pool   *pgxpool.Pool
	client *http.Client
	server *httptest.Server
}

func newConfigBundleIntegrationHarness(t *testing.T) *configBundleIntegrationHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	harness := newPostgresHarness(t)
	databaseName := "config_bundle_context_" + randomSuffix(t)
	conn := harness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: harness.connectionString(databaseName), SecretEncryptionKey: integrationConfigBundleSecretKey})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: harness.connectionString(databaseName), SecretEncryptionKey: integrationConfigBundleSecretKey, ConfigBundleEncryptionKey: integrationConfigBundlePreviewTokenKey, CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)

	configBundleService, err := managementconfigbundle.NewService(settings, managementconfigbundle.Options{Pool: pool, Now: func() time.Time { return integrationConfigBundleTime }})
	if err != nil {
		t.Fatalf("build config bundle service: %v", err)
	}
	t.Cleanup(configBundleService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "context-capability-integration-test", ConfigBundleService: configBundleService})
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	client := server.Client()
	client.Jar = jar
	return &configBundleIntegrationHarness{conn: conn, pool: pool, client: client, server: server}
}

func seedIntegrationConfigBundleGraph(t *testing.T, conn *pgx.Conn, profileID int, strategyName string, strategyType string) {
	t.Helper()
	now := integrationConfigBundleTime
	var endpointID int
	if err := conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, 'Primary OpenAI', 'https://api.openai.com', 'plain-api-key', 0, $2, $2) RETURNING id`, profileID, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert integration endpoint: %v", err)
	}
	var strategyID int
	if err := conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, $2, $3, ARRAY[429], 'until_reset', 60000, 2.0, 0.2, 900000, 2, 4, 0, $4, $4) RETURNING id`, profileID, strategyName, strategyType, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert integration strategy: %v", err)
	}
	var modelConfigID int
	if err := conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'gpt-4o-mini', 'GPT 4o Mini', $2, 'dual_native', TRUE, $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert integration model: %v", err)
	}
	var connectionID int
	if err := conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, context_window_tokens, default_output_token_reserve, max_context_utilization, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, 200000, 4096, 0.90, NULL, 60, 8, 4, 'responses_minimal', 'responses_only', TRUE, 0, 'Primary OpenAI connection', 'openai', '{"X-Test":"integration"}', 'healthy', NULL, NULL, $3, $3) RETURNING id`, profileID, endpointID, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert integration connection: %v", err)
	}
	if _, err := conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, modelConfigID, connectionID, now); err != nil {
		t.Fatalf("insert integration access target: %v", err)
	}
}

func loadIntegrationDefaultProfileID(t *testing.T, conn *pgx.Conn) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(context.Background(), `SELECT id FROM profiles WHERE is_default = TRUE LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile id: %v", err)
	}
	return profileID
}

func assertContextCapabilityColumnContracts(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	assertContextCapabilityColumnAbsent(t, ctx, conn, "model_configs", "context_window_tokens")
	assertContextCapabilityColumnAbsent(t, ctx, conn, "model_configs", "default_output_token_reserve")
	assertContextCapabilityColumnAbsent(t, ctx, conn, "model_configs", "max_context_utilization")
	assertContextCapabilityColumnAbsent(t, ctx, conn, "model_configs", "preferred_context_utilization_threshold")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "context_window_tokens", "integer", "YES", "")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "default_output_token_reserve", "integer", "NO", "4096")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "max_context_utilization", "double precision", "NO", "0.9")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "preferred_context_utilization_threshold", "double precision", "YES", "")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "context_window_tokens_overridden", "boolean", "NO", "false")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "default_output_token_reserve_overridden", "boolean", "NO", "false")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "max_context_utilization_overridden", "boolean", "NO", "false")
	assertContextCapabilityColumn(t, ctx, conn, "connections", "preferred_context_utilization_threshold_overridden", "boolean", "NO", "false")
}

func assertContextCapabilityColumnAbsent(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnName string) {
	t.Helper()
	var columnCount int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, tableName, columnName).Scan(&columnCount); err != nil {
		t.Fatalf("check %s.%s absence contract: %v", tableName, columnName, err)
	}
	if columnCount != 0 {
		t.Fatalf("expected %s.%s to be absent, got %d matching columns", tableName, columnName, columnCount)
	}
}

func assertContextCapabilityColumn(t *testing.T, ctx context.Context, conn *pgx.Conn, tableName string, columnName string, dataType string, isNullable string, defaultContains string) {
	t.Helper()
	var gotDataType string
	var gotNullable string
	var gotDefault string
	if err := conn.QueryRow(ctx, `SELECT data_type, is_nullable, COALESCE(column_default, '') FROM information_schema.columns WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2`, tableName, columnName).Scan(&gotDataType, &gotNullable, &gotDefault); err != nil {
		t.Fatalf("load %s.%s column contract: %v", tableName, columnName, err)
	}
	if gotDataType != dataType || gotNullable != isNullable {
		t.Fatalf("expected %s.%s to be %s nullable=%s, got type=%s nullable=%s", tableName, columnName, dataType, isNullable, gotDataType, gotNullable)
	}
	if defaultContains == "" {
		if strings.TrimSpace(gotDefault) != "" {
			t.Fatalf("expected %s.%s to have no default, got %q", tableName, columnName, gotDefault)
		}
		return
	}
	if !strings.Contains(strings.ToLower(gotDefault), strings.ToLower(defaultContains)) {
		t.Fatalf("expected %s.%s default to contain %q, got %q", tableName, columnName, defaultContains, gotDefault)
	}
}

func assertIntegrationStoredConnectionCapabilities(t *testing.T, conn *pgx.Conn, connectionName string, wantContextWindowTokens *int, wantDefaultOutputTokenReserve int, wantMaxContextUtilization float64) {
	t.Helper()
	var contextWindowTokens sql.NullInt32
	var defaultOutputTokenReserve int
	var maxContextUtilization float64
	if err := conn.QueryRow(context.Background(), `SELECT context_window_tokens, default_output_token_reserve, max_context_utilization FROM connections WHERE name = $1 LIMIT 1`, connectionName).Scan(&contextWindowTokens, &defaultOutputTokenReserve, &maxContextUtilization); err != nil {
		t.Fatalf("load connection %q capabilities: %v", connectionName, err)
	}
	assertIntegrationCapabilityValues(t, fmt.Sprintf("connection %s", connectionName), contextWindowTokens.Valid, int(contextWindowTokens.Int32), defaultOutputTokenReserve, maxContextUtilization, wantContextWindowTokens, wantDefaultOutputTokenReserve, wantMaxContextUtilization)
}

func assertIntegrationStoredConnectionProbeVariant(t *testing.T, conn *pgx.Conn, connectionName string, want string) {
	t.Helper()
	var probeVariant sql.NullString
	if err := conn.QueryRow(context.Background(), `SELECT openai_probe_endpoint_variant FROM connections WHERE name = $1 LIMIT 1`, connectionName).Scan(&probeVariant); err != nil {
		t.Fatalf("load connection %q openai_probe_endpoint_variant: %v", connectionName, err)
	}
	if !probeVariant.Valid || probeVariant.String != want {
		t.Fatalf("expected connection %q openai_probe_endpoint_variant %q, got %+v", connectionName, want, probeVariant)
	}
}

func assertIntegrationCapabilityValues(t *testing.T, label string, hasContextWindowTokens bool, gotContextWindowTokens int, gotDefaultOutputTokenReserve int, gotMaxContextUtilization float64, wantContextWindowTokens *int, wantDefaultOutputTokenReserve int, wantMaxContextUtilization float64) {
	t.Helper()
	if wantContextWindowTokens == nil {
		if hasContextWindowTokens {
			t.Fatalf("expected %s context_window_tokens to be NULL, got %d", label, gotContextWindowTokens)
		}
	} else if !hasContextWindowTokens || gotContextWindowTokens != *wantContextWindowTokens {
		t.Fatalf("expected %s context_window_tokens %d, got valid=%v value=%d", label, *wantContextWindowTokens, hasContextWindowTokens, gotContextWindowTokens)
	}
	if gotDefaultOutputTokenReserve != wantDefaultOutputTokenReserve || gotMaxContextUtilization != wantMaxContextUtilization {
		t.Fatalf("expected %s reserve/utilization %d/%0.2f, got %d/%0.2f", label, wantDefaultOutputTokenReserve, wantMaxContextUtilization, gotDefaultOutputTokenReserve, gotMaxContextUtilization)
	}
}

func requestJSONMap(t *testing.T, client *http.Client, method string, url string, payload any, headers map[string]string) map[string]any {
	t.Helper()
	var bodyReader *bytes.Reader
	if payload == nil {
		bodyReader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal request payload: %v", err)
		}
		bodyReader = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatalf("build request %s %s: %v", method, url, err)
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("execute request %s %s: %v", method, url, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var errorBody map[string]any
		_ = json.NewDecoder(response.Body).Decode(&errorBody)
		t.Fatalf("expected success for %s %s, got status=%d body=%+v", method, url, response.StatusCode, errorBody)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response %s %s: %v", method, url, err)
	}
	return body
}

func profileHeader(profileID int) map[string]string {
	return map[string]string{profiledomain.ProfileIDHeader: fmt.Sprintf("%d", profileID)}
}

func cloneJSONMap(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload clone: %v", err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("unmarshal payload clone: %v", err)
	}
	return cloned
}

func asMap(t *testing.T, raw any) map[string]any {
	t.Helper()
	value, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("expected map payload, got %T (%+v)", raw, raw)
	}
	return value
}

func integrationJSONInt(t *testing.T, raw any) int {
	t.Helper()
	value, ok := raw.(float64)
	if !ok {
		t.Fatalf("expected float64-backed int JSON value, got %T (%+v)", raw, raw)
	}
	return int(value)
}

func integrationJSONFloat(t *testing.T, raw any) float64 {
	t.Helper()
	value, ok := raw.(float64)
	if !ok {
		t.Fatalf("expected float64 JSON value, got %T (%+v)", raw, raw)
	}
	return value
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}

func TestContextCapabilities(t *testing.T) {
	t.Run("preferred-context-columns", func(t *testing.T) {
		testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		harness := newPostgresHarness(t)
		runner := newRunner(t)
		conn := harness.openDatabase(t, testContext, "preferred_context_capabilities")
		defer func() { _ = conn.Close(testContext) }()
		if _, err := runner.Run(testContext, conn); err != nil {
			t.Fatalf("run baseline for preferred context capability migration contract: %v", err)
		}
		assertContextCapabilityColumn(t, testContext, conn, "connections", "preferred_context_utilization_threshold", "double precision", "YES", "")
		assertContextCapabilityColumn(t, testContext, conn, "connections", "preferred_context_utilization_threshold_overridden", "boolean", "NO", "false")
	})

	t.Run("preferred-context-bundle-roundtrip", func(t *testing.T) {
		harness := newConfigBundleIntegrationHarness(t)
		profileID := loadIntegrationDefaultProfileID(t, harness.conn)
		seedIntegrationConfigBundleGraph(t, harness.conn, profileID, "Default single", "single")
		if _, err := harness.conn.Exec(context.Background(), `UPDATE connections SET preferred_context_utilization_threshold = 0.65 WHERE profile_id = $1`, profileID); err != nil {
			t.Fatalf("seed integration connection preferred_context_utilization_threshold: %v", err)
		}

		exported := requestJSONMap(t, harness.client, http.MethodGet, harness.server.URL+"/api/config/profile/export", nil, profileHeader(profileID))
		connection := asMap(t, exported["connections"].([]any)[0])
		if integrationJSONFloat(t, connection["preferred_context_utilization_threshold"]) != 0.65 {
			t.Fatalf("expected connection preferred_context_utilization_threshold export value, got %+v", connection)
		}

		mutated := cloneJSONMap(t, exported)
		delete(asMap(t, mutated["connections"].([]any)[0]), "preferred_context_utilization_threshold")
		roundTripPreview := requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import/preview", exported, profileHeader(profileID))
		roundTripHeaders := profileHeader(profileID)
		roundTripHeaders["X-Prism-Preview-Token"] = roundTripPreview["preview_token"].(string)
		_ = requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import", exported, roundTripHeaders)
		roundTripExported := requestJSONMap(t, harness.client, http.MethodGet, harness.server.URL+"/api/config/profile/export", nil, profileHeader(profileID))
		if integrationJSONFloat(t, asMap(t, roundTripExported["connections"].([]any)[0])["preferred_context_utilization_threshold"]) != 0.65 {
			t.Fatalf("expected non-null preferred_context_utilization_threshold to round-trip, got %+v", roundTripExported)
		}
		var roundTripConnectionPreferred sql.NullFloat64
		if err := harness.conn.QueryRow(context.Background(), `SELECT preferred_context_utilization_threshold FROM connections WHERE profile_id = $1 LIMIT 1`, profileID).Scan(&roundTripConnectionPreferred); err != nil {
			t.Fatalf("load integration connection preferred_context_utilization_threshold after non-null roundtrip: %v", err)
		}
		if !roundTripConnectionPreferred.Valid || roundTripConnectionPreferred.Float64 != 0.65 {
			t.Fatalf("expected integration connection preferred_context_utilization_threshold 0.65 after non-null roundtrip, got %+v", roundTripConnectionPreferred)
		}
		preview := requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import/preview", mutated, profileHeader(profileID))
		importHeaders := profileHeader(profileID)
		importHeaders["X-Prism-Preview-Token"] = preview["preview_token"].(string)
		_ = requestJSONMap(t, harness.client, http.MethodPost, harness.server.URL+"/api/config/profile/import", mutated, importHeaders)
		reExported := requestJSONMap(t, harness.client, http.MethodGet, harness.server.URL+"/api/config/profile/export", nil, profileHeader(profileID))
		if asMap(t, reExported["connections"].([]any)[0])["preferred_context_utilization_threshold"] != nil {
			t.Fatalf("expected legacy omission import to clear preferred_context_utilization_threshold, got %+v", reExported)
		}
		var connectionPreferred sql.NullFloat64
		if err := harness.conn.QueryRow(context.Background(), `SELECT preferred_context_utilization_threshold FROM connections WHERE profile_id = $1 LIMIT 1`, profileID).Scan(&connectionPreferred); err != nil {
			t.Fatalf("load integration connection preferred_context_utilization_threshold: %v", err)
		}
		if connectionPreferred.Valid {
			t.Fatalf("expected integration connection preferred_context_utilization_threshold NULL after legacy import, got %0.2f", connectionPreferred.Float64)
		}
	})
}
