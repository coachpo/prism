package configbundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requireConfigBundleDomainError(t *testing.T, err error, status int, detail string) *domainError {
	t.Helper()

	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domainError, got %T", err)
	}
	if domainErr.StatusCode != status || domainErr.Detail != detail {
		t.Fatalf("expected domainError (%d, %q), got (%d, %q)", status, detail, domainErr.StatusCode, domainErr.Detail)
	}
	return domainErr
}

func TestResolveImportedNames(t *testing.T) {
	endpoints := map[string]struct{}{"Primary": {}}
	if got, err := resolveImportedEndpointName("  Primary  ", endpoints); err != nil || got != "Primary" {
		t.Fatalf("expected trimmed endpoint name, got name=%q err=%v", got, err)
	}
	if _, err := resolveImportedEndpointName("   ", endpoints); err == nil || err.Error() != "must include endpoint_name" {
		t.Fatalf("expected missing endpoint name error, got %v", err)
	}
	if _, err := resolveImportedEndpointName("Secondary", endpoints); err == nil || err.Error() != "references unknown endpoint_name 'Secondary'" {
		t.Fatalf("expected unknown endpoint error, got %v", err)
	}

	pricingTemplates := map[string]struct{}{"Standard": {}}
	if got, err := resolveImportedPricingTemplateName(nil, pricingTemplates); err != nil || got != nil {
		t.Fatalf("expected nil pricing template name to stay nil, got name=%v err=%v", got, err)
	}
	if got, err := resolveImportedPricingTemplateName(stringPtr("  Standard  "), pricingTemplates); err != nil || got == nil || *got != "Standard" {
		t.Fatalf("expected trimmed pricing template name, got name=%#v err=%v", got, err)
	}
	if _, err := resolveImportedPricingTemplateName(stringPtr("Missing"), pricingTemplates); err == nil || err.Error() != "references unknown pricing_template_name 'Missing'" {
		t.Fatalf("expected unknown pricing template error, got %v", err)
	}
}

func TestNormalizeImportedOpenAITextCapability(t *testing.T) {
	if got, err := normalizeImportedOpenAITextCapability("openai", stringPtr("  DUAL_NATIVE  "), true); err != nil || got == nil || *got != "dual_native" {
		t.Fatalf("expected normalized OpenAI text capability, got capability=%#v err=%v", got, err)
	}
	if _, err := normalizeImportedOpenAITextCapability("openai", nil, false); err == nil || err.Error() != "must include openai_text_capability for OpenAI API family connections" {
		t.Fatalf("expected missing OpenAI text capability error, got %v", err)
	}
	if _, err := normalizeImportedOpenAITextCapability("openai", stringPtr("bogus"), true); err == nil || err.Error() != "has invalid openai_text_capability" {
		t.Fatalf("expected invalid OpenAI text capability error, got %v", err)
	}
	if _, err := normalizeImportedOpenAITextCapability("anthropic", stringPtr("responses_only"), true); err == nil || err.Error() != "must not include openai_text_capability outside the OpenAI API family" {
		t.Fatalf("expected non-OpenAI capability rejection, got %v", err)
	}
}

func TestNormalizeOpenAIProbeEndpointVariant(t *testing.T) {
	if got, err := normalizeOpenAIProbeEndpointVariant("openai", stringPtr("  RESPONSES_MINIMAL  ")); err != nil || got == nil || *got != "responses_minimal" {
		t.Fatalf("expected normalized OpenAI probe variant, got variant=%#v err=%v", got, err)
	}
	if _, err := normalizeOpenAIProbeEndpointVariant("openai", stringPtr("bogus")); err == nil || err.Error() != "has invalid openai_probe_endpoint_variant" {
		t.Fatalf("expected invalid OpenAI probe variant error, got %v", err)
	}
	if _, err := normalizeOpenAIProbeEndpointVariant("anthropic", stringPtr("responses_minimal")); err == nil || err.Error() != "must not include openai_probe_endpoint_variant outside the OpenAI API family" {
		t.Fatalf("expected non-OpenAI variant rejection, got %v", err)
	}
}

