package providerauth

import (
	"reflect"
	"testing"
)

func TestIsSupportedOpenAIImageCapability(t *testing.T) {
	supported := []string{OpenAIImageCapabilityGenerations, OpenAIImageCapabilityEdits, OpenAIImageCapabilityGenerationsAndEdits}
	for _, capability := range supported {
		if !IsSupportedOpenAIImageCapability(capability) {
			t.Fatalf("expected %q to be a supported image capability", capability)
		}
	}
	unsupported := []string{"", "none", "dual_native", "responses_only", "generations_and_variations", "Generations"}
	for _, capability := range unsupported {
		if IsSupportedOpenAIImageCapability(capability) {
			t.Fatalf("expected %q to be an unsupported image capability", capability)
		}
	}
}

func TestOpenAIImageOperationIdentity(t *testing.T) {
	for _, operation := range []string{OpenAIUpstreamOperationImagesGenerations, OpenAIUpstreamOperationImagesEdits} {
		if !IsOpenAIImageOperation(operation) {
			t.Fatalf("expected %q to be an image operation", operation)
		}
	}
	for _, operation := range []string{OpenAIUpstreamOperationChatCompletions, OpenAIUpstreamOperationResponses, OpenAIUpstreamOperationResponsesInputTokens, OpenAIUpstreamOperationResponsesCompact, "anthropic.messages", ""} {
		if IsOpenAIImageOperation(operation) {
			t.Fatalf("expected %q not to be an image operation", operation)
		}
	}
}

// The two capability dimensions must never answer for each other: a text
// capability serves no image operation, and an image capability serves no text
// operation.
func TestOpenAIImageAndTextGatesStayDisjoint(t *testing.T) {
	textCapabilities := []string{OpenAITextCapabilityResponsesOnly, OpenAITextCapabilityChatCompletionsOnly, OpenAITextCapabilityDualNative}
	imageOperations := []string{OpenAIUpstreamOperationImagesGenerations, OpenAIUpstreamOperationImagesEdits}
	for _, capability := range textCapabilities {
		for _, operation := range imageOperations {
			if OpenAITextCapabilitySupportsNativeOperation(capability, operation) {
				t.Fatalf("expected text capability %q not to serve image operation %q", capability, operation)
			}
			if OpenAIImageCapabilitySupportsOperation(capability, operation) {
				t.Fatalf("expected text capability %q not to be readable as an image capability", capability)
			}
		}
	}

	imageCapabilities := []string{OpenAIImageCapabilityGenerations, OpenAIImageCapabilityEdits, OpenAIImageCapabilityGenerationsAndEdits}
	textOperations := []string{OpenAIUpstreamOperationChatCompletions, OpenAIUpstreamOperationResponses, OpenAIUpstreamOperationResponsesInputTokens, OpenAIUpstreamOperationResponsesCompact}
	for _, capability := range imageCapabilities {
		for _, operation := range textOperations {
			if OpenAIImageCapabilitySupportsOperation(capability, operation) {
				t.Fatalf("expected image capability %q not to serve text operation %q", capability, operation)
			}
			if OpenAITextCapabilitySupportsNativeOperation(capability, operation) {
				t.Fatalf("expected image capability %q not to be readable as a text capability", capability)
			}
		}
	}

	// Image operations carry no text wire format, which is what makes the text
	// gate skip them instead of rejecting them.
	for _, operation := range imageOperations {
		if _, ok := OpenAICallerWireFormat(operation); ok {
			t.Fatalf("expected image operation %q to carry no text wire format", operation)
		}
	}
}

func TestOpenAIImageCapabilityOperations(t *testing.T) {
	tests := []struct {
		capability string
		want       []string
	}{
		{capability: OpenAIImageCapabilityGenerations, want: []string{OpenAIUpstreamOperationImagesGenerations}},
		{capability: OpenAIImageCapabilityEdits, want: []string{OpenAIUpstreamOperationImagesEdits}},
		{capability: OpenAIImageCapabilityGenerationsAndEdits, want: []string{OpenAIUpstreamOperationImagesGenerations, OpenAIUpstreamOperationImagesEdits}},
		{capability: "", want: nil},
		{capability: "none", want: nil},
	}
	for _, test := range tests {
		t.Run(test.capability, func(t *testing.T) {
			if got := OpenAIImageCapabilityOperations(test.capability); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("expected operations %+v, got %+v", test.want, got)
			}
		})
	}
}

func TestOpenAIImageCapabilityCoversIsContainment(t *testing.T) {
	tests := []struct {
		name     string
		accepted string
		target   string
		want     bool
	}{
		{name: "exact generations", accepted: OpenAIImageCapabilityGenerations, target: OpenAIImageCapabilityGenerations, want: true},
		{name: "exact edits", accepted: OpenAIImageCapabilityEdits, target: OpenAIImageCapabilityEdits, want: true},
		{name: "exact both", accepted: OpenAIImageCapabilityGenerationsAndEdits, target: OpenAIImageCapabilityGenerationsAndEdits, want: true},
		{name: "wider target covers generations", accepted: OpenAIImageCapabilityGenerations, target: OpenAIImageCapabilityGenerationsAndEdits, want: true},
		{name: "wider target covers edits", accepted: OpenAIImageCapabilityEdits, target: OpenAIImageCapabilityGenerationsAndEdits, want: true},
		{name: "narrower target leaves edits uncovered", accepted: OpenAIImageCapabilityGenerationsAndEdits, target: OpenAIImageCapabilityGenerations, want: false},
		{name: "narrower target leaves generations uncovered", accepted: OpenAIImageCapabilityGenerationsAndEdits, target: OpenAIImageCapabilityEdits, want: false},
		{name: "disjoint capabilities", accepted: OpenAIImageCapabilityGenerations, target: OpenAIImageCapabilityEdits, want: false},
		{name: "empty accepted covers nothing", accepted: "", target: OpenAIImageCapabilityGenerationsAndEdits, want: false},
		{name: "empty target covers nothing", accepted: OpenAIImageCapabilityGenerations, target: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := OpenAIImageCapabilityCovers(test.accepted, test.target); got != test.want {
				t.Fatalf("expected covers=%v for accepted %q target %q, got %v", test.want, test.accepted, test.target, got)
			}
		})
	}
}

func TestOpenAIImageCapabilitiesCoverFailsClosedOnMissingValues(t *testing.T) {
	generations := OpenAIImageCapabilityGenerations
	both := OpenAIImageCapabilityGenerationsAndEdits

	if OpenAIImageCapabilitiesCover(nil, nil) {
		t.Fatal("expected two missing capabilities to fail closed")
	}
	if OpenAIImageCapabilitiesCover(&generations, nil) {
		t.Fatal("expected a missing target capability to fail closed")
	}
	if OpenAIImageCapabilitiesCover(nil, &both) {
		t.Fatal("expected a missing accepted capability to fail closed")
	}
	if !OpenAIImageCapabilitiesCover(&generations, &both) {
		t.Fatal("expected a wider target to cover a narrower model")
	}
}
