package modelexport

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

const OpenCodeFileName = "opencode-prism.json"

var openCodeProviderPattern = regexp.MustCompile(`^prism(?:-[a-z0-9][a-z0-9_-]*)?$`)

type OpenCodeInput struct {
	Facts         OpenCodeSourceFacts
	Selection     []int
	BaseURL       string
	ProviderID    string
	IncludeAPIKey bool
	APIKey        string
}

// NormalizeOpenCodeProviderID reserves a distinct namespace rather than
// allowing built-in provider loaders to replace the per-model SDK contract.
func NormalizeOpenCodeProviderID(value string) (string, error) {
	id := strings.TrimSpace(value)
	if id == "" {
		id = "prism"
	}
	if !openCodeProviderPattern.MatchString(id) {
		return "", &ErrTargetSchema{Field: "provider_id", Reason: "must be prism or prism- followed by a lowercase letter or digit and optional lowercase letters, digits, underscores or hyphens"}
	}
	return id, nil
}

func OpenCodeSDKForModel(family string, format *string) string {
	switch family {
	case "openai":
		switch optionalString(format) {
		case "chat_completions_only":
			return "@ai-sdk/openai-compatible"
		case "responses_only", "dual_native":
			return "@ai-sdk/openai"
		}
	case "anthropic":
		return "@ai-sdk/anthropic"
	case "gemini":
		return "@ai-sdk/google"
	}
	return ""
}

func OpenCodeAPIPathForModel(family string) string {
	if family == "gemini" {
		return "/v1beta"
	}
	return "/v1"
}

func RenderOpenCode(input OpenCodeInput) (*RenderResult, error) {
	if input.Facts.TargetVersion != OpenCodeTargetVersion {
		return nil, &ErrTargetSchema{Field: "target_version", Reason: "unsupported OpenCode version"}
	}
	providerID, err := NormalizeOpenCodeProviderID(input.ProviderID)
	if err != nil {
		return nil, err
	}
	origin := strings.TrimRight(strings.TrimSpace(input.BaseURL), "/")
	if err := targetHTTPURL(origin, "base_url"); err != nil {
		return nil, targetSchemaError(err)
	}
	parsed, _ := url.Parse(origin)
	if parsed.Path != "" || parsed.RawPath != "" {
		return nil, &ErrTargetSchema{Field: "base_url", Reason: "must be an origin without a path"}
	}
	key := strings.TrimSpace(input.APIKey)
	if (input.IncludeAPIKey && key == "") || (!input.IncludeAPIKey && key != "") {
		return nil, &ErrTargetSchema{Field: "api_key", Reason: "must be a non-empty final value only when include_api_key is true"}
	}
	selection, err := NormalizeOpenCodeSelection(input.Selection, input.Facts)
	if err != nil {
		return nil, err
	}
	byID := map[int]OpenCodeModelFact{}
	for _, fact := range input.Facts.Models {
		byID[fact.ModelConfigID] = fact
	}
	models := map[string]any{}
	results := []ModelRenderResult{}
	warnings := []string{}
	for _, id := range selection {
		fact := byID[id]
		if fact.ModelID == "" {
			return nil, &ErrTargetSchema{Field: "model_id", Reason: "must not be empty"}
		}
		if _, exists := models[fact.ModelID]; exists {
			return nil, &ErrTargetSchema{Field: "model_id", Reason: "duplicate model identity"}
		}
		model, result := renderOpenCodeModel(fact, origin)
		models[fact.ModelID] = model
		results = append(results, result)
		warnings = append(warnings, result.WarningCodes...)
	}
	provider := map[string]any{"name": "Prism", "models": models}
	if input.IncludeAPIKey {
		provider["options"] = map[string]any{"apiKey": key}
	} else {
		provider["env"] = []string{"PRISM_API_KEY"}
	}
	document := map[string]any{"$schema": "https://opencode.ai/config.json", "provider": map[string]any{providerID: provider}}
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	// OpenCode substitutes raw {env:...}/{file:...} tokens once, before JSON
	// parsing. Escape the opening brace inside JSON strings so final keys,
	// metadata and complete model IDs retain their literal meaning on load.
	// Hashing and every delivery action consume these escaped bytes.
	escaped := strings.NewReplacer("{env:", `\u007benv:`, "{file:", `\u007bfile:`).Replace(string(raw))
	result := finalizeJSONBytes([]byte(escaped), OpenCodeFileName)
	result.ModelResults = results
	result.Warnings = sortWarningCodes(warnings)
	return result, nil
}

func renderOpenCodeModel(fact OpenCodeModelFact, origin string) (map[string]any, ModelRenderResult) {
	projection := ProjectOpenCodeMetadata(fact)
	model := map[string]any{
		"provider": map[string]any{"npm": OpenCodeSDKForModel(fact.APIFamily, fact.OpenAIAcceptedFormat), "api": origin + OpenCodeAPIPathForModel(fact.APIFamily)},
	}
	limits, modalities := map[string]any{}, map[string]any{}
	for leaf, raw := range projection.MergedMetadata {
		// Projection already validated these values; RawMessage preserves
		// explicit false, zero, empty arrays and exact safe integer limits.
		switch {
		case strings.HasPrefix(leaf, "limit_"):
			limits[strings.TrimPrefix(leaf, "limit_")] = json.RawMessage(raw)
		case strings.HasPrefix(leaf, "modalities_"):
			modalities[strings.TrimPrefix(leaf, "modalities_")] = json.RawMessage(raw)
		default:
			model[leaf] = json.RawMessage(raw)
		}
	}
	model["limit"] = limits
	if len(modalities) > 0 {
		model["modalities"] = modalities
	}
	prices := targetPriceSnapshots(fact.Targets)
	decision := DecideOpenCodePriceExport(prices)
	if decision.Exportable {
		model["cost"] = openCodeCostGroup(prices[0].Card)
	}
	warnings := decision.WarningCodes
	if len(projection.MissingMetadata) > 0 || len(projection.Issues) > 0 {
		warnings = append(warnings, WarningMetadataIncomplete)
	}
	return model, ModelRenderResult{
		ModelConfigID: fact.ModelConfigID, ModelID: fact.ModelID,
		CostExported: decision.Exportable, MissingMetadata: projection.MissingMetadata,
		WarningCodes: sortWarningCodes(warnings),
	}
}
