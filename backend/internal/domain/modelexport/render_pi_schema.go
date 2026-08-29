package modelexport

import (
	"errors"
	"fmt"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

func validatePiMergedMetadata(merged MetadataLayer) error {
	checks := []struct {
		leaf  string
		check func(any, string) error
	}{
		{MetaName, func(value any, field string) error { return targetString(value, field, false) }},
		{MetaReasoning, func(value any, field string) error { return targetBool(value, field) }},
		{MetaContextWindow, func(value any, field string) error { return targetPositiveInteger(value, field) }},
		{MetaMaxOutputTokens, func(value any, field string) error { return targetPositiveInteger(value, field) }},
		{MetaModalitiesInput, func(value any, field string) error { return targetStringArray(value, field, nil) }},
	}
	for _, item := range checks {
		raw, present := merged.Get(item.leaf)
		if !present {
			continue
		}
		value, err := decodeCanonicalJSON(raw)
		if err != nil {
			return &ErrTargetSchema{Field: item.leaf, Reason: "must contain exactly one valid JSON value"}
		}
		if err := item.check(value, item.leaf); err != nil {
			return targetSchemaError(err)
		}
	}
	return nil
}

func validatePiDocument(document map[string]any) error {
	root, err := normalizedTargetDocument(document)
	if err != nil {
		return err
	}
	if len(root) != 1 {
		return &ErrTargetSchema{Reason: "Pi root must contain only providers"}
	}
	providers, ok := root["providers"].(map[string]any)
	if !ok || len(providers) != 1 {
		return &ErrTargetSchema{Field: "providers", Reason: "must contain exactly one provider object"}
	}
	for providerID, rawProvider := range providers {
		if strings.TrimSpace(providerID) == "" {
			return &ErrTargetSchema{Field: "providers", Reason: "provider id must be non-empty"}
		}
		provider, ok := rawProvider.(map[string]any)
		if !ok {
			return &ErrTargetSchema{Field: "providers." + providerID, Reason: "must be an object"}
		}
		for _, key := range sortedAnyKeys(provider) {
			path := "providers." + providerID + "." + key
			switch key {
			case "name":
				if err := targetString(provider[key], path, true); err != nil {
					return targetSchemaError(err)
				}
			case "api":
				api, ok := provider[key].(string)
				if !ok || !stringInSet(api, piAPIOpenAIChat, piAPIResponses, piAPIAnthropic, piAPIGemini) {
					return &ErrTargetSchema{Field: path, Reason: "must be a supported Pi API literal"}
				}
			case "baseUrl":
				if err := targetHTTPURL(provider[key], path); err != nil {
					return targetSchemaError(err)
				}
			case "apiKey":
				if err := targetString(provider[key], path, true); err != nil {
					return targetSchemaError(err)
				}
			case "models":
				models, ok := provider[key].([]any)
				if !ok || len(models) == 0 {
					return &ErrTargetSchema{Field: path, Reason: "must be a non-empty array"}
				}
				for index, rawModel := range models {
					model, ok := rawModel.(map[string]any)
					if !ok {
						return &ErrTargetSchema{Field: fmt.Sprintf("%s[%d]", path, index), Reason: "must be an object"}
					}
					if err := validatePiRenderedModel(model, fmt.Sprintf("%s[%d]", path, index)); err != nil {
						return err
					}
				}
			default:
				return &ErrTargetSchema{Field: path, Reason: "is not allowed by the Pi 0.84.3 provider schema"}
			}
		}
	}
	return nil
}

func validatePiRenderedModel(model map[string]any, path string) error {
	if err := targetString(model["id"], path+".id", true); err != nil {
		return targetSchemaError(err)
	}
	api, ok := model["api"].(string)
	if !ok || !stringInSet(api, piAPIOpenAIChat, piAPIResponses, piAPIAnthropic, piAPIGemini) {
		return &ErrTargetSchema{Field: path + ".api", Reason: "must be a supported Pi API literal"}
	}
	if baseURL, present := model["baseUrl"]; present {
		if err := targetHTTPURL(baseURL, path+".baseUrl"); err != nil {
			return targetSchemaError(err)
		}
	}
	manualFields := map[string]any{}
	for key, value := range model {
		switch key {
		case "id", "api", "baseUrl":
		case "cost":
			if err := validatePiCost(value, path+".cost"); err != nil {
				return err
			}
		default:
			manualFields[key] = value
		}
	}
	return targetSchemaError(validatePiEnhancement(api, manualFields))
}

func validatePiEnhancement(api string, fields map[string]any) error {
	for _, field := range sortedAnyKeys(fields) {
		value := fields[field]
		switch field {
		case "name":
			if err := targetString(value, field, true); err != nil {
				return err
			}
		case "reasoning":
			if err := targetBool(value, field); err != nil {
				return err
			}
		case "contextWindow", "maxTokens":
			if err := targetPositiveInteger(value, field); err != nil {
				return err
			}
		case "input":
			if err := targetStringArray(value, field, map[string]struct{}{"text": {}, "image": {}}); err != nil {
				return err
			}
		case "thinkingLevelMap":
			if err := validatePiThinkingLevelMap(value, field); err != nil {
				return err
			}
		case "compat":
			if err := validatePiCompat(api, value, field); err != nil {
				return err
			}
		case "samplingParams":
			return invalidTargetField(field, "is intentionally excluded from this export")
		default:
			return invalidTargetField(field, "is not a Pi 0.84.3 model field supported by this export")
		}
	}
	return nil
}

func validatePiCost(value any, path string) error {
	cost, ok := value.(map[string]any)
	if !ok {
		return &ErrTargetSchema{Field: path, Reason: "must be an object"}
	}
	required := map[string]struct{}{"input": {}, "output": {}, "cacheRead": {}, "cacheWrite": {}}
	for key := range required {
		if _, present := cost[key]; !present {
			return &ErrTargetSchema{Field: path + "." + key, Reason: "is required"}
		}
	}
	for _, key := range sortedAnyKeys(cost) {
		if _, ok := required[key]; ok {
			if err := targetNumber(cost[key], path+"."+key); err != nil {
				return targetSchemaError(err)
			}
			continue
		}
		if key != "tiers" {
			return &ErrTargetSchema{Field: path + "." + key, Reason: "is not supported"}
		}
		tiers, ok := cost[key].([]any)
		if !ok || len(tiers) != 1 {
			return &ErrTargetSchema{Field: path + ".tiers", Reason: "must contain exactly one tier"}
		}
		tier, ok := tiers[0].(map[string]any)
		if !ok || len(tier) != 5 {
			return &ErrTargetSchema{Field: path + ".tiers[0]", Reason: "must contain the threshold and four cost rates"}
		}
		for _, tierKey := range []string{"inputTokensAbove", "input", "output", "cacheRead", "cacheWrite"} {
			if err := targetNumber(tier[tierKey], path+".tiers[0]."+tierKey); err != nil {
				return targetSchemaError(err)
			}
		}
	}
	return nil
}

func validatePiThinkingLevelMap(value any, field string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return invalidTargetField(field, "must be an object")
	}
	allowed := map[string]struct{}{"off": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {}}
	for key, item := range object {
		if _, ok := allowed[key]; !ok {
			return invalidTargetField(field+"."+key, "is not a supported Pi thinking level")
		}
		if item == nil {
			continue
		}
		if _, ok := item.(string); !ok {
			return invalidTargetField(field+"."+key, "must be a string or null")
		}
	}
	return nil
}

