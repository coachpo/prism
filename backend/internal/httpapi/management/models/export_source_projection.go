package models

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
)

// assembleSourceResponse projects the validated facts into per-model source
// rows: layered metadata with provenance and missing leaves, catalog evidence,
// platform completeness, price risk, and the replayable candidate.
func assembleSourceResponse(platform modelexport.Platform, facts modelexport.SourceFacts, candidates map[int]modelexport.PlatformCandidate) (*exportSourceResponse, error) {
	response := &exportSourceResponse{
		Platform: string(platform), TargetVersion: modelexport.TargetVersion(platform),
		CatalogRevision: facts.CatalogRevision, Models: []exportSourceModelRow{},
	}
	for _, fact := range facts.Models {
		candidate := candidates[fact.ModelConfigID]
		prismLayer := modelexport.NewMetadataLayer(fact.PrismMetadata)
		merge, err := modelexport.MergeKnownMetadata(modelexport.MergeOptions{
			Prism:     prismLayer,
			ModelsDev: candidate.Metadata,
		})
		if err != nil {
			return nil, err
		}
		priceTargets := reachablePricingTargets(fact)
		decision := modelexport.DecidePriceExport(platform, priceTargets)

		row := exportSourceModelRow{
			ModelConfigID:         fact.ModelConfigID,
			ModelID:               fact.ModelID,
			APIFamily:             fact.APIFamily,
			DisplayName:           fact.DisplayName,
			IsEnabled:             fact.IsEnabled,
			DefaultSelected:       fact.Selectable,
			Selectable:            fact.Selectable,
			UnselectableReason:    fact.UnselectableReason,
			OpenAIAcceptedFormat:  fact.OpenAIAcceptedFormat,
			OpenAIImageOperations: fact.OpenAIImageOperations,
			Catalog:               exportCatalogEvidenceWire(fact.CatalogBinding),
			Enrichment: exportEnrichmentEvidenceWire{
				Available:          fact.Enrichment.Available,
				OfferingProviderID: fact.Enrichment.OfferingProviderID,
				OfferingModelID:    fact.Enrichment.OfferingModelID,
			},
			Prism:      rawMessageMap(prismLayer),
			ModelsDev:  rawMessageMap(candidate.Metadata),
			Merged:     rawMessageMap(merge.Merged),
			Provenance: provenanceStrings(merge.Provenance),
			Missing:    merge.Missing,
			Completeness: exportPlatformCompletenessWire{
				MetadataFields: platformFieldProjection(platform, merge.Merged, candidate),
				CostExportable: decision.Exportable,
			},
			Targets:             sourceTargetRows(fact.Targets),
			PriceRisk:           exportPriceRiskWire{Exportable: decision.Exportable, WarningCodes: decision.WarningCodes},
			Warnings:            modelexport.MergeWarningCodes(modelexport.MetadataWarningCodes(platform, fact, merge.Merged), candidate.WarningCodes),
			EnrichmentCandidate: encodeEnrichmentCandidate(candidate),
		}
		response.Models = append(response.Models, row)
	}
	return response, nil
}

// platformFieldProjection states which client-facing fields this platform's
// file will carry for the model. Absent stays false; nothing downstream is
// allowed to render absence as zero.
func platformFieldProjection(platform modelexport.Platform, merged modelexport.MetadataLayer, candidate modelexport.PlatformCandidate) map[string]bool {
	_, _ = merged, candidate
	fields := map[string]bool{}
	switch platform {
	case modelexport.PlatformPi:
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
		_, fields["thinkingLevelMap"] = candidate.DerivedFields["thinkingLevelMap"]
	case modelexport.PlatformOpenCode:
		for leaf, key := range map[string]string{
			modelexport.MetaName:        "name",
			modelexport.MetaFamily:      "family",
			modelexport.MetaReleaseDate: "release_date",
			modelexport.MetaAttachment:  "attachment",
			modelexport.MetaReasoning:   "reasoning",
			modelexport.MetaTemperature: "temperature",
			modelexport.MetaToolCall:    "tool_call",
		} {
			_, known := merged.Get(leaf)
			fields[key] = known
		}
		_, contextKnown := merged.Get(modelexport.MetaContextWindow)
		_, inputLimitKnown := merged.Get(modelexport.MetaMaxInputTokens)
		_, outputKnown := merged.Get(modelexport.MetaMaxOutputTokens)
		fields["limit.context"] = contextKnown
		fields["limit.input"] = inputLimitKnown
		fields["limit.output"] = outputKnown
		_, inputKnown := merged.Get(modelexport.MetaModalitiesInput)
		fields["modalities.input"] = inputKnown
		_, outputModalitiesKnown := merged.Get(modelexport.MetaModalitiesOutput)
		fields["modalities.output"] = outputModalitiesKnown
		_, fields["interleaved"] = candidate.DerivedFields["interleaved"]
		fields["variants"] = false
	}
	return fields
}

// sourceTargetRows converts domain facts into wire rows without secrets.
func sourceTargetRows(targets []modelexport.TargetFact) []exportSourceTargetRow {
	rows := make([]exportSourceTargetRow, 0, len(targets))
	for _, target := range targets {
		wire := exportSourceTargetRow{
			TerminalTargetID:     target.TerminalTargetID,
			Position:             target.Position,
			EndpointID:           target.EndpointID,
			EndpointName:         target.EndpointName,
			OpenAITextCapability: target.OpenAITextCapability,
		}
		if pricing := target.Pricing; pricing != nil {
			card := func(value *modelexport.PriceCardSnapshot) *exportPriceCardWire {
				if value == nil {
					return nil
				}
				return &exportPriceCardWire{
					InputPrice:         value.InputPrice,
					OutputPrice:        value.OutputPrice,
					CachedInputPrice:   value.CachedInputPrice,
					CacheCreationPrice: value.CacheCreationPrice,
					ReasoningPrice:     value.ReasoningPrice,
				}
			}
			wire.Pricing = &exportTargetPricingWire{
				TerminalTargetID: target.TerminalTargetID,
				Kind:             string(pricing.Kind),
				CurrencyCode:     pricing.CurrencyCode,
				PricingUnit:      pricing.PricingUnit,
				TierThreshold:    pricing.TierThreshold,
				Card:             card(pricing.Card),
				BaseCard:         card(pricing.BaseCard),
				AboveCard:        card(pricing.AboveCard),
			}
		}
		rows = append(rows, wire)
	}
	return rows
}

func rawMessageMap(layer modelexport.MetadataLayer) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	for _, leaf := range layer.Leaves() {
		value, _ := layer.Get(leaf)
		values[leaf] = value
	}
	return values
}

func provenanceStrings(provenance map[string]modelexport.MetadataSource) map[string]string {
	out := map[string]string{}
	for leaf, source := range provenance {
		out[leaf] = string(source)
	}
	return out
}
