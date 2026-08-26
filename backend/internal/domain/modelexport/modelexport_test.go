package modelexport

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

func ptrString(value string) *string { return &value }
func ptrInt(value int) *int          { return &value }

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
		Type: modelsdev.ReasoningOptionEffort, Values: []string{"none", "low", "high"},
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

func standardTarget(card PriceCardSnapshot) TargetPriceSnapshot {
	return TargetPriceSnapshot{
		TerminalTargetID: 1,
		Kind:             pricingkind.Standard,
		Card:             &card,
		CurrencyCode:     "USD",
		PricingUnit:      "PER_1M",
	}
}

func completeCard() PriceCardSnapshot {
	return PriceCardSnapshot{
		InputPrice:         "3",
		OutputPrice:        "15",
		CachedInputPrice:   ptrString("0.3"),
		CacheCreationPrice: ptrString("3.75"),
		ReasoningPrice:     ptrString("15"),
	}
}

func TestDecidePriceExportHappyPathBothPlatforms(t *testing.T) {
	target := standardTarget(completeCard())
	for _, platform := range []Platform{PlatformPi, PlatformOpenCode} {
		decision := DecidePriceExport(platform, []TargetPriceSnapshot{target})
		if !decision.Exportable || len(decision.WarningCodes) != 0 {
			t.Fatalf("%s must export a complete flat price: %+v", platform, decision)
		}
	}
}

func TestDecidePriceExportGatesKeepModelAndOmitCost(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*TargetPriceSnapshot)
		want   string
	}{
		{name: "missing reasoning price", mutate: func(target *TargetPriceSnapshot) {
			target.Card.ReasoningPrice = nil
		}, want: WarningPriceIncompleteComponents},
		{name: "reasoning differs from output", mutate: func(target *TargetPriceSnapshot) {
			target.Card.ReasoningPrice = ptrString("20")
		}, want: WarningPriceReasoningMismatch},
		{name: "non USD currency", mutate: func(target *TargetPriceSnapshot) {
			target.CurrencyCode = "CNY"
		}, want: WarningPriceCurrencyNotUSD},
		{name: "wrong unit", mutate: func(target *TargetPriceSnapshot) {
			target.PricingUnit = "PER_1K"
		}, want: WarningPriceUnitNotPerMillion},
		{name: "peak valley", mutate: func(target *TargetPriceSnapshot) {
			target.Kind = pricingkind.PeakValley
			target.Card = nil
		}, want: WarningPricePeakValleyUnrepresentable},
	}
	for _, testCase := range cases {
		target := standardTarget(completeCard())
		testCase.mutate(&target)
		for _, platform := range []Platform{PlatformPi, PlatformOpenCode} {
			decision := DecidePriceExport(platform, []TargetPriceSnapshot{target})
			if decision.Exportable {
				t.Fatalf("%s/%s must keep the cost group omitted", testCase.name, platform)
			}
			found := false
			for _, code := range decision.WarningCodes {
				if code == testCase.want {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s/%s warnings = %v, want %s", testCase.name, platform, decision.WarningCodes, testCase.want)
			}
		}
	}
}

func TestDecidePriceExportTierRepresentability(t *testing.T) {
	base := completeCard()
	above := completeCard()
	above.InputPrice = "6"
	tiered := TargetPriceSnapshot{
		TerminalTargetID: 1,
		Kind:             pricingkind.Tiered,
		BaseCard:         &base,
		AboveCard:        &above,
		TierThreshold:    ptrInt(200000),
		CurrencyCode:     "USD",
		PricingUnit:      "PER_1M",
	}
	if decision := DecidePriceExport(PlatformOpenCode, []TargetPriceSnapshot{tiered}); !decision.Exportable {
		t.Fatalf("opencode can express the exact 200000-token tier: %+v", decision.WarningCodes)
	}
	if decision := DecidePriceExport(PlatformPi, []TargetPriceSnapshot{tiered}); !decision.Exportable {
		t.Fatalf("pi can express one strict tier: %+v", decision.WarningCodes)
	}
	*tiered.TierThreshold = 200001
	if decision := DecidePriceExport(PlatformOpenCode, []TargetPriceSnapshot{tiered}); decision.Exportable {
		t.Fatalf("opencode cannot express an arbitrary tier threshold")
	}
}

