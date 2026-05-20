package contract_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	configBundleSecretKey       = "configbundle-contract-secret"
	configBundleFixtureKeyID    = "sha256:go-rewrite-s1-contract-freeze"
	configBundleOpenAISecret    = "fixture-openai-secret"
	configBundleAnthropicSecret = "fixture-anthropic-secret"
)

var configBundleFixtureTime = time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)

func TestConfigBundleV1Export(t *testing.T) {
	harness := newConfigBundleContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)

	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	want := redactedProfileBundleFixture(t)
	assertJSONMatchesFixture(t, payload, want)
}

func TestConfigBundleV1ExportCarriesPortableTrafficRules(t *testing.T) {
	harness := newConfigBundleContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)

	var payload map[string]any
	decodeJSONResponse(t, response, &payload)

	headerRules, ok := payload["header_blocklist_rules"].([]any)
	if !ok || len(headerRules) != 1 {
		t.Fatalf("expected one exported header blocklist rule, got %+v", payload["header_blocklist_rules"])
	}
	headerRule := asMap(t, headerRules[0])
	if headerRule["name"] != "Authorization" || headerRule["match_type"] != "exact" || headerRule["pattern"] != "authorization" || headerRule["enabled"] != true {
		t.Fatalf("expected exported header blocklist rule contents, got %+v", headerRule)
	}

	userAgentRules, ok := payload["user_agent_client_rules"].([]any)
	if !ok || len(userAgentRules) != 1 {
		t.Fatalf("expected one exported user-agent client rule, got %+v", payload["user_agent_client_rules"])
	}
	userAgentRule := asMap(t, userAgentRules[0])
	if userAgentRule["name"] != "Acme Agent" || userAgentRule["pattern"] != "acme-agent" || userAgentRule["enabled"] != true {
		t.Fatalf("expected exported user-agent client rule contents, got %+v", userAgentRule)
	}
}

func TestConfigBundleV1ExportCarriesProxySelectorAndTargetMetadata(t *testing.T) {
	harness := newConfigBundleContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)

	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	nativeModel := profileBundleModel(t, payload, "gpt-4o-native")
	if value, ok := nativeModel["proxy_selection_strategy"]; !ok || value != nil {
		t.Fatalf("expected native model proxy_selection_strategy to be null, got %+v", nativeModel)
	}

	proxyModel := profileBundleModel(t, payload, "gpt-4o")
	if proxyModel["proxy_selection_strategy"] != "weighted_static" {
		t.Fatalf("expected proxy model selector weighted_static, got %+v", proxyModel)
	}
	target := profileBundleProxyTarget(t, proxyModel, 0)
	if target["target_model_id"] != "gpt-4o-native" || jsonInt(t, target["position"]) != 0 || jsonInt(t, target["weight"]) != 7 || jsonInt(t, target["target_priority"]) != 2 {
		t.Fatalf("expected exported proxy target metadata, got %+v", target)
	}
}

func TestConfigBundleImportPersistsProxySelectorAndTargetMetadata(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
	profileID := modelLoadDefaultProfileID(t, harness)
	bundle := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
	proxyModel := profileBundleModel(t, bundle, "gpt-4o")
	proxyModel["proxy_selection_strategy"] = "  WeIgHtEd_StAtIc  "
	target := profileBundleProxyTarget(t, proxyModel, 0)
	target["weight"] = 11
	target["target_priority"] = 4

	previewToken := readyProfileImportPreviewToken(t, harness, profileID, bundle)
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", bundle, mergeHeaders(modelHeader(profileID), previewTokenHeaders(previewToken)))
	assertStatus(t, importResponse, http.StatusOK)

	var selector string
	if err := harness.conn.QueryRow(context.Background(), `SELECT proxy_selection_strategy FROM model_configs WHERE profile_id = $1 AND model_id = 'gpt-4o'`, profileID).Scan(&selector); err != nil {
		t.Fatalf("load imported proxy selector: %v", err)
	}
	if selector != "weighted_static" {
		t.Fatalf("expected normalized proxy selector weighted_static, got %q", selector)
	}

	var weight int
	var targetPriority int
	if err := harness.conn.QueryRow(context.Background(), `SELECT model_proxy_targets.weight, model_proxy_targets.target_priority FROM model_proxy_targets JOIN model_configs AS source_models ON source_models.id = model_proxy_targets.source_model_config_id JOIN model_configs AS target_models ON target_models.id = model_proxy_targets.target_model_config_id WHERE source_models.profile_id = $1 AND source_models.model_id = 'gpt-4o' AND target_models.model_id = 'gpt-4o-native'`, profileID).Scan(&weight, &targetPriority); err != nil {
		t.Fatalf("load imported proxy target metadata: %v", err)
	}
	if weight != 11 || targetPriority != 4 {
		t.Fatalf("expected imported target metadata weight=11 target_priority=4, got weight=%d target_priority=%d", weight, targetPriority)
	}
}

