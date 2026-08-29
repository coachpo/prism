package pidev

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PiTargetVersion is the exact Pi version this export pins.
const PiTargetVersion = "0.84.3"

// Model is one validated pi.dev catalog entry. This is the typed projection
// used by Prism's Pi-only export: only safe fields survive, all other
// provider-specific fields (cost, headers, samplingParams, fallback/routing)
// are parsed for validation then dropped and never used to override Prism truth.
type Model struct {
	ProviderID string
	ModelID    string // exact id from catalog, case-sensitive

	Name             *string
	API              string // e.g. "openai-completions", "openai-responses"
	Provider         string // provider field inside entry, must equal outer provider_id
	BaseURL          *string
	Reasoning        *bool
	Input            []string // modalities input
	ContextWindow    *int64
	MaxTokens        *int64
	ThinkingLevelMap map[string]*string // off/minimal/low/medium/high/xhigh/max -> string|null
	Compat           map[string]any

	// Raw cost is retained for schema validation but never emitted into
	// Prism output. Pricing truth stays with Prism templates.
	RawCost json.RawMessage

	// Ignored fields are validated then dropped: headers, samplingParams,
	// fallback/routing etc. We keep them only to ensure they don't leak
	// into output.
}

// Provider is one validated pi.dev provider shard.
type Provider struct {
	ID     string
	Models map[string]*Model
}

// Catalog is a validated snapshot of the pi.dev directory.
type Catalog struct {
	ETag           string
	Revision       string // X-Pi-Model-Catalog-Revision (sha256-...)
	MinimumVersion string // X-Pi-Model-Catalog-Minimum-Version
	LastModified   string
	FetchedAt      time.Time
	CheckedAt      time.Time
	Providers      map[string]*Provider
}

// Revision returns the catalog revision (X-Pi-Model-Catalog-Revision) or ETag fallback.
func (c *Catalog) RevisionString() string {
	if c == nil {
		return ""
	}
	if c.Revision != "" {
		return c.Revision
	}
	return c.ETag
}

// Find locates one entry by exact provider_id + model_id.
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