func validatePiCompat(api string, value any, field string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return invalidTargetField(field, "must be an object")
	}
	if err := pidev.ValidateCompat(api, object); err != nil {
		var invalid *pidev.CompatValidationError
		if errors.As(err, &invalid) {
			return invalidTargetField(field+"."+invalid.Path, invalid.Reason)
		}
		return invalidTargetField(field, err.Error())
	}
	return nil
}

// ValidatePiSourceField validates one safe pi.dev leaf value (name, reasoning,
// input, context_window, max_tokens, thinking_level_map, or compat) against
// the same Pi 0.84.3 schema the renderer enforces. It is the single
// validation entry point shared by RenderPi and the persisted Pi binding's
// override surface, so a stored override can never carry a shape render
// would later reject.
func ValidatePiSourceField(api string, field string, value any) error {
	switch field {
	case "name":
		return targetSchemaError(targetString(value, field, true))
	case "reasoning":
		return targetSchemaError(targetBool(value, field))
	case "input":
		return targetSchemaError(targetStringArray(value, field, map[string]struct{}{"text": {}, "image": {}}))
	case "context_window", "max_tokens":
		return targetSchemaError(targetPositiveInteger(value, field))
	case "thinking_level_map":
		return targetSchemaError(validatePiThinkingLevelMap(value, field))
	case "compat":
		return targetSchemaError(validatePiCompat(api, value, field))
	default:
		return &ErrTargetSchema{Field: field, Reason: "is not a Pi binding source or override field"}
	}
}

func stringInSet(value any, allowed ...string) bool {
	text, ok := value.(string)
	if !ok {
		return false
	}
	for _, candidate := range allowed {
		if text == candidate {
			return true
		}
	}
	return false
}