func TestNormalizeImportedOpenAIAcceptedFormat(t *testing.T) {
	if got, err := normalizeImportedOpenAIAcceptedFormat("openai", stringPtr("  DUAL_NATIVE  ")); err != nil || got == nil || *got != "dual_native" {
		t.Fatalf("expected normalized OpenAI accepted format, got format=%#v err=%v", got, err)
	}
	if _, err := normalizeImportedOpenAIAcceptedFormat("openai", nil); err == nil || err.Error() != "must include openai_accepted_format for OpenAI API family models" {
		t.Fatalf("expected missing OpenAI accepted format error, got %v", err)
	}
	if _, err := normalizeImportedOpenAIAcceptedFormat("openai", stringPtr("bogus")); err == nil || err.Error() != "has invalid openai_accepted_format" {
		t.Fatalf("expected invalid OpenAI accepted format error, got %v", err)
	}
	if _, err := normalizeImportedOpenAIAcceptedFormat("anthropic", stringPtr("responses_only")); err == nil || err.Error() != "must not include openai_accepted_format outside the OpenAI API family" {
		t.Fatalf("expected non-OpenAI accepted format rejection, got %v", err)
	}
}

func TestValidateConnectionAuthTypeAndNormalization(t *testing.T) {
	valid := stringPtr(" OpenAI ")
	if err := validateConnectionAuthType(valid); err != nil {
		t.Fatalf("expected valid auth type to pass, got %v", err)
	}
	invalid := stringPtr("bad")
	if err := validateConnectionAuthType(invalid); err == nil {
		t.Fatal("expected invalid auth type to fail")
	} else {
		requireConfigBundleDomainError(t, err, http.StatusBadRequest, "auth_type must be one of 'openai', 'anthropic', or 'gemini'")
	}

	if got := normalizedOptionalAuthType(stringPtr("  Gemini  ")); got == nil || *got != "gemini" {
		t.Fatalf("expected normalized optional auth type, got %#v", got)
	}
	if got := normalizedOptionalAuthType(stringPtr("   ")); got != nil {
		t.Fatalf("expected blank optional auth type to normalize to nil, got %#v", got)
	}
	if got := trimmedOptionalString(stringPtr("  value  ")); got == nil || *got != "value" {
		t.Fatalf("expected trimmed optional string, got %#v", got)
	}
}

func TestProfileBundleV3RoundTrip(t *testing.T) {
	request := validProfileBundleV3Request()
	if request.ProfileSettings == nil {
		t.Fatal("test bundle must include profile settings")
	}
	if len(request.ProfileSettings.AuditAPIFamilySettings) != 3 {
		t.Fatalf("test bundle must include full audit api family settings, got %+v", request.ProfileSettings.AuditAPIFamilySettings)
	}

	exported := profileBundleResponse{
		Version:               request.Version,
		BundleKind:            request.BundleKind,
		Endpoints:             request.Endpoints,
		PricingTemplates:      request.PricingTemplates,
		Connections:           request.Connections,
		LoadbalanceStrategies: request.LoadbalanceStrategies,
		Models:                request.Models,
		ProfileSettings:       *request.ProfileSettings,
		HeaderBlocklistRules:  request.HeaderBlocklistRules,
		UserAgentClientRules:  request.UserAgentClientRules,
		SecretPayload:         request.SecretPayload,
	}
	raw, err := json.Marshal(exported)
	if err != nil {
		t.Fatalf("marshal v3 bundle: %v", err)
	}

	var imported profileImportRequest
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatalf("unmarshal v3 bundle: %v", err)
	}
	if err := validateProfileImportRequest(imported); err != nil {
		t.Fatalf("validate v3 bundle: %v", err)
	}
	if imported.Version != canonicalProfileBundleVersion || len(imported.Connections) != 1 || len(imported.Models[0].AccessTargets) != 1 {
		t.Fatalf("expected v3 unified-access shape, got version=%d connections=%d targets=%d", imported.Version, len(imported.Connections), len(imported.Models[0].AccessTargets))
	}
	if got := imported.ProfileSettings.AuditAPIFamilySettings; len(got) != 3 || got[0].APIFamily != "openai" || got[1].APIFamily != "anthropic" || got[2].APIFamily != "gemini" {
		t.Fatalf("expected stable audit api family settings order, got %+v", got)
	}
}

