package modelexport

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

// OpenCode 1.18.x config contract constants.
const (
	OpenCodeProviderID = "prism"
	OpenCodeFileName   = "opencode-prism.json"
	OpenCodeKeyEnvVar  = "PRISM_API_KEY"
	OpenCodeSchemaURL  = "https://opencode.ai/config.json"
	ocNpmOpenAI        = "@ai-sdk/openai"
	ocNpmOpenAICompat  = "@ai-sdk/openai-compatible"
	ocNpmAnthropic     = "@ai-sdk/anthropic"
	ocNpmGoogle        = "@ai-sdk/google"
)

// ocLockedPaths lists model-object keys owned by Prism truth: identity, the
// protocol slot (npm/api), the base URL, prices, and credential material.
var ocLockedPaths = []string{"id", "provider", "cost", "options.baseURL"}

// ocModelNPM maps one Prism family/format pair onto the model-level SDK
// package. dual_native pins to the Responses-capable OpenAI SDK.
func ocModelNPM(apiFamily string, acceptedFormat *string) string {
	switch strings.TrimSpace(apiFamily) {
	case "openai":
		switch strings.TrimSpace(optionalString(acceptedFormat)) {
		case "chat_completions_only":
			return ocNpmOpenAICompat
		default:
			return ocNpmOpenAI
		}
	case "anthropic":
		return ocNpmAnthropic
	case "gemini":
		return ocNpmGoogle
	default:
		return ""
	}
}

// OpenCodeInput carries everything RenderOpenCode needs without any I/O.
type OpenCodeInput struct {
	Facts        SourceFacts
	Selection    []int
	Enrichment   map[int]PlatformCandidate
	Enhancements map[int]ManualEnhancement
	// DefaultModel marks the model id advertised as config-level default.
	// Empty omits the field.
	DefaultModel  *int
	BaseURL       string
	ProviderID    string
	IncludeAPIKey bool
	// APIKey is the final operator-typed Prism proxy key, never an endpoint key.
	APIKey string
}

// RenderOpenCode assembles the single-provider config document deterministically.
func RenderOpenCode(input OpenCodeInput) (*RenderResult, error) {
	byID := make(map[int]ModelFact, len(input.Facts.Models))
	for _, fact := range input.Facts.Models {
		byID[fact.ModelConfigID] = fact
	}
	models := map[string]any{}
	documentWarnings := map[string]struct{}{}
	baseURLs := map[string]struct{}{}

	for _, id := range input.Selection {
		fact, ok := byID[id]
		if !ok {
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: "not_found_in_default_profile"}
		}
		modelObject, modelResult, err := renderOpenCodeModel(fact, input)
		if err != nil {
			return nil, err
		}
		models[fact.ModelID] = modelObject
		for _, code := range modelResult.WarningCodes {
			documentWarnings[code] = struct{}{}
		}
		if baseURL := clientBaseURL(PlatformOpenCode, input.BaseURL, fact.APIFamily); baseURL != "" {
			baseURLs[baseURL] = struct{}{}
		}
	}

	options := map[string]any{}
	uniform := len(baseURLs) == 1
	for url := range baseURLs {
		options["baseURL"] = url
	}
	if !uniform && len(baseURLs) > 1 {
		documentWarnings[WarningMixedBaseURLs] = struct{}{}
		delete(options, "baseURL")
	}
	if input.IncludeAPIKey {
		options["apiKey"] = input.APIKey
	}

	provider := map[string]any{
		"name": "Prism",
		"env":  []string{OpenCodeKeyEnvVar},
	}
	if len(options) > 0 {
		provider["options"] = options
	}
	provider["models"] = models

	document := map[string]any{
		"$schema":  OpenCodeSchemaURL,
		"provider": map[string]any{providerIDOrDefault(input.ProviderID): provider},
	}
	// A config-level default exists only when the operator explicitly chose
	// one, and it must be part of the explicit selection.
	if defaultID := input.DefaultModel; defaultID != nil {
		selected := false
		for _, id := range input.Selection {
			selected = selected || id == *defaultID
		}
		fact, exists := byID[*defaultID]
		if !selected || !exists {
			return nil, &ErrDefaultModel{Reason: "default_model_config_id must identify a selected model"}
		}
		document["model"] = providerIDOrDefault(input.ProviderID) + "/" + fact.ModelID
	}
	if err := validateOpenCodeDocument(document); err != nil {
		return nil, err
	}

	rendered, err := finalizeDocument(PlatformOpenCode, document)
	if err != nil {
		return nil, err
	}
	warnings := make([]string, 0, len(documentWarnings))
	for code := range documentWarnings {
		warnings = append(warnings, code)
	}
	rendered.Warnings = sortWarningCodes(warnings)
	return rendered, nil
}

