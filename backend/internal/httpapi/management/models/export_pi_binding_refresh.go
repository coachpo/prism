package models

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handleRefreshPiPreview serves POST /api/models/{model_config_id}/pi/refresh/preview.
func (s *Service) handleRefreshPiPreview(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requirePiCatalogClient(w, r) {
		return
	}
	var binding piBindingRecord
	_, loadErr := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (struct{}, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return struct{}{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); modelErr != nil {
			return struct{}{}, modelErr
		}
		if _, findErr := loadBoundPiBinding(r.Context(), tx, profile.ID, modelConfigID, &binding); findErr != nil {
			return struct{}{}, findErr
		}
		return struct{}{}, nil
	})
	if loadErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), loadErr)
		return
	}
	catalog, err := s.piCatalog.Fetch(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), piCatalogFetchFailed(err))
		return
	}
	nextModel, exists := catalog.Find(binding.ProviderID, binding.CatalogModelID)
	if !exists || nextModel.API != binding.API {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusConflict, "pi_candidate_missing: the bound candidate disappeared from the catalog; nothing was changed", map[string]any{"provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID, "api": binding.API}))
		return
	}
	next := piBindingMetadataFromModel(nextModel)
	changes, changed := diffPiBindingSource(binding.Source, next)
	responseutil.WriteJSON(w, http.StatusOK, piRefreshPreviewResponse{
		Bound:           true,
		ProviderID:      binding.ProviderID,
		CatalogModelID:  binding.CatalogModelID,
		API:             binding.API,
		Changed:         changed,
		Changes:         changes,
		CatalogRevision: catalog.RevisionString(),
		FetchedAt:       catalog.FetchedAt,
	})
}

// handleRefreshPiCommit serves POST /api/models/{model_config_id}/pi/refresh/commit.
func (s *Service) handleRefreshPiCommit(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requirePiCatalogClient(w, r) {
		return
	}
	var requestBody piRefreshCommitRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	expectedRevision := strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if expectedRevision == "" {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "expected_catalog_revision is required so stale data cannot commit", map[string]any{"field": "expected_catalog_revision"}))
		return
	}
	catalog, err := s.piCatalog.Fetch(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), piCatalogFetchFailed(err))
		return
	}
	if catalog.RevisionString() != expectedRevision {
		writeDomainError(w, r, s.corsSnapshot(), piCatalogStaleError(expectedRevision, catalog.RevisionString()))
		return
	}
	now := s.nowUTC()
	response, txErr := s.refreshPiInTransaction(r.Context(), r, modelConfigID, catalog, now)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) refreshPiInTransaction(ctx context.Context, r *http.Request, modelConfigID int, catalog *pidev.Catalog, now time.Time) (piBindingResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (piBindingResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return piBindingResponse{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(ctx, tx, profile.ID, modelConfigID); modelErr != nil {
			return piBindingResponse{}, modelErr
		}
		var binding piBindingRecord
		if _, boundErr := loadBoundPiBinding(ctx, tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return piBindingResponse{}, boundErr
		}
		// The coordinate is re-resolved inside the transaction against the same
		// fetched revision, so a concurrent rebind cannot smuggle a foreign
		// candidate's source values into this row.
		nextModel, exists := catalog.Find(binding.ProviderID, binding.CatalogModelID)
		if !exists || nextModel.API != binding.API {
			return piBindingResponse{}, newPiDomainError(http.StatusConflict, "pi_candidate_missing: the bound candidate disappeared from the catalog; nothing was changed", map[string]any{"provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID, "api": binding.API})
		}
		binding.Source = piBindingMetadataFromModel(nextModel)
		binding.CatalogRevision = catalog.RevisionString()
		binding.FetchedAt = catalog.FetchedAt
		binding.UpdatedAt = now
		if upsertErr := upsertPiBinding(ctx, tx, binding, now); upsertErr != nil {
			return piBindingResponse{}, upsertErr
		}
		saved, _, saveErr := loadPiBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return piBindingResponse{}, saveErr
		}
		return saved.response(), nil
	})
}