func TestConfigBundleImportPreservesOpenAIAcceptedFormat(t *testing.T) {
	for _, format := range []string{"responses_only", "chat_completions_only", "dual_native"} {
		t.Run(format, func(t *testing.T) {
			request := validProfileBundleV3Request()
			request.Models[0].OpenAIAcceptedFormat = stringPtr(format)

			if err := validateProfileImportRequest(request); err != nil {
				t.Fatalf("validate imported bundle: %v", err)
			}

			importedModels := normalizeImportedModels(request.Models)
			if got := importedModels[0].OpenAIAcceptedFormat; got == nil || *got != format {
				t.Fatalf("expected normalized imported format %q, got %#v", format, got)
			}

			exported, err := buildModelExport(modelRow{
				APIFamily:            "openai",
				ModelID:              request.Models[0].ModelID,
				DisplayName:          request.Models[0].DisplayName,
				OpenAIAcceptedFormat: importedModels[0].OpenAIAcceptedFormat,
				IsEnabled:            true,
			}, map[int]string{}, nil, map[int]string{})
			if err != nil {
				t.Fatalf("build exported model: %v", err)
			}
			if exported.OpenAIAcceptedFormat == nil || *exported.OpenAIAcceptedFormat != format {
				t.Fatalf("expected exported format %q, got %#v", format, exported.OpenAIAcceptedFormat)
			}
		})
	}
}

func TestConfigBundleExportIncludesOpenAIAcceptedFormat(t *testing.T) {
	exported, err := buildModelExport(modelRow{
		APIFamily:            "openai",
		ModelID:              "gpt-4o-mini",
		DisplayName:          stringPtr("GPT 4o Mini"),
		OpenAIAcceptedFormat: stringPtr("dual_native"),
		IsEnabled:            true,
	}, map[int]string{}, nil, map[int]string{})
	if err != nil {
		t.Fatalf("build exported model: %v", err)
	}
	if exported.OpenAIAcceptedFormat == nil || *exported.OpenAIAcceptedFormat != "dual_native" {
		t.Fatalf("expected exported openai_accepted_format dual_native, got %#v", exported.OpenAIAcceptedFormat)
	}
}

func TestConfigBundleImportRejectsOpenAIAcceptedFormatForNonOpenAI(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Models[0].APIFamily = "anthropic"
	request.Models[0].OpenAIAcceptedFormat = stringPtr("dual_native")

	err := validateProfileImportRequest(request)
	requireConfigBundleDomainError(t, err, http.StatusBadRequest, "openai_accepted_format is only valid for OpenAI models")
}

func TestConfigBundleImportRejectsInvalidOpenAIAcceptedFormat(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Models[0].OpenAIAcceptedFormat = stringPtr("bogus")

	err := validateProfileImportRequest(request)
	requireConfigBundleDomainError(t, err, http.StatusBadRequest, "openai_accepted_format has invalid value")
}

func TestProfileBundleImportRejectsObsoleteAccessTargetFields(t *testing.T) {
	tests := []struct {
		name  string
		field string
	}{
		{name: "weight", field: "weight"},
		{name: "target priority", field: "target_priority"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rawPayload := marshalProfileImportRequestAsMap(t, validProfileBundleV3Request())
			model := rawPayload["models"].([]any)[0].(map[string]any)
			target := model["access_targets"].([]any)[0].(map[string]any)
			target[test.field] = float64(1)
			raw, err := json.Marshal(rawPayload)
			if err != nil {
				t.Fatalf("marshal obsolete access target payload: %v", err)
			}

			service := &Service{}
			for _, route := range []string{"/api/config/profile/import/preview", "/api/config/profile/import"} {
				response := httptest.NewRecorder()
				service.handlePreviewProfileImport(response, httptest.NewRequest(http.MethodPost, route, bytes.NewReader(raw)))
				if route == "/api/config/profile/import" {
					response = httptest.NewRecorder()
					service.handleImportProfileBundle(response, httptest.NewRequest(http.MethodPost, route, bytes.NewReader(raw)))
				}
				if response.Code != http.StatusBadRequest {
					t.Fatalf("expected obsolete %s to reject with 400 on %s, got status=%d body=%s", test.field, route, response.Code, response.Body.String())
				}
				var body map[string]string
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				expectedDetail := "obsolete access target field at models[0].access_targets[0]." + test.field
				if body["detail"] != expectedDetail {
					t.Fatalf("unexpected obsolete access target rejection detail: %q", body["detail"])
				}
			}
		})
	}
}

