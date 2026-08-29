package modelexport

import (
	"encoding/json"
	"errors"
	"testing"
)

func ptrString(value string) *string { return &value }

func rawValue(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func TestMergeKnownMetadataPreservesPresenceAndProvenance(t *testing.T) {
	prism := NewMetadataLayer(map[string]json.RawMessage{
		MetaName:      rawValue("Stored Name"),
		MetaReasoning: rawValue(false), // explicit false stays present
	})
	pi := NewMetadataLayer(map[string]json.RawMessage{
		MetaName:          rawValue("Newer Name"),
		MetaContextWindow: rawValue(200000),
	})
	result, err := MergeKnownMetadata(MergeOptions{Prism: prism, Pi: pi})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if value, _ := result.Merged.Get(MetaName); string(value) != `"Stored Name"` {
		t.Fatalf("prism layer must win by default, got %s", value)
	}
	if value, ok := result.Merged.Get(MetaContextWindow); !ok || string(value) != "200000" {
		t.Fatalf("persisted Pi template must fill missing leaves, got %s (present=%v)", value, ok)
	}
	if provenance := result.Provenance[MetaName]; provenance != SourcePrism {
		t.Fatalf("name provenance = %s", provenance)
	}
	if provenance := result.Provenance[MetaContextWindow]; provenance != SourcePiCatalog {
		t.Fatalf("context provenance = %s", provenance)
	}
	for _, leaf := range []string{MetaReasoning} {
		found := false
		for _, missing := range result.Missing {
			if missing == leaf {
				found = true
			}
		}
		if found {
			t.Fatalf("explicit false must not count as missing: %v", result.Missing)
		}
	}
}

func TestMergeRejectsSensitiveKeysFailClosed(t *testing.T) {
	sensitive := NewMetadataLayer(map[string]json.RawMessage{"api_key": rawValue("x")})
	if _, err := MergeKnownMetadata(MergeOptions{Pi: sensitive}); err == nil {
		t.Fatalf("credential-shaped metadata leaf must fail closed")
	}
}

func TestKeyLooksSensitiveCoversRecursiveCredentialNames(t *testing.T) {
	for _, key := range []string{
		"apiKey", "api_key", "Authorization", "options.secretToken", "x-password", "private_key_pem",
		"proxyKey", "proxy_key", "Proxy-Key", "x-auth-token", "access-token", "session-token",
	} {
		if !KeyLooksSensitive(key) {
			t.Fatalf("%q must be detected as sensitive", key)
		}
	}
	for _, key := range []string{"contextWindow", "reasoning_effort", "thinkingLevelMap", "maxTokens"} {
		if KeyLooksSensitive(key) {
			t.Fatalf("%q must not be flagged", key)
		}
	}
}

func TestValidatePiSourceFieldRejectsNestedCredentialShapedCompat(t *testing.T) {
	err := ValidatePiSourceField(piAPIOpenAIChat, "compat", map[string]any{
		"chatTemplateKwargs": map[string]any{"authorization": "secret"},
	})
	if err == nil {
		t.Fatalf("credential-shaped nested compat must fail closed")
	}
}

func TestValidatePiSourceFieldUsesBoundAPISchema(t *testing.T) {
	if err := ValidatePiSourceField(piAPIResponses, "compat", map[string]any{
		"supportsExplicitPromptCacheMode": true,
	}); err != nil {
		t.Fatalf("Responses-safe compat was rejected: %v", err)
	}
	tests := []struct {
		name  string
		api   string
		value map[string]any
	}{
		{name: "cross API", api: piAPIResponses, value: map[string]any{"zaiToolStream": true}},
		{name: "routing", api: piAPIOpenAIChat, value: map[string]any{"openRouterRouting": map[string]any{}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := ValidatePiSourceField(test.api, "compat", test.value)
			var schemaErr *ErrTargetSchema
			if !errors.As(err, &schemaErr) {
				t.Fatalf("error = %v, want ErrTargetSchema", err)
			}
		})
	}
	if err := ValidatePiSourceField(piAPIResponses, "headers", map[string]any{"x": "y"}); err == nil {
		t.Fatalf("headers must never be accepted as a Pi binding field")
	}
	if err := ValidatePiSourceField(piAPIAnthropic, "compat", map[string]any{"allowEmptySignature": true}); err != nil {
		t.Fatalf("known Anthropic compat fields must not be rejected as credential-shaped: %v", err)
	}
	for _, field := range []string{"context_window", "max_tokens"} {
		for _, invalid := range []int64{-1, 0} {
			if err := ValidatePiSourceField(piAPIResponses, field, invalid); err == nil {
				t.Fatalf("%s must reject non-positive override %d before persistence", field, invalid)
			}
		}
		if err := ValidatePiSourceField(piAPIResponses, field, int64(1)); err != nil {
			t.Fatalf("%s must accept a positive value: %v", field, err)
		}
	}
}

func TestKnownMetadataLeavesArePiRendererFieldsOnly(t *testing.T) {
	want := []string{MetaName, MetaReasoning, MetaContextWindow, MetaMaxOutputTokens, MetaModalitiesInput}
	got := KnownMetadataLeaves()
	if len(got) != len(want) {
		t.Fatalf("KnownMetadataLeaves() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("KnownMetadataLeaves() = %v, want %v", got, want)
		}
	}
}

func TestMetadataWarningsReportOnlyPiMetadataGaps(t *testing.T) {
	warnings := MetadataWarningCodes(MetadataLayer{})
	if !containsWarning(warnings, WarningMetadataIncomplete) {
		t.Fatalf("missing Pi metadata warnings = %v", warnings)
	}
}
