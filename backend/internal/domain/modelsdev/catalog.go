// Package modelsdev is the HTTP-neutral domain boundary for the fixed
// official models.dev catalog. It owns the restricted catalog client
// (HTTPS-only, same-origin redirects, bounded body, ETag/304 reuse,
// single-flight), catalog schema validation, unique-exact offering matching,
// and the fail-closed price-plan mapping into Prism pricing template shapes.
//
// Catalog data is USD per million tokens from https://models.dev/api.json and
// stays management-only metadata: nothing in this package may participate in
// api_family compatibility truth, capability gating, or routing.
package modelsdev

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Model status enum values accepted by the schema.
const (
	StatusAlpha      = "alpha"
	StatusBeta       = "beta"
	StatusDeprecated = "deprecated"
)

// Limit carries the optional context/input/output token ceilings. Absent
// fields stay nil so "unknown" never renders as zero.
type Limit struct {
	Context *int64
	Input   *int64
	Output  *int64
}

// TierPrices is a flat price row (base cost row or one tier row). Missing
// optional components stay nil; explicit zeros are preserved as "0".
type TierPrices struct {
	Input         string
	Output        string
	CachedInput   *string // cache_read
	CacheCreation *string // cache_write
	Reasoning     *string
	AudioEvidence bool // input_audio/output_audio present on this row
}

// CostTier is one entry of cost.tiers with its discriminator.
type CostTier struct {
	Type   string // e.g. "context"
	Size   int64  // raw threshold size in tokens
	Prices TierPrices
}

// Cost is the parsed price block of one model.
type Cost struct {
	Base  TierPrices
	Tiers []CostTier
	// LegacyContextOver200k is the pre-tiers long-context evidence. It is
	// only ever accepted alongside an explicit single context tier whose
	// prices match exactly.
	LegacyContextOver200k    *TierPrices
	HasLegacyContextOver200k bool
}

// Model is one validated catalog model entry. Coordinates are ProviderID +
// ModelID (the provider-local key).
type Model struct {
	ProviderID string
	ModelID    string

	Name             string
	Description      *string
	Family           *string
	Attachment       *bool
	Reasoning        *bool
	ToolCall         *bool
	StructuredOutput *bool
	Temperature      *bool
	Knowledge        *string
	ReleaseDate      *string
	LastUpdated      *string
	ModalitiesInput  []string
	ModalitiesOutput []string
	OpenWeights      *bool
	Status           *string
	Limit            Limit
	Cost             *Cost
}

// Provider is one validated catalog provider entry.
type Provider struct {
	ID     string
	Name   string
	Models map[string]*Model
}

// Catalog is a validated snapshot of the whole models.dev catalog.
type Catalog struct {
	ETag      string
	FetchedAt time.Time
	Providers map[string]*Provider
}

// Offering identifies one provider+model coordinate inside the catalog.
type Offering struct {
	ProviderID string
	ModelID    string
}

// schemaError marks a catalog that does not conform to the expected
// models.dev shape. Fetch fails closed on it.
type schemaError struct {
	detail string
}

func (err *schemaError) Error() string {
	return fmt.Sprintf("models.dev catalog schema violation: %s", err.detail)
}

func schemaf(format string, args ...any) error {
	return &schemaError{detail: fmt.Sprintf(format, args...)}
}

// parseCatalog decodes and validates a full catalog payload that was already
// read verbatim off the wire. Numbers are decoded through json.Number so
// price literals survive losslessly.
func parseCatalog(payload []byte) (map[string]*Provider, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	raw := map[string]json.RawMessage{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, schemaf("top-level document must be a JSON object: %v", err)
	}
	if len(raw) == 0 {
		return nil, schemaf("catalog must contain at least one provider")
	}
	providers := make(map[string]*Provider, len(raw))
	for providerID, providerRaw := range raw {
		if strings.TrimSpace(providerID) == "" {
			return nil, schemaf("provider id must not be empty")
		}
		provider, err := parseProvider(providerID, providerRaw)
		if err != nil {
			return nil, err
		}
		providers[providerID] = provider
	}
	return providers, nil
}

