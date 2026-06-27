package contract_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
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
	configBundleSecretKey       = "configbundle-contract-secret"
	configBundlePreviewTokenKey = "configbundle-contract-bundle-key"
	configBundleFixtureKeyID    = "sha256:profile-v3-contract"
	configBundleOpenAISecret    = "fixture-openai-secret"
)

var configBundleFixtureTime = time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)

func TestProfileBundleV3Contract(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, exportResponse, http.StatusOK)
	if got := exportResponse.Header.Get("Content-Disposition"); got != "attachment; filename=\"prism-profile-config-v3-2026-04-18.json\"" {
		t.Fatalf("expected v3 profile export filename header, got %q", got)
	}

	var payload map[string]any
	decodeJSONResponse(t, exportResponse, &payload)
	assertProfileBundleV3Shape(t, payload)
	if _, ok := payload["vendor_"+"refs"]; ok {
		t.Fatalf("profile export must not include obsolete vendor refs: %+v", payload)
	}
	for _, model := range payload["models"].([]any) {
		if _, ok := asMap(t, model)["vendor_"+"key"]; ok {
			t.Fatalf("profile export model must not include obsolete vendor key: %+v", model)
		}
	}

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != true || previewPayload["preview_token"] == "" {
		t.Fatalf("expected ready v3 profile import preview, got %+v", previewPayload)
	}

	scope := asMap(t, previewPayload["replacement_scope"])
	if jsonInt(t, previewPayload["connections_imported"]) != 1 || jsonInt(t, scope["connections"]) != 1 {
		t.Fatalf("expected one top-level connection in v3 preview, got %+v", previewPayload)
	}

	importHeaders := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string))
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, importHeaders)
	assertStatus(t, importResponse, http.StatusOK)
	var importPayload map[string]any
	decodeJSONResponse(t, importResponse, &importPayload)
	if jsonInt(t, importPayload["connections_imported"]) != 1 || jsonInt(t, importPayload["models_imported"]) != 1 {
		t.Fatalf("expected v3 import counts for one connection and one model, got %+v", importPayload)
	}

	version2Payload := cloneProfileBundleV3Payload(t, payload)
	version2Payload["version"] = float64(2)
	version2Preview := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", version2Payload, modelHeader(profileID))
	assertErrorResponse(t, version2Preview, http.StatusBadRequest, "Unsupported profile config bundle version '2'; expected 3")
	version2Import := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", version2Payload, modelHeader(profileID))
	assertErrorResponse(t, version2Import, http.StatusBadRequest, "Unsupported profile config bundle version '2'; expected 3")

	legacyKeyPayload := cloneProfileBundleV3Payload(t, payload)
	legacyKeyStrategy := asMap(t, legacyKeyPayload["loadbalance_strategies"].([]any)[0])
	removedRetryField := removedRetryAttemptsField()
	legacyKeyStrategy[removedRetryField] = float64(3)
	legacyKeyPreview := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", legacyKeyPayload, modelHeader(profileID))
	assertErrorResponse(t, legacyKeyPreview, http.StatusBadRequest, fmt.Sprintf("json: unknown field %q", removedRetryField))
	legacyKeyImport := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", legacyKeyPayload, modelHeader(profileID))
	assertErrorResponse(t, legacyKeyImport, http.StatusBadRequest, fmt.Sprintf("json: unknown field %q", removedRetryField))

	removedModePayload := cloneProfileBundleV3Payload(t, payload)
	removedModeStrategy := asMap(t, removedModePayload["loadbalance_strategies"].([]any)[0])
	removedModeStrategy["ban_mode"] = removedBanModeValue()
	removedModeDetail := "ban_mode must be one of 'off', 'temporary', or 'until_reset'"
	removedModePreviewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", removedModePayload, modelHeader(profileID))
	assertErrorResponse(t, removedModePreviewResponse, http.StatusBadRequest, removedModeDetail)
	removedModeImport := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", removedModePayload, modelHeader(profileID))
	assertErrorResponse(t, removedModeImport, http.StatusBadRequest, removedModeDetail)
}

func TestProfileBundleImportRejectsRemovedFacadeFields(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := exportProfileBundlePayload(t, harness, profileID)
	models := payload["models"].([]any)
	models = append(models, map[string]any{
		"api_family":                "anthropic",
		"model_id":                  "claude-router",
		"display_name":              "Claude Router",
		"loadbalance_strategy_name": "Default round robin",
		"facade_enabled":            true,
		"facade_selection_policy":   "ordered_eligible_context",
		"facade_fallback_policy":    "skip_ineligible_targets",
		"is_enabled":                false,
		"access_targets":            []any{},
	})
	payload["models"] = models
	before := captureProfileImportState(t, harness, profileID)

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
	assertErrorResponse(t, previewResponse, http.StatusBadRequest, `json: unknown field "facade_enabled"`)

	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, modelHeader(profileID))
	assertErrorResponse(t, importResponse, http.StatusBadRequest, `json: unknown field "facade_enabled"`)
	assertProfileImportStateUnchanged(t, harness, profileID, before)
}

func TestProfileBundleImportAllowsSparseAccessTargetPositions(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := exportProfileBundlePayload(t, harness, profileID)
	models := payload["models"].([]any)
	firstModel := asMap(t, models[0])
	accessTargets := firstModel["access_targets"].([]any)
	asMap(t, accessTargets[0])["position"] = float64(4)
	payload["models"] = models

	models[len(models)-1].(map[string]any)["openai_accepted_format"] = "dual_native"

	previewPayload := previewProfileImportPayload(t, harness, profileID, payload)
	if previewPayload["ready"] != true || previewPayload["preview_token"] == "" {
		t.Fatalf("expected ready preview for sparse positions, got %+v", previewPayload)
	}
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string)))
	assertStatus(t, response, http.StatusOK)

	reExported := exportProfileBundlePayload(t, harness, profileID)
	reExportedModels := reExported["models"].([]any)
	reExportedTargets := asMap(t, reExportedModels[0])["access_targets"].([]any)
	if got := jsonInt(t, asMap(t, reExportedTargets[0])["position"]); got != 4 {
		t.Fatalf("expected sparse target position 4 to round-trip, got %+v", reExportedTargets)
	}
}

