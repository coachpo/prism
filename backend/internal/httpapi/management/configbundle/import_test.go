package configbundle

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func requireConfigBundleDomainError(t *testing.T, err error, status int, detail string) {
	t.Helper()

	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domainError, got %T", err)
	}
	if domainErr.StatusCode != status || domainErr.Detail != detail {
		t.Fatalf("expected domainError (%d, %q), got (%d, %q)", status, detail, domainErr.StatusCode, domainErr.Detail)
	}
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

func TestProfileBundleV2RoundTrip(t *testing.T) {
	request := validProfileBundleV2Request()
	if request.ProfileSettings == nil {
		t.Fatal("test bundle must include profile settings")
	}

	exported := profileBundleResponse{
		Version:               request.Version,
		BundleKind:            request.BundleKind,
		VendorRefs:            request.VendorRefs,
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
		t.Fatalf("marshal v2 bundle: %v", err)
	}

	var imported profileImportRequest
	if err := json.Unmarshal(raw, &imported); err != nil {
		t.Fatalf("unmarshal v2 bundle: %v", err)
	}
	if err := validateProfileImportRequest(imported); err != nil {
		t.Fatalf("validate v2 bundle: %v", err)
	}
	if imported.Version != canonicalProfileBundleVersion || len(imported.Connections) != 1 || len(imported.Models[0].AccessTargets) != 1 {
		t.Fatalf("expected v2 unified-access shape, got version=%d connections=%d targets=%d", imported.Version, len(imported.Connections), len(imported.Models[0].AccessTargets))
	}
}

func TestProfileBundleImportRejectsV1(t *testing.T) {
	request := validProfileBundleV2Request()
	request.Version = 1
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal v1 bundle: %v", err)
	}

	service := &Service{}
	response := httptest.NewRecorder()
	service.handlePreviewProfileImport(response, httptest.NewRequest(http.MethodPost, "/api/config/profile/import/preview", bytes.NewReader(raw)))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected v1 preview to reject with 400, got status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body["detail"] != "Unsupported profile config bundle version '1'; expected 2" {
		t.Fatalf("unexpected v1 rejection detail: %q", body["detail"])
	}
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
			},
			detail: "Model 'gpt-4o-mini' cannot target cross-api-family connection_ref 'openai-primary'",
		},
		{
			name: "unknown model target",
			mutate: func(request *profileImportRequest) {
				request.Models[0].AccessTargets[0] = accessTargetExport{Position: 0, IsEnabled: true, TargetType: "model", TargetModelID: stringPtr("missing-model")}
			},
			detail: "Model 'gpt-4o-mini' references unknown model access target 'missing-model'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := validProfileBundleV2Request()
			test.mutate(&request)

			err := validateProfileImportRequest(request)
			requireConfigBundleDomainError(t, err, http.StatusBadRequest, test.detail)
		})
	}
}

func TestProfileBundleImportCountsTopLevelConnections(t *testing.T) {
	request := validProfileBundleV2Request()
	request.Connections = append(request.Connections, connectionExport{
		Ref:                 "openai-secondary",
		APIFamily:           "openai",
		EndpointName:        "OpenAI",
		PricingTemplateName: stringPtr("Default pricing"),
		IsActive:            true,
		Priority:            1,
	})
	if err := validateProfileImportRequest(request); err != nil {
		t.Fatalf("validate top-level connections: %v", err)
	}

	preview := buildProfilePreviewResponse(request, nil, nil, nil, nil)
	if preview.ConnectionsImported != 2 || preview.ReplacementScope.Connections != 2 {
		t.Fatalf("expected two top-level connections in preview, got imported=%d scope=%d", preview.ConnectionsImported, preview.ReplacementScope.Connections)
	}
}

func validProfileBundleV2Request() profileImportRequest {
	return profileImportRequest{
		Version:    canonicalProfileBundleVersion,
		BundleKind: canonicalProfileBundleKind,
		VendorRefs: []vendorRefExport{{
			Key:      "openai",
			NameHint: "OpenAI",
		}},
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
			Ref:                 "openai-primary",
			APIFamily:           "openai",
			EndpointName:        "OpenAI",
			PricingTemplateName: stringPtr("Default pricing"),
			IsActive:            true,
			Priority:            0,
		}},
		LoadbalanceStrategies: []loadbalanceStrategyExport{{
			Name:                   "Default single",
			LegacyStrategyType:     stringPtr("single"),
			FailureStatusCodes:     []int{429, 500},
			BanMode:                stringPtr("off"),
			RetryBaseDelayMS:       intPtr(60000),
			RetryBackoffMultiplier: float64Ptr(2),
			RetryJitterRatio:       float64Ptr(0.2),
			RetryMaxDelayMS:        intPtr(900000),
			RetryMaxAttempts:       intPtr(3),
			BanDurationSeconds:     intPtr(0),
		}},
		Models: []modelExport{{
			VendorKey:               stringPtr("openai"),
			APIFamily:               "openai",
			ModelID:                 "gpt-4o-mini",
			DisplayName:             stringPtr("GPT 4o Mini"),
			LoadbalanceStrategyName: stringPtr("Default single"),
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
		},
		HeaderBlocklistRules: []headerBlocklistRuleExport{},
		UserAgentClientRules: []userAgentClientRuleExport{},
		SecretPayload:        secretPayloadExport{Kind: "encrypted", Cipher: bundleSecretCipher, KeyID: "kid", Entries: []secretPayloadEntry{}},
	}
}
