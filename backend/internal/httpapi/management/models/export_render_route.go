package models

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handlePostExportRender serves POST /api/models/exports/{platform}/render.
// It performs no network I/O: enrichment replays strictly from the request
// body against the digest-checked database snapshot. Typed failures map onto
// stable wire codes such as 409 export_source_stale and
// 422 export_model_unselectable.
func (s *Service) handlePostExportRender(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	platform, ok := parseExportPlatform(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	renderInput, ok := s.prepareExportRenderRequest(w, r, platform)
	if !ok {
		return
	}
	request := renderInput.Request
	// Snapshot is an immutable in-memory pointer and performs no I/O. Render
	// tries it plus the explicit no-enrichment candidate; the request never
	// supplies catalog facts.
	var catalog *modelsdev.Catalog
	if s.catalog != nil {
		catalog = s.catalog.Snapshot()
	}
	rendered, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "model export render", func(tx pgx.Tx) (*exportRenderResponse, error) {
		if _, err := resolveEffectiveProfile(r.Context(), tx, r); err != nil {
			return nil, err
		}
		modelRows, targetRows, bindings, graph, err := loadExportSnapshot(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		groupedTargets := sortTargetRowsByModel(targetRows)
		currentFacts, currentCandidates := buildSourceFacts(platform, exportFactsInput{
			ModelRows:  modelRows,
			TargetRows: groupedTargets,
			Bindings:   bindings,
			Catalog:    catalog,
			Graph:      graph,
		})
		noEnrichmentFacts, noEnrichmentCandidates := buildSourceFacts(platform, exportFactsInput{
			ModelRows:  modelRows,
			TargetRows: groupedTargets,
			Bindings:   bindings,
			Catalog:    nil,
			Graph:      graph,
		})
		facts, candidates := currentFacts, currentCandidates
		digest, err := modelexport.ComputeSourceDigest(currentFacts)
		if err != nil {
			return nil, err
		}
		if request.ExpectedSourceDigest != digest {
			withoutDigest, digestErr := modelexport.ComputeSourceDigest(noEnrichmentFacts)
			if digestErr != nil {
				return nil, digestErr
			}
			if request.ExpectedSourceDigest != withoutDigest {
				return nil, &modelexport.ErrSourceStale{}
			}
			facts, candidates = noEnrichmentFacts, noEnrichmentCandidates
		}
		selection, err := modelexport.NormalizeSelection(request.ModelConfigIDs, facts)
		if err != nil {
			return nil, err
		}
		enhancements := map[int]modelexport.ManualEnhancement{}
		selected := make(map[int]struct{}, len(selection))
		for _, id := range selection {
			selected[id] = struct{}{}
			if wire, present := request.Enhancements[id]; present && wire != nil {
				enhancements[id] = wire.decode()
			}
		}
		for id := range request.Enhancements {
			if _, ok := selected[id]; !ok {
				return nil, &modelexport.ErrUnselectableModel{ModelConfigID: id, Reason: "enhancement_model_not_selected"}
			}
		}
		result, err := modelexport.DispatchRender(platform, modelexport.RenderDispatch{
			Facts:         facts,
			Selection:     selection,
			Enrichment:    candidates,
			Enhancements:  enhancements,
			BaseURL:       renderInput.BaseURL,
			ProviderID:    renderInput.ProviderID,
			IncludeAPIKey: request.Credential.Include,
			APIKey:        renderInput.APIKey,
			DefaultModel:  request.DefaultModelConfigID,
		})
		if err != nil {
			return nil, err
		}
		return &exportRenderResponse{
			Platform:        string(platform),
			TargetVersion:   modelexport.TargetVersion(platform),
			CatalogRevision: facts.CatalogRevision,
			Content:         result.Content,
			ContentSHA256:   result.ContentSHA256,
			FileName:        result.FileName,
			MIMEType:        result.MIMEType,
			ModelResults:    result.ModelResults,
			Warnings:        result.Warnings,
		}, nil
	})
	if err != nil {
		s.writeExportDomainError(w, r, err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, rendered)
}