func TestProfileBundleImportRejectsObsoleteAccessTargetKeys(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	for _, field := range []string{"weight", "target_priority"} {
		t.Run(field, func(t *testing.T) {
			payload := exportProfileBundlePayload(t, harness, profileID)
			models := payload["models"].([]any)
			firstModel := asMap(t, models[0])
			accessTargets := firstModel["access_targets"].([]any)
			asMap(t, accessTargets[0])[field] = float64(1)
			before := captureProfileImportState(t, harness, profileID)
			detail := "obsolete access target field at models[0].access_targets[0]." + field

			previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
			assertErrorResponse(t, previewResponse, http.StatusBadRequest, detail)

			importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, modelHeader(profileID))
			assertErrorResponse(t, importResponse, http.StatusBadRequest, detail)
			assertProfileImportStateUnchanged(t, harness, profileID, before)
		})
	}
}

func TestProfileBundleAuditAPIFamilySettingsRoundTripAndValidation(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	exported := exportProfileBundlePayload(t, harness, profileID)
	profileSettings := asMap(t, exported["profile_settings"])
	auditSettings := profileSettings["audit_api_family_settings"].([]any)
	if len(auditSettings) != 3 || asMap(t, auditSettings[0])["api_family"] != "openai" || asMap(t, auditSettings[1])["api_family"] != "anthropic" || asMap(t, auditSettings[2])["api_family"] != "gemini" {
		t.Fatalf("expected stable audit api family export order, got %+v", auditSettings)
	}

	asMap(t, auditSettings[0])["audit_enabled"] = false
	asMap(t, auditSettings[0])["audit_capture_bodies"] = false
	asMap(t, auditSettings[1])["audit_enabled"] = true
	asMap(t, auditSettings[1])["audit_capture_bodies"] = false
	asMap(t, auditSettings[2])["audit_enabled"] = true
	asMap(t, auditSettings[2])["audit_capture_bodies"] = true
	previewPayload := previewProfileImportPayload(t, harness, profileID, exported)
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", exported, configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string)))
	assertStatus(t, importResponse, http.StatusOK)
	assertConfigBundleStoredAuditAPIFamilySettings(t, harness, profileID, []storedAuditAPIFamilySetting{
		{family: "openai", enabled: false, captureBodies: false},
		{family: "anthropic", enabled: true, captureBodies: false},
		{family: "gemini", enabled: true, captureBodies: true},
	})

	invalidCases := []struct {
		name   string
		mutate func(map[string]any)
		detail string
	}{
		{
			name: "unknown family",
			mutate: func(payload map[string]any) {
				settings := asMap(t, payload["profile_settings"])["audit_api_family_settings"].([]any)
				asMap(t, settings[1])["api_family"] = "mistral"
			},
			detail: `profile_settings.audit_api_family_settings api_family "mistral" is not supported`,
		},
		{
			name: "duplicate family",
			mutate: func(payload map[string]any) {
				settings := asMap(t, payload["profile_settings"])["audit_api_family_settings"].([]any)
				asMap(t, settings[1])["api_family"] = "openai"
			},
			detail: "Duplicate profile_settings.audit_api_family_settings entry for api_family=openai",
		},
		{
			name: "capture requires enabled",
			mutate: func(payload map[string]any) {
				settings := asMap(t, payload["profile_settings"])["audit_api_family_settings"].([]any)
				asMap(t, settings[0])["audit_enabled"] = false
				asMap(t, settings[0])["audit_capture_bodies"] = true
			},
			detail: "profile_settings.audit_api_family_settings audit_capture_bodies requires audit_enabled",
		},
	}
	for _, test := range invalidCases {
		t.Run(test.name, func(t *testing.T) {
			payload := exportProfileBundlePayload(t, harness, profileID)
			test.mutate(payload)
			before := captureProfileImportState(t, harness, profileID)
			previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
			assertErrorResponse(t, previewResponse, http.StatusBadRequest, test.detail)
			importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, modelHeader(profileID))
			assertErrorResponse(t, importResponse, http.StatusBadRequest, test.detail)
			assertProfileImportStateUnchanged(t, harness, profileID, before)
		})
	}
}

func TestProfileBundleImportRejectsUnknownModelTargetWithStableRoutingPlanIssue(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := exportProfileBundlePayload(t, harness, profileID)
	payload["connections"] = []any{}
	profileSettings := asMap(t, payload["profile_settings"])
	profileSettings["endpoint_fx_mappings"] = []any{}
	models := payload["models"].([]any)
	firstModel := asMap(t, models[0])
	firstModel["access_targets"] = []any{map[string]any{"position": 2, "is_enabled": true, "target_type": "model", "target_model_id": "missing-model"}}
	payload["models"] = models
	before := captureProfileImportState(t, harness, profileID)

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
	assertStatus(t, previewResponse, http.StatusBadRequest)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["detail"] != "Model 'gpt-4o-mini' references unknown model access target 'missing-model'" {
		t.Fatalf("unexpected preview detail: %+v", previewPayload)
	}
	previewIssues, ok := previewPayload["routing_plan_issues"].([]any)
	if !ok || len(previewIssues) != 1 {
		t.Fatalf("expected one preview routing_plan_issue, got %+v", previewPayload)
	}
	previewIssue := asMap(t, previewIssues[0])
	if previewIssue["code"] != "model_target_missing_model" || previewIssue["path"] != "models[0].access_targets[0].target_model_id" || previewIssue["message"] != "Model 'gpt-4o-mini' references unknown model access target 'missing-model'" {
		t.Fatalf("unexpected preview routing_plan_issue: %+v", previewIssue)
	}

	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, modelHeader(profileID))
	assertStatus(t, importResponse, http.StatusBadRequest)
	var importPayload map[string]any
	decodeJSONResponse(t, importResponse, &importPayload)
	if importPayload["detail"] != "Model 'gpt-4o-mini' references unknown model access target 'missing-model'" {
		t.Fatalf("unexpected import detail: %+v", importPayload)
	}
	importIssues, ok := importPayload["routing_plan_issues"].([]any)
	if !ok || len(importIssues) != 1 {
		t.Fatalf("expected one import routing_plan_issue, got %+v", importPayload)
	}
	importIssue := asMap(t, importIssues[0])
	if importIssue["code"] != "model_target_missing_model" || importIssue["path"] != "models[0].access_targets[0].target_model_id" || importIssue["message"] != "Model 'gpt-4o-mini' references unknown model access target 'missing-model'" {
		t.Fatalf("unexpected import routing_plan_issue: %+v", importIssue)
	}
	assertProfileImportStateUnchanged(t, harness, profileID, before)
}