func parseProvider(providerID string, raw json.RawMessage) (*Provider, error) {
	body := map[string]json.RawMessage{}
	if err := jsonUnmarshalUseNumber(raw, &body); err != nil {
		return nil, schemaf("provider %q must be an object: %v", providerID, err)
	}
	provider := &Provider{ID: providerID, Models: map[string]*Model{}}
	if nameRaw, ok := body["name"]; ok {
		var name string
		if err := json.Unmarshal(nameRaw, &name); err != nil {
			return nil, schemaf("provider %q name must be a string: %v", providerID, err)
		}
		provider.Name = name
	}
	modelsRaw, ok := body["models"]
	if !ok {
		// Providers without a models object carry no offerings; tolerate but
		// keep them addressable for candidate search metadata.
		return provider, nil
	}
	models := map[string]json.RawMessage{}
	if err := json.Unmarshal(modelsRaw, &models); err != nil {
		return nil, schemaf("provider %q models must be an object: %v", providerID, err)
	}
	for modelID, modelRaw := range models {
		if strings.TrimSpace(modelID) == "" {
			return nil, schemaf("provider %q contains an empty model id", providerID)
		}
		model, err := parseModel(providerID, modelID, modelRaw)
		if err != nil {
			return nil, err
		}
		provider.Models[modelID] = model
	}
	return provider, nil
}

func parseModel(providerID, modelID string, raw json.RawMessage) (*Model, error) {
	label := providerID + "/" + modelID
	body := map[string]json.RawMessage{}
	if err := jsonUnmarshalUseNumber(raw, &body); err != nil {
		return nil, schemaf("model %q must be an object: %v", label, err)
	}
	model := &Model{ProviderID: providerID, ModelID: modelID}
	// The inner id must repeat the provider-local key when present; a
	// mismatch means the catalog cannot be addressed unambiguously.
	if idRaw, ok := body["id"]; ok && string(idRaw) != "null" {
		var innerID string
		if err := json.Unmarshal(idRaw, &innerID); err != nil {
			return nil, schemaf("model %q id must be a string: %v", label, err)
		}
		if innerID != modelID {
			return nil, schemaf("model %q id %q does not match its catalog key", label, innerID)
		}
	}
	if nameRaw, ok := body["name"]; ok {
		if err := json.Unmarshal(nameRaw, &model.Name); err != nil {
			return nil, schemaf("model %q name must be a string: %v", label, err)
		}
	}
	var err error
	if model.Description, err = optionalString(body, "description", label); err != nil {
		return nil, err
	}
	if model.Family, err = optionalString(body, "family", label); err != nil {
		return nil, err
	}
	if model.Attachment, err = optionalBool(body, "attachment", label); err != nil {
		return nil, err
	}
	if model.Reasoning, err = optionalBool(body, "reasoning", label); err != nil {
		return nil, err
	}
	if model.ToolCall, err = optionalBool(body, "tool_call", label); err != nil {
		return nil, err
	}
	if model.StructuredOutput, err = optionalBool(body, "structured_output", label); err != nil {
		return nil, err
	}
	if model.Temperature, err = optionalBool(body, "temperature", label); err != nil {
		return nil, err
	}
	if model.Knowledge, err = optionalDateString(body, "knowledge", label); err != nil {
		return nil, err
	}
	if model.ReleaseDate, err = optionalDateString(body, "release_date", label); err != nil {
		return nil, err
	}
	if model.LastUpdated, err = optionalDateString(body, "last_updated", label); err != nil {
		return nil, err
	}
	if model.ModalitiesInput, err = optionalModalityList(body, "modalities", "input", label); err != nil {
		return nil, err
	}
	if model.ModalitiesOutput, err = optionalModalityList(body, "modalities", "output", label); err != nil {
		return nil, err
	}
	if model.OpenWeights, err = optionalBool(body, "open_weights", label); err != nil {
		return nil, err
	}
	if model.Status, err = optionalStatus(body, label); err != nil {
		return nil, err
	}
	if model.Limit, err = parseLimit(body, label); err != nil {
		return nil, err
	}
	if costRaw, ok := body["cost"]; ok && string(costRaw) != "null" {
		cost, costErr := parseCost(costRaw, label)
		if costErr != nil {
			return nil, costErr
		}
		model.Cost = cost
	}
	return model, nil
}