func renderOpenCodeModel(fact ModelFact, input OpenCodeInput) (map[string]any, *ModelRenderResult, error) {
	object := map[string]any{}
	warnings := map[string]struct{}{}
	candidate := input.Enrichment[fact.ModelConfigID]
	enhancement := input.Enhancements[fact.ModelConfigID]
	if err := enhancement.ValidateForPlatform(PlatformOpenCode); err != nil {
		return nil, nil, err
	}

	merge, err := MergeKnownMetadata(MergeOptions{
		Prism:          NewMetadataLayer(fact.PrismMetadata),
		ModelsDev:      candidate.Metadata,
		Manual:         ocManualMetadataLayer(enhancement),
		OverrideFields: enhancement.OverrideFields,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := validateOpenCodeMergedMetadata(merge.Merged); err != nil {
		return nil, nil, err
	}
	for _, code := range MetadataWarningCodes(PlatformOpenCode, fact, merge.Merged) {
		warnings[code] = struct{}{}
	}
	for _, code := range candidate.WarningCodes {
		warnings[code] = struct{}{}
	}

	setString := func(key, leaf string) {
		value, ok := merge.Merged.Get(leaf)
		if !ok {
			return
		}
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return
		}
		object[key] = text
	}
	setBool := func(key, leaf string) {
		value, ok := merge.Merged.Get(leaf)
		if !ok {
			return
		}
		var flag bool
		if err := json.Unmarshal(value, &flag); err != nil {
			return
		}
		object[key] = flag
	}
	setString("name", MetaName)
	setString("family", MetaFamily)
	setString("release_date", MetaReleaseDate)
	setBool("attachment", MetaAttachment)
	setBool("reasoning", MetaReasoning)
	setBool("temperature", MetaTemperature)
	setBool("tool_call", MetaToolCall)

	// limit requires context and output together when present at all.
	contextRaw, hasContext := merge.Merged.Get(MetaContextWindow)
	outputRaw, hasOutput := merge.Merged.Get(MetaMaxOutputTokens)
	if hasContext && hasOutput {
		var contextNumber, outputNumber json.Number
		if err := json.Unmarshal(contextRaw, &contextNumber); err == nil {
			if err := json.Unmarshal(outputRaw, &outputNumber); err == nil {
				limit := map[string]any{"context": contextNumber, "output": outputNumber}
				if inputRaw, hasInput := merge.Merged.Get(MetaMaxInputTokens); hasInput {
					var inputNumber json.Number
					if err := json.Unmarshal(inputRaw, &inputNumber); err == nil {
						limit["input"] = inputNumber
					}
				}
				object["limit"] = limit
			}
		}
	} else if hasContext || hasOutput {
		// One half of the pair alone would coerce to 0 in the client; omit
		// the whole group instead of disguising unknown as zero.
		warnings[WarningMetadataIncomplete] = struct{}{}
	}

	// modalities project only known sides; each side must be non-empty.
	if raw, ok := merge.Merged.Get(MetaModalitiesInput); ok {
		var values []string
		if err := json.Unmarshal(raw, &values); err == nil {
			modalitiesAny, _ := object["modalities"].(map[string]any)
			if modalitiesAny == nil {
				modalitiesAny = map[string]any{}
				object["modalities"] = modalitiesAny
			}
			modalitiesAny["input"] = values
		}
	}
	if raw, ok := merge.Merged.Get(MetaModalitiesOutput); ok {
		var values []string
		if err := json.Unmarshal(raw, &values); err == nil {
			modalitiesAny, _ := object["modalities"].(map[string]any)
			if modalitiesAny == nil {
				modalitiesAny = map[string]any{}
				object["modalities"] = modalitiesAny
			}
			modalitiesAny["output"] = values
		}
	}

	// Derived enrichment is limited to the interleaved passthrough. Variants
	// remain an explicitly confirmed manual/uploaded field.
	for _, field := range sortedRawKeys(candidate.DerivedFields) {
		switch field {
		case "interleaved":
		default:
			continue
		}
		if _, exists := object[field]; exists {
			continue
		}
		if err := checkLockedPath(field, ocLockedPaths); err != nil {
			return nil, nil, err
		}
		decoded, err := decodeCanonicalJSON(candidate.DerivedFields[field])
		if err != nil {
			return nil, nil, fmt.Errorf("field %s: %w", field, err)
		}
		object[field] = decoded
	}

	if err := applyEnhancement(object, enhancement, ocLockedPaths); err != nil {
		return nil, nil, err
	}
	if err := validateOpenCodeRequiredGroups(object); err != nil {
		return nil, nil, err
	}

	// The per-model protocol slot is written last so no enhancement can
	// preempt it, mirroring Prism truth rules.
	npm := ocModelNPM(fact.APIFamily, fact.OpenAIAcceptedFormat)
	protocolSlot := map[string]any{"npm": npm}
	if baseURL := clientBaseURL(PlatformOpenCode, input.BaseURL, fact.APIFamily); baseURL != "" {
		protocolSlot["api"] = baseURL
	}
	object["provider"] = protocolSlot

	priceTargets := priceSnapshots(fact)
	decision := DecidePriceExport(PlatformOpenCode, priceTargets)
	for _, code := range decision.WarningCodes {
		warnings[code] = struct{}{}
	}
	if decision.Exportable {
		object["cost"] = openCodeCostGroup(priceTargets[0])
	}

	modelResult := &ModelRenderResult{
		ModelConfigID:   fact.ModelConfigID,
		ModelID:         fact.ModelID,
		CostExported:    decision.Exportable,
		WarningCodes:    sortedWarningSet(warnings),
		MissingMetadata: merge.Missing,
	}
	return object, modelResult, nil
}

func openCodeCostGroup(reference TargetPriceSnapshot) map[string]any {
	card := reference.Card
	if reference.Kind == pricingkind.Tiered {
		card = reference.BaseCard
	}
	cost := map[string]any{
		"input":       decimal(card.InputPrice),
		"output":      decimal(card.OutputPrice),
		"cache_read":  decimal(derefOrZero(card.CachedInputPrice)),
		"cache_write": decimal(derefOrZero(card.CacheCreationPrice)),
	}
	if reference.Kind == pricingkind.Tiered {
		above := reference.AboveCard
		cost["context_over_200k"] = map[string]any{
			"input":       decimal(above.InputPrice),
			"output":      decimal(above.OutputPrice),
			"cache_read":  decimal(derefOrZero(above.CachedInputPrice)),
			"cache_write": decimal(derefOrZero(above.CacheCreationPrice)),
		}
	}
	return cost
}

// ocManualMetadataLayer projects manual OpenCode field names onto canonical
// metadata leaves. Everything else applies at key level via applyEnhancement.
func ocManualMetadataLayer(enhancement ManualEnhancement) MetadataLayer {
	payload := strings.TrimSpace(string(enhancement.Fields))
	if payload == "" || payload == "null" {
		return MetadataLayer{}
	}
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		return MetadataLayer{}
	}
	values := map[string]json.RawMessage{}
	for leaf, raw := range fields {
		switch leaf {
		case "name":
			values[MetaName] = raw
		case "family":
			values[MetaFamily] = raw
		case "release_date":
			values[MetaReleaseDate] = raw
		case "attachment":
			values[MetaAttachment] = raw
		case "reasoning":
			values[MetaReasoning] = raw
		case "temperature":
			values[MetaTemperature] = raw
		case "tool_call":
			values[MetaToolCall] = raw
		case "interleaved":
			values[MetaInterleaved] = raw
		}
	}
	if raw, ok := fields["limit"]; ok {
		var limit map[string]json.RawMessage
		if json.Unmarshal(raw, &limit) == nil {
			if value, present := limit["context"]; present {
				values[MetaContextWindow] = value
			}
			if value, present := limit["input"]; present {
				values[MetaMaxInputTokens] = value
			}
			if value, present := limit["output"]; present {
				values[MetaMaxOutputTokens] = value
			}
		}
	}
	if raw, ok := fields["modalities"]; ok {
		var modalities map[string]json.RawMessage
		if json.Unmarshal(raw, &modalities) == nil {
			if value, present := modalities["input"]; present {
				values[MetaModalitiesInput] = value
			}
			if value, present := modalities["output"]; present {
				values[MetaModalitiesOutput] = value
			}
		}
	}
	return NewMetadataLayer(values)
}