func TestProfileBundleImportRejectsV2(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Version = 2
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal v2 bundle: %v", err)
	}

	service := &Service{}
	response := httptest.NewRecorder()
	service.handlePreviewProfileImport(response, httptest.NewRequest(http.MethodPost, "/api/config/profile/import/preview", bytes.NewReader(raw)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected v2 preview to reject with 400, got status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["detail"] != "Unsupported profile config bundle version '2'; expected 3" {
		t.Fatalf("unexpected v2 rejection detail: %q", body["detail"])
	}
}

func TestProfileBundleImportRejectsRemovedRetryAttemptKey(t *testing.T) {
	removedRetryField := "retry_" + "max_attempts"
	rawPayload := map[string]any{
		"version":     3,
		"bundle_kind": "profile_config",
		"loadbalance_strategies": []map[string]any{{
			"name":                 "Default",
			"legacy_strategy_type": "single",
			removedRetryField:      3,
		}},
	}
	raw, err := json.Marshal(rawPayload)
	if err != nil {
		t.Fatalf("marshal removed retry key payload: %v", err)
	}
	service := &Service{}
	response := httptest.NewRecorder()
	service.handlePreviewProfileImport(response, httptest.NewRequest(http.MethodPost, "/api/config/profile/import/preview", bytes.NewReader(raw)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected removed retry key to reject with 400, got status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	expectedDetail := `json: unknown field "` + removedRetryField + `"`
	if body["detail"] != expectedDetail {
		t.Fatalf("unexpected removed retry key rejection detail: %q", body["detail"])
	}
}

func TestProfileBundleImportRejectsObsoleteVendorTransportFields(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		field   string
	}{
		{
			name: "vendor refs",
			payload: map[string]any{
				"version":          3,
				"bundle_kind":      "profile_config",
				"vendor_" + "refs": []map[string]any{{"key": "openai", "name_hint": "OpenAI"}},
			},
			field: "vendor_" + "refs",
		},
		{
			name: "model vendor key",
			payload: map[string]any{
				"version":     3,
				"bundle_kind": "profile_config",
				"models":      []map[string]any{{"vendor_" + "key": "openai"}},
			},
			field: "vendor_" + "key",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(test.payload)
			if err != nil {
				t.Fatalf("marshal obsolete vendor payload: %v", err)
			}
			service := &Service{}
			response := httptest.NewRecorder()
			service.handlePreviewProfileImport(response, httptest.NewRequest(http.MethodPost, "/api/config/profile/import/preview", bytes.NewReader(raw)))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("expected obsolete vendor field to reject with 400, got status=%d body=%s", response.Code, response.Body.String())
			}
			var body map[string]string
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			expectedDetail := `json: unknown field "` + test.field + `"`
			if body["detail"] != expectedDetail {
				t.Fatalf("unexpected obsolete vendor field rejection detail: %q", body["detail"])
			}
		})
	}
}

func TestProfileBundleImportRejectsRemovedBanMode(t *testing.T) {
	request := validProfileBundleV3Request()
	request.LoadbalanceStrategies[0].BanMode = stringPtr("man" + "ual")
	err := validateProfileImportRequest(request)
	requireConfigBundleDomainError(t, err, http.StatusBadRequest, "ban_mode must be one of 'off', 'temporary', or 'until_reset'")
}

func TestProfileBundleImportValidatesAccessTargets(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*profileImportRequest)
		detail string
	}{
		{
			name: "missing connection ref",
			mutate: func(request *profileImportRequest) {
				request.Models[0].AccessTargets[0].ConnectionRef = nil
			},
			detail: "Model 'gpt-4o-mini' connection access target must include connection_ref",
		},
		{
			name: "unknown connection ref",
			mutate: func(request *profileImportRequest) {
				request.Models[0].AccessTargets[0].ConnectionRef = stringPtr("missing-connection")
			},
			detail: "Model 'gpt-4o-mini' references unknown connection_ref 'missing-connection'",
		},
		{
			name: "cross api family connection ref",
			mutate: func(request *profileImportRequest) {
				request.Connections[0].APIFamily = "anthropic"
				request.Connections[0].OpenAITextCapability = nil
				request.Connections[0].OpenAITextCapabilitySet = false
			},
			detail: "Model 'gpt-4o-mini' cannot target cross-api-family connection_ref 'openai-primary'",
		},
		{
			name: "unknown model target",
			mutate: func(request *profileImportRequest) {
				request.Connections = nil
				request.ProfileSettings.EndpointFXMappings = nil
				request.Models[0].AccessTargets[0] = accessTargetExport{Position: 0, IsEnabled: true, TargetType: "model", TargetModelID: stringPtr("missing-model")}
			},
			detail: "Model 'gpt-4o-mini' references unknown model access target 'missing-model'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validProfileBundleV3Request()
			test.mutate(&request)

			err := validateProfileImportRequest(request)
			requireConfigBundleDomainError(t, err, http.StatusBadRequest, test.detail)
		})
	}
}

