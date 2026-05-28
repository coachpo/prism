package contract_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	managementvendors "github.com/coachpo/prism/backend/internal/httpapi/management/vendors"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformhttp "github.com/coachpo/prism/backend/internal/platform/http"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

func TestVendorCRUD(t *testing.T) {
	harness := newVendorContractHarness(t)

	listResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/vendors", nil, nil)
	assertStatus(t, listResponse, http.StatusOK)
	var listed []map[string]any
	decodeJSONResponse(t, listResponse, &listed)
	if len(listed) < 3 {
		t.Fatalf("expected startup-backed vendor list to include canonical vendors, got %+v", listed)
	}

	openaiVendor := vendorListItemByKey(t, listed, "openai")
	anthropicVendor := vendorListItemByKey(t, listed, "anthropic")
	geminiVendor := vendorListItemByKey(t, listed, "gemini")

	for _, vendor := range []map[string]any{openaiVendor, anthropicVendor, geminiVendor} {
		if vendor["is_readonly"] != true {
			t.Fatalf("expected canonical vendor to be readonly, got %+v", vendor)
		}
	}

	openaiID := jsonInt(t, openaiVendor["id"])
	getReadonly := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/vendors/%d", openaiID), nil, nil)
	assertStatus(t, getReadonly, http.StatusOK)
	var readonlyPayload map[string]any
	decodeJSONResponse(t, getReadonly, &readonlyPayload)
	if readonlyPayload["key"] != "openai" || readonlyPayload["name"] != "OpenAI" {
		t.Fatalf("expected readonly vendor payload to preserve canonical identity, got %+v", readonlyPayload)
	}

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/vendors",
		map[string]any{
			"key":         " OpenRouter ",
			"name":        " OpenRouter ",
			"description": " Shared vendor metadata for OpenRouter-backed models ",
			"icon_key":    " OPENROUTER ",
		},
		nil,
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createdVendor map[string]any
	decodeJSONResponse(t, createResponse, &createdVendor)
	createdVendorID := jsonInt(t, createdVendor["id"])

	if createdVendor["key"] != "openrouter" || createdVendor["name"] != "OpenRouter" || createdVendor["description"] != "Shared vendor metadata for OpenRouter-backed models" || createdVendor["icon_key"] != "openrouter" {
		t.Fatalf("expected created vendor fields to be normalized, got %+v", createdVendor)
	}
	if createdVendor["is_readonly"] != false || createdVendor["audit_enabled"] != false || createdVendor["audit_capture_bodies"] != true {
		t.Fatalf("expected editable vendor defaults, got %+v", createdVendor)
	}

	duplicateKey := harness.requestJSON(t, harness.client, http.MethodPost, "/api/vendors", map[string]any{"key": " OPENROUTER ", "name": "OpenRouter Duplicate"}, nil)
	assertErrorResponse(t, duplicateKey, http.StatusConflict, "Vendor key 'openrouter' already exists")
	duplicateName := harness.requestJSON(t, harness.client, http.MethodPost, "/api/vendors", map[string]any{"key": "openrouter-duplicate", "name": " OpenRouter "}, nil)
	assertErrorResponse(t, duplicateName, http.StatusConflict, "Vendor name 'OpenRouter' already exists")
	readonlyCreate := harness.requestJSON(t, harness.client, http.MethodPost, "/api/vendors", map[string]any{"key": " openai ", "name": "OpenAI Copy"}, nil)
	assertErrorResponse(t, readonlyCreate, http.StatusForbidden, "Vendor 'openai' is readonly and cannot be modified here")

	readonlyIdentityPatch := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/vendors/%d", openaiID), map[string]any{"name": "OpenAI Labs"}, nil)
	assertErrorResponse(t, readonlyIdentityPatch, http.StatusForbidden, "Vendor 'openai' is readonly and cannot be modified here")
	readonlyAuditPatch := harness.requestJSON(t, harness.client, http.MethodPatch, fmt.Sprintf("/api/vendors/%d", openaiID), map[string]any{"audit_enabled": true, "audit_capture_bodies": false}, nil)
	assertStatus(t, readonlyAuditPatch, http.StatusOK)
	var readonlyAuditPayload map[string]any
	decodeJSONResponse(t, readonlyAuditPatch, &readonlyAuditPayload)
	if readonlyAuditPayload["audit_enabled"] != true || readonlyAuditPayload["audit_capture_bodies"] != false || readonlyAuditPayload["is_readonly"] != true {
		t.Fatalf("expected readonly vendor audit toggles to stay mutable, got %+v", readonlyAuditPayload)
	}

	updateResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPatch,
		fmt.Sprintf("/api/vendors/%d", createdVendorID),
		map[string]any{
			"key":                  " OPENROUTER-HQ ",
			"name":                 " OpenRouter HQ ",
			"description":          " Primary shared vendor metadata ",
			"icon_key":             " OPENROUTER-HQ ",
			"audit_enabled":        true,
			"audit_capture_bodies": false,
		},
		nil,
	)
	assertStatus(t, updateResponse, http.StatusOK)
	var updatedVendor map[string]any
	decodeJSONResponse(t, updateResponse, &updatedVendor)
	if updatedVendor["key"] != "openrouter-hq" || updatedVendor["name"] != "OpenRouter HQ" || updatedVendor["description"] != "Primary shared vendor metadata" || updatedVendor["icon_key"] != "openrouter-hq" || updatedVendor["audit_enabled"] != true || updatedVendor["audit_capture_bodies"] != false {
		t.Fatalf("expected editable vendor update to persist normalized fields, got %+v", updatedVendor)
	}

	deleteReadonly := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/vendors/%d", openaiID), nil, nil)
	assertErrorResponse(t, deleteReadonly, http.StatusForbidden, "Vendor 'openai' is readonly and cannot be modified here")

	defaultProfileID, defaultProfileName := vendorLoadDefaultProfile(t, harness)
	modelConfigID := vendorInsertProxyModel(t, harness, defaultProfileID, createdVendorID, "vendor-delete-check", nil, "openai", true)
	helperBeforeDelete := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/vendors/%d/models", createdVendorID), nil, nil)
	assertStatus(t, helperBeforeDelete, http.StatusOK)
	var deleteContextRows []map[string]any
	decodeJSONResponse(t, helperBeforeDelete, &deleteContextRows)
	if len(deleteContextRows) != 1 {
		t.Fatalf("expected one delete-context row before vendor delete, got %+v", deleteContextRows)
	}

	assertVendorUsageRow(t, deleteContextRows[0], map[string]any{
		"model_config_id": modelConfigID,
		"profile_id":      defaultProfileID,
		"profile_name":    defaultProfileName,
		"model_id":        "vendor-delete-check",
		"display_name":    nil,
		"api_family":      "openai",
		"is_enabled":      true,
	})

	deleteEditable := harness.requestJSON(t, harness.client, http.MethodDelete, fmt.Sprintf("/api/vendors/%d", createdVendorID), nil, nil)
	assertStatus(t, deleteEditable, http.StatusNoContent)
	missingVendor := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/vendors/%d", createdVendorID), nil, nil)
	assertErrorResponse(t, missingVendor, http.StatusNotFound, "Vendor not found")
	if vendorID := vendorLoadModelVendorID(t, harness, modelConfigID); vendorID != nil {
		t.Fatalf("expected model_config %d vendor_id to be nulled after vendor delete, got %v", modelConfigID, *vendorID)
	}
}

