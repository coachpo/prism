package runtime

import (
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/providerauth"
)

// The OpenAI capability gate.
//
// OpenAI ingress operations fall into two independent dimensions, and each has
// its own gate. The dimensions never answer for each other: a text capability
// serves no image operation and an image capability serves no text operation.
//
//   - Text: Chat Completions and Responses are mutually exclusive wire
//     protocols, so the model mode and the target capability must be equal and
//     that shared mode must serve the ingress operation.
//   - Image: generations and edits are additive capabilities on one protocol, so
//     both sides only have to serve the one operation being requested. A target
//     that serves more image operations than the model accepts stays eligible.
//
// Operations outside both dimensions (openai.models, and every non-OpenAI
// family) are not capability gated at all.

// runtimeOpenAICapabilityDimensions carries one side of the gate: either the
// requested model's authored dimensions or a Terminal Target's.
type runtimeOpenAICapabilityDimensions struct {
	TextMode        *string
	ImageOperations *string
}

func runtimeModelCapabilityDimensions(model runtimeModelRecord) runtimeOpenAICapabilityDimensions {
	return runtimeOpenAICapabilityDimensions{TextMode: model.OpenAIAcceptedFormat, ImageOperations: model.OpenAIImageOperations}
}

func runtimeConnectionCapabilityDimensions(connection runtimeConnection) runtimeOpenAICapabilityDimensions {
	return runtimeOpenAICapabilityDimensions{TextMode: connection.OpenAITextCapability, ImageOperations: connection.OpenAIImageCapability}
}

// runtimeOperationIsOpenAICapabilityGated reports whether an ingress operation
// is subject to either capability gate.
func runtimeOperationIsOpenAICapabilityGated(operation RuntimeOperation) bool {
	return runtimeOperationIsOpenAIImage(operation) || runtimeOperationIsOpenAIText(operation)
}

func runtimeOperationIsOpenAIImage(operation RuntimeOperation) bool {
	return providerauth.IsOpenAIImageOperation(operation.Name)
}

func runtimeOperationIsOpenAIText(operation RuntimeOperation) bool {
	return openai.IsTextOperation(providerOperationFromRuntime(operation))
}

// runtimeOpenAICapabilitySatisfied reports whether a requested model and a
// Terminal Target can natively serve an ingress operation together.
func runtimeOpenAICapabilitySatisfied(operation RuntimeOperation, model runtimeOpenAICapabilityDimensions, target runtimeOpenAICapabilityDimensions) bool {
	if runtimeOperationIsOpenAIImage(operation) {
		return runtimeOpenAIImageCapabilitySatisfied(operation, model.ImageOperations, target.ImageOperations)
	}
	if !runtimeOperationIsOpenAIText(operation) {
		return false
	}
	return runtimeOpenAITextCapabilitySatisfied(operation, model.TextMode, target.TextMode)
}

func runtimeOpenAIImageCapabilitySatisfied(operation RuntimeOperation, modelImageOperations *string, targetImageCapability *string) bool {
	if modelImageOperations == nil || targetImageCapability == nil {
		return false
	}
	return providerauth.OpenAIImageCapabilitySupportsOperation(*modelImageOperations, operation.Name) &&
		providerauth.OpenAIImageCapabilitySupportsOperation(*targetImageCapability, operation.Name)
}

func runtimeOpenAITextCapabilitySatisfied(operation RuntimeOperation, modelTextMode *string, targetTextCapability *string) bool {
	if modelTextMode == nil || targetTextCapability == nil {
		return false
	}
	return providerauth.OpenAITextModesMatch(modelTextMode, targetTextCapability) &&
		providerauth.OpenAITextCapabilitySupportsNativeOperation(*targetTextCapability, operation.Name)
}

// runtimeModelAcceptsOpenAIOperation reports whether the requested model itself
// accepts an ingress operation, independent of any target. It is the model-side
// half of the gate, used to reject before target selection.
func runtimeModelAcceptsOpenAIOperation(operation RuntimeOperation, model runtimeOpenAICapabilityDimensions) bool {
	if runtimeOperationIsOpenAIImage(operation) {
		return model.ImageOperations != nil && providerauth.OpenAIImageCapabilitySupportsOperation(*model.ImageOperations, operation.Name)
	}
	if !runtimeOperationIsOpenAIText(operation) {
		return false
	}
	return model.TextMode != nil && providerauth.OpenAITextCapabilitySupportsNativeOperation(*model.TextMode, operation.Name)
}
