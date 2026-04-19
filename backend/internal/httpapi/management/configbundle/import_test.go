package configbundle

import (
	"errors"
	"net/http"
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