func TestDecidePriceExportConflictingTargetsFailClosed(t *testing.T) {
	first := standardTarget(completeCard())
	second := standardTarget(completeCard())
	second.TerminalTargetID = 2
	second.Card.OutputPrice = "16"
	decision := DecidePriceExport(PlatformPi, []TargetPriceSnapshot{first, second})
	if decision.Exportable {
		t.Fatalf("conflicting target prices must fail closed")
	}
	found := false
	for _, code := range decision.WarningCodes {
		if code == WarningPriceTargetConflict {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings %v must include target conflict", decision.WarningCodes)
	}
}

func TestDecidePriceExportKeepsMissingReachableTargets(t *testing.T) {
	configured := standardTarget(completeCard())
	missing := TargetPriceSnapshot{TerminalTargetID: 2}
	decision := DecidePriceExport(PlatformPi, []TargetPriceSnapshot{configured, missing})
	if decision.Exportable {
		t.Fatalf("a reachable target without a current template must omit the whole cost group")
	}
	if !containsWarning(decision.WarningCodes, WarningPriceNoTemplate) || !containsWarning(decision.WarningCodes, WarningPriceTargetConflict) {
		t.Fatalf("warnings = %v, want no-template and target-conflict evidence", decision.WarningCodes)
	}
}

func TestDecidePriceExportTreatsExplicitZeroAsConfigured(t *testing.T) {
	zero := "0"
	card := PriceCardSnapshot{
		InputPrice: "0", OutputPrice: "0", CachedInputPrice: &zero,
		CacheCreationPrice: &zero, ReasoningPrice: &zero,
	}
	decision := DecidePriceExport(PlatformPi, []TargetPriceSnapshot{standardTarget(card)})
	if !decision.Exportable || len(decision.WarningCodes) != 0 {
		t.Fatalf("explicit zero prices are configured and free: %+v", decision)
	}
}

func containsWarning(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

func TestNormalizeSelectionDedupesSortsAndFailsClosed(t *testing.T) {
	facts := SourceFacts{Models: []ModelFact{
		{ModelConfigID: 7, ModelID: "b", Selectable: true},
		{ModelConfigID: 3, ModelID: "a", Selectable: true},
		{ModelConfigID: 9, ModelID: "c", Selectable: false, UnselectableReason: ptrString("model_disabled")},
	}}
	selection, err := NormalizeSelection([]int{7, 3, 7}, facts)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(selection) != 2 || selection[0] != 3 || selection[1] != 7 {
		t.Fatalf("selection = %v", selection)
	}
	if _, err := NormalizeSelection(nil, facts); err == nil {
		t.Fatalf("empty selection must fail")
	}
	if _, err := NormalizeSelection([]int{11}, facts); err == nil {
		t.Fatalf("unknown id must fail with not-found semantics")
	}
	if _, err := NormalizeSelection([]int{9}, facts); err == nil {
		t.Fatalf("unselectable id must fail the whole request")
	}
}

func TestDecimalMarshalsVerbatimIncludingZeros(t *testing.T) {
	for literal, want := range map[string]string{
		"0":       "0",
		"0.0":     "0.0",
		"12.5":    "12.5",
		"300.75":  "300.75",
		"1000000": "1000000",
	} {
		raw, err := json.Marshal(decimal(literal))
		if err != nil || string(raw) != want {
			t.Fatalf("decimal(%q) = %s, %v; want %s", literal, raw, err, want)
		}
	}
	for _, literal := range []string{"", "-1", "1e5", "abc", "1.2.3"} {
		if _, err := json.Marshal(decimal(literal)); err == nil {
			t.Fatalf("decimal(%q) must fail closed", literal)
		}
	}
}

func TestComputeSourceDigestExcludesClocksAndIsStable(t *testing.T) {
	facts := SourceFacts{
		Platform: PlatformPi,
		Models: []ModelFact{
			{
				ModelConfigID: 1, ModelID: "a", APIFamily: "openai", IsEnabled: true, Selectable: true,
				CatalogBinding: CatalogEvidence{Bound: true, ProviderID: "openai", CatalogModelID: "gpt"},
				Enrichment:     EnrichmentEvidence{OfferingProviderID: "openai", OfferingModelID: "gpt"},
				Targets: []TargetFact{{
					TerminalTargetID: 5, Position: 0, EndpointID: 2,
					EndpointName: "primary",
				}},
			},
		},
	}
	first, err := ComputeSourceDigest(facts)
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	second, err := ComputeSourceDigest(facts)
	if err != nil || first != second {
		t.Fatalf("digest must be deterministic: %s vs %s (%v)", first, second, err)
	}
	if len(first) != 64 {
		t.Fatalf("digest must be sha256 hex, got %d chars", len(first))
	}
	changed := facts
	changed.Models[0].Targets[0].EndpointName = "secondary"
	third, err := ComputeSourceDigest(changed)
	if err != nil || third == first {
		t.Fatalf("fact changes must move the digest")
	}
}

func TestManualEnhancementAcceptsSafePrimitiveValues(t *testing.T) {
	enhancement := ManualEnhancement{Fields: json.RawMessage(`{"reasoning":false,"options":{"temperature":0},"tags":[],"note":""}`)}
	if err := enhancement.Validate(); err != nil {
		t.Fatalf("safe false/zero/empty values must stay valid and present: %v", err)
	}
}

func TestManualEnhancementRejectsTrailingJSON(t *testing.T) {
	for _, fields := range []json.RawMessage{
		json.RawMessage(`{"name":"safe"}{"reasoning":true}`),
		json.RawMessage(`{"name":"safe"} trailing`),
	} {
		if err := (ManualEnhancement{Fields: fields}).Validate(); err == nil {
			t.Fatalf("trailing JSON/input must fail closed: %s", fields)
		}
	}
}

func TestManualEnhancementRejectsWrongTypesAndUnknownTargetFields(t *testing.T) {
	cases := []struct {
		name     string
		platform Platform
		fields   json.RawMessage
	}{
		{name: "Pi wrong type", platform: PlatformPi, fields: json.RawMessage(`{"reasoning":"yes"}`)},
		{name: "Pi unknown field", platform: PlatformPi, fields: json.RawMessage(`{"mystery":true}`)},
		{name: "OpenCode wrong nested type", platform: PlatformOpenCode, fields: json.RawMessage(`{"limit":{"context":"many","output":1}}`)},
		{name: "OpenCode unknown field", platform: PlatformOpenCode, fields: json.RawMessage(`{"mystery":true}`)},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := (ManualEnhancement{Fields: testCase.fields}).ValidateForPlatform(testCase.platform)
			var invalid *ErrInvalidEnhancement
			if !errors.As(err, &invalid) {
				t.Fatalf("target-schema rejection = %T %v, want ErrInvalidEnhancement", err, err)
			}
		})
	}
}