func TestVendorModelsHelper(t *testing.T) {
	harness := newVendorContractHarness(t)
	missingVendor := harness.requestJSON(t, harness.client, http.MethodGet, "/api/vendors/999999/models", nil, nil)
	assertErrorResponse(t, missingVendor, http.StatusNotFound, "Vendor not found")

	createResponse := harness.requestJSON(
		t,
		harness.client,
		http.MethodPost,
		"/api/vendors",
		map[string]any{"key": "delete-context", "name": "Delete Context Vendor", "description": "helper route test"},
		nil,
	)
	assertStatus(t, createResponse, http.StatusCreated)
	var createdVendor map[string]any
	decodeJSONResponse(t, createResponse, &createdVendor)
	vendorID := jsonInt(t, createdVendor["id"])

	emptyUsage := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/vendors/%d/models", vendorID), nil, nil)
	assertStatus(t, emptyUsage, http.StatusOK)
	var emptyRows []map[string]any
	decodeJSONResponse(t, emptyUsage, &emptyRows)
	if len(emptyRows) != 0 {
		t.Fatalf("expected helper route to return an empty list for unused vendor, got %+v", emptyRows)
	}

	defaultProfileID, defaultProfileName := vendorLoadDefaultProfile(t, harness)
	secondaryProfileID := vendorInsertProfile(t, harness, "Vendor Usage Secondary")
	firstModelID := vendorInsertProxyModel(t, harness, defaultProfileID, vendorID, "helper-default-model", nil, "openai", true)
	secondModelID := vendorInsertProxyModel(t, harness, secondaryProfileID, vendorID, "helper-secondary-model", stringPtr("Secondary Helper Model"), "anthropic", false)

	helperResponse := harness.requestJSON(t, harness.client, http.MethodGet, fmt.Sprintf("/api/vendors/%d/models", vendorID), nil, nil)
	assertStatus(t, helperResponse, http.StatusOK)
	var helperRows []map[string]any
	decodeJSONResponse(t, helperResponse, &helperRows)
	if len(helperRows) != 2 {
		t.Fatalf("expected helper route to return two rows, got %+v", helperRows)
	}

	assertVendorUsageRow(t, helperRows[0], map[string]any{
		"model_config_id": firstModelID,
		"profile_id":      defaultProfileID,
		"profile_name":    defaultProfileName,
		"model_id":        "helper-default-model",
		"display_name":    nil,
		"api_family":      "openai",
		"is_enabled":      true,
	})
	assertVendorUsageRow(t, helperRows[1], map[string]any{
		"model_config_id": secondModelID,
		"profile_id":      secondaryProfileID,
		"profile_name":    "Vendor Usage Secondary",
		"model_id":        "helper-secondary-model",
		"display_name":    "Secondary Helper Model",
		"api_family":      "anthropic",
		"is_enabled":      false,
	})
}

func newVendorContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "vendor_contract_" + randomSuffix()
	conn := sharedPostgresHarness.openDatabase(t, testContext, databaseName)
	t.Cleanup(func() { _ = conn.Close(context.Background()) })

	startupService, err := startup.New(startup.Options{DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "vendor-contract-secret"})
	if err != nil {
		t.Fatalf("build startup service: %v", err)
	}
	if _, err := startupService.RunWithConn(testContext, conn); err != nil {
		t.Fatalf("run startup service: %v", err)
	}

	settings := config.Settings{Host: "127.0.0.1", Port: 8000, AppEnv: config.EnvironmentProduction, DatabaseURL: sharedPostgresHarness.connectionString(databaseName), SecretEncryptionKey: "vendor-contract-secret", CORSAllowedOrigins: "http://localhost:5173,http://127.0.0.1:5173"}
	pool, err := pgxpool.New(testContext, settings.DatabaseURL)
	if err != nil {
		t.Fatalf("create pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	vendorsService, err := managementvendors.NewService(settings, managementvendors.Options{Pool: pool})
	if err != nil {
		t.Fatalf("build vendors service: %v", err)
	}
	t.Cleanup(vendorsService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "vendor-contract-test", VendorsService: vendorsService})
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

func vendorListItemByKey(t *testing.T, vendors []map[string]any, key string) map[string]any {
	t.Helper()
	for _, vendor := range vendors {
		if vendor["key"] == key {
			return vendor
		}
	}
	t.Fatalf("expected vendor list to include key %q, got %+v", key, vendors)
	return nil
}

func vendorLoadDefaultProfile(t *testing.T, harness *contractHarness) (int, string) {
	t.Helper()
	var profileID int
	var profileName string
	if err := harness.conn.QueryRow(context.Background(), `SELECT id, name FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID, &profileName); err != nil {
		t.Fatalf("load default profile: %v", err)
	}
	return profileID, profileName
}

func vendorInsertProfile(t *testing.T, harness *contractHarness, name string) int {
	t.Helper()
	now := time.Now().UTC()
	var profileID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`, name, nil, false, false, true, 0, nil, now, now).Scan(&profileID); err != nil {
		t.Fatalf("insert profile %q: %v", name, err)
	}
	return profileID
}

func vendorInsertProxyModel(t *testing.T, harness *contractHarness, profileID int, vendorID int, modelID string, displayName *string, apiFamily string, isEnabled bool) int {
	t.Helper()
	now := time.Now().UTC()
	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, NULL, $6, $7, $8) RETURNING id`, profileID, vendorID, apiFamily, modelID, displayName, isEnabled, now, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert proxy model %q for vendor %d: %v", modelID, vendorID, err)
	}
	var targetModelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, vendor_id, api_family, model_id, display_name, loadbalance_strategy_id, is_enabled, created_at, updated_at) VALUES ($1, NULL, $2, $3, NULL, NULL, TRUE, $4, $4) RETURNING id`, profileID, apiFamily, modelID+"-target", now).Scan(&targetModelConfigID); err != nil {
		t.Fatalf("insert proxy target model for %q: %v", modelID, err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_model_config_id, position, weight, target_priority, is_enabled, created_at, updated_at) VALUES ($1, $2, 'model', $3, 0, 1, 0, TRUE, $4, $4)`, profileID, modelConfigID, targetModelConfigID, now); err != nil {
		t.Fatalf("insert proxy target for vendor model %q: %v", modelID, err)
	}
	return modelConfigID
}

func vendorLoadModelVendorID(t *testing.T, harness *contractHarness, modelConfigID int) *int {
	t.Helper()
	var vendorID sql.NullInt32
	if err := harness.conn.QueryRow(context.Background(), `SELECT vendor_id FROM model_configs WHERE id = $1`, modelConfigID).Scan(&vendorID); err != nil {
		t.Fatalf("load model_config %d vendor_id: %v", modelConfigID, err)
	}
	if !vendorID.Valid {
		return nil
	}
	value := int(vendorID.Int32)
	return &value
}

func assertVendorUsageRow(t *testing.T, row map[string]any, want map[string]any) {
	t.Helper()
	if len(row) != 7 {
		t.Fatalf("expected helper row to expose 7 stable fields, got %+v", row)
	}
	if jsonInt(t, row["model_config_id"]) != want["model_config_id"].(int) || jsonInt(t, row["profile_id"]) != want["profile_id"].(int) || row["profile_name"] != want["profile_name"] || row["model_id"] != want["model_id"] || row["display_name"] != want["display_name"] || row["api_family"] != want["api_family"] || row["is_enabled"] != want["is_enabled"] {
		t.Fatalf("expected helper row %+v, got %+v", want, row)
	}
}