func TestProfileBundleImportRejectsExistingConnectionOwnerCollision(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, exportResponse, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, exportResponse, &payload)

	previewResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
	assertStatus(t, previewResponse, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, previewResponse, &previewPayload)
	if previewPayload["ready"] != true || previewPayload["preview_token"] == "" {
		t.Fatalf("expected ready preview before injecting collision, got %+v", previewPayload)
	}

	connectionID := seedConfigBundleOwnerCollision(t, harness, profileID)
	exportDetail := "connection_ref 'openai-primary-openai' is owned by multiple models: model_id 'gpt-4o-mini' (display_name 'GPT 4o Mini') and model_id 'gpt-4o-collision' (display_name 'Collision GPT 4o')"
	importDetail := fmt.Sprintf("Target profile has existing connection ownership collision for connection_ref 'openai-primary-openai' (connection_id %d): model_id 'gpt-4o-collision' (display_name 'Collision GPT 4o') and model_id 'gpt-4o-mini' (display_name 'GPT 4o Mini')", connectionID)

	collisionExport := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertErrorResponse(t, collisionExport, http.StatusBadRequest, exportDetail)

	importHeaders := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string))
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, importHeaders)
	assertErrorResponse(t, importResponse, http.StatusBadRequest, importDetail)

	var ownerCount int
	if err := harness.conn.QueryRow(context.Background(), `SELECT COUNT(*) FROM model_access_targets WHERE profile_id = $1 AND target_connection_id = $2`, profileID, connectionID).Scan(&ownerCount); err != nil {
		t.Fatalf("count collision owners after rejected import: %v", err)
	}
	if ownerCount != 2 {
		t.Fatalf("expected rejected import to leave collision rows untouched, got owner count %d", ownerCount)
	}
}

func TestProfileBundleDangerousExportHeaderContract(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	missingConfirm := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/export/with-secrets", nil, modelHeader(profileID))
	assertErrorResponse(t, missingConfirm, http.StatusBadRequest, "X-Prism-Dangerous-Confirm header must be 'profile-export'")

	confirmedHeaders := modelHeader(profileID)
	confirmedHeaders["X-Prism-Dangerous-Confirm"] = "profile-export"
	confirmedExport := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/export/with-secrets", nil, confirmedHeaders)
	assertStatus(t, confirmedExport, http.StatusOK)
	if got := confirmedExport.Header.Get("Content-Disposition"); got != "attachment; filename=\"prism-profile-config-v3-2026-04-18.json\"" {
		t.Fatalf("expected dangerous profile export filename header, got %q", got)
	}

	var payload map[string]any
	decodeJSONResponse(t, confirmedExport, &payload)
	secretPayload := asMap(t, payload["secret_payload"])
	if secretPayload["kind"] != "encrypted" || secretPayload["key_id"] != configBundleFixtureKeyID {
		t.Fatalf("expected dangerous export secret payload metadata, got %+v", secretPayload)
	}
	entries := secretPayload["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("expected one exported dangerous secret entry, got %+v", secretPayload)
	}
	entry := asMap(t, entries[0])
	if entry["ref"] != "endpoint:Primary OpenAI:api_key" || !strings.HasPrefix(entry["ciphertext"].(string), "enc:") {
		t.Fatalf("expected dangerous export ciphertext entry, got %+v", entry)
	}
}

func TestProfileBundleImportRejectsMissingPreviewTokenWithoutMutation(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := mutateFirstProfileEndpointBaseURL(t, exportProfileBundlePayload(t, harness, profileID), "https://missing-preview-token.example.com")
	before := captureProfileImportState(t, harness, profileID)

	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, modelHeader(profileID))
	assertErrorResponse(t, response, http.StatusBadRequest, "X-Prism-Preview-Token header is required")
	assertProfileImportStateUnchanged(t, harness, profileID, before)
}

func TestProfileBundleImportRejectsExpiredPreviewTokenWithoutMutation(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := mutateFirstProfileEndpointBaseURL(t, exportProfileBundlePayload(t, harness, profileID), "https://expired-preview-token.example.com")
	before := captureProfileImportState(t, harness, profileID)
	previewPayload := previewProfileImportPayload(t, harness, profileID, payload)
	if previewPayload["ready"] != true || previewPayload["preview_token"] == "" {
		t.Fatalf("expected ready preview before expiring token, got %+v", previewPayload)
	}

	headers := configBundleHeadersWithPreviewToken(modelHeader(profileID), expireConfigBundlePreviewToken(t, previewPayload["preview_token"].(string)))
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, headers)
	assertErrorResponse(t, response, http.StatusConflict, "Preview token is invalid, expired, or does not match this bundle. Run preview again and retry.")
	assertProfileImportStateUnchanged(t, harness, profileID, before)
}

