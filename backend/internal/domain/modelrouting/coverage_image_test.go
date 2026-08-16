package modelrouting

import (
	"reflect"
	"testing"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

func stringPointer(value string) *string {
	return &value
}

func TestOpenAIImageAcceptedOperationSetHasNoDualNativeFallback(t *testing.T) {
	// The text set falls back to dual_native for an unknown value because every
	// openai row used to carry a text mode. The image set must not: an absent
	// image dimension means the model accepts no image operation at all.
	if got := OpenAIImageAcceptedOperationSet(""); got != nil {
		t.Fatalf("expected empty image dimension to accept nothing, got %+v", got)
	}
	if got := OpenAIImageAcceptedOperationSet("dual_native"); got != nil {
		t.Fatalf("expected a text mode value to accept no image operation, got %+v", got)
	}
	if got := OpenAIAcceptedOperationSet(""); len(got) == 0 {
		t.Fatal("expected the text set to keep its dual_native fallback for unknown values")
	}
}

func TestOpenAIAcceptedOperationSetForDimensions(t *testing.T) {
	chatOnly := providerauth.OpenAITextCapabilityChatCompletionsOnly
	generations := providerauth.OpenAIImageCapabilityGenerations
	both := providerauth.OpenAIImageCapabilityGenerationsAndEdits

	tests := []struct {
		name   string
		text   *string
		images *string
		want   []string
	}{
		{
			name: "text only model keeps its text operations",
			text: &chatOnly,
			want: []string{providerauth.OpenAIUpstreamOperationChatCompletions},
		},
		{
			name:   "image only model accepts no text operation",
			images: &generations,
			want:   []string{providerauth.OpenAIUpstreamOperationImagesGenerations},
		},
		{
			name:   "dual dimension model accepts both",
			text:   &chatOnly,
			images: &both,
			want: []string{
				providerauth.OpenAIUpstreamOperationChatCompletions,
				providerauth.OpenAIUpstreamOperationImagesGenerations,
				providerauth.OpenAIUpstreamOperationImagesEdits,
			},
		},
		{
			name: "both dimensions absent accepts nothing",
			want: []string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := OpenAIAcceptedOperationSetForDimensions(test.text, test.images)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected %+v, got %+v", test.want, got)
			}
		})
	}
}

// A nil text mode must contribute no text operation. Reading it through the
// string-keyed set instead would silently report a pure image model as
// accepting Chat Completions and Responses.
func TestImageOnlyModelDoesNotInheritDualNativeText(t *testing.T) {
	generations := providerauth.OpenAIImageCapabilityGenerations
	accepted := OpenAIAcceptedOperationSetForDimensions(nil, &generations)
	for _, operation := range accepted {
		if !providerauth.IsOpenAIImageOperation(operation) {
			t.Fatalf("expected image-only model to accept image operations only, got %q", operation)
		}
	}
}

func TestOpenAITargetSupportedOperationSetForDimensions(t *testing.T) {
	responsesOnly := providerauth.OpenAITextCapabilityResponsesOnly
	edits := providerauth.OpenAIImageCapabilityEdits

	supported := OpenAITargetSupportedOperationSetForDimensions(&responsesOnly, &edits)
	want := append(responsesOperationSet(), providerauth.OpenAIUpstreamOperationImagesEdits)
	if !reflect.DeepEqual(supported, want) {
		t.Fatalf("expected %+v, got %+v", want, supported)
	}

	if got := OpenAITargetSupportedOperationSetForDimensions(nil, nil); len(got) != 0 {
		t.Fatalf("expected a target with neither dimension to support nothing, got %+v", got)
	}
}

func TestOpenAIOperationGroupCoversImageOperations(t *testing.T) {
	tests := map[string]string{
		providerauth.OpenAIUpstreamOperationChatCompletions:   OpenAIOperationGroupChatCompletions,
		providerauth.OpenAIUpstreamOperationResponses:         OpenAIOperationGroupResponses,
		providerauth.OpenAIUpstreamOperationImagesGenerations: OpenAIOperationGroupImagesGenerations,
		providerauth.OpenAIUpstreamOperationImagesEdits:       OpenAIOperationGroupImagesEdits,
		"anthropic.messages":                                  "",
	}
	for operation, want := range tests {
		if got := OpenAIOperationGroup(operation); got != want {
			t.Fatalf("expected group %q for %q, got %q", want, operation, got)
		}
	}

	order := OpenAIOperationGroupOrder()
	want := []string{
		OpenAIOperationGroupChatCompletions,
		OpenAIOperationGroupResponses,
		OpenAIOperationGroupImagesGenerations,
		OpenAIOperationGroupImagesEdits,
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("expected group order %+v, got %+v", want, order)
	}
}

