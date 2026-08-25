package modelsdev

import (
	"sort"
	"strings"
)

// AutoMatchProviders maps Prism api_family values onto the canonical
// models.dev provider ids used for unique-exact auto matching. The mapping is
// deliberately narrow: it names the provider whose catalog model ids are the
// upstream-native identifiers for that family. Operators behind aggregators
// (azure, openrouter, ...) bind explicitly instead.
var AutoMatchProviders = map[string][]string{
	"openai":    {"openai"},
	"anthropic": {"anthropic"},
	"gemini":    {"google"},
}

// AutoMatchProviderIDs returns the mapped provider ids for an api_family in
// deterministic order (empty slice for families without a catalog mapping).
func AutoMatchProviderIDs(apiFamily string) []string {
	ids := append([]string(nil), AutoMatchProviders[strings.TrimSpace(apiFamily)]...)
	sort.Strings(ids)
	return ids
}

// Candidate is one addressable catalog offering in search results.
type Candidate struct {
	ProviderID   string `json:"provider_id"`
	ProviderName string `json:"provider_name"`
	ModelID      string `json:"model_id"`
	Name         string `json:"name"`
}

// ExactMatches returns every offering whose provider-local model id equals
// modelID exactly across the api_family's mapped providers, ordered by
// provider id. A single-element result is the unique exact match; zero or
// multiple elements must never auto-bind.
func ExactMatches(catalog *Catalog, apiFamily, modelID string) []Candidate {
	if catalog == nil || strings.TrimSpace(modelID) == "" {
		return nil
	}
	matches := make([]Candidate, 0)
	for _, providerID := range AutoMatchProviderIDs(apiFamily) {
		provider := catalog.Providers[providerID]
		if provider == nil {
			continue
		}
		if model, ok := provider.Models[modelID]; ok {
			matches = append(matches, candidateFrom(provider, model))
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ProviderID < matches[j].ProviderID })
	return matches
}

// SearchCandidates performs a bounded case-insensitive substring search over
// model ids and display names. scope "family" restricts the search to the
// api_family's mapped providers; scope "all" covers every provider so manual
// binding can reach aggregator offerings. Results are ordered by
// (provider_id, model_id); total reports the unbounded match count.
func SearchCandidates(catalog *Catalog, apiFamily, query, scope string, limit, offset int) ([]Candidate, int) {
	if catalog == nil {
		return nil, 0
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	needle := strings.ToLower(strings.TrimSpace(query))
	scopedProviders := map[string]struct{}{}
	if scope != "all" {
		for _, providerID := range AutoMatchProviderIDs(apiFamily) {
			scopedProviders[providerID] = struct{}{}
		}
	}
	all := make([]Candidate, 0)
	for _, providerID := range catalog.SortedProviderIDs() {
		if _, scoped := scopedProviders[providerID]; scope != "all" && !scoped {
			continue
		}
		provider := catalog.Providers[providerID]
		modelIDs := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			model := provider.Models[modelID]
			if needle != "" && !candidateMatches(needle, providerID, modelID, model.Name) {
				continue
			}
			all = append(all, candidateFrom(provider, model))
		}
	}
	total := len(all)
	if offset >= total {
		return []Candidate{}, total
	}
	end := min(offset+limit, total)
	return all[offset:end], total
}

func candidateMatches(needle, providerID, modelID, name string) bool {
	return strings.Contains(strings.ToLower(modelID), needle) ||
		strings.Contains(strings.ToLower(name), needle) ||
		strings.Contains(strings.ToLower(providerID), needle)
}

func candidateFrom(provider *Provider, model *Model) Candidate {
	name := model.Name
	if name == "" {
		name = model.ModelID
	}
	return Candidate{ProviderID: provider.ID, ProviderName: provider.Name, ModelID: model.ModelID, Name: name}
}