func TestProfileBundleImportRejectsMismatchedPreviewTokenWithoutMutation(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	previewPayloadBody := mutateFirstProfileEndpointBaseURL(t, exportProfileBundlePayload(t, harness, profileID), "https://preview-fingerprint.example.com")
	before := captureProfileImportState(t, harness, profileID)
	previewPayload := previewProfileImportPayload(t, harness, profileID, previewPayloadBody)
	if previewPayload["ready"] != true || previewPayload["preview_token"] == "" {
		t.Fatalf("expected ready preview before mismatched apply, got %+v", previewPayload)
	}

	mismatchedPayload := mutateFirstProfileEndpointBaseURL(t, previewPayloadBody, "https://apply-fingerprint.example.com")
	headers := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string))
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", mismatchedPayload, headers)
	assertErrorResponse(t, response, http.StatusConflict, "Preview token is invalid, expired, or does not match this bundle. Run preview again and retry.")
	assertProfileImportStateUnchanged(t, harness, profileID, before)
}

func TestProfileBundleImportRejectsBundleKeyMismatchWithoutMutation(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := mutateFirstProfileEndpointBaseURL(t, exportProfileBundleWithSecretsPayload(t, harness, profileID), "https://bundle-key-mismatch.example.com")
	secretPayload := asMap(t, payload["secret_payload"])
	secretPayload["key_id"] = "sha256:wrong-contract-key"
	before := captureProfileImportState(t, harness, profileID)
	previewPayload := previewProfileImportPayload(t, harness, profileID, payload)
	wantDetail := "Config import bundle key mismatch: bundle key_id 'sha256:wrong-contract-key' does not match server key_id 'sha256:profile-v3-contract'"
	if previewPayload["ready"] != false || previewPayload["preview_token"] == "" {
		t.Fatalf("expected blocking preview with token for key mismatch, got %+v", previewPayload)
	}
	if got := firstBlockingErrorDetail(t, previewPayload); got != wantDetail {
		t.Fatalf("expected key mismatch preview detail %q, got %q", wantDetail, got)
	}

	headers := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string))
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, headers)
	assertErrorResponse(t, response, http.StatusBadRequest, wantDetail)
	assertProfileImportStateUnchanged(t, harness, profileID, before)
}

func TestProfileBundleImportRejectsUndecryptableSecretPayloadWithoutMutation(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := mutateFirstProfileEndpointBaseURL(t, exportProfileBundleWithSecretsPayload(t, harness, profileID), "https://undecryptable-secret.example.com")
	before := captureProfileImportState(t, harness, profileID)
	previewPayload := previewProfileImportPayload(t, harness, profileID, payload)
	wantDetail := "Config import could not decrypt secret ref 'endpoint:Primary OpenAI:api_key'"
	if previewPayload["ready"] != false || previewPayload["preview_token"] == "" {
		t.Fatalf("expected blocking preview with token for undecryptable secret payload, got %+v", previewPayload)
	}
	if got := firstBlockingErrorDetail(t, previewPayload); got != wantDetail {
		t.Fatalf("expected secret payload preview detail %q, got %q", wantDetail, got)
	}

	headers := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string))
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, headers)
	assertErrorResponse(t, response, http.StatusBadRequest, wantDetail)
	assertProfileImportStateUnchanged(t, harness, profileID, before)
}

func newConfigBundleV3ContractHarness(t *testing.T) *contractHarness {
	t.Helper()
	testContext, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	databaseName := "configbundle_v3_contract_" + randomSuffix()
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
		ConfigBundleEncryptionKey: configBundlePreviewTokenKey,
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
			return "enc:gAAAAABlProfileV3OpenAI", nil
		},
	})
	if err != nil {
		t.Fatalf("build config bundle service: %v", err)
	}
	t.Cleanup(configBundleService.Close)

	handler, err := platformhttp.NewHandlerWithDependencies(settings, platformhttp.Dependencies{Version: "configbundle-v3-contract-test", ConfigBundleService: configBundleService})
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

