package modelexport

import (
	"encoding/json"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
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
	modelsDev := NewMetadataLayer(map[string]json.RawMessage{
		MetaName:          rawValue("Newer Name"),
		MetaContextWindow: rawValue(200000),
	})
	result, err := MergeKnownMetadata(MergeOptions{Prism: prism, ModelsDev: modelsDev})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if value, _ := result.Merged.Get(MetaName); string(value) != `"Stored Name"` {
		t.Fatalf("prism layer must win by default, got %s", value)
	}
	if value, ok := result.Merged.Get(MetaContextWindow); !ok || string(value) != "200000" {
		t.Fatalf("models.dev must fill missing leaves, got %s (present=%v)", value, ok)
	}
	if provenance := result.Provenance[MetaName]; provenance != SourcePrism {
		t.Fatalf("name provenance = %s", provenance)
	}
	if provenance := result.Provenance[MetaContextWindow]; provenance != SourceModelsDev {
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

func TestMergeKnownMetadataManualFillsAndOverrides(t *testing.T) {
	prism := NewMetadataLayer(map[string]json.RawMessage{MetaName: rawValue("Prism Name")})
	manual := NewMetadataLayer(map[string]json.RawMessage{
		MetaFamily: rawValue("glm"),
		MetaName:   rawValue("Renamed"),
	})
	// Without override_fields the present name is untouched and family fills.
	filled, err := MergeKnownMetadata(MergeOptions{Prism: prism, Manual: manual})
	if err != nil {
		t.Fatalf("merge fill: %v", err)
	}
	if value, _ := filled.Merged.Get(MetaName); string(value) != `"Prism Name"` {
		t.Fatalf("manual must not overwrite without override_fields")
	}
	if value, _ := filled.Merged.Get(MetaFamily); string(value) != `"glm"` {
		t.Fatalf("manual must fill missing leaves")
	}
	if provenance := filled.Provenance[MetaFamily]; provenance != SourceManual {
		t.Fatalf("family provenance = %s", provenance)
	}
	// With override_fields the name is replaced.
	overridden, err := MergeKnownMetadata(MergeOptions{
		Prism: prism, Manual: manual, OverrideFields: []string{"name"},
	})
	if err != nil {
		t.Fatalf("merge override: %v", err)
	}
	if value, _ := overridden.Merged.Get(MetaName); string(value) != `"Renamed"` {
		t.Fatalf("override_fields must allow replacement, got %s", value)
	}
}

func TestMergeRejectsSensitiveKeysFailClosed(t *testing.T) {
	prism := MetadataLayer{}
	manual := MetadataLayer{}
	// Sensitive keys never enter layers; the guard runs on manual keys via
	// the enhancement path tested below. The metadata merge itself rejects
	// credential-shaped leaves defensively.
	sensitive := NewMetadataLayer(map[string]json.RawMessage{"api_key": rawValue("x")})
	if _, err := MergeKnownMetadata(MergeOptions{Prism: prism, Manual: sensitive}); err == nil {
		t.Fatalf("credential-shaped metadata leaf must fail closed")
	}
	_ = manual
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

func TestDerivePiThinkingLevelMapIsTotalAndMapsNoneToOff(t *testing.T) {
	raw := derivePiThinkingLevelMap([]modelsdev.ReasoningOption{{
		Type:   modelsdev.ReasoningOptionEffort,
		Values: []*string{nil, ptrString("none"), ptrString("low"), ptrString("high")},
	}})
	if raw == nil {
		t.Fatalf("effort options must produce a thinkingLevelMap")
	}
	var levels map[string]*string
	if err := json.Unmarshal(raw, &levels); err != nil {
		t.Fatalf("decode thinkingLevelMap: %v", err)
	}
	if len(levels) != 7 {
		t.Fatalf("thinkingLevelMap keys = %v, want all seven Pi levels", levels)
	}
	if levels["off"] == nil || *levels["off"] != "none" || levels["low"] == nil || *levels["low"] != "low" {
		t.Fatalf("supported effort mapping drifted: %v", levels)
	}
	for _, unsupported := range []string{"minimal", "medium", "xhigh", "max"} {
		if value, exists := levels[unsupported]; !exists || value != nil {
			t.Fatalf("unsupported level %q = %v (present=%v), want explicit null", unsupported, value, exists)
		}
	}
}

func TestDerivePiCandidateWarnsWhenReasoningOptionsCannotMap(t *testing.T) {
	candidate := DeriveCandidate(PlatformPi, "openai", ptrString("responses_only"), &modelsdev.Model{
		ReasoningOptions: []modelsdev.ReasoningOption{{Type: modelsdev.ReasoningOptionToggle}},
	})
	if _, exists := candidate.DerivedFields["thinkingLevelMap"]; exists {
		t.Fatalf("toggle-only reasoning must not invent a thinkingLevelMap")
	}
	if !containsWarning(candidate.WarningCodes, WarningThinkingMapUnrepresentable) {
		t.Fatalf("candidate warnings = %v, want %s", candidate.WarningCodes, WarningThinkingMapUnrepresentable)
	}
}

func TestMetadataWarningsDistinguishBoundOutageFromMissingMetadata(t *testing.T) {
	fact := ModelFact{
		CatalogBinding: CatalogEvidence{Bound: true},
		Enrichment:     EnrichmentEvidence{Available: false},
	}
	warnings := MetadataWarningCodes(PlatformPi, fact, MetadataLayer{})
	if !containsWarning(warnings, WarningEnrichmentUnavailable) || !containsWarning(warnings, WarningMetadataIncomplete) {
		t.Fatalf("bound unavailable warnings = %v", warnings)
	}
	fact.CatalogBinding.Bound = false
	warnings = MetadataWarningCodes(PlatformPi, fact, MetadataLayer{})
	if containsWarning(warnings, WarningEnrichmentUnavailable) || !containsWarning(warnings, WarningMetadataIncomplete) {
		t.Fatalf("unbound missing-metadata warnings = %v", warnings)
	}
}