func TestOpenAIRegisteredOperationListCoversBothDimensions(t *testing.T) {
	list := OpenAIRegisteredOperationList()
	seen := operationSet(list)
	required := []string{
		providerauth.OpenAIUpstreamOperationChatCompletions,
		providerauth.OpenAIUpstreamOperationResponses,
		providerauth.OpenAIUpstreamOperationResponsesInputTokens,
		providerauth.OpenAIUpstreamOperationResponsesCompact,
		providerauth.OpenAIUpstreamOperationImagesGenerations,
		providerauth.OpenAIUpstreamOperationImagesEdits,
	}
	for _, operation := range required {
		if _, ok := seen[operation]; !ok {
			t.Fatalf("expected registered operation list to contain %q, got %+v", operation, list)
		}
	}
	if len(list) != len(required) {
		t.Fatalf("expected exactly %d registered operations, got %+v", len(required), list)
	}
}

// Image coverage is containment, so a target that serves both image operations
// fully covers a model that only accepts generations. The text dimension keeps
// its strict-equality behaviour alongside it.
func TestImageCoverageIsContainmentInDiagnostics(t *testing.T) {
	generations := providerauth.OpenAIImageCapabilityGenerations
	both := providerauth.OpenAIImageCapabilityGenerationsAndEdits

	accepted := OpenAIAcceptedOperationSetForDimensions(nil, &generations)
	supported := OpenAITargetSupportedOperationSetForDimensions(nil, &both)
	coverage, supportedAccepted, unsupportedAccepted := ClassifyOpenAICoverage(accepted, supported)
	if coverage != CoverageFull {
		t.Fatalf("expected a wider image target to be full coverage, got %q", coverage)
	}
	if len(unsupportedAccepted) != 0 {
		t.Fatalf("expected nothing uncovered, got %+v", unsupportedAccepted)
	}
	if len(supportedAccepted) != 1 {
		t.Fatalf("expected one covered operation, got %+v", supportedAccepted)
	}

	accepted = OpenAIAcceptedOperationSetForDimensions(nil, &both)
	supported = OpenAITargetSupportedOperationSetForDimensions(nil, &generations)
	coverage, _, unsupportedAccepted = ClassifyOpenAICoverage(accepted, supported)
	if coverage != CoveragePartial {
		t.Fatalf("expected a narrower image target to be partial coverage, got %q", coverage)
	}
	if len(unsupportedAccepted) != 1 || unsupportedAccepted[0] != providerauth.OpenAIUpstreamOperationImagesEdits {
		t.Fatalf("expected edits to be the uncovered operation, got %+v", unsupportedAccepted)
	}
}

func TestOpenAIImageRelationGateIgnoresModelsWithoutImages(t *testing.T) {
	both := providerauth.OpenAIImageCapabilityGenerationsAndEdits
	generations := providerauth.OpenAIImageCapabilityGenerations

	if openAIImageCapabilityUncovered(nil, nil) {
		t.Fatal("expected a source without images to impose no image requirement")
	}
	if openAIImageCapabilityUncovered(nil, &generations) {
		t.Fatal("expected a target that serves images anyway to stay valid")
	}
	if openAIImageCapabilityUncovered(stringPointer(""), &generations) {
		t.Fatal("expected an empty source image dimension to impose no requirement")
	}
	if openAIImageCapabilityUncovered(&generations, &both) {
		t.Fatal("expected a wider target to cover a narrower source")
	}
	if !openAIImageCapabilityUncovered(&both, &generations) {
		t.Fatal("expected a narrower target to leave edits uncovered")
	}
	if !openAIImageCapabilityUncovered(&generations, nil) {
		t.Fatal("expected a target without an image dimension to leave images uncovered")
	}
}
