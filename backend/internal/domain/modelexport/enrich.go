package modelexport

import (
	"encoding/json"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

// DerivePiCandidate builds the safe Pi metadata projection from a
// pi.dev catalog entry. Only name/reasoning/input/contextWindow/maxTokens/
// thinkingLevelMap/compat survive; cost/headers/samplingParams/fallback are
// validated then dropped and never override Prism truth.
func DerivePiCandidate(model *pidev.Model) PlatformCandidate {
	candidate := PlatformCandidate{Metadata: MetadataLayer{}, DerivedFields: map[string]json.RawMessage{}}
	if model == nil {
		return candidate
	}
	values := map[string]json.RawMessage{}
	if model.Name != nil && strings.TrimSpace(*model.Name) != "" {
		values[MetaName] = marshalRaw(*model.Name)
	}
	if model.Reasoning != nil {
		values[MetaReasoning] = marshalRaw(*model.Reasoning)
	}
	if model.Input != nil {
		values[MetaModalitiesInput] = marshalRaw(model.Input)
	}
	if model.ContextWindow != nil {
		values[MetaContextWindow] = marshalRaw(*model.ContextWindow)
	}
	if model.MaxTokens != nil {
		values[MetaMaxOutputTokens] = marshalRaw(*model.MaxTokens)
	}
	candidate.Metadata = NewMetadataLayer(values)
	if model.ThinkingLevelMap != nil {
		if raw, err := json.Marshal(model.ThinkingLevelMap); err == nil {
			candidate.DerivedFields["thinkingLevelMap"] = raw
		}
	}
	if model.Compat != nil {
		if raw, err := json.Marshal(model.Compat); err == nil {
			candidate.DerivedFields["compat"] = raw
		}
	}
	return candidate
}

func marshalRaw(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}