// Candidates returns all entries with exact model_id == targetModelID (case-sensitive)
// and api == expectedAPI. No fuzzy, slug, contains, or name matching.
func (c *Catalog) Candidates(targetModelID string, expectedAPI string) []*Model {
	if c == nil || targetModelID == "" || expectedAPI == "" {
		return nil
	}
	var out []*Model
	for _, provider := range c.Providers {
		if model, ok := provider.Models[targetModelID]; ok {
			if model.API == expectedAPI {
				out = append(out, model)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ProviderID != out[j].ProviderID {
			return out[i].ProviderID < out[j].ProviderID
		}
		return out[i].ModelID < out[j].ModelID
	})
	return out
}

// HasExactID reports whether any provider has an entry with exact model_id
// regardless of API. Used to distinguish 未收录 vs API不兼容.
func (c *Catalog) HasExactID(modelID string) bool {
	if c == nil || modelID == "" {
		return false
	}
	for _, provider := range c.Providers {
		if _, ok := provider.Models[modelID]; ok {
			return true
		}
	}
	return false
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

// --- validation helpers ---

func parseCatalog(payload []byte) (map[string]*Provider, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	raw := map[string]json.RawMessage{}
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("top-level document must be a JSON object: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("catalog must contain at least one provider")
	}
	providers := make(map[string]*Provider, len(raw))
	for providerID, providerRaw := range raw {
		if strings.TrimSpace(providerID) == "" {
			return nil, fmt.Errorf("provider id must not be empty")
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
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return &Provider{ID: providerID, Models: map[string]*Model{}}, nil
	}
	mapping := map[string]json.RawMessage{}
	if err := jsonUnmarshalUseNumber(raw, &mapping); err != nil {
		return nil, fmt.Errorf("provider %q must be an object: %w", providerID, err)
	}
	provider := &Provider{ID: providerID, Models: map[string]*Model{}}
	for modelID, modelRaw := range mapping {
		if strings.TrimSpace(modelID) == "" {
			return nil, fmt.Errorf("provider %q contains an empty model id", providerID)
		}
		model, err := parseModel(providerID, modelID, modelRaw)
		if err != nil {
			return nil, err
		}
		provider.Models[modelID] = model
	}
	return provider, nil
}

func parseModel(outerProviderID, modelID string, raw json.RawMessage) (*Model, error) {
	body := map[string]json.RawMessage{}
	if err := jsonUnmarshalUseNumber(raw, &body); err != nil {
		return nil, fmt.Errorf("model %q/%q must be an object: %w", outerProviderID, modelID, err)
	}
	// id is required and must equal map key exactly (case-sensitive)
	idRaw, ok := body["id"]
	if !ok || string(idRaw) == "null" {
		return nil, fmt.Errorf("model %q/%q missing required id", outerProviderID, modelID)
	}
	var innerID string
	if err := json.Unmarshal(idRaw, &innerID); err != nil {
		return nil, fmt.Errorf("model %q/%q id must be a string: %w", outerProviderID, modelID, err)
	}
	if innerID != modelID {
		return nil, fmt.Errorf("model %q/%q id %q does not match catalog key %q", outerProviderID, modelID, innerID, modelID)
	}
	// api is required, non-empty string
	apiRaw, ok := body["api"]
	if !ok || string(apiRaw) == "null" {
		return nil, fmt.Errorf("model %q/%q missing required api", outerProviderID, modelID)
	}
	var api string
	if err := json.Unmarshal(apiRaw, &api); err != nil || strings.TrimSpace(api) == "" {
		return nil, fmt.Errorf("model %q/%q api must be a non-empty string", outerProviderID, modelID)
	}
	// provider inside entry must equal outer provider_id
	providerRaw, ok := body["provider"]
	if !ok || string(providerRaw) == "null" {
		return nil, fmt.Errorf("model %q/%q missing required provider", outerProviderID, modelID)
	}
	var innerProvider string
	if err := json.Unmarshal(providerRaw, &innerProvider); err != nil || innerProvider != outerProviderID {
		return nil, fmt.Errorf("model %q/%q provider %q does not match outer provider %q", outerProviderID, modelID, innerProvider, outerProviderID)
	}
	model := &Model{
		ProviderID: outerProviderID,
		ModelID:    modelID,
		API:        api,
		Provider:   innerProvider,
	}
	// optional name
	if nameRaw, ok := body["name"]; ok && string(nameRaw) != "null" {
		var name string
		if err := json.Unmarshal(nameRaw, &name); err != nil {
			return nil, fmt.Errorf("model %q/%q name must be a string", outerProviderID, modelID)
		}
		model.Name = &name
	}
	// baseUrl optional
	if baseURLRaw, ok := body["baseUrl"]; ok && string(baseURLRaw) != "null" {
		var baseURL string
		if err := json.Unmarshal(baseURLRaw, &baseURL); err != nil {
			return nil, fmt.Errorf("model %q/%q baseUrl must be a string", outerProviderID, modelID)
		}
		model.BaseURL = &baseURL
	}
	// reasoning optional bool
	if reasoningRaw, ok := body["reasoning"]; ok && string(reasoningRaw) != "null" {
		var reasoning bool
		if err := json.Unmarshal(reasoningRaw, &reasoning); err != nil {
			return nil, fmt.Errorf("model %q/%q reasoning must be a boolean", outerProviderID, modelID)
		}
		model.Reasoning = &reasoning
	}
	// input optional array of text|image
	if inputRaw, ok := body["input"]; ok && string(inputRaw) != "null" {
		var input []string
		if err := json.Unmarshal(inputRaw, &input); err != nil {
			return nil, fmt.Errorf("model %q/%q input must be an array", outerProviderID, modelID)
		}
		for _, v := range input {
			if v != "text" && v != "image" {
				return nil, fmt.Errorf("model %q/%q input contains unsupported value %q", outerProviderID, modelID, v)
			}
		}
		model.Input = input
	}
	// contextWindow optional number
	if cwRaw, ok := body["contextWindow"]; ok && string(cwRaw) != "null" {
		var cwNumber json.Number
		if err := json.Unmarshal(cwRaw, &cwNumber); err != nil {
			return nil, fmt.Errorf("model %q/%q contextWindow must be a number", outerProviderID, modelID)
		}
		val, err := parsePositiveInt(cwNumber.String())
		if err != nil {
			return nil, fmt.Errorf("model %q/%q contextWindow invalid: %w", outerProviderID, modelID, err)
		}
		model.ContextWindow = &val
	}
	// maxTokens optional
	if mtRaw, ok := body["maxTokens"]; ok && string(mtRaw) != "null" {
		var mtNumber json.Number
		if err := json.Unmarshal(mtRaw, &mtNumber); err != nil {
			return nil, fmt.Errorf("model %q/%q maxTokens must be a number", outerProviderID, modelID)
		}
		val, err := parsePositiveInt(mtNumber.String())
		if err != nil {
			return nil, fmt.Errorf("model %q/%q maxTokens invalid: %w", outerProviderID, modelID, err)
		}
		model.MaxTokens = &val
	}
	// thinkingLevelMap optional
	if tlmRaw, ok := body["thinkingLevelMap"]; ok && string(tlmRaw) != "null" {
		var tlm map[string]*string
		if err := json.Unmarshal(tlmRaw, &tlm); err != nil {
			return nil, fmt.Errorf("model %q/%q thinkingLevelMap must be an object", outerProviderID, modelID)
		}
		allowed := map[string]struct{}{"off": {}, "minimal": {}, "low": {}, "medium": {}, "high": {}, "xhigh": {}, "max": {}}
		for k := range tlm {
			if _, ok := allowed[k]; !ok {
				return nil, fmt.Errorf("model %q/%q thinkingLevelMap contains unsupported key %q", outerProviderID, modelID, k)
			}
		}
		model.ThinkingLevelMap = tlm
	}
	// compat optional
	if compatRaw, ok := body["compat"]; ok && string(compatRaw) != "null" {
		var compat map[string]any
		if err := json.Unmarshal(compatRaw, &compat); err != nil {
			return nil, fmt.Errorf("model %q/%q compat must be an object", outerProviderID, modelID)
		}
		model.Compat = compat
	}
	if costRaw, ok := body["cost"]; ok && string(costRaw) != "null" {
		// keep raw for validation but not used in output
		model.RawCost = costRaw
		// Validate it is an object with numbers
		var costMap map[string]json.RawMessage
		if err := json.Unmarshal(costRaw, &costMap); err != nil {
			return nil, fmt.Errorf("model %q/%q cost must be an object", outerProviderID, modelID)
		}
		for _, field := range []string{"input", "output", "cacheRead", "cacheWrite"} {
			if raw, ok := costMap[field]; ok && string(raw) != "null" {
				var n json.Number
				if err := json.Unmarshal(raw, &n); err != nil {
					return nil, fmt.Errorf("model %q/%q cost.%s must be a number", outerProviderID, modelID, field)
				}
			}
		}
	}
	// headers and samplingParams are explicitly ignored after validation that they are objects if present
	// but they must NOT be used to override.
	// We accept any JSON for them to keep schema flexible, but ensure they are not HTML.
	return model, nil
}

func parsePositiveInt(s string) (int64, error) {
	if strings.ContainsAny(s, ".eE") {
		return 0, fmt.Errorf("must be whole number, got %s", s)
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		return 0, fmt.Errorf("must be non-negative")
	}
	return v, nil
}

func jsonUnmarshalUseNumber(raw json.RawMessage, target any) error {
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	return decoder.Decode(target)
}
