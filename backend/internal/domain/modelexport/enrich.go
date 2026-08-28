package modelexport

import (
	"encoding/json"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
)

// DeriveCandidate builds the replayable enrichment payload for one platform
// from a validated models.dev offering. It projects only safe metadata:
// descriptive facts, token limits, modalities, reasoning options, and the
// interleaved flag. Prices, provider bodies/headers, experimental flags, and
// request shapes never pass through.
func DeriveCandidate(platform Platform, apiFamily string, acceptedFormat *string, model *modelsdev.Model) PlatformCandidate {
	candidate := PlatformCandidate{Metadata: MetadataLayer{}, DerivedFields: map[string]json.RawMessage{}}
	if model == nil {
		return candidate
	}
	values := map[string]json.RawMessage{}
	if strings.TrimSpace(model.Name) != "" {
		values[MetaName] = marshalRaw(model.Name)
	}
	if model.Description != nil {
		values[MetaDescription] = marshalRaw(*model.Description)
	}
	if model.Family != nil {
		values[MetaFamily] = marshalRaw(*model.Family)
	}
	if model.Reasoning != nil {
		values[MetaReasoning] = marshalRaw(*model.Reasoning)
	}
	if model.Attachment != nil {
		values[MetaAttachment] = marshalRaw(*model.Attachment)
	}
	if model.ToolCall != nil {
		values[MetaToolCall] = marshalRaw(*model.ToolCall)
	}
	if model.Temperature != nil {
		values[MetaTemperature] = marshalRaw(*model.Temperature)
	}
	if model.Status != nil {
		// Status is expressed verbatim including deprecated: hiding it would
		// disguise known facts.
		values[MetaStatus] = marshalRaw(*model.Status)
	}
	if model.ReleaseDate != nil {
		values[MetaReleaseDate] = marshalRaw(*model.ReleaseDate)
	}
	if model.Knowledge != nil {
		values[MetaKnowledge] = marshalRaw(*model.Knowledge)
	}
	if model.Limit.Context != nil {
		values[MetaContextWindow] = marshalRaw(*model.Limit.Context)
	}
	if model.Limit.Output != nil {
		values[MetaMaxOutputTokens] = marshalRaw(*model.Limit.Output)
	}
	if model.ModalitiesInput != nil {
		values[MetaModalitiesInput] = marshalRaw(model.ModalitiesInput)
	}
	if model.ModalitiesOutput != nil {
		values[MetaModalitiesOutput] = marshalRaw(model.ModalitiesOutput)
	}
	candidate.Metadata = NewMetadataLayer(values)

	switch platform {
	case PlatformPi:
		if thinkingMap := derivePiThinkingLevelMap(model.ReasoningOptions); thinkingMap != nil {
			candidate.DerivedFields["thinkingLevelMap"] = thinkingMap
		} else if len(model.ReasoningOptions) > 0 {
			candidate.WarningCodes = append(candidate.WarningCodes, WarningThinkingMapUnrepresentable)
		}
	case PlatformOpenCode:
		if interleaved := deriveOpenCodeInterleaved(model.Interleaved); interleaved != nil {
			candidate.DerivedFields["interleaved"] = interleaved
		}
	}
	return candidate
}

// derivePiThinkingLevelMap projects effort options onto Pi's level set.
// Catalog levels inside Pi's vocabulary map one to one; "none" maps to off;
// unlisted Pi levels become explicit nulls so the client hides them rather
// than guessing defaults. Toggle/budget-only models return nil: there is no
// lossless projection onto string-valued levels.
func derivePiThinkingLevelMap(options []modelsdev.ReasoningOption) json.RawMessage {
	var efforts []*string
	for _, option := range options {
		if option.Type == modelsdev.ReasoningOptionEffort {
			efforts = append(efforts, option.Values...)
		}
	}
	if len(efforts) == 0 {
		return nil
	}
	present := map[string]string{}
	for _, effort := range efforts {
		if effort == nil {
			continue
		}
		lower := strings.ToLower(strings.TrimSpace(*effort))
		switch lower {
		case "none", "off":
			present["off"] = *effort
		case "minimal", "low", "medium", "high", "xhigh", "max":
			if _, exists := present[lower]; !exists {
				present[lower] = lower
			}
		}
	}
	if len(present) == 0 {
		return nil
	}
	mapped := map[string]*string{}
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"} {
		if value, ok := present[level]; ok {
			copied := value
			mapped[level] = &copied
		} else {
			// Pi distinguishes a missing mapping from an explicit null. The
			// complete level map prevents the client from inventing support for a
			// level the catalog did not advertise.
			mapped[level] = nil
		}
	}
	raw, err := json.Marshal(mapped)
	if err != nil {
		return nil
	}
	return raw
}

// deriveOpenCodeInterleaved re-emits the catalog's interleaved fact in the
// OpenCode config union shape (boolean or field object).
func deriveOpenCodeInterleaved(value *modelsdev.Interleaved) json.RawMessage {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case "bool":
		return marshalRaw(value.Bool)
	case "field":
		return marshalRaw(map[string]string{"field": value.Field})
	default:
		return nil
	}
}

func marshalRaw(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}