func TestConfigBundleImportRejectsInvalidProxySelectorAndTargetMetadata(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
	profileID := modelLoadDefaultProfileID(t, harness)

	cases := []struct {
		name   string
		mutate func(map[string]any)
		detail string
	}{
		{
			name: "missing proxy selector",
			mutate: func(payload map[string]any) {
				delete(profileBundleModel(t, payload, "gpt-4o"), "proxy_selection_strategy")
			},
			detail: "Proxy model 'gpt-4o' must include proxy_selection_strategy",
		},
		{
			name: "null proxy selector",
			mutate: func(payload map[string]any) {
				profileBundleModel(t, payload, "gpt-4o")["proxy_selection_strategy"] = nil
			},
			detail: "Proxy model 'gpt-4o' must include proxy_selection_strategy",
		},
		{
			name: "empty proxy selector",
			mutate: func(payload map[string]any) {
				profileBundleModel(t, payload, "gpt-4o")["proxy_selection_strategy"] = "   "
			},
			detail: "Proxy model 'gpt-4o' must include proxy_selection_strategy",
		},
		{
			name: "unknown proxy selector",
			mutate: func(payload map[string]any) {
				profileBundleModel(t, payload, "gpt-4o")["proxy_selection_strategy"] = "unknown_selector"
			},
			detail: "Proxy model 'gpt-4o' has unknown proxy_selection_strategy 'unknown_selector'",
		},
		{
			name: "native selector",
			mutate: func(payload map[string]any) {
				profileBundleModel(t, payload, "gpt-4o-native")["proxy_selection_strategy"] = "ordered_fallback"
			},
			detail: "Native model 'gpt-4o-native' must not include proxy_selection_strategy",
		},
		{
			name: "missing target weight",
			mutate: func(payload map[string]any) {
				delete(profileBundleProxyTarget(t, profileBundleModel(t, payload, "gpt-4o"), 0), "weight")
			},
			detail: "Proxy model 'gpt-4o' target 'gpt-4o-native' must include weight",
		},
		{
			name: "null target weight",
			mutate: func(payload map[string]any) {
				profileBundleProxyTarget(t, profileBundleModel(t, payload, "gpt-4o"), 0)["weight"] = nil
			},
			detail: "Proxy model 'gpt-4o' target 'gpt-4o-native' must include weight",
		},
		{
			name: "zero target weight",
			mutate: func(payload map[string]any) {
				profileBundleProxyTarget(t, profileBundleModel(t, payload, "gpt-4o"), 0)["weight"] = 0
			},
			detail: "Proxy model 'gpt-4o' target 'gpt-4o-native' has invalid weight '0'",
		},
		{
			name: "missing target priority",
			mutate: func(payload map[string]any) {
				delete(profileBundleProxyTarget(t, profileBundleModel(t, payload, "gpt-4o"), 0), "target_priority")
			},
			detail: "Proxy model 'gpt-4o' target 'gpt-4o-native' must include target_priority",
		},
		{
			name: "null target priority",
			mutate: func(payload map[string]any) {
				profileBundleProxyTarget(t, profileBundleModel(t, payload, "gpt-4o"), 0)["target_priority"] = nil
			},
			detail: "Proxy model 'gpt-4o' target 'gpt-4o-native' must include target_priority",
		},
		{
			name: "negative target priority",
			mutate: func(payload map[string]any) {
				profileBundleProxyTarget(t, profileBundleModel(t, payload, "gpt-4o"), 0)["target_priority"] = -1
			},
			detail: "Proxy model 'gpt-4o' target 'gpt-4o-native' has invalid target_priority '-1'",
		},
	}

	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			payload := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
			item.mutate(payload)
			assertProfileImportRejected(t, harness, profileID, payload, item.detail)
		})
	}
}

