package models

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

type piExportFactsInput struct {
	ModelRows     []exportModelRow
	TargetRows    map[int][]exportTargetRow
	PiBindings    map[int]piBindingRecord // persisted pi.dev bindings, authoritative for render
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
// re-selecting anything. The status/match helpers live in
// pi_catalog_projection.go and are shared with the single-model Pi read.
func buildPiSourceFacts(input piExportFactsInput) (modelexport.SourceFacts, map[int]modelexport.PiTemplate, error) {
	facts := modelexport.SourceFacts{
		TargetVersion: modelexport.PiTargetVersion,
		PiCatalog: modelexport.PiCatalogEvidence{
			Revision:       catalogRevision(input.Catalog),
			Status:         input.CatalogStatus,
			MinimumVersion: catalogMinimumVersion(input.Catalog),
			ETag:           catalogETag(input.Catalog),
		},
	}
	templates := map[int]modelexport.PiTemplate{}
	for _, model := range input.ModelRows {
		routableTargetIDs, primaryRoutable := exportStaticRouteEvidence(model, input.Graph)
		targets := filterExportTargets(input.TargetRows[model.ID], routableTargetIDs)
		selectable, reason := exportSelectable(model, primaryRoutable)
		prismMetadata := map[string]json.RawMessage{}
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
			PrismMetadata:         prismMetadata,
			Targets:               exportTargetFacts(targets),
		}
		if !selectable && reason != nil {
			fact.UnselectableReason = reason
		}

		// Live pi.dev candidate evidence: full model_id case-sensitive exact
		// match plus final Pi API compatibility. This never selects anything by
		// itself; it is discovery evidence for bind/rebind decisions.
		expectedAPI := modelexport.PiAPIForModel(fact.APIFamily, fact.OpenAIAcceptedFormat)
		var liveCandidates []*pidev.Model
		if input.Catalog != nil && expectedAPI != "" {
			liveCandidates = input.Catalog.Candidates(fact.ModelID, expectedAPI)
		}
		for _, c := range liveCandidates {
			fact.PiCandidates = append(fact.PiCandidates, modelexport.PiCandidate{
				ProviderID: c.ProviderID, ModelID: c.ModelID, API: c.API, Name: c.Name,
				DroppedFields: normalizePiDroppedFields(c.DroppedFields),
			})
		}
		fact.PiCandidateStatus = piCandidateStatus(input.Catalog, expectedAPI, fact.ModelID, liveCandidates)

		// Persisted binding truth: authoritative for render regardless of live
		// catalog outcome above.
		piBinding, isBound := input.PiBindings[fact.ModelConfigID]
		if isBound {
			coordinate := modelexport.SelectedCoordinate{
				ProviderID: piBinding.ProviderID, ModelID: piBinding.CatalogModelID,
				API: piBinding.API, PrismModelID: piBinding.PrismModelIDAtBind,
				CatalogRevision: piBinding.CatalogRevision,
			}
			fact.PiSelected = &coordinate
			template := piBindingPiTemplate(piBinding.Source.effective(piBinding.Override), piBinding.DroppedFields)
			fact.PiTemplate = template
			templates[fact.ModelConfigID] = template
			if piBindingMatchesModel(piBinding, fact.ModelID, expectedAPI) {
				fact.PiBindingStatus = piBindingStatus(input.Catalog, input.CatalogStatus, piBinding)
			} else {
				fact.PiBindingStatus = "bound_drifted"
			}
		} else {
			fact.PiBindingStatus = "unbound"
		}
		facts.Models = append(facts.Models, fact)
	}
	return facts, templates, nil
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