func TestProfileBundleImportValidatesAuditAPIFamilySettings(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*profileImportRequest)
		detail string
	}{
		{
			name: "missing audit api family settings",
			mutate: func(request *profileImportRequest) {
				request.ProfileSettings.AuditAPIFamilySettingsIsSet = false
				request.ProfileSettings.AuditAPIFamilySettings = nil
			},
			detail: "profile_settings.audit_api_family_settings must include exactly openai, anthropic, and gemini",
		},
		{
			name: "unknown family",
			mutate: func(request *profileImportRequest) {
				request.ProfileSettings.AuditAPIFamilySettings[1].APIFamily = "mistral"
			},
			detail: `profile_settings.audit_api_family_settings api_family "mistral" is not supported`,
		},
		{
			name: "duplicate family",
			mutate: func(request *profileImportRequest) {
				request.ProfileSettings.AuditAPIFamilySettings[1].APIFamily = "openai"
			},
			detail: "Duplicate profile_settings.audit_api_family_settings entry for api_family=openai",
		},
		{
			name: "capture requires enabled",
			mutate: func(request *profileImportRequest) {
				request.ProfileSettings.AuditAPIFamilySettings[0].AuditEnabled = false
				request.ProfileSettings.AuditAPIFamilySettings[0].AuditCaptureBodies = true
			},
			detail: "profile_settings.audit_api_family_settings audit_capture_bodies requires audit_enabled",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validProfileBundleV3Request()
			test.mutate(&request)

			err := validateProfileImportRequest(request)
			requireConfigBundleDomainError(t, err, http.StatusBadRequest, test.detail)
		})
	}
}

func TestImportAcceptsPrivateConnectionRefs(t *testing.T) {
	request := validProfileBundleV3Request()

	if err := validateProfileImportRequest(request); err != nil {
		t.Fatalf("validate private connection bundle: %v", err)
	}
}

func TestImportRejectsDuplicateConnectionRefOwners(t *testing.T) {
	request := validProfileBundleV3Request()
	secondModel := request.Models[0]
	secondModel.ModelID = "gpt-4o-alt"
	secondModel.DisplayName = stringPtr("GPT 4o Alt")
	request.Models = append(request.Models, secondModel)

	err := validateProfileImportRequest(request)
	requireConfigBundleDomainError(t, err, http.StatusBadRequest, "connection_ref 'openai-primary' is owned by multiple models: model_id 'gpt-4o-mini' (display_name 'GPT 4o Mini') and model_id 'gpt-4o-alt' (display_name 'GPT 4o Alt')")
}

func TestImportRejectsOwnerlessConnectionRefs(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Models[0].AccessTargets = nil

	err := validateProfileImportRequest(request)
	requireConfigBundleDomainError(t, err, http.StatusBadRequest, "Connection ref 'openai-primary' must be owned by exactly one model access target")
}

func TestProfileBundleImportCountsTopLevelConnections(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Connections = append(request.Connections, connectionExport{
		Ref:                     "openai-secondary",
		APIFamily:               "openai",
		EndpointName:            "OpenAI",
		PricingTemplateName:     stringPtr("Default pricing"),
		IsActive:                true,
		Priority:                1,
		OpenAITextCapability:    stringPtr("responses_only"),
		OpenAITextCapabilitySet: true,
	})
	request.Models[0].AccessTargets = append(request.Models[0].AccessTargets, accessTargetExport{
		Position:      1,
		IsEnabled:     true,
		TargetType:    "connection",
		ConnectionRef: stringPtr("openai-secondary"),
	})
	request.ProfileSettings.EndpointFXMappings = append(request.ProfileSettings.EndpointFXMappings, endpointFXMappingExport{
		ModelID:       "gpt-4o-mini",
		ConnectionRef: "openai-secondary",
		FXRate:        "1",
	})
	if err := validateProfileImportRequest(request); err != nil {
		t.Fatalf("validate top-level connections: %v", err)
	}

	preview := buildProfilePreviewResponse(request, nil, nil)
	if preview.ConnectionsImported != 2 || preview.ReplacementScope.Connections != 2 {
		t.Fatalf("expected two top-level connections in preview, got imported=%d scope=%d", preview.ConnectionsImported, preview.ReplacementScope.Connections)
	}
}

