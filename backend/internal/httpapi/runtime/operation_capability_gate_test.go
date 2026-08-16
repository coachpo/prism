package runtime

import (
	"net/http"
	"testing"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

func imageGateOperation(t *testing.T, path string) RuntimeOperation {
	t.Helper()
	return mustResolveRuntimeOperation(t, http.MethodPost, path).Operation
}

func capabilityPointer(value string) *string {
	return &value
}

func TestRuntimeOperationIsOpenAICapabilityGated(t *testing.T) {
	gated := map[string]bool{
		"/v1/chat/completions":         true,
		"/v1/responses":                true,
		"/v1/responses/input_tokens":   true,
		"/v1/responses/compact":        true,
		"/v1/images/generations":       true,
		"/v1/images/edits":             true,
		"/v1/messages":                 false,
		"/v1beta/models/g:countTokens": false,
	}
	for path, want := range gated {
		operation := imageGateOperation(t, path)
		if got := runtimeOperationIsOpenAICapabilityGated(operation); got != want {
			t.Fatalf("expected capability gating=%v for %s, got %v", want, path, got)
		}
	}
}

// The two gates must stay disjoint at the runtime seam as well: a text-only
// model or target can never serve an image request, and vice versa.
func TestRuntimeImageGateIgnoresTextDimension(t *testing.T) {
	generationsOperation := imageGateOperation(t, "/v1/images/generations")
	dualNative := capabilityPointer(providerauth.OpenAITextCapabilityDualNative)
	generations := capabilityPointer(providerauth.OpenAIImageCapabilityGenerations)

	textOnly := runtimeOpenAICapabilityDimensions{TextMode: dualNative}
	if runtimeOpenAICapabilitySatisfied(generationsOperation, textOnly, textOnly) {
		t.Fatal("expected a text-only pair not to serve an image operation")
	}
	if runtimeModelAcceptsOpenAIOperation(generationsOperation, textOnly) {
		t.Fatal("expected a text-only model not to accept an image operation")
	}

	imageOnly := runtimeOpenAICapabilityDimensions{ImageOperations: generations}
	chatOperation := imageGateOperation(t, "/v1/chat/completions")
	if runtimeOpenAICapabilitySatisfied(chatOperation, imageOnly, imageOnly) {
		t.Fatal("expected an image-only pair not to serve a text operation")
	}
	if runtimeModelAcceptsOpenAIOperation(chatOperation, imageOnly) {
		t.Fatal("expected an image-only model not to accept a text operation")
	}
}

func TestRuntimeImageGateAllowsWiderTarget(t *testing.T) {
	generationsOperation := imageGateOperation(t, "/v1/images/generations")
	editsOperation := imageGateOperation(t, "/v1/images/edits")

	model := runtimeOpenAICapabilityDimensions{ImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerations)}
	widerTarget := runtimeOpenAICapabilityDimensions{ImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerationsAndEdits)}

	if !runtimeOpenAICapabilitySatisfied(generationsOperation, model, widerTarget) {
		t.Fatal("expected a target serving both image operations to serve a generations-only model")
	}
	// The model does not accept edits, so an edits request is rejected on the
	// model side even though the target could serve it.
	if runtimeOpenAICapabilitySatisfied(editsOperation, model, widerTarget) {
		t.Fatal("expected a generations-only model to reject an edits request")
	}
}

func TestRuntimeImageGateRejectsNarrowerTarget(t *testing.T) {
	editsOperation := imageGateOperation(t, "/v1/images/edits")
	model := runtimeOpenAICapabilityDimensions{ImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerationsAndEdits)}
	narrowTarget := runtimeOpenAICapabilityDimensions{ImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerations)}

	if runtimeOpenAICapabilitySatisfied(editsOperation, model, narrowTarget) {
		t.Fatal("expected a generations-only target not to serve an edits request")
	}
	if !runtimeModelAcceptsOpenAIOperation(editsOperation, model) {
		t.Fatal("expected the model itself to accept edits")
	}
}