func TestFullDocumentValidatorsReturnTypedTargetSchemaErrors(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		validate func() error
	}{
		{name: "Pi", validate: func() error {
			return validatePiDocument(map[string]any{"providers": map[string]any{}})
		}},
		{name: "OpenCode", validate: func() error {
			return validateOpenCodeDocument(map[string]any{"$schema": "wrong", "provider": map[string]any{}})
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := testCase.validate()
			var schemaError *ErrTargetSchema
			if !errors.As(err, &schemaError) {
				t.Fatalf("full document validation = %T %v, want ErrTargetSchema", err, err)
			}
		})
	}
}

func TestApplyEnhancementFillOverrideLockedSensitive(t *testing.T) {
	object := map[string]any{"id": "model-a", "cost": map[string]any{"input": decimal("1")}}
	enhancement := ManualEnhancement{
		Fields: rawValue(map[string]any{
			"name":   "Custom",
			"id":     "hijack",
			"apiKey": "sk-attack",
		}),
	}
	if err := enhancement.Validate(); err == nil {
		t.Fatalf("sensitive keys must fail validation")
	}
	err := applyEnhancement(object, enhancement, piLockedPaths)
	if err == nil {
		t.Fatalf("locked id must fail closed")
	}
	if _, ok := object["name"]; ok {
		t.Fatalf("nothing may apply after a locked-path failure")
	}

	fill := ManualEnhancement{Fields: rawValue(map[string]any{"name": "Custom"})}
	if err := applyEnhancement(object, fill, piLockedPaths); err != nil {
		t.Fatalf("fill enhancement: %v", err)
	}
	if object["name"] != "Custom" {
		t.Fatalf("missing key must fill, got %v", object["name"])
	}
	if _, ok := object["compat"]; !ok {
		second := ManualEnhancement{Fields: rawValue(map[string]any{"name": "Ignored"})}
		if err := applyEnhancement(object, second, piLockedPaths); err != nil {
			t.Fatalf("second pass: %v", err)
		}
		if object["name"] != "Custom" {
			t.Fatalf("existing keys stay untouched without override_fields, got %v", object["name"])
		}
		override := ManualEnhancement{
			Fields:         rawValue(map[string]any{"name": "Final"}),
			OverrideFields: []string{"name"},
		}
		if err := applyEnhancement(object, override, piLockedPaths); err != nil {
			t.Fatalf("override pass: %v", err)
		}
		if object["name"] != "Final" {
			t.Fatalf("override_fields must replace, got %v", object["name"])
		}
	}
}

func TestCheckLockedPathBlocksSubtrees(t *testing.T) {
	locked := []string{"provider", "options.baseURL"}
	for _, key := range []string{"provider", "provider.npm", "options.baseURL"} {
		if err := checkLockedPath(key, locked); err == nil {
			t.Fatalf("%q must be locked", key)
		}
	}
	if err := checkLockedPath("options", locked); err == nil {
		t.Fatalf("parent of locked subtree must be rejected to protect the child")
	}
	if err := checkLockedPath("limit", locked); err != nil {
		t.Fatalf("unrelated key must pass: %v", err)
	}
}
