package models

import (
	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

type piExportFactsInput struct {
	ModelRows     []exportModelRow
	TargetRows    map[int][]exportTargetRow
	Bindings      map[int]catalogBindingRecord // models.dev bindings (unrelated catalog, unchanged)
	PiBindings    map[int]piBindingRecord      // persisted pi.dev bindings, authoritative for render
	Catalog       *pidev.Catalog
	CatalogStatus string
	Graph         *modelrouting.DiagnosticsGraph
}

// buildPiSourceFacts projects one DB snapshot plus one pi.dev catalog fetch
// into the clock-free SourceFacts the digest and renderer both replay
// against. Live catalog evidence (PiCandidates/PiCandidateStatus) and
// persisted-binding truth (PiSelected/PiBindingStatus) are computed
// independently: a bound coordinate stays the render authority even when the
// live catalog fetch fails or no longer lists it, and drift between the two
// is surfaced as pi_binding_status=bound_drifted rather than silently
// re-selecting anything.
func buildPiSourceFacts(input piExportFactsInput) (modelexport.SourceFacts, map[int]modelexport.PlatformCandidate, error) {
	facts := modelexport.SourceFacts{
		Platform:      modelexport.PlatformPi,
		TargetVersion: modelexport.PiTargetVersion,
		PiCatalog: modelexport.PiCatalogEvidence{
			Revision:       catalogRevision(input.Catalog),
			Status:         input.CatalogStatus,
			MinimumVersion: catalogMinimumVersion(input.Catalog),
			ETag:           catalogETag(input.Catalog),
		},
		PiSelections: map[int]modelexport.SelectedCoordinate{},
	}
	candidates := map[int]modelexport.PlatformCandidate{}
	for _, model := range input.ModelRows {
		routableTargetIDs, primaryRoutable := exportStaticRouteEvidence(model, input.Graph)
		targets := filterExportTargets(input.TargetRows[model.ID], routableTargetIDs)
		selectable, reason := exportSelectable(model, primaryRoutable)
		binding := input.Bindings[model.ID]
		prismMetadata := canonicalMetadataFromBinding(binding)
		if model.DisplayName != nil {
			prismMetadata[modelexport.MetaName] = marshalRawJSON(*model.DisplayName)
		}
		fact := modelexport.ModelFact{
			ModelConfigID:         model.ID,
			ModelID:               model.ModelID,
			APIFamily:             model.APIFamily,
			DisplayName:           model.DisplayName,
			IsEnabled:             model.IsEnabled,
			Selectable:            selectable,
			OpenAIAcceptedFormat:  model.OpenAIAcceptedFormat,
			OpenAIImageOperations: model.OpenAIImageOperations,
			CatalogBinding:        exportCatalogEvidence(binding),
			Enrichment:            modelexport.EnrichmentEvidence{},
			PrismMetadata:         prismMetadata,
			Targets:               exportTargetFacts(targets),
		}
		if !selectable && reason != nil {
			fact.UnselectableReason = reason
		}

		// Live pi.dev candidate evidence: full model_id case-sensitive exact
		// match plus final Pi API compatibility. This never selects anything by
		// itself; it is discovery evidence for bind/rebind decisions.
		expectedAPI := piExpectedAPI(fact.APIFamily, fact.OpenAIAcceptedFormat)
		var liveCandidates []*pidev.Model
		if input.Catalog != nil && expectedAPI != "" {
			liveCandidates = input.Catalog.Candidates(fact.ModelID, expectedAPI)
		}
		for _, c := range liveCandidates {
			fact.PiCandidates = append(fact.PiCandidates, modelexport.PiCandidate{
				ProviderID: c.ProviderID, ModelID: c.ModelID, API: c.API, Name: c.Name,
			})
		}
		fact.PiCandidateStatus = piCandidateStatus(input.Catalog, expectedAPI, fact.ModelID, liveCandidates)

		// Persisted binding truth: authoritative for render regardless of live
		// catalog outcome above.
		piBinding, isBound := input.PiBindings[fact.ModelConfigID]
		if isBound {
			coordinate := modelexport.SelectedCoordinate{
				ProviderID: piBinding.ProviderID, ModelID: piBinding.CatalogModelID,
				API: piBinding.API, CatalogRevision: piBinding.CatalogRevision,
			}
			fact.PiSelected = &coordinate
			facts.PiSelections[fact.ModelConfigID] = coordinate
			fact.PiBindingStatus = piBindingStatus(input.Catalog, piBinding)
			candidates[fact.ModelConfigID] = piBindingPlatformCandidate(piBinding.Source.effective(piBinding.Override))
		} else {
			fact.PiBindingStatus = "unbound"
		}
		facts.Models = append(facts.Models, fact)
	}
	// facts.Enrichment must carry the same map the renderer receives: it is
	// what makes a rebind or an override's effective metadata change move
	// the digest, since the binding's frozen source/override values
	// otherwise appear nowhere else in the fact set.
	facts.Enrichment = candidates
	return facts, candidates, nil
}

// piExpectedAPI maps api_family (plus the openai accepted-format split) onto
// the final Pi API literal. It is the single source of this mapping; both
// live candidate matching and the binding surface share it.
func piExpectedAPI(apiFamily string, openAIAcceptedFormat *string) string {
	switch apiFamily {
	case "openai":
		if openAIAcceptedFormat != nil && *openAIAcceptedFormat == "chat_completions_only" {
			return "openai-completions"
		}
		return "openai-responses"
	case "anthropic":
		return "anthropic-messages"
	case "gemini":
		return "google-generative-ai"
	default:
		return ""
	}
}

func piCandidateStatus(catalog *pidev.Catalog, expectedAPI, modelID string, liveCandidates []*pidev.Model) string {
	if catalog == nil {
		return "catalog_unavailable"
	}
	if expectedAPI == "" {
		return "api_mismatch"
	}
	switch len(liveCandidates) {
	case 0:
		return piZeroCandidateStatus(catalog, modelID)
	case 1:
		return "single"
	default:
		return "multiple"
	}
}

// piBindingStatus reports whether a persisted binding still matches live
// catalog evidence. It stays "bound" (benefit of the doubt) whenever the
// live fetch itself is unavailable: drift is only ever asserted from
// positive evidence, never from an absent check.
func piBindingStatus(catalog *pidev.Catalog, binding piBindingRecord) string {
	if catalog == nil {
		return "bound"
	}
	model, found := catalog.Find(binding.ProviderID, binding.CatalogModelID)
	if !found || model.API != binding.API {
		return "bound_drifted"
	}
	return "bound"
}

func catalogRevision(c *pidev.Catalog) string {
	if c == nil {
		return ""
	}
	return c.Revision
}
func catalogMinimumVersion(c *pidev.Catalog) string {
	if c == nil {
		return ""
	}
	return c.MinimumVersion
}
func catalogETag(c *pidev.Catalog) string {
	if c == nil {
		return ""
	}
	return c.ETag
}
