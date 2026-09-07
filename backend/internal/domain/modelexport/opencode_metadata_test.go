package modelexport

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestOpenCodeMetadataSafeProjection(t *testing.T) {
	fact := openCodeFixture().Models[0]
	fact.SourceMetadata["apiKey"], fact.SourceMetadata["variants"] = rawValue("never"), rawValue(map[string]any{"high": true})
	fact.SourceMetadata["structured_output"] = rawValue(true)
	projection := ProjectOpenCodeMetadata(fact)
	if string(projection.MergedMetadata["reasoning"]) != "false" || string(projection.MergedMetadata["limit_input"]) != "0" || string(projection.MergedMetadata["name"]) != `"Prism Chat"` {
		t.Fatalf("presence or precedence lost: %+v", projection)
	}
	if projection.Provenance["reasoning"] != "models_dev_override" || projection.Provenance["name"] != "prism_display_name" || projection.Provenance["limit_context"] != "models_dev_source" {
		t.Fatalf("wrong source provenance: %v", projection.Provenance)
	}
	for _, unsafe := range []string{"apiKey", "variants", "structured_output"} {
		if _, ok := projection.MergedMetadata[unsafe]; ok {
			t.Fatalf("unsupported field %s leaked", unsafe)
		}
	}
}

func TestOpenCodeMetadataNameAndInvalidOverrides(t *testing.T) {
	fact := openCodeFixture().Models[0]
	fact.DisplayName = nil
	projection := ProjectOpenCodeMetadata(fact)
	if projection.Provenance["name"] != "models_dev_source" {
		t.Fatal("catalog name not selected")
	}
	fact.OverrideMetadata["name"] = rawValue(" ")
	fact.OverrideMetadata["reasoning"] = rawValue("false")
	fact.OverrideMetadata["modalities_input"] = rawValue([]string{"unknown"})
	projection = ProjectOpenCodeMetadata(fact)
	if projection.Provenance["name"] != "model_id" || string(projection.MergedMetadata["name"]) != string(rawValue(fact.ModelID)) {
		t.Fatal("invalid effective catalog name must fall back to model identity")
	}
	if !slices.Contains(projection.MissingMetadata, "reasoning") || !slices.Contains(projection.MissingMetadata, "modalities_input") || len(projection.Issues) == 0 {
		t.Fatal("invalid overrides must stay visible, not silently use source values")
	}
}

func TestOpenCodeLimitsFailClosedAsAGroup(t *testing.T) {
	tests := []struct{ name, leaf, raw string }{
		{"missing context", "limit_context", "null"}, {"zero context", "limit_context", "0"},
		{"missing output", "limit_output", "null"}, {"zero output", "limit_output", "0"},
		{"negative", "limit_output", "-1"}, {"fraction", "limit_output", "1.5"},
		{"string", "limit_output", `"8192"`}, {"unsafe integer", "limit_context", "9007199254740992"},
		{"rounded fraction", "limit_context", "9007199254740991.1"},
		{"output exceeds context", "limit_output", "200001"}, {"input exceeds context", "limit_input", "200001"},
		{"invalid optional input", "limit_input", "-1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := openCodeInput()
			input.Facts.Models[0].SourceMetadata[test.leaf] = json.RawMessage(test.raw)
			delete(input.Facts.Models[0].OverrideMetadata, test.leaf)
			if ProjectOpenCodeMetadata(input.Facts.Models[0]).LimitsValid {
				t.Fatal("invalid limit group accepted")
			}
			if _, err := RenderOpenCode(input); err == nil {
				t.Fatal("invalid limit group rendered")
			}
		})
	}
}

func TestOpenCodeDigestConsumesOnlyEffectiveFacts(t *testing.T) {
	facts := openCodeFixture()
	initial, err := ComputeOpenCodeSourceDigest(facts)
	if err != nil {
		t.Fatal(err)
	}
	facts.Models[0].SourceMetadata["description"] = rawValue("irrelevant")
	facts.Models[0].SourceMetadata["reasoning"] = rawValue(false) // shadowed
	slices.Reverse(facts.Models)
	same, err := ComputeOpenCodeSourceDigest(facts)
	if err != nil || same != initial {
		t.Fatalf("unconsumed facts or order changed digest: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*OpenCodeSourceFacts)
	}{
		{"version", func(f *OpenCodeSourceFacts) { f.TargetVersion = "next" }},
		{"identity", func(f *OpenCodeSourceFacts) { f.Models[0].ModelID += "/renamed" }},
		{"capability", func(f *OpenCodeSourceFacts) { f.Models[0].OverrideMetadata["reasoning"] = rawValue(true) }},
		{"limits", func(f *OpenCodeSourceFacts) { f.Models[0].SourceMetadata["limit_output"] = rawValue(4096) }},
		{"price", func(f *OpenCodeSourceFacts) { f.Models[0].Targets[0].Pricing.Card.InputPrice = "4" }},
		{"route", func(f *OpenCodeSourceFacts) { f.Models[0].Selectable = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := openCodeFixture()
			test.mutate(&changed)
			digest, err := ComputeOpenCodeSourceDigest(changed)
			if err != nil || digest == initial {
				t.Fatalf("consumed %s did not change digest: %v", test.name, err)
			}
		})
	}
}
