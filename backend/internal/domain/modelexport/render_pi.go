package modelexport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

const (
	PiProviderID    = "prism"
	PiFileName      = "prism-pi-models.json"
	piAPIOpenAIChat = "openai-completions"
	piAPIResponses  = "openai-responses"
	piAPIAnthropic  = "anthropic-messages"
	piAPIGemini     = "google-generative-ai"
)

var piLockedPaths = []string{"id", "api", "baseUrl", "cost"}

func piModelAPI(apiFamily string, acceptedFormat *string) string {
	switch strings.TrimSpace(apiFamily) {
	case "openai":
		switch strings.TrimSpace(optionalString(acceptedFormat)) {
		case "chat_completions_only":
			return piAPIOpenAIChat
		default:
			return piAPIResponses
		}
	case "anthropic":
		return piAPIAnthropic
	case "gemini":
		return piAPIGemini
	default:
		return ""
	}
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type PiInput struct {
	Facts     SourceFacts
	Selection []int
	// PiCandidates holds the safe Pi projection for the selected coordinate per model.
	// It is derived from the selected pidev entry via DerivePiCandidate.
	Enrichment    map[int]PlatformCandidate
	BaseURL       string
	ProviderID    string
	IncludeAPIKey bool
	APIKey        string
}

type PlatformCandidate struct {
	Metadata      MetadataLayer
	DerivedFields map[string]json.RawMessage
	WarningCodes  []string
}

func (c PlatformCandidate) MarshalJSON() ([]byte, error) {
	type wire struct {
		Metadata map[string]json.RawMessage `json:"metadata"`
		Derived  map[string]json.RawMessage `json:"derived,omitempty"`
		Warnings []string                   `json:"warnings,omitempty"`
	}
	return json.Marshal(wire{Metadata: c.Metadata.Values(), Derived: c.DerivedFields, Warnings: sortWarningCodes(c.WarningCodes)})
}

func RenderPi(input PiInput) (*RenderResult, error) {
	byID := make(map[int]ModelFact, len(input.Facts.Models))
	for _, fact := range input.Facts.Models {
		byID[fact.ModelConfigID] = fact
	}
	models := make([]any, 0, len(input.Selection))
	baseURLs := map[string]struct{}{}
	apiValues := map[string]struct{}{}
	documentWarnings := map[string]struct{}{}

	for _, id := range input.Selection {
		fact, ok := byID[id]
		if !ok {
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: "not_found_in_default_profile"}
		}
		// For multi-candidate models, PiSelected must be non-nil; otherwise block.
		if len(fact.PiCandidates) > 1 && fact.PiSelected == nil {
			return nil, &ErrCandidateUnselected{ModelConfigID: id}
		}
		if fact.PiSelected != nil {
			// Verify selected is among candidates
			found := false
			for _, cand := range fact.PiCandidates {
				if cand.ProviderID == fact.PiSelected.ProviderID && cand.ModelID == fact.PiSelected.ModelID {
					found = true
					break
				}
			}
			if !found {
				return nil, &ErrCandidateInvalid{ModelConfigID: id}
			}
		}
		modelObject, modelResult, err := renderPiModel(fact, input)
		if err != nil {
			return nil, err
		}
		models = append(models, modelObject)
		for _, code := range modelResult.WarningCodes {
			documentWarnings[code] = struct{}{}
		}
		if baseURL := clientBaseURL(PlatformPi, input.BaseURL, fact.APIFamily); baseURL != "" {
			baseURLs[baseURL] = struct{}{}
		}
		apiValues[piModelAPI(fact.APIFamily, fact.OpenAIAcceptedFormat)] = struct{}{}
	}

	provider := map[string]any{"name": "Prism"}
	if len(baseURLs) == 1 {
		for url := range baseURLs {
			provider["baseUrl"] = url
		}
	} else if len(baseURLs) > 1 {
		documentWarnings[WarningMixedBaseURLs] = struct{}{}
	}
	if len(apiValues) == 1 {
		for api := range apiValues {
			provider["api"] = api
		}
	}
	if input.IncludeAPIKey {
		provider["apiKey"] = input.APIKey
	}
	provider["models"] = models

	document := map[string]any{
		"providers": map[string]any{
			providerIDOrDefault(input.ProviderID): provider,
		},
	}
	if err := validatePiDocument(document); err != nil {
		return nil, err
	}
	rendered, err := finalizeDocument(PlatformPi, document)
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

func renderPiModel(fact ModelFact, input PiInput) (map[string]any, *ModelRenderResult, error) {
	object := map[string]any{"id": fact.ModelID}
	object["api"] = piModelAPI(fact.APIFamily, fact.OpenAIAcceptedFormat)
	if baseURL := clientBaseURL(PlatformPi, input.BaseURL, fact.APIFamily); baseURL != "" {
		object["baseUrl"] = baseURL
	}

	warnings := map[string]struct{}{}
	candidate := input.Enrichment[fact.ModelConfigID]

	merge, err := MergeKnownMetadata(MergeOptions{
		Prism:     NewMetadataLayer(fact.PrismMetadata),
		ModelsDev: candidate.Metadata,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := validatePiMergedMetadata(merge.Merged); err != nil {
		return nil, nil, err
	}
	for _, code := range MetadataWarningCodes(PlatformPi, fact, merge.Merged) {
		warnings[code] = struct{}{}
	}
	for _, code := range candidate.WarningCodes {
		warnings[code] = struct{}{}
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
	setNumber := func(key, leaf string) {
		value, ok := merge.Merged.Get(leaf)
		if !ok {
			return
		}
		var number json.Number
		if err := json.Unmarshal(value, &number); err != nil {
			return
		}
		object[key] = number
	}
	if value, ok := merge.Merged.Get(MetaName); ok {
		var name string
		if err := json.Unmarshal(value, &name); err == nil {
			if name != "" {
				object["name"] = name
			} else {
				warnings[WarningMetadataIncomplete] = struct{}{}
			}
		}
	}
	setBool("reasoning", MetaReasoning)
	setNumber("contextWindow", MetaContextWindow)
	setNumber("maxTokens", MetaMaxOutputTokens)
	if raw, ok := merge.Merged.Get(MetaModalitiesInput); ok {
		var modalities []string
		if err := json.Unmarshal(raw, &modalities); err == nil {
			filtered := intersectStrings(modalities, []string{"text", "image"})
			object["input"] = filtered
		}
	}

	for _, field := range sortedRawKeys(candidate.DerivedFields) {
		if _, exists := object[field]; exists {
			continue
		}
		if err := checkLockedPath(field, piLockedPaths); err != nil {
			return nil, nil, err
		}
		decoded, err := decodeCanonicalJSON(candidate.DerivedFields[field])
		if err != nil {
			return nil, nil, fmt.Errorf("field %s: %w", field, err)
		}
		object[field] = decoded
	}

	priceTargets := priceSnapshots(fact)
	decision := DecidePriceExport(PlatformPi, priceTargets)
	for _, code := range decision.WarningCodes {
		warnings[code] = struct{}{}
	}
	if decision.Exportable {
		object["cost"] = piCostGroup(priceTargets[0])
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

func validatePiMergedMetadata(merged MetadataLayer) error {
	checks := []struct {
		leaf  string
		check func(any, string) error
	}{
		{MetaName, func(value any, field string) error { return targetString(value, field, false) }},
		{MetaReasoning, func(value any, field string) error { return targetBool(value, field) }},
		{MetaContextWindow, func(value any, field string) error { return targetNumber(value, field) }},
		{MetaMaxOutputTokens, func(value any, field string) error { return targetNumber(value, field) }},
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
				if err := targetString(provider[key], path, false); err != nil {
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
	return targetSchemaError(validatePiEnhancement(manualFields))
}

func validatePiEnhancement(fields map[string]any) error {
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
			if err := targetNumber(value, field); err != nil {
				return err
			}
		case "input":
			if err := targetStringArray(value, field, map[string]struct{}{"text": {}, "image": {}}); err != nil {
				return err
			}
		case "headers":
			if err := targetStringRecord(value, field); err != nil {
				return err
			}
		case "thinkingLevelMap":
			if err := validatePiThinkingLevelMap(value, field); err != nil {
				return err
			}
		case "compat":
			if err := validatePiCompat(value, field); err != nil {
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

func validatePiCompat(value any, field string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return invalidTargetField(field, "must be an object")
	}
	booleanFields := map[string]struct{}{
		"supportsStore": {}, "supportsDeveloperRole": {}, "supportsReasoningEffort": {},
		"supportsUsageInStreaming": {}, "supportsFinishReason": {}, "requiresToolResultName": {},
		"requiresAssistantAfterToolResult": {}, "requiresThinkingAsText": {},
		"requiresReasoningContentOnAssistantMessages": {}, "supportsOpenAIGrammarTools": {},
		"supportsStrictMode": {}, "sendSessionAffinityHeaders": {}, "supportsLongCacheRetention": {},
		"supportsAdditionalTools": {}, "supportsToolSearch": {}, "supportsEagerToolInputStreaming": {},
		"supportsCacheControlOnTools": {}, "supportsTemperature": {}, "forceAdaptiveThinking": {},
		"allowEmptySignature": {}, "supportsStrictTools": {}, "supportsToolReferences": {},
	}
	for _, key := range sortedAnyKeys(object) {
		item := object[key]
		path := field + "." + key
		if _, ok := booleanFields[key]; ok {
			if err := targetBool(item, path); err != nil {
				return err
			}
			continue
		}
		switch key {
		case "maxTokensField":
			if !stringInSet(item, "max_completion_tokens", "max_tokens") {
				return invalidTargetField(path, "has an unsupported value")
			}
		case "thinkingFormat":
			if !stringInSet(item, "openai", "openrouter", "together", "baseten", "deepseek", "zai", "qwen", "chat-template", "qwen-chat-template", "string-thinking", "ant-ling") {
				return invalidTargetField(path, "has an unsupported value")
			}
		case "cacheControlFormat":
			if !stringInSet(item, "anthropic") {
				return invalidTargetField(path, "has an unsupported value")
			}
		case "deferredToolsMode":
			if !stringInSet(item, "kimi") {
				return invalidTargetField(path, "has an unsupported value")
			}
		case "sessionAffinityFormat":
			if !stringInSet(item, "openai", "openai-nosession", "openrouter") {
				return invalidTargetField(path, "has an unsupported value")
			}
		case "chatTemplateKwargs", "chatTemplateArgs", "openRouterRouting", "vercelGatewayRouting":
			if _, ok := item.(map[string]any); !ok {
				return invalidTargetField(path, "must be an object")
			}
		default:
			return invalidTargetField(path, "is not supported by the Pi 0.84.3 compat schema")
		}
	}
	return nil
}

// ValidatePiSourceField validates one safe pi.dev leaf value (name, reasoning,
// input, context_window, max_tokens, thinking_level_map, or compat) against
// the same Pi 0.84.3 schema the renderer enforces. It is the single
// validation entry point shared by RenderPi and the persisted Pi binding's
// override surface, so a stored override can never carry a shape render
// would later reject.
func ValidatePiSourceField(field string, value any) error {
	switch field {
	case "name":
		return targetSchemaError(targetString(value, field, true))
	case "reasoning":
		return targetSchemaError(targetBool(value, field))
	case "input":
		return targetSchemaError(targetStringArray(value, field, map[string]struct{}{"text": {}, "image": {}}))
	case "context_window", "max_tokens":
		return targetSchemaError(targetNumber(value, field))
	case "thinking_level_map":
		return targetSchemaError(validatePiThinkingLevelMap(value, field))
	case "compat":
		return targetSchemaError(validatePiCompat(value, field))
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

func piCostGroup(reference TargetPriceSnapshot) map[string]any {
	switch reference.Kind {
	case pricingkind.Tiered:
		tiers := []map[string]any{
			{
				"inputTokensAbove": *reference.TierThreshold,
				"input":            decimal(reference.AboveCard.InputPrice),
				"output":           decimal(reference.AboveCard.OutputPrice),
				"cacheRead":        decimal(derefOrZero(reference.AboveCard.CachedInputPrice)),
				"cacheWrite":       decimal(derefOrZero(reference.AboveCard.CacheCreationPrice)),
			},
		}
		return map[string]any{
			"input":      decimal(reference.BaseCard.InputPrice),
			"output":     decimal(reference.BaseCard.OutputPrice),
			"cacheRead":  decimal(derefOrZero(reference.BaseCard.CachedInputPrice)),
			"cacheWrite": decimal(derefOrZero(reference.BaseCard.CacheCreationPrice)),
			"tiers":      tiers,
		}
	default:
		card := reference.Card
		return map[string]any{
			"input":      decimal(card.InputPrice),
			"output":     decimal(card.OutputPrice),
			"cacheRead":  decimal(derefOrZero(card.CachedInputPrice)),
			"cacheWrite": decimal(derefOrZero(card.CacheCreationPrice)),
		}
	}
}

func derefOrZero(value *string) string {
	if value == nil {
		return "0"
	}
	return *value
}

func priceSnapshots(fact ModelFact) []TargetPriceSnapshot {
	targets := make([]TargetPriceSnapshot, 0, len(fact.Targets))
	for _, target := range fact.Targets {
		if target.Pricing == nil {
			targets = append(targets, TargetPriceSnapshot{TerminalTargetID: target.TerminalTargetID})
			continue
		}
		snapshot := *target.Pricing
		snapshot.TerminalTargetID = target.TerminalTargetID
		targets = append(targets, snapshot)
	}
	sort.SliceStable(targets, func(i, j int) bool { return targets[i].TerminalTargetID < targets[j].TerminalTargetID })
	return targets
}

func providerIDOrDefault(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return PiProviderID
	}
	return trimmed
}

func clientBaseURL(platform Platform, origin string, apiFamily string) string {
	origin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
	if origin == "" {
		return ""
	}
	switch strings.TrimSpace(apiFamily) {
	case "anthropic":
		return origin
	case "gemini":
		return origin + "/v1beta"
	default:
		return origin + "/v1"
	}
}

func intersectStrings(values []string, allowed []string) []string {
	set := make(map[string]struct{}, len(allowed))
	for _, item := range allowed {
		set[item] = struct{}{}
	}
	out := make([]string, 0, len(values))
	for _, item := range values {
		if _, ok := set[item]; ok {
			out = append(out, item)
		}
	}
	return out
}

func sortedWarningSet(set map[string]struct{}) []string {
	codes := make([]string, 0, len(set))
	for code := range set {
		codes = append(codes, code)
	}
	return sortWarningCodes(codes)
}

func sortedRawKeys(values map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