func TestConfigBundleDownloadHeaders(t *testing.T) {
	harness := newConfigBundleContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	if got := response.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("expected profile export content type application/json; charset=utf-8, got %q", got)
	}
	if got := response.Header.Get("Content-Disposition"); got != "attachment; filename=\"prism-profile-config-v1-2026-04-18.json\"" {
		t.Fatalf("expected profile export filename header, got %q", got)
	}
}

func TestConfigBundleV1ExportAllowsNullOptionalPricingFields(t *testing.T) {
	harness := newConfigBundleContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	insertContractPricingTemplate(t, harness, profileID, "Null Optional Pricing")

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)

	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	items, ok := payload["pricing_templates"].([]any)
	if !ok {
		t.Fatalf("expected pricing_templates array, got %+v", payload)
	}

	for _, raw := range items {
		item := asMap(t, raw)
		if item["name"] != "Null Optional Pricing" {
			continue
		}
		if item["cached_input_price"] != nil || item["cache_creation_price"] != nil || item["reasoning_price"] != nil {
			t.Fatalf("expected null optional pricing fields in export, got %+v", item)
		}
		return
	}

	t.Fatalf("expected exported pricing template %q, got %+v", "Null Optional Pricing", items)
}

func TestConfigBundleImportNormalizesOptionalPricingFields(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
	profileID := modelLoadDefaultProfileID(t, harness)
	bundle := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))

	templates, ok := bundle["pricing_templates"].([]any)
	if !ok || len(templates) == 0 {
		t.Fatalf("expected pricing_templates array, got %+v", bundle)
	}
	primary := asMap(t, templates[0])
	primary["cached_input_price"] = "  1.250000  "
	primary["cache_creation_price"] = nil
	primary["reasoning_price"] = "   "
	bundle["pricing_templates"] = append(templates, map[string]any{
		"name":                  "Omitted Optional Pricing",
		"description":           nil,
		"pricing_unit":          "PER_1M",
		"pricing_currency_code": "USD",
		"input_price":           "1",
		"output_price":          "2",
		"version":               1,
	})

	previewToken := readyProfileImportPreviewToken(t, harness, profileID, bundle)
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", bundle, mergeHeaders(modelHeader(profileID), previewTokenHeaders(previewToken)))
	assertStatus(t, importResponse, http.StatusOK)

	assertPricingTemplateOptionalPrices(t, harness, profileID, "OpenAI Standard", stringPtr("1.250000"), nil, nil)
	assertPricingTemplateOptionalPrices(t, harness, profileID, "Omitted Optional Pricing", nil, nil, nil)

	exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, exportResponse, http.StatusOK)
	var exported map[string]any
	decodeJSONResponse(t, exportResponse, &exported)
	if jsonInt(t, exported["version"]) != 1 {
		t.Fatalf("expected round-tripped profile bundle version 1, got %+v", exported["version"])
	}
	assertProfileBundleOptionalPricingFields(t, exported, "OpenAI Standard", stringPtr("1.250000"), nil, nil)
	assertProfileBundleOptionalPricingFields(t, exported, "Omitted Optional Pricing", nil, nil, nil)
}

func TestConfigBundleImportRejectsInvalidOptionalPricingFields(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
	profileID := modelLoadDefaultProfileID(t, harness)

	cases := []string{"cached_input_price", "cache_creation_price", "reasoning_price"}
	for _, field := range cases {
		t.Run(field, func(t *testing.T) {
			bundle := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
			templates := bundle["pricing_templates"].([]any)
			template := asMap(t, templates[0])
			template[field] = "not-a-decimal"
			assertProfileImportRejected(t, harness, profileID, bundle, fmt.Sprintf("Pricing template 'OpenAI Standard' %s must be a non-negative decimal string", field))
		})
	}
}

func TestVendorCatalogV1Export(t *testing.T) {
	harness := newConfigBundleContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/vendors/export", nil, nil)
	assertStatus(t, response, http.StatusOK)

	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	want := loadBundleFixture(t, "vendor-v1-example.json")
	assertJSONMatchesFixture(t, payload, want)
}

