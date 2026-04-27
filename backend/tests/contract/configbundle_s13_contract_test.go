package contract_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	managementconfigbundle "github.com/coachpo/prism/backend/internal/httpapi/management/configbundle"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type configBundleImportHarnessOptions struct {
	afterProfileImport func(context.Context, pgx.Tx) error
}

func newConfigBundleImportHarness(t *testing.T, options configBundleImportHarnessOptions) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "configbundle_import_contract_" + randomSuffix()
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
		BundleSecretDecrypter: func(value string) (string, error) {
			switch value {
			case "enc:gAAAAABlProfileFreezeOpenAI":
				return configBundleOpenAISecret, nil
			case "enc:gAAAAABlProfileFreezeAnthropic":
				return configBundleAnthropicSecret, nil
			default:
				return "", fmt.Errorf("unexpected bundle ciphertext %q", value)
			}
		},
		AfterProfileImport: options.afterProfileImport,
	})
	if err != nil {

		t.Fatalf("build config bundle service: %v", err)
	}
	t.Cleanup(configBundleService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "configbundle-import-contract-test", ConfigBundleService: configBundleService})
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

func mustCloneBundlePayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal bundle payload clone: %v", err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("unmarshal bundle payload clone: %v", err)
	}
	return cloned
}

func mutateReplacementProfileBundle(t *testing.T) map[string]any {
	t.Helper()
	payload := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
	endpoints := payload["endpoints"].([]any)
	openAIEndpoint := endpoints[0].(map[string]any)
	openAIEndpoint["name"] = "Imported OpenAI"
	openAIEndpoint["base_url"] = "https://imported.openai.invalid"
	openAIEndpoint["api_key_secret_ref"] = "endpoint:Imported OpenAI:api_key"

	models := payload["models"].([]any)
	nativeOpenAI := models[0].(map[string]any)
	nativeOpenAI["display_name"] = "Imported GPT-4o Native"
	nativeConnections := nativeOpenAI["connections"].([]any)
	nativeConnections[0].(map[string]any)["endpoint_name"] = "Imported OpenAI"

	profileSettings := payload["profile_settings"].(map[string]any)
	fxMappings := profileSettings["endpoint_fx_mappings"].([]any)
	fxMappings[0].(map[string]any)["endpoint_name"] = "Imported OpenAI"

	secretData := payload["secret_payload"].(map[string]any)
	entries := secretData["entries"].([]any)
	entries[0].(map[string]any)["ref"] = "endpoint:Imported OpenAI:api_key"
	return payload
}

func seedLegacyProfileImportState(t *testing.T, harness *contractHarness, profileID int) {
	t.Helper()
	vendorID := modelLoadVendorIDByKey(t, harness, "openai")
	strategyID := modelInsertLoadbalanceStrategy(t, harness, profileID, "legacy-import-extra")
	endpointID := modelInsertEndpoint(t, harness, profileID, "Legacy Import Endpoint", 99)
	modelConfigID := modelInsertModel(t, harness, profileID, &vendorID, "openai", "legacy-import-model", stringPtr("Legacy Import Model"), "native", &strategyID, true)
	connectionID := modelInsertConnection(t, harness, profileID, modelConfigID, endpointID, 0, true, nil)
	modelInsertFXRateSetting(t, harness, profileID, "legacy-import-model", endpointID, "1.234000")
	now := configBundleFixtureTime
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, 'prefix', 'x-legacy-import', TRUE, FALSE, $3, $3)`, profileID, "Legacy Import Header", now); err != nil {
		t.Fatalf("insert legacy import header rule: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, $2, $3, TRUE, FALSE, $4, $4)`, profileID, "Legacy Import Client", "legacy-import-client", now); err != nil {
		t.Fatalf("insert legacy import user-agent client rule: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO loadbalance_round_robin_state (profile_id, model_config_id, next_cursor, created_at, updated_at) VALUES ($1, $2, 3, $3, $3)`, profileID, modelConfigID, now); err != nil {
		t.Fatalf("insert legacy round robin state: %v", err)
	}
	_ = connectionID
}

func countRows(t *testing.T, harness *contractHarness, query string, args ...any) int {
	t.Helper()
	var count int
	if err := harness.conn.QueryRow(context.Background(), query, args...).Scan(&count); err != nil {
		t.Fatalf("count rows with query %q: %v", query, err)
	}
	return count
}