func optionalString(body map[string]json.RawMessage, field, label string) (*string, error) {
	raw, ok := body[field]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, schemaf("model %s %q must be a string: %v", label, field, err)
	}
	return &value, nil
}

func optionalBool(body map[string]json.RawMessage, field, label string) (*bool, error) {
	raw, ok := body[field]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, schemaf("model %s %q must be a boolean: %v", label, field, err)
	}
	return &value, nil
}

func optionalDateString(body map[string]json.RawMessage, field, label string) (*string, error) {
	value, err := optionalString(body, field, label)
	if err != nil || value == nil {
		return value, err
	}
	trimmed := strings.TrimSpace(*value)
	if !isCatalogDate(trimmed) {
		return nil, schemaf("model %s %q must match YYYY-MM or YYYY-MM-DD, got %q", label, field, trimmed)
	}
	return &trimmed, nil
}

func isCatalogDate(value string) bool {
	parts := strings.Split(value, "-")
	if len(parts) == 2 {
		return len(parts[0]) == 4 && allDigits(parts[0]) && len(parts[1]) == 2 && allDigits(parts[1])
	}
	if len(parts) == 3 {
		return len(parts[0]) == 4 && allDigits(parts[0]) && len(parts[1]) == 2 && allDigits(parts[1]) && len(parts[2]) == 2 && allDigits(parts[2])
	}
	return false
}

func allDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var knownModalities = map[string]struct{}{
	"text": {}, "image": {}, "audio": {}, "video": {}, "pdf": {},
}

func optionalModalityList(body map[string]json.RawMessage, field, side, label string) ([]string, error) {
	modalitiesRaw, ok := body[field]
	if !ok || string(modalitiesRaw) == "null" {
		return nil, nil
	}
	modalityBody := map[string]json.RawMessage{}
	if err := json.Unmarshal(modalitiesRaw, &modalityBody); err != nil {
		return nil, schemaf("model %s %q must be an object: %v", label, field, err)
	}
	sideRaw, ok := modalityBody[side]
	if !ok || string(sideRaw) == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(sideRaw, &values); err != nil {
		return nil, schemaf("model %s modalities.%s must be an array of strings: %v", label, side, err)
	}
	for _, value := range values {
		if _, known := knownModalities[value]; !known {
			return nil, schemaf("model %s modalities.%s carries unknown modality %q", label, side, value)
		}
	}
	return values, nil
}

func optionalStatus(body map[string]json.RawMessage, label string) (*string, error) {
	value, err := optionalString(body, "status", label)
	if err != nil || value == nil {
		return value, err
	}
	switch *value {
	case StatusAlpha, StatusBeta, StatusDeprecated:
		return value, nil
	default:
		return nil, schemaf("model %s status %q is outside alpha|beta|deprecated", label, *value)
	}
}

func parseLimit(body map[string]json.RawMessage, label string) (Limit, error) {
	limitRaw, ok := body["limit"]
	if !ok || string(limitRaw) == "null" {
		return Limit{}, nil
	}
	limitBody := map[string]json.RawMessage{}
	if err := jsonUnmarshalUseNumber(limitRaw, &limitBody); err != nil {
		return Limit{}, schemaf("model %s limit must be an object: %v", label, err)
	}
	context, err := optionalTokenCount(limitBody, "context", label)
	if err != nil {
		return Limit{}, err
	}
	input, err := optionalTokenCount(limitBody, "input", label)
	if err != nil {
		return Limit{}, err
	}
	output, err := optionalTokenCount(limitBody, "output", label)
	if err != nil {
		return Limit{}, err
	}
	return Limit{Context: context, Input: input, Output: output}, nil
}

func optionalTokenCount(body map[string]json.RawMessage, field, label string) (*int64, error) {
	raw, ok := body[field]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, schemaf("model %s limit.%s must be a number: %v", label, field, err)
	}
	parsed, err := parseCatalogInteger(number, "limit."+field, label)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