func seedConfigBundleV3Graph(t *testing.T, harness *contractHarness, profileID int) {
	t.Helper()
	now := configBundleFixtureTime
	for _, statement := range []string{
		`DELETE FROM endpoint_fx_rate_settings WHERE profile_id = $1`,
		`DELETE FROM model_access_targets WHERE profile_id = $1`,
		`DELETE FROM connections WHERE profile_id = $1`,
		`DELETE FROM model_configs WHERE profile_id = $1`,
		`DELETE FROM pricing_templates WHERE profile_id = $1`,
		`DELETE FROM loadbalance_strategies WHERE profile_id = $1`,
		`DELETE FROM endpoints WHERE profile_id = $1`,
		`DELETE FROM profile_api_family_audit_settings WHERE profile_id = $1`,
		`DELETE FROM header_blocklist_rules WHERE profile_id = $1 AND is_system = FALSE`,
		`DELETE FROM user_agent_client_rules WHERE profile_id = $1 AND is_system = FALSE`,
	} {
		if _, err := harness.conn.Exec(context.Background(), statement, profileID); err != nil {
			t.Fatalf("clear v3 fixture state with %q: %v", statement, err)
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
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO loadbalance_strategies (profile_id, name, legacy_strategy_type, failure_status_codes, ban_mode, retry_base_delay_ms, retry_backoff_multiplier, retry_jitter_ratio, retry_max_delay_ms, cycle_retry_attempt_limit, ban_cumulative_retry_attempt_threshold, ban_duration_seconds, created_at, updated_at) VALUES ($1, 'Default round robin', 'round-robin', ARRAY[429,500], 'until_reset', 60000, 2.0, 0.2, 900000, 2, 4, 0, $2, $2) RETURNING id`, profileID, now).Scan(&strategyID); err != nil {
		t.Fatalf("insert strategy: %v", err)
	}

	var modelConfigID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'gpt-4o-mini', 'GPT 4o Mini', $2, 'dual_native', TRUE, $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("insert model: %v", err)
	}
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO connections (profile_id, api_family, endpoint_id, pricing_template_id, qps_limit, max_in_flight_non_stream, max_in_flight_stream, openai_probe_endpoint_variant, openai_text_capability, is_active, priority, name, auth_type, custom_headers, health_status, health_detail, last_health_check, created_at, updated_at) VALUES ($1, 'openai', $2, $3, 60, 8, 4, 'responses_minimal', 'responses_only', TRUE, 0, 'Primary OpenAI connection', 'openai', $4, 'healthy', NULL, NULL, $5, $5) RETURNING id`, profileID, endpointID, pricingID, `{"X-Prism-Trace":"v3-contract"}`, now).Scan(&connectionID); err != nil {
		t.Fatalf("insert connection: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, modelConfigID, connectionID, now); err != nil {
		t.Fatalf("insert access target: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO endpoint_fx_rate_settings (profile_id, model_id, endpoint_id, fx_rate, created_at, updated_at) VALUES ($1, 'gpt-4o-mini', $2, '1.000000', $3, $3)`, profileID, endpointID, now); err != nil {
		t.Fatalf("insert fx mapping: %v", err)
	}
	for _, setting := range []struct {
		family        string
		enabled       bool
		captureBodies bool
	}{
		{family: "openai", enabled: true, captureBodies: true},
		{family: "anthropic", enabled: false, captureBodies: false},
		{family: "gemini", enabled: true, captureBodies: false},
	} {
		if _, err := harness.conn.Exec(context.Background(), `INSERT INTO profile_api_family_audit_settings (profile_id, api_family, audit_enabled, audit_capture_bodies, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $5)`, profileID, setting.family, setting.enabled, setting.captureBodies, now); err != nil {
			t.Fatalf("insert audit api family setting %s: %v", setting.family, err)
		}
	}

	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO header_blocklist_rules (profile_id, name, match_type, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, 'Authorization', 'exact', 'authorization', TRUE, FALSE, $2, $2)`, profileID, now); err != nil {
		t.Fatalf("insert header rule: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO user_agent_client_rules (profile_id, name, pattern, enabled, is_system, created_at, updated_at) VALUES ($1, 'Acme Agent', 'acme-agent', TRUE, FALSE, $2, $2)`, profileID, now); err != nil {
		t.Fatalf("insert user-agent rule: %v", err)
	}
}

func seedConfigBundleOwnerCollision(t *testing.T, harness *contractHarness, profileID int) int {
	t.Helper()
	now := configBundleFixtureTime.Add(time.Minute)

	var strategyID int
	var connectionID int
	if err := harness.conn.QueryRow(context.Background(), `SELECT loadbalance_strategy_id FROM model_configs WHERE profile_id = $1 AND model_id = 'gpt-4o-mini'`, profileID).Scan(&strategyID); err != nil {
		t.Fatalf("load owner model references: %v", err)
	}
	if err := harness.conn.QueryRow(context.Background(), `SELECT target_connection_id FROM model_access_targets WHERE profile_id = $1 AND target_type = 'connection'`, profileID).Scan(&connectionID); err != nil {
		t.Fatalf("load owned connection id: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `DROP INDEX IF EXISTS uq_model_access_targets_connection_owner`); err != nil {
		t.Fatalf("drop owner uniqueness index for collision fixture: %v", err)
	}

	var collisionModelID int
	if err := harness.conn.QueryRow(context.Background(), `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'gpt-4o-collision', 'Collision GPT 4o', $2, 'dual_native', TRUE, $3, $3) RETURNING id`, profileID, strategyID, now).Scan(&collisionModelID); err != nil {
		t.Fatalf("insert collision owner model: %v", err)
	}
	if _, err := harness.conn.Exec(context.Background(), `INSERT INTO model_access_targets (profile_id, source_model_config_id, target_type, target_connection_id, position, is_enabled, created_at, updated_at) VALUES ($1, $2, 'connection', $3, 0, TRUE, $4, $4)`, profileID, collisionModelID, connectionID, now); err != nil {
		t.Fatalf("insert collision owner target: %v", err)
	}
	return connectionID
}

func assertProfileBundleV3Shape(t *testing.T, payload map[string]any) {
	t.Helper()
	if jsonInt(t, payload["version"]) != 3 || payload["bundle_kind"] != "profile_config" {
		t.Fatalf("expected profile_config v3 bundle, got %+v", payload)
	}
	connections := payload["connections"].([]any)
	if len(connections) != 1 {
		t.Fatalf("expected one top-level connection, got %+v", connections)
	}
	connection := asMap(t, connections[0])
	connectionRef := connection["ref"].(string)
	if !strings.HasPrefix(connectionRef, "openai-primary-openai") || connection["api_family"] != "openai" || connection["endpoint_name"] != "Primary OpenAI" {
		t.Fatalf("expected v3 standalone OpenAI connection export, got %+v", connection)
	}
	for _, removedKey := range []string{"context_window_tokens", "context_window_tokens_overridden", "default_output_token_reserve", "default_output_token_reserve_overridden", "max_context_utilization", "max_context_utilization_overridden", "preferred_context_utilization_threshold", "preferred_context_utilization_threshold_overridden", "context_capability_overrides"} {
		if _, ok := connection[removedKey]; ok {
			t.Fatalf("connection export must not include removed capability field %q: %+v", removedKey, connection)
		}
	}
	if connection["openai_probe_endpoint_variant"] != "responses_minimal" || connection["openai_text_capability"] != "responses_only" {
		t.Fatalf("expected v3 connection export to keep probe variant and text capability, got %+v", connection)
	}
	if _, ok := connection["openai_upstream_operation"]; ok {
		t.Fatalf("v3 connection export must not include removed openai_upstream_operation: %+v", connection)
	}
	strategies := payload["loadbalance_strategies"].([]any)
	if len(strategies) != 1 {
		t.Fatalf("expected one exported loadbalance strategy, got %+v", strategies)
	}
	strategy := asMap(t, strategies[0])
	removedRetryField := removedRetryAttemptsField()
	if _, ok := strategy[removedRetryField]; ok {
		t.Fatalf("v3 strategy export must not include removed retry field: %+v", strategy)
	}
	if strategy["ban_mode"] != "until_reset" || jsonInt(t, strategy["cycle_retry_attempt_limit"]) != 2 || jsonInt(t, strategy["ban_cumulative_retry_attempt_threshold"]) != 4 {
		t.Fatalf("expected v3 explicit Ban Policy fields, got %+v", strategy)
	}
	models := payload["models"].([]any)
	if len(models) != 1 {
		t.Fatalf("expected one exported model, got %+v", models)
	}
	model := asMap(t, models[0])
	for _, removedKey := range []string{"model_type", "proxy_selection_strategy", "proxy_targets", "connections", "facade_enabled", "facade_selection_policy", "facade_fallback_policy"} {
		if _, ok := model[removedKey]; ok {
			t.Fatalf("model export must not include removed key %q: %+v", removedKey, model)
		}
	}
	for _, removedKey := range []string{"context_window_tokens", "default_output_token_reserve", "max_context_utilization", "preferred_context_utilization_threshold"} {
		if _, ok := model[removedKey]; ok {
			t.Fatalf("model export must not include removed capability field %q: %+v", removedKey, model)
		}
	}
	targets := model["access_targets"].([]any)
	if len(targets) != 1 {
		t.Fatalf("expected one access target, got %+v", targets)
	}
	target := asMap(t, targets[0])
	if target["target_type"] != "connection" || target["connection_ref"] != connectionRef || jsonInt(t, target["position"]) != 0 || target["is_enabled"] != true {
		t.Fatalf("expected v3 connection access target, got %+v", target)
	}
	if _, ok := payload["runtime"]; ok {
		t.Fatalf("profile config bundle export must not include bootstrap runtime rollout controls: %+v", payload)
	}
	settings := asMap(t, payload["profile_settings"])
	fxMappings := settings["endpoint_fx_mappings"].([]any)
	if len(fxMappings) != 1 || asMap(t, fxMappings[0])["connection_ref"] != connectionRef {
		t.Fatalf("expected v3 FX mapping keyed by connection_ref, got %+v", settings)
	}
	auditSettings := settings["audit_api_family_settings"].([]any)
	if len(auditSettings) != 3 {
		t.Fatalf("expected three audit api family settings, got %+v", settings)
	}
	wantAuditSettings := []struct {
		family        string
		enabled       bool
		captureBodies bool
	}{
		{family: "openai", enabled: true, captureBodies: true},
		{family: "anthropic", enabled: false, captureBodies: false},
		{family: "gemini", enabled: true, captureBodies: false},
	}
	for index, want := range wantAuditSettings {
		setting := asMap(t, auditSettings[index])
		if setting["api_family"] != want.family || setting["audit_enabled"] != want.enabled || setting["audit_capture_bodies"] != want.captureBodies {
			t.Fatalf("expected audit setting %d to be %+v, got %+v", index, want, setting)
		}
	}
}

func findProfileBundleModelByID(t *testing.T, payload map[string]any, modelID string) map[string]any {
	t.Helper()
	models, ok := payload["models"].([]any)
	if !ok {
		t.Fatalf("expected profile bundle payload models, got %+v", payload)
	}
	for _, item := range models {
		model := asMap(t, item)
		if model["model_id"] == modelID {
			return model
		}
	}
	t.Fatalf("expected profile bundle payload to contain model_id %q: %+v", modelID, payload)
	return nil
}

func cloneProfileBundleV3Payload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal profile bundle payload: %v", err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("clone profile bundle payload: %v", err)
	}
	return cloned
}

func configBundleHeadersWithPreviewToken(headers map[string]string, token string) map[string]string {
	merged := make(map[string]string, len(headers)+1)
	maps.Copy(merged, headers)
	merged["X-Prism-Preview-Token"] = token
	return merged
}

type configBundlePreviewTokenClaims struct {
	Version           int       `json:"version"`
	Scope             string    `json:"scope"`
	ProfileID         *int      `json:"profile_id,omitempty"`
	BundleFingerprint string    `json:"bundle_fingerprint"`
	IssuedAt          time.Time `json:"issued_at"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type profileImportStateSnapshot struct {
	EndpointCount        int
	PricingTemplateCount int
	StrategyCount        int
	ModelCount           int
	ConnectionCount      int
	AccessTargetCount    int
	AuditSettingCount    int
	HeaderRuleCount      int
	UserAgentRuleCount   int
	FXMappingCount       int
	EndpointBaseURL      string
	HeaderRulePattern    string
	UserAgentPattern     string
}

type storedAuditAPIFamilySetting struct {
	family        string
	enabled       bool
	captureBodies bool
}

func exportProfileBundlePayload(t *testing.T, harness *contractHarness, profileID int) map[string]any {
	t.Helper()
	response := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

func exportProfileBundleWithSecretsPayload(t *testing.T, harness *contractHarness, profileID int) map[string]any {
	t.Helper()
	headers := modelHeader(profileID)
	headers["X-Prism-Dangerous-Confirm"] = "profile-export"
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/export/with-secrets", nil, headers)
	assertStatus(t, response, http.StatusOK)
	var payload map[string]any
	decodeJSONResponse(t, response, &payload)
	return payload
}

func previewProfileImportPayload(t *testing.T, harness *contractHarness, profileID int, payload map[string]any) map[string]any {
	t.Helper()
	response := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", payload, modelHeader(profileID))
	assertStatus(t, response, http.StatusOK)
	var previewPayload map[string]any
	decodeJSONResponse(t, response, &previewPayload)
	return previewPayload
}

func firstBlockingErrorDetail(t *testing.T, payload map[string]any) string {
	t.Helper()
	blockingErrors, ok := payload["blocking_errors"].([]any)
	if !ok || len(blockingErrors) == 0 {
		t.Fatalf("expected blocking_errors in preview payload, got %+v", payload)
	}
	detail, ok := blockingErrors[0].(string)
	if !ok || strings.TrimSpace(detail) == "" {
		t.Fatalf("expected first blocking error detail string, got %+v", blockingErrors[0])
	}
	return detail
}

func mutateFirstProfileEndpointBaseURL(t *testing.T, payload map[string]any, baseURL string) map[string]any {
	t.Helper()
	mutated := cloneProfileBundleV3Payload(t, payload)
	endpoints := mutated["endpoints"].([]any)
	if len(endpoints) == 0 {
		t.Fatalf("expected exported profile bundle endpoints, got %+v", mutated)
	}
	endpoint := asMap(t, endpoints[0])
	endpoint["base_url"] = baseURL
	return mutated
}

func expireConfigBundlePreviewToken(t *testing.T, token string) string {
	t.Helper()
	claims := parseConfigBundlePreviewToken(t, token)
	claims.ExpiresAt = configBundleFixtureTime.Add(-time.Minute)
	if !claims.IssuedAt.Before(claims.ExpiresAt) {
		claims.IssuedAt = claims.ExpiresAt.Add(-time.Minute)
	}
	return signConfigBundlePreviewToken(t, claims)
}

func parseConfigBundlePreviewToken(t *testing.T, token string) configBundlePreviewTokenClaims {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		t.Fatalf("expected signed preview token, got %q", token)
	}
	rawClaims, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode preview token claims: %v", err)
	}
	var claims configBundlePreviewTokenClaims
	if err := json.Unmarshal(rawClaims, &claims); err != nil {
		t.Fatalf("unmarshal preview token claims: %v", err)
	}
	return claims
}

func signConfigBundlePreviewToken(t *testing.T, claims configBundlePreviewTokenClaims) string {
	t.Helper()
	rawClaims, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal preview token claims: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(configBundlePreviewTokenKey))
	mac.Write(rawClaims)
	signature := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(rawClaims) + "." + base64.RawURLEncoding.EncodeToString(signature)
}

func captureProfileImportState(t *testing.T, harness *contractHarness, profileID int) profileImportStateSnapshot {
	t.Helper()
	ctx := context.Background()
	state := profileImportStateSnapshot{}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM endpoints WHERE profile_id = $1`, profileID).Scan(&state.EndpointCount); err != nil {
		t.Fatalf("count endpoints for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM pricing_templates WHERE profile_id = $1`, profileID).Scan(&state.PricingTemplateCount); err != nil {
		t.Fatalf("count pricing templates for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM loadbalance_strategies WHERE profile_id = $1`, profileID).Scan(&state.StrategyCount); err != nil {
		t.Fatalf("count loadbalance strategies for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM model_configs WHERE profile_id = $1`, profileID).Scan(&state.ModelCount); err != nil {
		t.Fatalf("count model configs for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM connections WHERE profile_id = $1`, profileID).Scan(&state.ConnectionCount); err != nil {
		t.Fatalf("count connections for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM model_access_targets WHERE profile_id = $1`, profileID).Scan(&state.AccessTargetCount); err != nil {
		t.Fatalf("count access targets for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM profile_api_family_audit_settings WHERE profile_id = $1`, profileID).Scan(&state.AuditSettingCount); err != nil {
		t.Fatalf("count audit api family settings for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM header_blocklist_rules WHERE profile_id = $1 AND is_system = FALSE`, profileID).Scan(&state.HeaderRuleCount); err != nil {
		t.Fatalf("count header rules for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM user_agent_client_rules WHERE profile_id = $1 AND is_system = FALSE`, profileID).Scan(&state.UserAgentRuleCount); err != nil {
		t.Fatalf("count user-agent rules for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT COUNT(*) FROM endpoint_fx_rate_settings WHERE profile_id = $1`, profileID).Scan(&state.FXMappingCount); err != nil {
		t.Fatalf("count fx mappings for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT base_url FROM endpoints WHERE profile_id = $1 AND name = 'Primary OpenAI' LIMIT 1`, profileID).Scan(&state.EndpointBaseURL); err != nil {
		t.Fatalf("load endpoint base_url for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT pattern FROM header_blocklist_rules WHERE profile_id = $1 AND name = 'Authorization' AND is_system = FALSE LIMIT 1`, profileID).Scan(&state.HeaderRulePattern); err != nil {
		t.Fatalf("load header rule pattern for profile import snapshot: %v", err)
	}
	if err := harness.conn.QueryRow(ctx, `SELECT pattern FROM user_agent_client_rules WHERE profile_id = $1 AND name = 'Acme Agent' AND is_system = FALSE LIMIT 1`, profileID).Scan(&state.UserAgentPattern); err != nil {
		t.Fatalf("load user-agent rule pattern for profile import snapshot: %v", err)
	}
	return state
}

func assertProfileImportStateUnchanged(t *testing.T, harness *contractHarness, profileID int, before profileImportStateSnapshot) {
	t.Helper()
	after := captureProfileImportState(t, harness, profileID)
	if after != before {
		t.Fatalf("expected rejected profile import to preserve state, before=%+v after=%+v", before, after)
	}
}

func assertConfigBundleStoredAuditAPIFamilySettings(t *testing.T, harness *contractHarness, profileID int, want []storedAuditAPIFamilySetting) {
	t.Helper()
	rows, err := harness.conn.Query(context.Background(), `SELECT api_family, audit_enabled, audit_capture_bodies FROM profile_api_family_audit_settings WHERE profile_id = $1 ORDER BY CASE api_family WHEN 'openai' THEN 1 WHEN 'anthropic' THEN 2 WHEN 'gemini' THEN 3 ELSE 4 END`, profileID)
	if err != nil {
		t.Fatalf("query stored audit api family settings: %v", err)
	}
	defer rows.Close()
	got := make([]storedAuditAPIFamilySetting, 0, len(want))
	for rows.Next() {
		var setting storedAuditAPIFamilySetting
		if err := rows.Scan(&setting.family, &setting.enabled, &setting.captureBodies); err != nil {
			t.Fatalf("scan stored audit api family setting: %v", err)
		}
		got = append(got, setting)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate stored audit api family settings: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d stored audit api family settings, got %+v", len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected stored audit api family setting %d to be %+v, got %+v", index, want[index], got[index])
		}
	}
}

func TestConfigBundleOpenAITextCapabilityAndProbeVariantRoundTrip(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	exportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, exportResponse, http.StatusOK)
	var exported map[string]any
	decodeJSONResponse(t, exportResponse, &exported)
	connection := asMap(t, exported["connections"].([]any)[0])
	if connection["openai_probe_endpoint_variant"] != "responses_minimal" || connection["openai_text_capability"] != "responses_only" {
		t.Fatalf("expected exported connection probe variant and text capability, got %+v", connection)
	}

	missingCapability := cloneProfileBundleV3Payload(t, exported)
	delete(asMap(t, missingCapability["connections"].([]any)[0]), "openai_text_capability")
	missingPreview := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", missingCapability, modelHeader(profileID))
	assertErrorResponse(t, missingPreview, http.StatusBadRequest, "Connection 'openai-primary-openai' must include openai_text_capability for OpenAI API family connections")

	mutated := cloneProfileBundleV3Payload(t, exported)
	asMap(t, mutated["connections"].([]any)[0])["openai_probe_endpoint_variant"] = "chat_completions_reasoning_none"
	asMap(t, mutated["connections"].([]any)[0])["openai_text_capability"] = "dual_native"
	preview := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import/preview", mutated, modelHeader(profileID))
	assertStatus(t, preview, http.StatusOK)
	var previewBody map[string]any
	decodeJSONResponse(t, preview, &previewBody)
	importHeaders := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewBody["preview_token"].(string))
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", mutated, importHeaders)
	assertStatus(t, importResponse, http.StatusOK)

	reExportResponse := harness.requestJSON(t, harness.client, http.MethodGet, "/api/config/profile/export", nil, modelHeader(profileID))
	assertStatus(t, reExportResponse, http.StatusOK)
	var reExported map[string]any
	decodeJSONResponse(t, reExportResponse, &reExported)
	reExportedConnection := asMap(t, reExported["connections"].([]any)[0])
	if reExportedConnection["openai_probe_endpoint_variant"] != "chat_completions_reasoning_none" || reExportedConnection["openai_text_capability"] != "dual_native" {
		t.Fatalf("expected round-tripped connection probe variant and text capability, got %+v", reExportedConnection)
	}
	assertConfigBundleStoredConnectionProbeVariant(t, harness, profileID, "chat_completions_reasoning_none")
	assertConfigBundleStoredConnectionTextCapability(t, harness, profileID, "dual_native")
}

func TestConfigBundleModelTargetMetadataRoundTrip(t *testing.T) {
	harness := newConfigBundleV3ContractHarness(t)
	profileID := modelLoadDefaultProfileID(t, harness)
	seedConfigBundleV3Graph(t, harness, profileID)

	payload := exportProfileBundlePayload(t, harness, profileID)
	models := payload["models"].([]any)
	models = append(models, map[string]any{
		"api_family":                "openai",
		"openai_accepted_format":    "dual_native",
		"model_id":                  "gpt-4o-router",
		"display_name":              "GPT 4o Router",
		"loadbalance_strategy_name": "Default round robin",
		"is_enabled":                true,
		"access_targets": []any{map[string]any{
			"position":        float64(0),
			"is_enabled":      true,
			"target_type":     "model",
			"target_model_id": "gpt-4o-mini",
		}},
	})
	payload["models"] = models

	previewPayload := previewProfileImportPayload(t, harness, profileID, payload)
	importHeaders := configBundleHeadersWithPreviewToken(modelHeader(profileID), previewPayload["preview_token"].(string))
	importResponse := harness.requestJSON(t, harness.client, http.MethodPost, "/api/config/profile/import", payload, importHeaders)
	assertStatus(t, importResponse, http.StatusOK)

	reExported := exportProfileBundlePayload(t, harness, profileID)
	routerModel := findProfileBundleModelByID(t, reExported, "gpt-4o-router")
	for _, removedKey := range []string{"facade_enabled", "facade_selection_policy", "facade_fallback_policy"} {
		if _, ok := routerModel[removedKey]; ok {
			t.Fatalf("model target export must not include removed facade key %q: %+v", removedKey, routerModel)
		}
	}
	routerTarget := asMap(t, routerModel["access_targets"].([]any)[0])
	if routerTarget["target_type"] != "model" || routerTarget["target_model_id"] != "gpt-4o-mini" || jsonInt(t, routerTarget["position"]) != 0 || routerTarget["is_enabled"] != true {
		t.Fatalf("expected flat ordered model target to round-trip, got %+v", routerTarget)
	}
	if _, ok := routerTarget["weight"]; ok {
		t.Fatalf("model target export must not include obsolete weight: %+v", routerTarget)
	}
	if _, ok := routerTarget["target_priority"]; ok {
		t.Fatalf("model target export must not include obsolete target_priority: %+v", routerTarget)
	}
}

func assertConfigBundleStoredConnectionProbeVariant(t *testing.T, harness *contractHarness, profileID int, want string) {
	t.Helper()
	var probeVariant sql.NullString
	if err := harness.conn.QueryRow(context.Background(), `SELECT openai_probe_endpoint_variant FROM connections WHERE profile_id = $1 AND name = 'Primary OpenAI connection' LIMIT 1`, profileID).Scan(&probeVariant); err != nil {
		t.Fatalf("load stored connection openai_probe_endpoint_variant: %v", err)
	}
	if !probeVariant.Valid || probeVariant.String != want {
		t.Fatalf("expected stored connection openai_probe_endpoint_variant %q, got %+v", want, probeVariant)
	}
}

func assertConfigBundleStoredConnectionTextCapability(t *testing.T, harness *contractHarness, profileID int, want string) {
	t.Helper()
	var textCapability sql.NullString
	if err := harness.conn.QueryRow(context.Background(), `SELECT openai_text_capability FROM connections WHERE profile_id = $1 AND name = 'Primary OpenAI connection' LIMIT 1`, profileID).Scan(&textCapability); err != nil {
		t.Fatalf("load stored connection openai_text_capability: %v", err)
	}
	if !textCapability.Valid || textCapability.String != want {
		t.Fatalf("expected stored connection openai_text_capability %q, got %+v", want, textCapability)
	}
}