func TestRuntimeImageGateFailsClosedOnMissingDimensions(t *testing.T) {
	generationsOperation := imageGateOperation(t, "/v1/images/generations")
	present := runtimeOpenAICapabilityDimensions{ImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerations)}
	absent := runtimeOpenAICapabilityDimensions{}

	if runtimeOpenAICapabilitySatisfied(generationsOperation, absent, present) {
		t.Fatal("expected a model without an image dimension to fail closed")
	}
	if runtimeOpenAICapabilitySatisfied(generationsOperation, present, absent) {
		t.Fatal("expected a target without an image dimension to fail closed")
	}
}

// A pure image model carries no text mode. Text routing must keep working
// unchanged for text models alongside it.
func TestRuntimeTextGateUnchangedByImageDimension(t *testing.T) {
	chatOperation := imageGateOperation(t, "/v1/chat/completions")
	responsesOperation := imageGateOperation(t, "/v1/responses")

	chatOnly := runtimeOpenAICapabilityDimensions{TextMode: capabilityPointer(providerauth.OpenAITextCapabilityChatCompletionsOnly)}
	dualNative := runtimeOpenAICapabilityDimensions{TextMode: capabilityPointer(providerauth.OpenAITextCapabilityDualNative)}

	if !runtimeOpenAICapabilitySatisfied(chatOperation, chatOnly, chatOnly) {
		t.Fatal("expected equal chat-only modes to serve a chat request")
	}
	// Strict equality: a dual target may not back a chat-only model.
	if runtimeOpenAICapabilitySatisfied(chatOperation, chatOnly, dualNative) {
		t.Fatal("expected the text dimension to keep requiring strict mode equality")
	}
	if runtimeOpenAICapabilitySatisfied(responsesOperation, chatOnly, chatOnly) {
		t.Fatal("expected a chat-only pair not to serve a Responses request")
	}
}

func TestResolveTranslationModeNeverTranslates(t *testing.T) {
	generationsOperation := imageGateOperation(t, "/v1/images/generations")
	model := runtimeOpenAICapabilityDimensions{ImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerationsAndEdits)}
	target := runtimeOpenAICapabilityDimensions{ImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerationsAndEdits)}

	mode, supported := resolveTranslationMode(generationsOperation, model, target)
	if !supported {
		t.Fatal("expected a matching image pair to be supported")
	}
	if mode != TranslationModeNone {
		t.Fatalf("expected translation mode to stay none, got %q", mode)
	}
}

func TestValidateOpenAIModelAcceptedFormatCoversImageOperations(t *testing.T) {
	generationsOperation := imageGateOperation(t, "/v1/images/generations")

	imageModel := runtimeModelRecord{APIFamily: "openai", OpenAIImageOperations: capabilityPointer(providerauth.OpenAIImageCapabilityGenerations)}
	if err := validateOpenAIModelAcceptedFormat(generationsOperation, imageModel); err != nil {
		t.Fatalf("expected an image model to accept a generations request, got %v", err)
	}

	textModel := runtimeModelRecord{APIFamily: "openai", OpenAIAcceptedFormat: capabilityPointer(providerauth.OpenAITextCapabilityDualNative)}
	if err := validateOpenAIModelAcceptedFormat(generationsOperation, textModel); err == nil {
		t.Fatal("expected a text-only model to reject a generations request")
	}

	// A pure image model must still be rejected for text operations rather than
	// silently inheriting a text mode.
	chatOperation := imageGateOperation(t, "/v1/chat/completions")
	if err := validateOpenAIModelAcceptedFormat(chatOperation, imageModel); err == nil {
		t.Fatal("expected an image-only model to reject a chat request")
	}

	// Non-OpenAI families stay outside both gates.
	anthropicOperation := imageGateOperation(t, "/v1/messages")
	if err := validateOpenAIModelAcceptedFormat(anthropicOperation, runtimeModelRecord{APIFamily: "anthropic"}); err != nil {
		t.Fatalf("expected non-OpenAI families to skip the gate, got %v", err)
	}
}
