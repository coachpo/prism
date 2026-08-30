package pidev

import (
	"sort"
	"strings"
)

// Search bounds for the bounded pi.dev model-id directory search. The limit
// is a page bound, not a scan bound: ranking always considers the whole
// fetched revision so the best matches never depend on provider iteration
// order.
const (
	SearchDefaultLimit = 20
	SearchMaxLimit     = 100
)

// SearchTier ranks one directory hit within a model-id-only search. It is a
// property of the literal text match only: no fuzzy, phonetic, slug, name,
// provider, or host similarity ever produces a hit.
type SearchTier int

const (
	// SearchTierExact is a case-insensitive equality with the whole query.
	SearchTierExact SearchTier = iota
	// SearchTierPrefix is a case-insensitive leading-substring match.
	SearchTierPrefix
	// SearchTierSubstring is any other case-insensitive literal containment.
	SearchTierSubstring
)

// MatchTier classifies this entry's model id against a lower-cased query.
// It is exported so the search contract stays testable without duplicating
// the ranking rule.
func (m *Model) MatchTier(lowerQuery string) (SearchTier, bool) {
	if m == nil || lowerQuery == "" {
		return 0, false
	}
	lowerID := strings.ToLower(m.ModelID)
	switch {
	case lowerID == lowerQuery:
		return SearchTierExact, true
	case strings.HasPrefix(lowerID, lowerQuery):
		return SearchTierPrefix, true
	case strings.Contains(lowerID, lowerQuery):
		return SearchTierSubstring, true
	default:
		return 0, false
	}
}

// SearchModelIDs returns the bounded, deterministically ranked page of
// directory entries whose model id literally contains query and whose API
// equals expectedAPI.
//
// The contract is deliberately narrow:
//   - matching is model-id-only. Provider id, display name, baseUrl, and
//     every other field are never searched;
//   - matching is a case-insensitive literal containment. No fuzzy, slug,
//     token, edit-distance, or wildcard expansion happens;
//   - only entries whose API is exactly expectedAPI can appear, so a search
//     can never offer a cross-API coordinate;
//   - ordering is exact, then prefix, then substring, then provider id, then
//     model id, all compared byte-wise on the original case-sensitive values.
//     The order is total and independent of map iteration;
//   - nothing is selected. The caller receives a page of equally-unselected
//     hits and must obtain an explicit operator choice before binding.
//
// An empty or whitespace-only query, an empty expectedAPI, and a nil catalog
// all return no hits rather than an unfiltered listing.
func (c *Catalog) SearchModelIDs(query, expectedAPI string, limit int) ([]*Model, int) {
	if c == nil || expectedAPI == "" {
		return nil, 0
	}
	lowerQuery := strings.ToLower(strings.TrimSpace(query))
	if lowerQuery == "" {
		return nil, 0
	}
	if limit <= 0 {
		limit = SearchDefaultLimit
	}
	if limit > SearchMaxLimit {
		limit = SearchMaxLimit
	}

	type hit struct {
		model *Model
		tier  SearchTier
	}
	var hits []hit
	for _, providerID := range c.SortedProviderIDs() {
		provider := c.Providers[providerID]
		if provider == nil {
			continue
		}
		modelIDs := make([]string, 0, len(provider.Models))
		for modelID := range provider.Models {
			modelIDs = append(modelIDs, modelID)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			model := provider.Models[modelID]
			if model == nil || model.API != expectedAPI {
				continue
			}
			tier, matched := model.MatchTier(lowerQuery)
			if !matched {
				continue
			}
			hits = append(hits, hit{model: model, tier: tier})
		}
	}

	total := len(hits)
	sort.SliceStable(hits, func(left, right int) bool {
		a, b := hits[left], hits[right]
		if a.tier != b.tier {
			return a.tier < b.tier
		}
		if a.model.ProviderID != b.model.ProviderID {
			return a.model.ProviderID < b.model.ProviderID
		}
		return a.model.ModelID < b.model.ModelID
	})
	if total > limit {
		hits = hits[:limit]
	}
	page := make([]*Model, 0, len(hits))
	for _, item := range hits {
		page = append(page, item.model)
	}
	return page, total
}
