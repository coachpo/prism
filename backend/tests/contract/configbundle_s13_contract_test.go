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
	assertStatus(t, oldVersionResponse, http.StatusOK)
	var oldVersionPreview map[string]any
	decodeJSONResponse(t, oldVersionResponse, &oldVersionPreview)
	if oldVersionPreview["ready"] != false || oldVersionPreview["blocking_errors"].([]any)[0] != "Unsupported profile config bundle version '3'; expected 1" {
		t.Fatalf("expected explicit v3 rejection in preview response, got %+v", oldVersionPreview)
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