func validateOpenCodeEnhancement(fields map[string]any) error {
	for _, field := range sortedAnyKeys(fields) {
		value := fields[field]
		switch field {
		case "name", "family", "release_date":
			if err := targetString(value, field, false); err != nil {
				return err
			}
		case "attachment", "reasoning", "temperature", "tool_call":
			if err := targetBool(value, field); err != nil {
				return err
			}
		case "interleaved":
			if err := validateOpenCodeInterleaved(value, field); err != nil {
				return err
			}
		case "limit":
			if err := validateOpenCodeLimit(value, field, false); err != nil {
				return err
			}
		case "modalities":
			if err := validateOpenCodeModalities(value, field); err != nil {
				return err
			}
		case "headers":
			if err := targetStringRecord(value, field); err != nil {
				return err
			}
		case "options":
			if _, ok := value.(map[string]any); !ok {
				return invalidTargetField(field, "must be an object")
			}
		case "variants":
			if err := validateOpenCodeVariants(value, field); err != nil {
				return err
			}
		case "experimental", "status":
			return invalidTargetField(field, "is intentionally excluded from this export")
		default:
			return invalidTargetField(field, "is not an OpenCode 1.18.23 model field supported by this export")
		}
	}
	return nil
}

func validateOpenCodeMergedMetadata(merged MetadataLayer) error {
	stringLeaves := []string{MetaName, MetaFamily, MetaReleaseDate}
	boolLeaves := []string{MetaAttachment, MetaReasoning, MetaTemperature, MetaToolCall}
	numberLeaves := []string{MetaContextWindow, MetaMaxInputTokens, MetaMaxOutputTokens}
	for _, leaf := range stringLeaves {
		if err := validateMergedLeaf(merged, leaf, func(value any) error { return targetString(value, leaf, false) }); err != nil {
			return err
		}
	}
	for _, leaf := range boolLeaves {
		if err := validateMergedLeaf(merged, leaf, func(value any) error { return targetBool(value, leaf) }); err != nil {
			return err
		}
	}
	for _, leaf := range numberLeaves {
		if err := validateMergedLeaf(merged, leaf, func(value any) error { return targetNumber(value, leaf) }); err != nil {
			return err
		}
	}
	allowedModalities := map[string]struct{}{"text": {}, "audio": {}, "image": {}, "video": {}, "pdf": {}}
	for _, leaf := range []string{MetaModalitiesInput, MetaModalitiesOutput} {
		if err := validateMergedLeaf(merged, leaf, func(value any) error { return targetStringArray(value, leaf, allowedModalities) }); err != nil {
			return err
		}
	}
	return nil
}