func insertVendorCatalogVendor(t *testing.T, harness *contractHarness, key string, name string, description *string, iconKey *string, auditEnabled bool, auditCaptureBodies bool) {
	t.Helper()
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO vendors (key, name, description, icon_key, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $7)`, key, name, description, iconKey, auditEnabled, auditCaptureBodies, configBundleFixtureTime); err != nil {
		t.Fatalf("insert vendor %q: %v", key, err)
	}
}

func vendorCatalogEntryByKey(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()
	vendors, ok := payload["vendors"].([]any)
	if !ok {
		t.Fatalf("expected vendors array in vendor catalog payload, got %+v", payload)
	}
	for _, raw := range vendors {
		item, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected vendor catalog entry map, got %T", raw)
		}
		if item["key"] == key {
			return item
		}
	}

	t.Fatalf("expected vendor catalog payload to include key %q, got %+v", key, vendors)
	return nil
}

func TestConfigBundlePreviewResponses(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", loadBundleFixture(t, "profile-v1-example.json"), nil)
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != true || previewPayload["version"] != float64(1) || previewPayload["bundle_kind"] != "profile_config" || previewPayload["secret_key_id"] != configBundleFixtureKeyID {
		t.Fatalf("expected ready v1 preview payload, got %+v", previewPayload)
	}
	if got := len(previewPayload["decryptable_secret_refs"].([]any)); got != 2 {
		t.Fatalf("expected 2 decryptable secret refs, got %+v", previewPayload)
	}
	if got := len(previewPayload["vendor_resolutions"].([]any)); got != 2 {
		t.Fatalf("expected 2 vendor resolutions, got %+v", previewPayload)
	}

	warningPayload := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
	warningPayload["vendor_refs"].([]any)[0].(map[string]any)["description_hint"] = "Drifted OpenAI vendor description"
	warningResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", warningPayload, nil)
	assertStatus(t, warningResponse, http.StatusOK)
	var warningPreview map[string]any
	decodeJSONResponse(t, warningResponse, &warningPreview)
	if warningPreview["ready"] != true || len(warningPreview["warnings"].([]any)) != 1 {
		t.Fatalf("expected non-blocking vendor warning preview, got %+v", warningPreview)
	}

	oldVersionPayload := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
	oldVersionPayload["version"] = 3
	oldVersionResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", oldVersionPayload, nil)
	assertErrorResponse(t, oldVersionResponse, http.StatusBadRequest, "Unsupported profile config bundle version '3'; expected 1")
}

func TestConfigBundleImportRejectsSupersededVersion(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleFixtureGraph(t, harness, profileID)

	oldVersionPayload := mustCloneBundlePayload(t, loadBundleFixture(t, "profile-v1-example.json"))
	oldVersionPayload["version"] = 3
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", oldVersionPayload, modelHeader(profileID))
	assertErrorResponse(t, importResponse, http.StatusBadRequest, "Unsupported profile config bundle version '3'; expected 1")
}

func TestVendorCatalogImportVersionContracts(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})

	exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/vendors/export", nil, nil)
	assertStatus(t, exportResponse, http.StatusOK)
	var canonicalPayload map[string]any
	decodeJSONResponse(t, exportResponse, &canonicalPayload)

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import/preview", canonicalPayload, nil)
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != true || previewPayload["version"] != float64(1) || previewPayload["bundle_kind"] != "vendor_catalog" || previewPayload["create_count"] != float64(0) || previewPayload["update_count"] != float64(0) {
		t.Fatalf("expected ready canonical vendor catalog preview payload, got %+v", previewPayload)
	}

	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import", canonicalPayload, nil)
	assertStatus(t, importResponse, http.StatusOK)
	var importPayload map[string]any
	decodeJSONResponse(t, importResponse, &importPayload)
	if importPayload["created_count"] != float64(0) || importPayload["updated_count"] != float64(0) {
		t.Fatalf("expected canonical vendor catalog import to be accepted without changes, got %+v", importPayload)
	}

	oldVersionPayload := mustCloneBundlePayload(t, canonicalPayload)
	oldVersionPayload["version"] = 2
	oldPreviewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import/preview", oldVersionPayload, nil)
	assertErrorResponse(t, oldPreviewResponse, http.StatusBadRequest, "Unsupported vendor catalog bundle version '2'; expected 1")

	oldImportResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import", oldVersionPayload, nil)
	assertErrorResponse(t, oldImportResponse, http.StatusBadRequest, "Unsupported vendor catalog bundle version '2'; expected 1")
}

func TestVendorCatalogImportPreviewAndImportCounts(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
	insertVendorCatalogVendor(t, harness, "openrouter", "OpenRouter", stringPtr("Shared vendor metadata"), stringPtr("openrouter"), false, true)

	exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/vendors/export", nil, nil)
	assertStatus(t, exportResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, exportResponse, &payload)

	openRouter := vendorCatalogEntryByKey(t, payload, "openrouter")
	openRouter["key"] = " OPENROUTER "
	openRouter["name"] = " OpenRouter HQ "
	openRouter["description"] = " Primary shared vendor metadata "
	openRouter["icon_key"] = " OPENROUTER-HQ "
	openRouter["audit_enabled"] = true
	openRouter["audit_capture_bodies"] = false

	payload["vendors"] = append(payload["vendors"].([]any), map[string]any{
		"key":                  " MISTRAL ",
		"name":                 " Mistral ",
		"description":          " European frontier model vendor ",
		"icon_key":             " MISTRAL ",
		"audit_enabled":        false,
		"audit_capture_bodies": true,
	})

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import/preview", payload, nil)
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != true || previewPayload["create_count"] != float64(1) || previewPayload["update_count"] != float64(1) {
		t.Fatalf("expected vendor catalog preview to report one create and one update, got %+v", previewPayload)
	}
	if got := len(previewPayload["blocking_errors"].([]any)); got != 0 {
		t.Fatalf("expected vendor catalog preview to have no blocking errors, got %+v", previewPayload)
	}

	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import", payload, nil)
	assertStatus(t, importResponse, http.StatusOK)
	var importPayload map[string]any
	decodeJSONResponse(t, importResponse, &importPayload)
	if importPayload["created_count"] != float64(1) || importPayload["updated_count"] != float64(1) {
		t.Fatalf("expected vendor catalog import to persist one create and one update, got %+v", importPayload)
	}

	afterExport := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/vendors/export", nil, nil)
	assertStatus(t, afterExport, http.StatusOK)
	var afterPayload map[string]any
	decodeJSONResponse(t, afterExport, &afterPayload)

	updatedOpenRouter := vendorCatalogEntryByKey(t, afterPayload, "openrouter")
	if updatedOpenRouter["name"] != "OpenRouter HQ" || updatedOpenRouter["description"] != "Primary shared vendor metadata" || updatedOpenRouter["icon_key"] != "openrouter-hq" || updatedOpenRouter["audit_enabled"] != true || updatedOpenRouter["audit_capture_bodies"] != false {
		t.Fatalf("expected normalized openrouter vendor update after import, got %+v", updatedOpenRouter)
	}
	createdMistral := vendorCatalogEntryByKey(t, afterPayload, "mistral")
	if createdMistral["name"] != "Mistral" || createdMistral["description"] != "European frontier model vendor" || createdMistral["icon_key"] != "mistral" || createdMistral["audit_enabled"] != false || createdMistral["audit_capture_bodies"] != true {
		t.Fatalf("expected normalized mistral vendor create after import, got %+v", createdMistral)
	}
}

func TestVendorCatalogImportRejectsReadonlyOverwrite(t *testing.T) {
	harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})

	beforeExport := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/vendors/export", nil, nil)
	assertStatus(t, beforeExport, http.StatusOK)
	var beforePayload map[string]any
	decodeJSONResponse(t, beforeExport, &beforePayload)

	readonlyPayload := mustCloneBundlePayload(t, beforePayload)
	openAI := vendorCatalogEntryByKey(t, readonlyPayload, "openai")
	openAI["name"] = "OpenAI Labs"
	openAI["audit_enabled"] = true
	openAI["audit_capture_bodies"] = false

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import/preview", readonlyPayload, nil)
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != false || previewPayload["create_count"] != float64(0) || previewPayload["update_count"] != float64(0) {
		t.Fatalf("expected readonly vendor preview rejection, got %+v", previewPayload)
	}
	blockingErrors := previewPayload["blocking_errors"].([]any)
	if len(blockingErrors) != 1 || blockingErrors[0] != "Readonly system vendor 'openai' cannot be overwritten by vendor catalog import" {
		t.Fatalf("expected readonly vendor blocking error, got %+v", previewPayload)
	}

	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/vendors/import", readonlyPayload, nil)
	assertErrorResponse(t, importResponse, http.StatusBadRequest, "Readonly system vendor 'openai' cannot be overwritten by vendor catalog import")

	afterExport := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/vendors/export", nil, nil)
	assertStatus(t, afterExport, http.StatusOK)
	var afterPayload map[string]any
	decodeJSONResponse(t, afterExport, &afterPayload)
	if !reflect.DeepEqual(beforePayload, afterPayload) {
		t.Fatalf("expected readonly vendor rejection to preserve prior vendor catalog\nwant: %+v\n got: %+v", beforePayload, afterPayload)
	}
}

func TestConfigBundleImportReplace(t *testing.T) {
	t.Run("success path replaces profile-owned config", func(t *testing.T) {
		harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{})
		profileID := modelLoadDefaultProfileID(t, harness)
		seedConfigBundleFixtureGraph(t, harness, profileID)
		seedLegacyProfileImportState(t, harness, profileID)
		replacementPayload := mutateReplacementProfileBundle(t)

		importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", replacementPayload, modelHeader(profileID))
		assertStatus(t, importResponse, http.StatusOK)
		var importPayload map[string]any
		decodeJSONResponse(t, importResponse, &importPayload)
		if importPayload["endpoints_imported"] != float64(2) || importPayload["models_imported"] != float64(3) || importPayload["connections_imported"] != float64(2) {
			t.Fatalf("expected replace import counts, got %+v", importPayload)
		}

		exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
		assertStatus(t, exportResponse, http.StatusOK)
		var exportPayload map[string]any
		decodeJSONResponse(t, exportResponse, &exportPayload)
		if !reflect.DeepEqual(exportPayload, replacementPayload) {
			t.Fatalf("expected exported profile bundle to match replaced payload\nwant: %+v\n got: %+v", replacementPayload, exportPayload)
		}

		if countRows(t, harness, `SELECT COUNT(*) FROM endpoints WHERE profile_id = $1 AND name = 'Legacy Import Endpoint'`, profileID) != 0 {
			t.Fatal("expected destructive replace to remove legacy endpoint")
		}
		if countRows(t, harness, `SELECT COUNT(*) FROM header_blocklist_rules WHERE profile_id = $1 AND name = 'Legacy Import Header'`, profileID) != 0 {
			t.Fatal("expected destructive replace to remove legacy header rule")
		}
		if countRows(t, harness, `SELECT COUNT(*) FROM user_agent_client_rules WHERE profile_id = $1 AND name = 'Legacy Import Client'`, profileID) != 0 {
			t.Fatal("expected destructive replace to remove legacy user-agent client rule")
		}
		if countRows(t, harness, `SELECT COUNT(*) FROM loadbalance_round_robin_state WHERE profile_id = $1`, profileID) != 0 {
			t.Fatal("expected destructive replace to clear round robin runtime state")
		}
	})

	t.Run("rollback restores prior profile state", func(t *testing.T) {
		const injectedFailure = "Injected rollback failure"
		harness := newConfigBundleImportHarness(t, configBundleImportHarnessOptions{afterProfileImport: func(_ context.Context, _ pgx.Tx) error {
			return &profiledomain.HTTPError{StatusCode: http.StatusConflict, Detail: injectedFailure}
		}})
		profileID := modelLoadDefaultProfileID(t, harness)
		seedConfigBundleFixtureGraph(t, harness, profileID)
		beforeExport := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
		assertStatus(t, beforeExport, http.StatusOK)
		var beforePayload map[string]any
		decodeJSONResponse(t, beforeExport, &beforePayload)

		replacementPayload := mutateReplacementProfileBundle(t)
		failedImport := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", replacementPayload, modelHeader(profileID))
		assertErrorResponse(t, failedImport, http.StatusConflict, injectedFailure)

		afterExport := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
		assertStatus(t, afterExport, http.StatusOK)
		var afterPayload map[string]any
		decodeJSONResponse(t, afterExport, &afterPayload)
		if !reflect.DeepEqual(beforePayload, afterPayload) {
			t.Fatalf("expected rollback to preserve pre-import profile bundle\nwant: %+v\n got: %+v", beforePayload, afterPayload)
		}
	})
}
