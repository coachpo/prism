package modelexport

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
	"github.com/coachpo/prism/backend/internal/domain/pricingkind"
)

const (
	PiProviderID    = "prism"
	PiFileName      = "prism-pi-models.json"
	piAPIOpenAIChat = pidev.APIOpenAICompletions
	piAPIResponses  = pidev.APIOpenAIResponses
	piAPIAnthropic  = pidev.APIAnthropicMessages
	piAPIGemini     = pidev.APIGoogleGenerative
)

var piLockedPaths = []string{"id", "api", "baseUrl", "cost"}

// PiAPIForModel maps Prism's authoritative API family and accepted OpenAI
// operation shape to the final Pi API literal used by matching and rendering.
func PiAPIForModel(apiFamily string, acceptedFormat *string) string {
	switch strings.TrimSpace(apiFamily) {
	case "openai":
		switch strings.TrimSpace(optionalString(acceptedFormat)) {
		case "chat_completions_only":
			return piAPIOpenAIChat
		case "responses_only", "dual_native":
			return piAPIResponses
		default:
			return ""
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
	Facts         SourceFacts
	Selection     []int
	BaseURL       string
	ProviderID    string
	IncludeAPIKey bool
	APIKey        string
}

// PiTemplate is the effective API-sanitized metadata frozen with a persisted
// Pi source binding. DroppedFields records unsafe or unsupported source paths
// in stable lexical order; those paths are evidence only and never render.
type PiTemplate struct {
	Metadata      MetadataLayer
	DerivedFields map[string]json.RawMessage
	DroppedFields []string
}

func (c PiTemplate) MarshalJSON() ([]byte, error) {
	type wire struct {
		Metadata      map[string]json.RawMessage `json:"metadata"`
		Derived       map[string]json.RawMessage `json:"derived,omitempty"`
		DroppedFields []string                   `json:"dropped_fields,omitempty"`
	}
	dropped := append([]string(nil), c.DroppedFields...)
	sort.Strings(dropped)
	return json.Marshal(wire{
		Metadata: c.Metadata.Values(), Derived: c.DerivedFields,
		DroppedFields: dedupeSortedStrings(dropped),
	})
}

func RenderPi(input PiInput) (*RenderResult, error) {
	if input.IncludeAPIKey && strings.TrimSpace(input.APIKey) == "" {
		return nil, &ErrTargetSchema{Field: "apiKey", Reason: "must be non-empty when included"}
	}
	selection, err := NormalizeSelection(input.Selection, input.Facts)
	if err != nil {
		return nil, err
	}
	byID := make(map[int]ModelFact, len(input.Facts.Models))
	for _, fact := range input.Facts.Models {
		byID[fact.ModelConfigID] = fact
	}
	models := make([]any, 0, len(selection))
	modelResults := make([]ModelRenderResult, 0, len(selection))
	baseURLs := map[string]struct{}{}
	apiValues := map[string]struct{}{}
	documentWarnings := map[string]struct{}{}

	for _, id := range selection {
		fact, ok := byID[id]
		if !ok {
			return nil, &ErrUnselectableModel{ModelConfigID: id, Reason: "not_found_in_default_profile"}
		}
		modelObject, modelResult, err := renderPiModel(fact, input)
		if err != nil {
			return nil, err
		}
		models = append(models, modelObject)
		modelResults = append(modelResults, *modelResult)
		for _, code := range modelResult.WarningCodes {
			documentWarnings[code] = struct{}{}
		}
		if baseURL := clientBaseURL(input.BaseURL, fact.APIFamily); baseURL != "" {
			baseURLs[baseURL] = struct{}{}
		}
		apiValues[PiAPIForModel(fact.APIFamily, fact.OpenAIAcceptedFormat)] = struct{}{}
	}

	provider := map[string]any{"name": "Prism"}
	if len(baseURLs) == 1 {
		for url := range baseURLs {
			provider["baseUrl"] = url
		}
		// One shared client URL lives on the provider definition only: Pi
		// models inherit it when they omit baseUrl, so repeating it per
		// model is redundant noise. Per-model entries are emitted solely
		// when families disagree and the provider cannot carry a single URL.
		for _, model := range models {
			delete(model.(map[string]any), "baseUrl")
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
	rendered, err := finalizeDocument(document)
	if err != nil {
		return nil, err
	}
	warnings := make([]string, 0, len(documentWarnings))
	for code := range documentWarnings {
		warnings = append(warnings, code)
	}
	rendered.Warnings = sortWarningCodes(warnings)
	rendered.ModelResults = modelResults
	return rendered, nil
}

func renderPiModel(fact ModelFact, input PiInput) (map[string]any, *ModelRenderResult, error) {
	object := map[string]any{"id": fact.ModelID}
	object["api"] = PiAPIForModel(fact.APIFamily, fact.OpenAIAcceptedFormat)
	if baseURL := clientBaseURL(input.BaseURL, fact.APIFamily); baseURL != "" {
		object["baseUrl"] = baseURL
	}

	warnings := map[string]struct{}{}
	candidate := fact.PiTemplate

	merge, err := MergeKnownMetadata(MergeOptions{
		Prism: NewMetadataLayer(fact.PrismMetadata),
		Pi:    candidate.Metadata,
	})
	if err != nil {
		return nil, nil, err
	}
	if err := validatePiMergedMetadata(merge.Merged); err != nil {
		return nil, nil, err
	}
	for _, code := range MetadataWarningCodes(merge.Merged) {
		warnings[code] = struct{}{}
	}
	if len(candidate.DroppedFields) > 0 {
		warnings[WarningPiSourceFieldsDropped] = struct{}{}
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
	decision := DecidePriceExport(priceTargets)
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

func clientBaseURL(origin string, apiFamily string) string {
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

func dedupeSortedStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