func TestVendorCatalogDownloadHeaders(t *testing.T) {
	harness := newConfigBundleContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/vendors/export", nil, nil)
	assertStatus(t, response, http.StatusOK)
	if got := response.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("expected vendor export content type application/json; charset=utf-8, got %q", got)
	}
	if got := response.Header.Get("Content-Disposition"); got != "attachment; filename=\"prism-vendor-catalog-v1-2026-04-18.json\"" {
		t.Fatalf("expected vendor export filename header, got %q", got)
	}
}

func newConfigBundleContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "configbundle_contract_" + randomSuffix()
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
			switch value {
			case configBundleOpenAISecret:
				return "enc:gAAAAABlProfileFreezeOpenAI", nil
			case configBundleAnthropicSecret:
				return "enc:gAAAAABlProfileFreezeAnthropic", nil
			default:
				return "", fmt.Errorf("unexpected bundle secret %q", value)
			}
		},
	})
	if err != nil {
		t.Fatalf("build config bundle service: %v", err)
	}
	t.Cleanup(configBundleService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "configbundle-contract-test", ConfigBundleService: configBundleService})
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

func seedConfigBundleFixtureGraph(t *testing.T, harness *contractHarness, profileID int) {
	t.Helper()
	now := configBundleFixtureTime
	openaiVendorID := modelLoadVendorIDByKey(t, harness, "openai")
	anthropicVendorID := modelLoadVendorIDByKey(t, harness, "anthropic")

	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM model_proxy_targets`); err != nil {
		t.Fatalf("clear model proxy targets: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM connections WHERE profile_id = $1`, profileID); err != nil {
		t.Fatalf("clear connections: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM model_configs WHERE profile_id = $1`, profileID); err != nil {
		t.Fatalf("clear model configs: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM endpoint_fx_rate_settings WHERE profile_id = $1`, profileID); err != nil {
		t.Fatalf("clear endpoint fx mappings: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM pricing_templates WHERE profile_id = $1`, profileID); err != nil {
		t.Fatalf("clear pricing templates: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM loadbalance_strategies WHERE profile_id = $1`, profileID); err != nil {
		t.Fatalf("clear loadbalance strategies: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM endpoints WHERE profile_id = $1`, profileID); err != nil {
		t.Fatalf("clear endpoints: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM header_blocklist_rules WHERE profile_id = $1 AND is_system = FALSE`, profileID); err != nil {
		t.Fatalf("clear profile header blocklist rules: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM user_agent_client_rules WHERE profile_id = $1 AND is_system = FALSE`, profileID); err != nil {
		t.Fatalf("clear profile user-agent client rules: %v", err)
	}

	if _, err := harness.conn.Exec(context.Background(), `DELETE FROM vendors WHERE key = 'gemini'`); err != nil {
		t.Fatalf("delete gemini vendor for fixture alignment: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE user_settings SET report_currency_code = 'USD', report_currency_symbol = '$', timezone_preference = 'Europe/Helsinki', updated_at = $2 WHERE profile_id = $1`, profileID, now); err != nil {
		t.Fatalf("update user settings: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE vendors SET description = $2, icon_key = $3, audit_enabled = $4, audit_capture_bodies = $5, updated_at = $6 WHERE key = $1`, "openai", "OpenAI API (GPT models)", "openai", false, true, now); err != nil {
		t.Fatalf("update openai vendor: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `UPDATE vendors SET description = $2, icon_key = $3, audit_enabled = $4, audit_capture_bodies = $5, updated_at = $6 WHERE key = $1`, "anthropic", "Anthropic Claude models", "anthropic", true, false, now); err != nil {
		t.Fatalf("update anthropic vendor: %v", err)
	}

	openAIAPIKey, err := endpointdomain.EncryptSecret(configBundleOpenAISecret, configBundleSecretKey, func() time.Time { return now })
	if err != nil {
		t.Fatalf("encrypt openai endpoint secret: %v", err)
	}
	anthropicAPIKey, err := endpointdomain.EncryptSecret(configBundleAnthropicSecret, configBundleSecretKey, func() time.Time { return now })
	if err != nil {
		t.Fatalf("encrypt anthropic endpoint secret: %v", err)
	}

	var openAIEndpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, profileID, "Primary OpenAI", "https://api.openai.com", openAIAPIKey, 0, now).Scan(&openAIEndpointID); err != nil {
		t.Fatalf("insert openai endpoint: %v", err)
	}
	var anthropicEndpointID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO endpoints (profile_id, name, base_url, api_key, position, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6) RETURNING id`, profileID, "Primary Anthropic", "https://api.anthropic.com", anthropicAPIKey, 1, now).Scan(&anthropicEndpointID); err != nil {
		t.Fatalf("insert anthropic endpoint: %v", err)
	}

	var pricingTemplateID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO pricing_templates (profile_id, name, description, pricing_unit, pricing_currency_code, input_price, output_price, cached_input_price, cache_creation_price, reasoning_price, version, created_at, updated_at) VALUES ($1, $2, $3, 'PER_1M', 'USD', '2.500000', '10.000000', '1.250000', '0.000000', '0.000000', 1, $4, $4) RETURNING id`, profileID, "OpenAI Standard", "Example per-1M pricing", now).Scan(&pricingTemplateID); err != nil {
		t.Fatalf("insert pricing template: %v", err)
	}

	legacyAutoRecovery := mustModelJSON(t, map[string]any{
		"mode":         "enabled",
		"status_codes": []int{403, 422, 429, 500, 502, 503, 504, 529},
		"cooldown":     map[string]any{"base_seconds": 45, "failure_threshold": 4, "backoff_multiplier": 3.5, "max_cooldown_seconds": 720},
		"ban":          map[string]any{"mode": "temporary", "max_cooldown_strikes_before_ban": 3, "ban_duration_seconds": 1800},
	})
	adaptiveRoutingPolicy := mustModelJSON(t, map[string]any{
		"kind":              "adaptive",
		"routing_objective": "minimize_latency",
		"hedge":             map[string]any{"enabled": true, "delay_ms": 1500, "max_additional_attempts": 1},
		"circuit_breaker":   map[string]any{"failure_status_codes": []int{403, 422, 429, 500, 502, 503, 504, 529}, "base_open_seconds": 45, "failure_threshold": 4, "backoff_multiplier": 3.5, "max_open_seconds": 720, "ban_mode": "temporary", "max_open_strikes_before_ban": 3, "ban_duration_seconds": 1800},
		"admission":         map[string]any{"respect_qps_limit": true, "respect_in_flight_limits": true},
	})

	var legacyStrategyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at) VALUES ($1, $2, 'legacy', 'round-robin', $3::jsonb, NULL, $4, $4) RETURNING id`, profileID, "legacy-primary", legacyAutoRecovery, now).Scan(&legacyStrategyID); err != nil {
		t.Fatalf("insert legacy strategy: %v", err)
	}
	var adaptiveStrategyID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, strategy_type, legacy_strategy_type, auto_recovery, routing_policy, created_at, updated_at) VALUES ($1, $2, 'adaptive', NULL, NULL, $3::jsonb, $4, $4) RETURNING id`, profileID, "adaptive-primary", adaptiveRoutingPolicy, now).Scan(&adaptiveStrategyID); err != nil {
		t.Fatalf("insert adaptive strategy: %v", err)
	}

	var nativeOpenAIModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, 'openai', $3, $4, 'native', $5, TRUE, $6, $6) RETURNING id`, profileID, openaiVendorID, "gpt-4o-native", "GPT-4o Native", legacyStrategyID, now).Scan(&nativeOpenAIModelID); err != nil {
		t.Fatalf("insert native openai model: %v", err)
	}
	var nativeAnthropicModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, 'anthropic', $3, $4, 'native', $5, TRUE, $6, $6) RETURNING id`, profileID, anthropicVendorID, "claude-3-5-sonnet", "Claude 3.5 Sonnet", adaptiveStrategyID, now).Scan(&nativeAnthropicModelID); err != nil {
		t.Fatalf("insert native anthropic model: %v", err)
	}
	var proxyModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, model_type, proxy_selection_strategy, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, 'openai', $3, $4, 'proxy', 'weighted_static', NULL, TRUE, $5, $5) RETURNING id`, profileID, openaiVendorID, "gpt-4o", "GPT-4o Proxy", now).Scan(&proxyModelID); err != nil {
		t.Fatalf("insert proxy model: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_proxy_targets (source_model_config_id, target_model_config_id, position, weight, target_priority) VALUES ($1, $2, 0, 7, 2)`, proxyModelID, nativeOpenAIModelID); err != nil {
		t.Fatalf("insert proxy target: %v", err)
	}

	openAIHeaders := mustModelJSON(t, map[string]any{"X-Prism-Trace": "freeze-s1", "OpenAI-Beta": "assistants=v2"})
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO connections (profile_id, model_config_id, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, $4, 60, 8, 4, 'responses_minimal', TRUE, 0, $5, 'openai', $6::jsonb, 'healthy', NULL, NULL, $7, $7)`, profileID, nativeOpenAIModelID, openAIEndpointID, pricingTemplateID, "Primary OpenAI connection", openAIHeaders, now); err != nil {
		t.Fatalf("insert openai connection: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO connections (profile_id, model_config_id, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, $2, $3, NULL, 30, 6, 2, NULL, TRUE, 0, $4, 'anthropic', NULL, 'healthy', NULL, NULL, $5, $5)`, profileID, nativeAnthropicModelID, anthropicEndpointID, "Primary Anthropic connection", now); err != nil {
		t.Fatalf("insert anthropic connection: %v", err)
	}

	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, $2, $3, '1.000000', $4, $4)`, profileID, "gpt-4o-native", openAIEndpointID, now); err != nil {
		t.Fatalf("insert endpoint fx mapping: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, 'exact', 'authorization', TRUE, FALSE, $3, $3)`, profileID, "Authorization", now); err != nil {
		t.Fatalf("insert profile header blocklist rule: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, TRUE, FALSE, $4, $4)`, profileID, "Acme Agent", "acme-agent", now); err != nil {
		t.Fatalf("insert profile user-agent client rule: %v", err)
	}
}

