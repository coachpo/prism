package modelexport

import (
	"encoding/json"
	"testing"
)

func TestNormalizeSelectionDedupesSortsAndFailsClosed(t *testing.T) {
	responses := SelectedCoordinate{ProviderID: "openai", ModelID: "a", API: piAPIResponses}
	chat := SelectedCoordinate{ProviderID: "provider-b", ModelID: "b", API: piAPIOpenAIChat}
	chatOnly := "chat_completions_only"
	dualNative := "dual_native"
	facts := SourceFacts{Models: []ModelFact{
		{ModelConfigID: 7, ModelID: "b", APIFamily: "openai", OpenAIAcceptedFormat: &chatOnly, Selectable: true, PiSelected: &chat},
		{ModelConfigID: 3, ModelID: "a", APIFamily: "openai", OpenAIAcceptedFormat: &dualNative, Selectable: true, PiSelected: &responses},
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
	unbound := facts
	unbound.Models = append([]ModelFact(nil), facts.Models...)
	unbound.Models[0].PiSelected = nil
	if _, err := NormalizeSelection([]int{7}, unbound); err == nil {
		t.Fatalf("a model without a persisted Pi source must fail")
	}
}

func TestPiAPIForModelRequiresSupportedOpenAITextFormat(t *testing.T) {
	chatOnly := "chat_completions_only"
	responsesOnly := "responses_only"
	dualNative := "dual_native"
	imageOnly := "image_only"
	tests := []struct {
		name   string
		family string
		format *string
		want   string
	}{
		{name: "chat", family: "openai", format: &chatOnly, want: piAPIOpenAIChat},
		{name: "responses", family: "openai", format: &responsesOnly, want: piAPIResponses},
		{name: "dual native", family: "openai", format: &dualNative, want: piAPIResponses},
		{name: "missing", family: "openai", format: nil, want: ""},
		{name: "image only", family: "openai", format: &imageOnly, want: ""},
		{name: "anthropic", family: "anthropic", want: piAPIAnthropic},
		{name: "gemini", family: "gemini", want: piAPIGemini},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := PiAPIForModel(test.family, test.format); got != test.want {
				t.Fatalf("PiAPIForModel(%q, %v) = %q, want %q", test.family, test.format, got, test.want)
			}
		})
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
		TargetVersion: PiTargetVersion,
		Models: []ModelFact{
			{
				ModelConfigID: 1, ModelID: "a", APIFamily: "openai", IsEnabled: true, Selectable: true,
				PiSelected: &SelectedCoordinate{ProviderID: "openai", ModelID: "a", API: piAPIResponses, CatalogRevision: "sha256-one"},
				PiTemplate: PiTemplate{Metadata: NewMetadataLayer(map[string]json.RawMessage{
					MetaName: rawValue("Model A"),
				})},
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

func TestComputeSourceDigestCoversSelectedCoordinateAndPersistedTemplateOnly(t *testing.T) {
	facts := SourceFacts{TargetVersion: PiTargetVersion, Models: []ModelFact{{
		ModelConfigID: 1, ModelID: "same-id", APIFamily: "openai", Selectable: true,
		PiSelected: &SelectedCoordinate{ProviderID: "provider-a", ModelID: "same-id", API: piAPIResponses, CatalogRevision: "sha256-one"},
		PiTemplate: PiTemplate{Metadata: NewMetadataLayer(map[string]json.RawMessage{MetaName: rawValue("Same")})},
	}}}
	first, err := ComputeSourceDigest(facts)
	if err != nil {
		t.Fatalf("first digest: %v", err)
	}

	coordinateChanged := facts
	coordinateChanged.Models = append([]ModelFact(nil), facts.Models...)
	coordinate := *facts.Models[0].PiSelected
	coordinate.ProviderID = "provider-b"
	coordinateChanged.Models[0].PiSelected = &coordinate
	second, err := ComputeSourceDigest(coordinateChanged)
	if err != nil || second == first {
		t.Fatalf("switching exact provider coordinate must move digest: first=%s second=%s err=%v", first, second, err)
	}

	templateChanged := facts
	templateChanged.Models = append([]ModelFact(nil), facts.Models...)
	templateChanged.Models[0].PiTemplate = PiTemplate{Metadata: NewMetadataLayer(map[string]json.RawMessage{MetaName: rawValue("Changed")})}
	third, err := ComputeSourceDigest(templateChanged)
	if err != nil || third == first {
		t.Fatalf("persisted effective template changes must move digest: first=%s third=%s err=%v", first, third, err)
	}

	liveChanged := facts
	liveChanged.Models = append([]ModelFact(nil), facts.Models...)
	liveChanged.Models[0].PiCandidates = []PiCandidate{{ProviderID: "new-live-provider", ModelID: "same-id", API: piAPIResponses}}
	liveChanged.PiCatalog = PiCatalogEvidence{Revision: "sha256-live", Status: "fresh"}
	fourth, err := ComputeSourceDigest(liveChanged)
	if err != nil || fourth != first {
		t.Fatalf("transient live catalog evidence must not move frozen render digest: first=%s fourth=%s err=%v", first, fourth, err)
	}
}