func validateMergedLeaf(merged MetadataLayer, leaf string, check func(any) error) error {
	raw, present := merged.Get(leaf)
	if !present {
		return nil
	}
	value, err := decodeCanonicalJSON(raw)
	if err != nil {
		return &ErrTargetSchema{Field: leaf, Reason: "must contain exactly one valid JSON value"}
	}
	return targetSchemaError(check(value))
}

func validateOpenCodeInterleaved(value any, field string) error {
	switch typed := value.(type) {
	case bool, string:
		return nil
	case map[string]any:
		if len(typed) != 1 {
			return invalidTargetField(field, "must contain only field")
		}
		return targetString(typed["field"], field+".field", false)
	default:
		return invalidTargetField(field, "must be a boolean, string, or {field:string}")
	}
}

func validateOpenCodeLimit(value any, field string, requirePair bool) error {
	object, ok := value.(map[string]any)
	if !ok {
		return invalidTargetField(field, "must be an object")
	}
	for _, key := range sortedAnyKeys(object) {
		if key != "context" && key != "input" && key != "output" {
			return invalidTargetField(field+"."+key, "is not supported")
		}
		if err := targetNumber(object[key], field+"."+key); err != nil {
			return err
		}
	}
	if requirePair {
		if _, ok := object["context"]; !ok {
			return invalidTargetField(field+".context", "is required when limit is present")
		}
		if _, ok := object["output"]; !ok {
			return invalidTargetField(field+".output", "is required when limit is present")
		}
	}
	return nil
}