func loadBundleFixture(t *testing.T, fileName string) map[string]any {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve config bundle contract test path")
	}
	fixturePath := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "testdata", "bundles", fileName))
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read bundle fixture %s: %v", fileName, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("decode bundle fixture %s: %v", fileName, err)
	}
	return payload
}

func redactedProfileBundleFixture(t *testing.T) map[string]any {
	t.Helper()
	payload := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
	endpoints, ok := payload["endpoints"].([]any)
	if !ok {
		t.Fatalf("expected endpoints array in profile fixture, got %+v", payload)
	}
	for _, raw := range endpoints {
		endpoint, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected endpoint fixture entry map, got %T", raw)
		}
		endpoint["api_key_secret_ref"] = nil
	}
	secretPayload, ok := payload["secret_payload"].(map[string]any)
	if !ok {
		t.Fatalf("expected secret payload map in profile fixture, got %+v", payload)
	}
	secretPayload["entries"] = []any{}
	return payload
}

func profileBundlePricingTemplate(t *testing.T, payload map[string]any, name string) map[string]any {
	t.Helper()
	templates, ok := payload["pricing_templates"].([]any)
	if !ok {
		t.Fatalf("expected pricing_templates array in profile bundle, got %+v", payload)
	}
	for _, raw := range templates {
		template := asMap(t, raw)
		if template["name"] == name {
			return template
		}
	}
	t.Fatalf("expected pricing template %q in profile bundle, got %+v", name, templates)
	return nil
}