// parseCatalogInteger converts a JSON number into an int64 without going
// through float64, rejecting fractions and exponent notation outright.
func parseCatalogInteger(number json.Number, field, label string) (int64, error) {
	text := number.String()
	if text == "" {
		return 0, schemaf("model %s %s is empty", label, field)
	}
	if strings.ContainsAny(text, ".eE") {
		return 0, schemaf("model %s %s must be a whole number, got %s", label, field, text)
	}
	parsed, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, schemaf("model %s %s is out of range: %s", label, field, text)
	}
	return parsed, nil
}

func parseCost(raw json.RawMessage, label string) (*Cost, error) {
	body := map[string]json.RawMessage{}
	if err := jsonUnmarshalUseNumber(raw, &body); err != nil {
		return nil, schemaf("model %s cost must be an object: %v", label, err)
	}
	base, err := parseTierPrices(body, label, "cost")
	if err != nil {
		return nil, err
	}
	cost := &Cost{Base: base}
	if tiersRaw, ok := body["tiers"]; ok && string(tiersRaw) != "null" {
		var tiersBody []json.RawMessage
		if err := json.Unmarshal(tiersRaw, &tiersBody); err != nil {
			return nil, schemaf("model %s cost.tiers must be an array: %v", label, err)
		}
		for index, tierRaw := range tiersBody {
			tier, tierErr := parseCostTier(tierRaw, label, index)
			if tierErr != nil {
				return nil, tierErr
			}
			cost.Tiers = append(cost.Tiers, *tier)
		}
	}
	if legacyRaw, ok := body["context_over_200k"]; ok && string(legacyRaw) != "null" {
		legacyBody := map[string]json.RawMessage{}
		if err := jsonUnmarshalUseNumber(legacyRaw, &legacyBody); err != nil {
			return nil, schemaf("model %s cost.context_over_200k must be an object: %v", label, err)
		}
		legacy, err := parseTierPrices(legacyBody, label, "cost.context_over_200k")
		if err != nil {
			return nil, err
		}
		cost.LegacyContextOver200k = &legacy
		cost.HasLegacyContextOver200k = true
	}
	return cost, nil
}

func parseCostTier(raw json.RawMessage, label string, index int) (*CostTier, error) {
	body := map[string]json.RawMessage{}
	if err := jsonUnmarshalUseNumber(raw, &body); err != nil {
		return nil, schemaf("model %s cost.tiers[%d] must be an object: %v", label, index, err)
	}
	// The published shape nests the discriminator under "tier"; tolerate the
	// flat variant so a future schema flattening cannot strand the mapping.
	discriminator := body
	if tierRaw, ok := body["tier"]; ok && string(tierRaw) != "null" {
		nested := map[string]json.RawMessage{}
		if err := jsonUnmarshalUseNumber(tierRaw, &nested); err != nil {
			return nil, schemaf("model %s cost.tiers[%d].tier must be an object: %v", label, index, err)
		}
		discriminator = nested
	}
	tierLabel := label + " cost.tiers[" + strconv.Itoa(index) + "]"
	tierType, err := optionalString(discriminator, "type", tierLabel)
	if err != nil {
		return nil, err
	}
	sizeRaw, ok := discriminator["size"]
	if !ok || string(sizeRaw) == "null" {
		return nil, schemaf("model %s cost.tiers[%d].size is required", label, index)
	}
	var sizeNumber json.Number
	if err := json.Unmarshal(sizeRaw, &sizeNumber); err != nil {
		return nil, schemaf("model %s cost.tiers[%d].size must be a number: %v", label, index, err)
	}
	size, err := parseCatalogInteger(sizeNumber, "cost.tiers.size", label)
	if err != nil {
		return nil, err
	}
	prices, err := parseTierPrices(body, label, "cost.tiers["+strconv.Itoa(index)+"]")
	if err != nil {
		return nil, err
	}
	tierTypeValue := ""
	if tierType != nil {
		tierTypeValue = *tierType
	}
	return &CostTier{Type: tierTypeValue, Size: size, Prices: prices}, nil
}