func validateOpenCodeModalities(value any, field string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return invalidTargetField(field, "must be an object")
	}
	allowed := map[string]struct{}{"text": {}, "audio": {}, "image": {}, "video": {}, "pdf": {}}
	for _, key := range sortedAnyKeys(object) {
		if key != "input" && key != "output" {
			return invalidTargetField(field+"."+key, "is not supported")
		}
		if err := targetStringArray(object[key], field+"."+key, allowed); err != nil {
			return err
		}
	}
	return nil
}

func validateOpenCodeVariants(value any, field string) error {
	variants, ok := value.(map[string]any)
	if !ok {
		return invalidTargetField(field, "must be an object")
	}
	for name, value := range variants {
		variant, ok := value.(map[string]any)
		if !ok {
			return invalidTargetField(field+"."+name, "must be an object")
		}
		if disabled, present := variant["disabled"]; present {
			if err := targetBool(disabled, field+"."+name+".disabled"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateOpenCodeRequiredGroups(object map[string]any) error {
	if limit, present := object["limit"]; present {
		if err := validateOpenCodeLimit(limit, "limit", true); err != nil {
			return err
		}
	}
	return nil
}

func validateOpenCodeDocument(document map[string]any) error {
	root, err := normalizedTargetDocument(document)
	if err != nil {
		return err
	}
	for _, key := range sortedAnyKeys(root) {
		if key != "$schema" && key != "provider" && key != "model" {
			return &ErrTargetSchema{Field: key, Reason: "is not an OpenCode 1.18.23 root field emitted by this export"}
		}
	}
	if schema, ok := root["$schema"].(string); !ok || schema != OpenCodeSchemaURL {
		return &ErrTargetSchema{Field: "$schema", Reason: "must pin the OpenCode config schema"}
	}
	providers, ok := root["provider"].(map[string]any)
	if !ok || len(providers) != 1 {
		return &ErrTargetSchema{Field: "provider", Reason: "must contain exactly one provider object"}
	}
	for providerID, rawProvider := range providers {
		if strings.TrimSpace(providerID) == "" || strings.Contains(providerID, "/") {
			return &ErrTargetSchema{Field: "provider", Reason: "provider id must be non-empty and slash-free"}
		}
		provider, ok := rawProvider.(map[string]any)
		if !ok {
			return &ErrTargetSchema{Field: "provider." + providerID, Reason: "must be an object"}
		}
		models, err := validateOpenCodeProvider(provider, "provider."+providerID)
		if err != nil {
			return err
		}
		if rawDefault, present := root["model"]; present {
			defaultModel, ok := rawDefault.(string)
			prefix := providerID + "/"
			if !ok || !strings.HasPrefix(defaultModel, prefix) {
				return &ErrTargetSchema{Field: "model", Reason: "must reference the exported provider"}
			}
			if _, exists := models[strings.TrimPrefix(defaultModel, prefix)]; !exists {
				return &ErrTargetSchema{Field: "model", Reason: "must reference an exported model"}
			}
		}
	}
	return nil
}

func validateOpenCodeProvider(provider map[string]any, path string) (map[string]any, error) {
	for _, key := range sortedAnyKeys(provider) {
		field := path + "." + key
		switch key {
		case "name":
			if err := targetString(provider[key], field, false); err != nil {
				return nil, targetSchemaError(err)
			}
		case "env":
			if err := targetStringArray(provider[key], field, nil); err != nil {
				return nil, targetSchemaError(err)
			}
			env := provider[key].([]any)
			if len(env) != 1 || env[0] != OpenCodeKeyEnvVar {
				return nil, &ErrTargetSchema{Field: field, Reason: "must contain only PRISM_API_KEY"}
			}
		case "options":
			options, ok := provider[key].(map[string]any)
			if !ok {
				return nil, &ErrTargetSchema{Field: field, Reason: "must be an object"}
			}
			for _, option := range sortedAnyKeys(options) {
				switch option {
				case "baseURL":
					if err := targetHTTPURL(options[option], field+"."+option); err != nil {
						return nil, targetSchemaError(err)
					}
				case "apiKey":
					if err := targetString(options[option], field+"."+option, false); err != nil {
						return nil, targetSchemaError(err)
					}
				default:
					return nil, &ErrTargetSchema{Field: field + "." + option, Reason: "is not emitted by this export"}
				}
			}
		case "models":
		case "api", "id", "npm", "whitelist", "blacklist":
			return nil, &ErrTargetSchema{Field: field, Reason: "is not emitted by this export"}
		default:
			return nil, &ErrTargetSchema{Field: field, Reason: "is not supported"}
		}
	}
	models, ok := provider["models"].(map[string]any)
	if !ok || len(models) == 0 {
		return nil, &ErrTargetSchema{Field: path + ".models", Reason: "must be a non-empty object"}
	}
	for _, modelID := range sortedAnyKeys(models) {
		if strings.TrimSpace(modelID) == "" {
			return nil, &ErrTargetSchema{Field: path + ".models", Reason: "model id must be non-empty"}
		}
		model, ok := models[modelID].(map[string]any)
		if !ok {
			return nil, &ErrTargetSchema{Field: path + ".models." + modelID, Reason: "must be an object"}
		}
		if err := validateOpenCodeRenderedModel(model, path+".models."+modelID); err != nil {
			return nil, err
		}
	}
	return models, nil
}

func validateOpenCodeRenderedModel(model map[string]any, path string) error {
	provider, ok := model["provider"].(map[string]any)
	if !ok || len(provider) != 2 {
		return &ErrTargetSchema{Field: path + ".provider", Reason: "must contain exactly npm and api"}
	}
	if npm, ok := provider["npm"].(string); !ok || !stringInSet(npm, ocNpmOpenAI, ocNpmOpenAICompat, ocNpmAnthropic, ocNpmGoogle) {
		return &ErrTargetSchema{Field: path + ".provider.npm", Reason: "must be a supported SDK package"}
	}
	if err := targetHTTPURL(provider["api"], path+".provider.api"); err != nil {
		return targetSchemaError(err)
	}
	manualFields := map[string]any{}
	for key, value := range model {
		switch key {
		case "provider":
		case "cost":
			if err := validateOpenCodeCost(value, path+".cost"); err != nil {
				return err
			}
		default:
			manualFields[key] = value
		}
	}
	if err := targetSchemaError(validateOpenCodeEnhancement(manualFields)); err != nil {
		return err
	}
	return targetSchemaError(validateOpenCodeRequiredGroups(manualFields))
}

func validateOpenCodeCost(value any, path string) error {
	cost, ok := value.(map[string]any)
	if !ok {
		return &ErrTargetSchema{Field: path, Reason: "must be an object"}
	}
	for _, required := range []string{"input", "output"} {
		if _, present := cost[required]; !present {
			return &ErrTargetSchema{Field: path + "." + required, Reason: "is required"}
		}
	}
	for _, key := range sortedAnyKeys(cost) {
		switch key {
		case "input", "output", "cache_read", "cache_write":
			if err := targetNumber(cost[key], path+"."+key); err != nil {
				return targetSchemaError(err)
			}
		case "context_over_200k":
			above, ok := cost[key].(map[string]any)
			if !ok {
				return &ErrTargetSchema{Field: path + "." + key, Reason: "must be an object"}
			}
			for _, required := range []string{"input", "output"} {
				if _, present := above[required]; !present {
					return &ErrTargetSchema{Field: path + "." + key + "." + required, Reason: "is required"}
				}
			}
			for _, component := range sortedAnyKeys(above) {
				if component != "input" && component != "output" && component != "cache_read" && component != "cache_write" {
					return &ErrTargetSchema{Field: path + "." + key + "." + component, Reason: "is not supported"}
				}
				if err := targetNumber(above[component], path+"."+key+"."+component); err != nil {
					return targetSchemaError(err)
				}
			}
		default:
			return &ErrTargetSchema{Field: path + "." + key, Reason: "is not supported"}
		}
	}
	return nil
}
