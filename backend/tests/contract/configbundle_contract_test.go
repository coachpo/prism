package contract_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/endpointdomain"
	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

const (
	configBundleSecretKey    = "configbundle-contract-secret"
	configBundleFixtureKeyID = "sha256:profile-v2-contract"
	configBundleOpenAISecret = "fixture-openai-secret"
)

var configBundleFixtureTime = time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)

func TestProfileBundleV2Contract(t *testing.T) {
	harness := newConfigBundleV2ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV2Graph(t, harness, profileID)

	exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, exportResponse, http.StatusOK)
	if got := exportResponse.Header.Get("Content-Disposition"); got != "attachment; filename=\"prism-profile-config-v2-2026-04-18.json\"" {
		t.Fatalf("expected v2 profile export filename header, got %q", got)
	}

	var payload map[string]any
	decodeJSONResponse(t, exportResponse, &payload)
	assertProfileBundleV2Shape(t, payload)

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != true || previewPayload["preview_token"] == "" {
		t.Fatalf("expected ready v2 profile import preview, got %+v", previewPayload)
	}

	scope := asMap(t, previewPayload["replacement_scope"])
	if jsonInt(t, previewPayload["connections_imported"]) != 1 || jsonInt(t, scope["connections"]) != 1 {
		t.Fatalf("expected one top-level connection in v2 preview, got %+v", previewPayload)
	}

	importHeaders := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string))
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, importHeaders)
	assertStatus(t, importResponse, http.StatusOK)
	var importPayload map[string]any
	decodeJSONResponse(t, importResponse, &importPayload)
	if jsonInt(t, importPayload["connections_imported"]) != 1 || jsonInt(t, importPayload["models_imported"]) != 1 {
		t.Fatalf("expected v2 import counts for one connection and one model, got %+v", importPayload)
	}

	legacyPayload := cloneProfileBundleV2Payload(t, payload)
	legacyPayload["version"] = float64(1)
	legacyResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", legacyPayload, modelHeader(profileID))
	assertErrorResponse(t, legacyResponse, http.StatusBadRequest, "Unsupported profile config bundle version '1'; expected 2")
}

func newConfigBundleV2ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "configbundle_v2_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: configBundleSecretKey})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{
		Host:                      "127.0.0.1",
		Port:                      8000,
		AppEnv:                    config.EnvironmentProduction,
		DatabaseURL:               sharedPostgresHarness.connectionString(databaseName),
		SecretEncryptionKey:       configBundleSecretKey,
		ConfigBundleEncryptionKey: "configbundle-contract-bundle-key",
		CORSAllowedOrigins:        "http://localhost:5173,http://127.0.0.1:5173",
	}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)

	configBundleService, err := managementconfigbundle.NewService(settings, managementconfigbundle.Options{
		Pool:              pool,
		Now:               func() time.Time { return configBundleFixtureTime },
		BundleSecretKeyID: configBundleFixtureKeyID,
		BundleSecretEncrypter: func(value string) (string, error) {
			if value != configBundleOpenAISecret {
				return "", fmt.Errorf("unexpected bundle secret %q", value)
			}
			return "enc:gAAAAABlProfileV2OpenAI", nil
		},
	})
	if err != nil {
		t.Fatalf("build config bundle service: %v", err)
	}
	t.Cleanup(configBundleService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "configbundle-v2-contract-test", ConfigBundleService: configBundleService})
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
	return &contractHarness{client: client, conn: conn, dsn: settings.DatabaseURL, mailer: nil, server: server, service: nil, url: server.URL}
}

