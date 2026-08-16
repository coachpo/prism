package providerauth

import "strings"

// OpenAI image operations are a dimension of their own. They are deliberately
// absent from OpenAITextCapabilitySupportsNativeOperation and from
// OpenAICallerWireFormat: those two describe which text wire protocol a row
// speaks, and both already fall through to "not a text operation" for anything
// outside Chat Completions and Responses. The image gate below is what applies
// instead, and the two gates never consult each other.
const (
	OpenAIUpstreamOperationImagesGenerations = "openai.images.generations"
	OpenAIUpstreamOperationImagesEdits       = "openai.images.edits"

	OpenAIImageCapabilityGenerations         = "generations"
	OpenAIImageCapabilityEdits               = "edits"
	OpenAIImageCapabilityGenerationsAndEdits = "generations_and_edits"
)

func IsSupportedOpenAIImageCapability(value string) bool {
	switch strings.TrimSpace(value) {
	case OpenAIImageCapabilityGenerations, OpenAIImageCapabilityEdits, OpenAIImageCapabilityGenerationsAndEdits:
		return true
	default:
		return false
	}
}

// IsOpenAIImageOperation reports whether an ingress operation belongs to the
// image dimension. Callers use it to choose which capability gate applies.
func IsOpenAIImageOperation(ingressOperation string) bool {
	switch strings.TrimSpace(ingressOperation) {
	case OpenAIUpstreamOperationImagesGenerations, OpenAIUpstreamOperationImagesEdits:
		return true
	default:
		return false
	}
}

func OpenAIImageCapabilitySupportsOperation(capability string, ingressOperation string) bool {
	operation := strings.TrimSpace(ingressOperation)
	for _, supported := range OpenAIImageCapabilityOperations(capability) {
		if supported == operation {
			return true
		}
	}
	return false
}

// OpenAIImageCapabilityOperations returns the canonical operation set a single
// image capability value serves, in stable order. An unsupported or empty value
// serves nothing.
func OpenAIImageCapabilityOperations(capability string) []string {
	switch strings.TrimSpace(capability) {
	case OpenAIImageCapabilityGenerations:
		return []string{OpenAIUpstreamOperationImagesGenerations}
	case OpenAIImageCapabilityEdits:
		return []string{OpenAIUpstreamOperationImagesEdits}
	case OpenAIImageCapabilityGenerationsAndEdits:
		return []string{OpenAIUpstreamOperationImagesGenerations, OpenAIUpstreamOperationImagesEdits}
	default:
		return nil
	}
}

// OpenAIImageCapabilityCovers reports whether a Terminal Target capability
// serves every image operation the owner model accepts.
//
// This is containment, not equality, and that difference from the text
// dimension is deliberate. Chat Completions and Responses are mutually
// exclusive wire protocols, so a text target that speaks the other protocol is
// useless and management requires strict mode equality. Image generations and
// edits are additive capabilities on one protocol: a target that serves both
// can safely back a model that only accepts one, and forcing equality would
// require a second Terminal Target for that ordinary case.
func OpenAIImageCapabilityCovers(acceptedCapability string, targetCapability string) bool {
	accepted := OpenAIImageCapabilityOperations(acceptedCapability)
	if len(accepted) == 0 {
		return false
	}
	for _, operation := range accepted {
		if !OpenAIImageCapabilitySupportsOperation(targetCapability, operation) {
			return false
		}
	}
	return true
}

// OpenAIImageCapabilitiesCover is the optional-value form used for persisted
// relations. A missing value on either side fails closed, matching how
// OpenAITextModesMatch treats absent text modes.
func OpenAIImageCapabilitiesCover(acceptedCapability *string, targetCapability *string) bool {
	if acceptedCapability == nil || targetCapability == nil {
		return false
	}
	return OpenAIImageCapabilityCovers(*acceptedCapability, *targetCapability)
}
