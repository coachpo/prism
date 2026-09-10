package models

import (
	"net/http"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleGetOpenCodeExportSource(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "OpenCode export source", func(tx pgx.Tx) (*openCodeSourceResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		facts, err := loadOpenCodeSourceFacts(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		digest, err := modelexport.ComputeOpenCodeSourceDigest(facts)
		if err != nil {
			return nil, err
		}
		return assembleOpenCodeSourceResponse(facts, digest), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func assembleOpenCodeSourceResponse(facts modelexport.OpenCodeSourceFacts, digest string) *openCodeSourceResponse {
	response := &openCodeSourceResponse{TargetVersion: modelexport.OpenCodeTargetVersion, SourceDigest: digest, Models: []openCodeSourceModelRow{}}
	for _, fact := range facts.Models {
		metadata := modelexport.ProjectOpenCodeMetadata(fact)
		prices := exportPricingSnapshots(fact.Targets)
		decision := modelexport.DecideOpenCodePriceExport(prices)
		response.Models = append(response.Models, openCodeSourceModelRow{
			Readiness:     openCodeReadiness(fact),
			ModelConfigID: fact.ModelConfigID, ModelID: fact.ModelID, APIFamily: fact.APIFamily,
			DisplayName: fact.DisplayName, IsEnabled: fact.IsEnabled, DirectRequestEnabled: true,
			Selectable: fact.Selectable, UnselectableReason: fact.UnselectableReason,
			OpenAIAcceptedFormat: fact.OpenAIAcceptedFormat, OpenAIImageOperations: fact.OpenAIImageOperations,
			NPM:     modelexport.OpenCodeSDKForModel(fact.APIFamily, fact.OpenAIAcceptedFormat),
			APIPath: modelexport.OpenCodeAPIPathForModel(fact.APIFamily), Targets: sourceTargetRows(fact.Targets),
			PriceRisk:      exportPriceRiskWire{Exportable: decision.Exportable, WarningCodes: decision.WarningCodes},
			SourceMetadata: fact.SourceMetadata, OverrideMetadata: fact.OverrideMetadata,
			MergedMetadata: metadata.MergedMetadata, Provenance: metadata.Provenance,
			MissingMetadata: append([]string{}, metadata.MissingMetadata...),
			MetadataIssues:  append([]modelexport.OpenCodeMetadataIssue{}, metadata.Issues...),
		})
	}
	return response
}
