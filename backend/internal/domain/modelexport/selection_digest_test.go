package modelexport

import (
	"encoding/json"
	"testing"
)

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