func seedConfigBundleV2Graph(t *testing.T, harness *contractHarness, profileID int) {
	t.Helper()
	now := configBundleFixtureTime
	openaiVendorID := modelLoadVendorIDByKey(t, harness, "openai")

	for _, statement := range []string{
		`DELETE FROM endpoint_fx_rate_settings WHERE profile_id = $1`,
		`DELETE FROM model_access_targets WHERE profile_id = $1`,
		`DELETE FROM connections WHERE profile_id = $1`,
		`DELETE FROM model_configs WHERE profile_id = $1`,
		`DELETE FROM pricing_templates WHERE profile_id = $1`,
		`DELETE FROM loadbalance_strategies WHERE profile_id = $1`,
		`DELETE FROM endpoints WHERE profile_id = $1`,
		`DELETE FROM header_blocklist_rules WHERE profile_id = $1 AND is_system = FALSE`,
		`DELETE FROM user_agent_client_rules WHERE profile_id = $1 AND is_system = FALSE`,
	} {
		if _, err := harness.conn.Exec(context.Background(), statement, profileID); err != nil {
			t.Fatalf("clear v2 fixture state with %q: %v", statement, err)
		}
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE user_settings SET report_currency_code = 'USD', report_currency_symbol = '$', timezone_preference = 'Europe/Helsinki', updated_at = $2 WHERE profile_id = $1`, profileID, now); err != nil {
		t.Fatalf("update user settings: %v", err)
	}

	apiKey, err := endpointdomain.EncryptSecret(configBundleOpenAISecret, configBundleSecretKey, func() time.Time { return now })
	if err != nil {
		t.Fatalf("encrypt endpoint secret: %v", err)
	}
	var endpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, 'Primary OpenAI', 'https://api.openai.com', $2, 0, $3, $3) RETURNING id`, profileID, apiKey, now).Scan(&endpointID); err != nil {
		t.Fatalf("insert endpoint: %v", err)
	}
	var pricingID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, 'OpenAI Standard', 'Example pricing', 'PER_1M', 'USD', '2.500000', '10.000000', '1.250000', '0.000000', '0.000000', 1, $2, $2) RETURNING id`, profileID, now).Scan(&pricingID); err != nil {
		t.Fatalf("insert pricing template: %v", err)
	}
	var strategyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, retry_max_attempts, ban_duration_seconds, created_at, updated_at) VALUES ($1, 'Default round robin', 'round-robin', ARRAY[429,500], 'off', 60000, 2.0, 0.2, 900000, 3, 0, $2, $2) RETURNING id`, profileID, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert strategy: %v", err)
	}

	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, 'openai', 'gpt-4o-mini', 'GPT 4o Mini', $3, TRUE, $4, $4) RETURNING id`, profileID, openaiVendorID, strategyID, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, $3, 60, 8, 4, 'responses_minimal', TRUE, 0, 'Primary OpenAI connection', 'openai', $4, 'healthy', NULL, NULL, $5, $5) RETURNING id`, profileID, endpointID, pricingID, `{"X-Prism-Trace":"v2-contract"}`, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, modelConfigID, connectionID, now); err != nil {
		t.Fatalf("insert access target: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, 'gpt-4o-mini', $2, '1.000000', $3, $3)`, profileID, endpointID, now); err != nil {
		t.Fatalf("insert fx mapping: %v", err)
	}

	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, 'Authorization', 'exact', 'authorization', TRUE, FALSE, $2, $2)`, profileID, now); err != nil {
		t.Fatalf("insert header rule: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, 'Acme Agent', 'acme-agent', TRUE, FALSE, $2, $2)`, profileID, now); err != nil {
		t.Fatalf("insert user-agent rule: %v", err)
	}
}

func assertProfileBundleV2Shape(t *testing.T, payload map[string]any) {
	t.Helper()
	if jsonInt(t, payload["version"]) != 2 || payload["bundle_kind"] != "profile_config" {
		t.Fatalf("expected profile_config v2 bundle, got %+v", payload)
	}
	connections := payload["connections"].([]any)
	if len(connections) != 1 {
		t.Fatalf("expected one top-level connection, got %+v", connections)
	}
	connection := asMap(t, connections[0])
	connectionRef := connection["ref"].(string)
	if !strings.HasPrefix(connectionRef, "openai-primary-openai") || connection["api_family"] != "openai" || connection["endpoint_name"] != "Primary OpenAI" {
		t.Fatalf("expected v2 standalone OpenAI connection export, got %+v", connection)
	}
	models := payload["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("expected one exported model, got %+v", models)
	}
	model := asMap(t, models[0])
	for _, removedKey := range []string{"model_type", "proxy_selection_strategy", "proxy_targets", "connections"} {
		if _, ok := model[removedKey]; ok {
			t.Fatalf("model export must not include removed key %q: %+v", removedKey, model)
		}
	}
	targets := model["access_targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("expected one access target, got %+v", targets)
	}
	target := asMap(t, targets[0])
	if target["target_type"] != "connection" || target["connection_ref"] != connectionRef || jsonInt(t, target["position"]) != 0 || target["is_enabled"] != true {
		t.Fatalf("expected v2 connection access target, got %+v", target)
	}
	settings := asMap(t, payload["profile_settings"])
	fxMappings := settings["endpoint_fx_mappings"].([]any)
	if len(fxMappings) != 1 || asMap(t, fxMappings[0])["connection_ref"] != connectionRef {
		t.Fatalf("expected v2 FX mapping keyed by connection_ref, got %+v", settings)
	}
}

func cloneProfileBundleV2Payload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	cloned := make(map[string]any, len(payload))
	for key, value := range payload {
		cloned[key] = value
	}
	return cloned
}

func configBundleHeadersWithPreviewToken(headers map[string]string, token string) map[string]string {
	merged := make(map[string]string, len(headers)+1)
	for key, value := range headers {
		merged[key] = value
	}
	merged["X-Prism-Preview-Token"] = token
	return merged
}