func parseTierPrices(body map[string]json.RawMessage, label, prefix string) (TierPrices, error) {
	prices := TierPrices{}
	input, err := optionalPrice(body, "input", label, prefix)
	if err != nil {
		return prices, err
	}
	if input == nil {
		return prices, nil
	}
	prices.Input = *input
	output, err := optionalPrice(body, "output", label, prefix)
	if err != nil {
		return prices, err
	}
	if output == nil {
		// input without output is a broken price row; both are mandatory
		// whenever any price is expressed.
		return prices, schemaf("model %s %s.output is required when %s.input is present", label, prefix, prefix)
	}
	prices.Output = *output
	cacheRead, err := optionalPricePtr(body, "cache_read", label, prefix)
	if err != nil {
		return prices, err
	}
	prices.CachedInput = cacheRead
	cacheWrite, err := optionalPricePtr(body, "cache_write", label, prefix)
	if err != nil {
		return prices, err
	}
	prices.CacheCreation = cacheWrite
	reasoning, err := optionalPricePtr(body, "reasoning", label, prefix)
	if err != nil {
		return prices, err
	}
	prices.Reasoning = reasoning
	audioFields := []string{"input_audio", "output_audio"}
	for _, field := range audioFields {
		if _, ok := body[field]; ok && string(body[field]) != "null" {
			prices.AudioEvidence = true
		}
	}
	return prices, nil
}

func optionalPrice(body map[string]json.RawMessage, field, label, prefix string) (*string, error) {
	return optionalPricePtr(body, field, label, prefix)
}

func optionalPricePtr(body map[string]json.RawMessage, field, label, prefix string) (*string, error) {
	raw, ok := body[field]
	if !ok || string(raw) == "null" {
		return nil, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil, schemaf("model %s %s.%s must be a number: %v", label, prefix, field, err)
	}
	canonical, err := CanonicalPrice(number.String())
	if err != nil {
		return nil, schemaf("model %s %s.%s is not a plain non-negative decimal: %v", label, prefix, field, err)
	}
	return &canonical, nil
}

// CanonicalPrice normalizes a catalog decimal literal into the Prism pricing
// canonical form (^\\d+(\\.\\d+)?$ without insignificant zeros). Exponent
// notation and signs are rejected so the wire literal survives losslessly.
func CanonicalPrice(literal string) (string, error) {
	text := strings.TrimSpace(literal)
	if text == "" || len(text) > 20 {
		return "", fmt.Errorf("price %q must be 1..20 plain digits", literal)
	}
	integral, fractional := text, ""
	if cutIntegral, cutFractional, found := strings.Cut(text, "."); found {
		integral, fractional = cutIntegral, cutFractional
	}
	if integral == "" {
		integral = "0"
	}
	for _, r := range integral {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("price %q must be a non-negative decimal", literal)
		}
	}
	for _, r := range fractional {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("price %q must be a non-negative decimal", literal)
		}
	}
	integral = strings.TrimLeft(integral, "0")
	if integral == "" {
		integral = "0"
	}
	fractional = strings.TrimRight(fractional, "0")
	canonical := integral
	if fractional != "" {
		canonical = canonical + "." + fractional
	}
	if len(canonical) > 20 {
		return "", fmt.Errorf("price %q exceeds the canonical length budget", literal)
	}
	return canonical, nil
}

func jsonUnmarshalUseNumber(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

// SortedProviderIDs returns provider ids in deterministic order.
func (c *Catalog) SortedProviderIDs() []string {
	if c == nil {
		return nil
	}
	ids := make([]string, 0, len(c.Providers))
	for id := range c.Providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Find locates one validated offering by coordinates.
func (c *Catalog) Find(providerID, modelID string) (*Model, bool) {
	if c == nil {
		return nil, false
	}
	provider, ok := c.Providers[providerID]
	if !ok {
		return nil, false
	}
	model, ok := provider.Models[modelID]
	if !ok {
		return nil, false
	}
	return model, true
}
