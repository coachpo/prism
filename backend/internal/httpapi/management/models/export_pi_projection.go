package models

import (
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
)

func assemblePiSourceResponse(facts modelexport.SourceFacts, candidates map[int]modelexport.PlatformCandidate, catalogStatus string, catalog *pidev.Catalog, piBindings map[int]piBindingRecord) (*piSourceResponse, error) {
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
		cand := candidates[fact.ModelConfigID]
		prismLayer := modelexport.NewMetadataLayer(fact.PrismMetadata)
		merge, err := modelexport.MergeKnownMetadata(modelexport.MergeOptions{
			Prism:     prismLayer,
			ModelsDev: cand.Metadata,
		})
		if err != nil {
			return nil, err
		}
		priceTargets := reachablePricingTargets(fact)
		decision := modelexport.DecidePriceExport(modelexport.PlatformPi, priceTargets)
		var piWires []piCandidateWire
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
		var fetchedAt, updatedAt *time.Time
		if binding, bound := piBindings[fact.ModelConfigID]; bound {
			bindSource = binding.BindSource
			fetchedAtCopy, updatedAtCopy := binding.FetchedAt, binding.UpdatedAt
			fetchedAt, updatedAt = &fetchedAtCopy, &updatedAtCopy
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
			Targets:               sourceTargetRows(fact.Targets),
			PriceRisk:             exportPriceRiskWire{Exportable: decision.Exportable, WarningCodes: decision.WarningCodes},
			Warnings:              modelexport.MergeWarningCodes(modelexport.MetadataWarningCodes(modelexport.PlatformPi, fact, merge.Merged), cand.WarningCodes),
			PiCandidates:          piWires,
			CandidateStatus:       fact.PiCandidateStatus,
			PiSelected:            selectedWire,
			BindingStatus:         fact.PiBindingStatus,
			BindSource:            bindSource,
			CatalogRevision:       optionalCoordinateRevision(fact.PiSelected),
			FetchedAt:             fetchedAt,
			UpdatedAt:             updatedAt,
			Prism:                 rawMessageMap(prismLayer),
			Merged:                rawMessageMap(merge.Merged),
			Provenance:            provenanceStrings(merge.Provenance),
			Missing:               merge.Missing,
			Completeness: exportPlatformCompletenessWire{
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

func piFieldProjection(merged modelexport.MetadataLayer, cand modelexport.PlatformCandidate) map[string]bool {
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