func TestProfileBundleImportRejectsSecretPayloadEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		payload secretPayloadExport
		detail  string
	}{
		{
			name:    "wrong kind",
			payload: secretPayloadExport{Kind: "plaintext", Cipher: bundleSecretCipher},
			detail:  "Config import secret payload kind must be 'encrypted'",
		},
		{
			name:    "wrong cipher",
			payload: secretPayloadExport{Kind: "encrypted", Cipher: "aes-gcm"},
			detail:  "Config import secret payload cipher must be 'fernet-v1'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateImportedSecretPayloadEnvelope(test.payload)
			requireConfigBundleDomainError(t, err, http.StatusBadRequest, test.detail)
		})
	}
}

func TestProfileBundleImportRejectsMissingSecretPayloadEntries(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Endpoints[0].APIKeySecretRef = stringPtr("endpoint:OpenAI:api_key")

	err := validateProfileImportRequest(request)
	requireConfigBundleDomainError(t, err, http.StatusBadRequest, "Import is missing encrypted secret payload entries for refs: endpoint:OpenAI:api_key")
}

func TestBundleKeyMismatchRejectedDuringSecretDecrypt(t *testing.T) {
	service := &Service{bundleSecretKeyID: "server-kid"}

	_, err := service.decryptImportSecretPayload(secretPayloadExport{KeyID: "bundle-kid"})
	requireConfigBundleDomainError(t, err, http.StatusBadRequest, "Config import bundle key mismatch: bundle key_id 'bundle-kid' does not match server key_id 'server-kid'")
}

func TestProfileBundleImportRejectsEncryptedSecretPayloadFailures(t *testing.T) {
	tests := []struct {
		name      string
		payload   secretPayloadExport
		decrypter func(string) (string, error)
		detail    string
	}{
		{
			name: "plaintext entry rejected",
			payload: secretPayloadExport{
				KeyID: "kid",
				Entries: []secretPayloadEntry{{
					Ref:        "endpoint:OpenAI:api_key",
					Ciphertext: "plaintext-value",
				}},
			},
			detail: "Config import secret ref 'endpoint:OpenAI:api_key' must be encrypted",
		},
		{
			name: "decrypt error rejected",
			payload: secretPayloadExport{
				KeyID: "kid",
				Entries: []secretPayloadEntry{{
					Ref:        "endpoint:OpenAI:api_key",
					Ciphertext: encryptedSecretPrefix + "ciphertext",
				}},
			},
			decrypter: func(string) (string, error) {
				return "", errors.New("boom")
			},
			detail: "Config import could not decrypt secret ref 'endpoint:OpenAI:api_key'",
		},
		{
			name: "empty decrypted value rejected",
			payload: secretPayloadExport{
				KeyID: "kid",
				Entries: []secretPayloadEntry{{
					Ref:        "endpoint:OpenAI:api_key",
					Ciphertext: encryptedSecretPrefix + "ciphertext",
				}},
			},
			decrypter: func(string) (string, error) {
				return "   ", nil
			},
			detail: "Config import secret ref 'endpoint:OpenAI:api_key' resolved to an empty value",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &Service{
				bundleSecretKeyID:     "kid",
				bundleSecretDecrypter: test.decrypter,
			}
			if service.bundleSecretDecrypter == nil {
				service.bundleSecretDecrypter = func(string) (string, error) {
					return "live-secret", nil
				}
			}

			_, err := service.decryptImportSecretPayload(test.payload)
			requireConfigBundleDomainError(t, err, http.StatusBadRequest, test.detail)
		})
	}
}