func profileBundleModel(t *testing.T, payload map[string]any, modelID string) map[string]any {
	t.Helper()
	models, ok := payload["models"].([]any)
	if !ok {
		t.Fatalf("expected models array in profile bundle, got %+v", payload)
	}
	for _, raw := range models {
		model := asMap(t, raw)
		if model["model_id"] == modelID {
			return model
		}
	}
	t.Fatalf("expected model %q in profile bundle, got %+v", modelID, models)
	return nil
}

func profileBundleProxyTarget(t *testing.T, model map[string]any, index int) map[string]any {
	t.Helper()
	proxyTargets, ok := model["proxy_targets"].([]any)
	if !ok || index < 0 || index >= len(proxyTargets) {
		t.Fatalf("expected proxy target index %d in model, got %+v", index, model)
	}
	return asMap(t, proxyTargets[index])
}

func readyProfileImportPreviewToken(t *testing.T, harness *contractHarness, profileID int, bundle map[string]any) string {
	t.Helper()
	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", bundle, modelHeader(profileID))
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != true || previewPayload["preview_token"] == "" {
		t.Fatalf("expected ready profile import preview, got %+v", previewPayload)
	}
	return previewPayload["preview_token"].(string)
}

func assertProfileImportRejected(t *testing.T, harness *contractHarness, profileID int, bundle map[string]any, detail string) {
	t.Helper()
	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", bundle, modelHeader(profileID))
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	blockingErrors, ok := previewPayload["blocking_errors"].([]any)
	if previewPayload["ready"] != false || !ok || len(blockingErrors) != 1 || blockingErrors[0] != detail {
		t.Fatalf("expected profile import preview rejection %q, got %+v", detail, previewPayload)
	}
	previewToken, ok := previewPayload["preview_token"].(string)
	if !ok || previewToken == "" {
		t.Fatalf("expected rejected preview to still include preview token, got %+v", previewPayload)
	}

	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", bundle, mergeHeaders(modelHeader(profileID), previewTokenHeaders(previewToken)))
	assertErrorResponse(t, importResponse, http.StatusBadRequest, detail)
}

