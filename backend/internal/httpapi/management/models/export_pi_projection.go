package models

import (
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

func assemblePiSourceResponse(facts modelexport.SourceFacts, templates map[int]modelexport.PiTemplate, catalogStatus string, catalog *pidev.Catalog, piBindings map[int]piBindingRecord) (*piSourceResponse, error) {
	resp := &piSourceResponse{
		TargetVersion: modelexport.PiTargetVersion,
		Catalog: piCatalogWire{
			Revision:       facts.PiCatalog.Revision,
			Status:         catalogStatus,
			MinimumVersion: facts.PiCatalog.MinimumVersion,
			ETag:           facts.PiCatalog.ETag,
		},
		Models: []piSourceModelRow{},
	}
	for _, fact := range facts.Models {
		cand := templates[fact.ModelConfigID]
		prismLayer := modelexport.NewMetadataLayer(fact.PrismMetadata)
		merge, err := modelexport.MergeKnownMetadata(modelexport.MergeOptions{
			Prism: prismLayer,
			Pi:    cand.Metadata,
		})
		if err != nil {
			return nil, err
		}
		priceTargets := reachablePricingTargets(fact)
		decision := modelexport.DecidePriceExport(priceTargets)
		metadataWarnings := modelexport.MetadataWarningCodes(merge.Merged)
		if len(cand.DroppedFields) > 0 {
			metadataWarnings = modelexport.MergeWarningCodes(metadataWarnings, []string{modelexport.WarningPiSourceFieldsDropped})
		}
		piWires := make([]piCandidateWire, 0, len(fact.PiCandidates))
		for _, pc := range fact.PiCandidates {
			wire := piCandidateWire{
				ProviderID: pc.ProviderID,
				ModelID:    pc.ModelID,
				API:        pc.API,
				Name:       pc.Name,
			}
			if catalog != nil {
				if m, ok := catalog.Find(pc.ProviderID, pc.ModelID); ok {
					wire.Reasoning = m.Reasoning
					wire.Input = m.Input
					wire.ContextWindow = m.ContextWindow
					wire.MaxTokens = m.MaxTokens
					wire.ThinkingLevelMap = m.ThinkingLevelMap
					wire.Compat = m.Compat
					wire.DroppedFields = normalizePiDroppedFields(m.DroppedFields)
				}
			}
			piWires = append(piWires, wire)
		}
		var selectedWire *piSelectedWire
		if fact.PiSelected != nil {
			selectedWire = &piSelectedWire{
				ProviderID: fact.PiSelected.ProviderID,
				ModelID:    fact.PiSelected.ModelID,
				API:        fact.PiSelected.API,
			}
		}
		var bindSource string
		bindingRenderable := false
		var prismModelIDAtBind string
		var fetchedAt, updatedAt *time.Time
		var bindingSource, bindingOverride, bindingEffective *piBindingMetadataPayload
		var bindingDroppedFields []string
		if binding, bound := piBindings[fact.ModelConfigID]; bound {
			bindingRenderable = piBindingMatchesModel(binding, fact.ModelID, modelexport.PiAPIForModel(fact.APIFamily, fact.OpenAIAcceptedFormat))
			bindSource = binding.BindSource
			prismModelIDAtBind = binding.PrismModelIDAtBind
			fetchedAtCopy, updatedAtCopy := binding.FetchedAt, binding.UpdatedAt
			fetchedAt, updatedAt = &fetchedAtCopy, &updatedAtCopy
			bindingSource = binding.Source.payload()
			bindingOverride = binding.Override.payload()
			bindingEffective = binding.Source.effective(binding.Override).payload()
			bindingDroppedFields = normalizePiDroppedFields(binding.DroppedFields)
		}
		row := piSourceModelRow{
			ModelConfigID:         fact.ModelConfigID,
			ModelID:               fact.ModelID,
			APIFamily:             fact.APIFamily,
			DisplayName:           fact.DisplayName,
			IsEnabled:             fact.IsEnabled,
			Selectable:            fact.Selectable,
			UnselectableReason:    fact.UnselectableReason,
			OpenAIAcceptedFormat:  fact.OpenAIAcceptedFormat,
			OpenAIImageOperations: fact.OpenAIImageOperations,
			// PiAPI is Prism's own final Pi API mapping for this model. It is
			// published so the UI never re-derives it and can tell an
			// undeterminable model (empty) apart from a merely-uncatalogued one.
			PiAPI:                    modelexport.PiAPIForModel(fact.APIFamily, fact.OpenAIAcceptedFormat),
			Targets:                  sourceTargetRows(fact.Targets),
			PriceRisk:                exportPriceRiskWire{Exportable: decision.Exportable, WarningCodes: decision.WarningCodes},
			Warnings:                 metadataWarnings,
			PiCandidates:             piWires,
			CandidateStatus:          fact.PiCandidateStatus,
			PiSelected:               selectedWire,
			BindingStatus:            fact.PiBindingStatus,
			BindingRenderable:        bindingRenderable,
			BindSource:               bindSource,
			BindingPrismModelID:      prismModelIDAtBind,
			CatalogRevision:          optionalCoordinateRevision(fact.PiSelected),
			FetchedAt:                fetchedAt,
			UpdatedAt:                updatedAt,
			BindingSourceMetadata:    bindingSource,
			BindingOverrideMetadata:  bindingOverride,
			BindingEffectiveMetadata: bindingEffective,
			BindingDroppedFields:     bindingDroppedFields,
			Prism:                    rawMessageMap(prismLayer),
			Merged:                   rawMessageMap(merge.Merged),
			Provenance:               provenanceStrings(merge.Provenance),
			Missing:                  append([]string{}, merge.Missing...),
			Completeness: exportCompletenessWire{
				MetadataFields: piFieldProjection(merge.Merged, cand),
				CostExportable: decision.Exportable,
			},
		}
		resp.Models = append(resp.Models, row)
	}
	return resp, nil
}

func optionalCoordinateRevision(coordinate *modelexport.SelectedCoordinate) string {
	if coordinate == nil {
		return ""
	}
	return coordinate.CatalogRevision
}

func piFieldProjection(merged modelexport.MetadataLayer, cand modelexport.PiTemplate) map[string]bool {
	fields := map[string]bool{}
	for leaf, key := range map[string]string{
		modelexport.MetaName:            "name",
		modelexport.MetaReasoning:       "reasoning",
		modelexport.MetaContextWindow:   "contextWindow",
		modelexport.MetaMaxOutputTokens: "maxTokens",
		modelexport.MetaModalitiesInput: "input",
	} {
		_, ok := merged.Get(leaf)
		fields[key] = ok
	}
	_, fields["thinkingLevelMap"] = cand.DerivedFields["thinkingLevelMap"]
	return fields
}