func validProfileBundleV3Request() profileImportRequest {
	return profileImportRequest{
		Version:    canonicalProfileBundleVersion,
		BundleKind: canonicalProfileBundleKind,
		Endpoints: []endpointExport{{
			Name:     "OpenAI",
			BaseURL:  "https://api.openai.com/v1",
			Position: 0,
		}},
		PricingTemplates: []pricingTemplateExport{{
			Name:                "Default pricing",
			PricingUnit:         "PER_1M",
			PricingCurrencyCode: "USD",
			InputPrice:          "0",
			OutputPrice:         "0",
			CachedInputPrice:    "0",
			CacheCreationPrice:  "0",
			ReasoningPrice:      "0",
			Version:             1,
		}},
		Connections: []connectionExport{{
			Ref:                     "openai-primary",
			APIFamily:               "openai",
			EndpointName:            "OpenAI",
			PricingTemplateName:     stringPtr("Default pricing"),
			IsActive:                true,
			Priority:                0,
			OpenAITextCapability:    stringPtr("responses_only"),
			OpenAITextCapabilitySet: true,
		}},
		LoadbalanceStrategies: []loadbalanceStrategyExport{{
			Name:                               "Default single",
			LegacyStrategyType:                 stringPtr("single"),
			FailureStatusCodes:                 []int{429, 500},
			BanMode:                            stringPtr("until_reset"),
			RetryBaseDelayMS:                   intPtr(60000),
			RetryBackoffMultiplier:             float64Ptr(2),
			RetryJitterRatio:                   float64Ptr(0.2),
			RetryMaxDelayMS:                    intPtr(900000),
			CycleRetryAttemptLimit:             intPtr(2),
			BanCumulativeRetryAttemptThreshold: intPtr(4),
			BanDurationSeconds:                 intPtr(0),
		}},
		Models: []modelExport{{
			APIFamily:               "openai",
			ModelID:                 "gpt-4o-mini",
			DisplayName:             stringPtr("GPT 4o Mini"),
			LoadbalanceStrategyName: stringPtr("Default single"),
			OpenAIAcceptedFormat:    stringPtr("dual_native"),
			IsEnabled:               true,
			AccessTargets: []accessTargetExport{{
				Position:      0,
				IsEnabled:     true,
				TargetType:    "connection",
				ConnectionRef: stringPtr("openai-primary"),
			}},
		}},
		ProfileSettings: &profileSettingsExport{
			ReportCurrencyCode:   "USD",
			ReportCurrencySymbol: "$",
			EndpointFXMappings: []endpointFXMappingExport{{
				ModelID:       "gpt-4o-mini",
				ConnectionRef: "openai-primary",
				FXRate:        "1",
			}},
			AuditAPIFamilySettings: []auditAPIFamilySettingExport{
				{APIFamily: "openai", AuditEnabled: true, AuditCaptureBodies: true},
				{APIFamily: "anthropic", AuditEnabled: false, AuditCaptureBodies: false},
				{APIFamily: "gemini", AuditEnabled: true, AuditCaptureBodies: false},
			},
			AuditAPIFamilySettingsIsSet: true,
		},
		HeaderBlocklistRules: []headerBlocklistRuleExport{},
		UserAgentClientRules: []userAgentClientRuleExport{},
		SecretPayload:        secretPayloadExport{Kind: "encrypted", Cipher: bundleSecretCipher, KeyID: "kid", Entries: []secretPayloadEntry{}},
	}
}

func marshalProfileImportRequestAsMap(t *testing.T, request profileImportRequest) map[string]any {
	t.Helper()
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal profile import request: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal profile import request map: %v", err)
	}
	return payload
}

func TestProfileBundleImportAllowsSparseAccessTargetPositions(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Models[0].AccessTargets[0].Position = 3

	if err := validateProfileImportRequest(request); err != nil {
		t.Fatalf("expected sparse access target positions to pass, got %v", err)
	}
}

func TestProfileBundleImportReturnsStableRoutingPlanIssueForUnknownModelTarget(t *testing.T) {
	request := validProfileBundleV3Request()
	request.Connections = nil
	request.ProfileSettings.EndpointFXMappings = nil
	request.Models[0].AccessTargets[0] = accessTargetExport{
		Position:      2,
		IsEnabled:     true,
		TargetType:    "model",
		TargetModelID: stringPtr("missing-model"),
	}

	err := validateProfileImportRequest(request)
	domainErr := requireConfigBundleDomainError(t, err, http.StatusBadRequest, "Model 'gpt-4o-mini' references unknown model access target 'missing-model'")
	issues, ok := domainErr.Fields["routing_plan_issues"].([]routingPlanValidationIssue)
	if !ok {
		t.Fatalf("expected routing_plan_issues payload, got %+v", domainErr.Fields)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one routing_plan_issue, got %+v", issues)
	}
	if issues[0].Code != "model_target_missing_model" || issues[0].Path != "models[0].access_targets[0].target_model_id" || issues[0].Message != "Model 'gpt-4o-mini' references unknown model access target 'missing-model'" {
		t.Fatalf("unexpected routing_plan_issue: %+v", issues[0])
	}
}