func assertProfileBundleOptionalPricingFields(t *testing.T, payload map[string]any, name string, wantCachedInputPrice *string, wantCacheCreationPrice *string, wantReasoningPrice *string) {
	t.Helper()
	template := profileBundlePricingTemplate(t, payload, name)
	assertOptionalBundleString(t, template["cached_input_price"], wantCachedInputPrice, "cached_input_price")
	assertOptionalBundleString(t, template["cache_creation_price"], wantCacheCreationPrice, "cache_creation_price")
	assertOptionalBundleString(t, template["reasoning_price"], wantReasoningPrice, "reasoning_price")
}

func assertOptionalBundleString(t *testing.T, got any, want *string, fieldName string) {
	t.Helper()
	if want == nil {
		if got != nil {
			t.Fatalf("expected exported %s to be null, got %+v", fieldName, got)
		}
		return
	}
	if got != *want {
		t.Fatalf("expected exported %s to be %q, got %+v", fieldName, *want, got)
	}
}

func assertPricingTemplateOptionalPrices(t *testing.T, harness *contractHarness, profileID int, name string, wantCachedInputPrice *string, wantCacheCreationPrice *string, wantReasoningPrice *string) {
	t.Helper()
	var cachedInputPrice sql.NullString
	var cacheCreationPrice sql.NullString
	var reasoningPrice sql.NullString
	if err := harness.conn.QueryRow(context.Background(), `SELECT cached_input_price, cache_creation_price, reasoning_price FROM pricing_templates WHERE profile_id = $1 AND name = $2`, profileID, name).Scan(&cachedInputPrice, &cacheCreationPrice, &reasoningPrice); err != nil {
		t.Fatalf("load pricing template optional prices for %q: %v", name, err)
	}
	assertNullStringValue(t, cachedInputPrice, wantCachedInputPrice, "cached_input_price")
	assertNullStringValue(t, cacheCreationPrice, wantCacheCreationPrice, "cache_creation_price")
	assertNullStringValue(t, reasoningPrice, wantReasoningPrice, "reasoning_price")
}

func assertNullStringValue(t *testing.T, got sql.NullString, want *string, fieldName string) {
	t.Helper()
	if want == nil {
		if got.Valid {
			t.Fatalf("expected %s to be NULL, got %q", fieldName, got.String)
		}
		return
	}
	if !got.Valid || got.String != *want {
		t.Fatalf("expected %s to be %q, got valid=%t value=%q", fieldName, *want, got.Valid, got.String)
	}
}

func assertJSONMatchesFixture(t *testing.T, got map[string]any, want map[string]any) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshal actual payload: %v", err)
	}
	wantJSON, err := json.MarshalIndent(want, "", "  ")
	if err != nil {
		t.Fatalf("marshal expected payload: %v", err)
	}
	t.Fatalf("bundle payload mismatch\nwant:\n%s\n\ngot:\n%s", string(wantJSON), string(gotJSON))
}
