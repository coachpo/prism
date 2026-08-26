package models

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handleRefreshCatalogPreview(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var binding catalogBindingRecord
	_, loadErr := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (struct{}, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return struct{}{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID); modelErr != nil {
			return struct{}{}, modelErr
		}
		if _, findErr := loadBoundCatalogBinding(r.Context(), tx, profile.ID, modelConfigID, &binding); findErr != nil {
			return struct{}{}, findErr
		}
		return struct{}{}, nil
	})
	if loadErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), loadErr)
		return
	}
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	nextModel, exists := catalog.Find(binding.ProviderID, binding.CatalogModelID)
	if !exists {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusConflict, "models_dev_offering_missing: the bound offering disappeared from the catalog; nothing was changed", map[string]any{"provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID}))
		return
	}
	next := catalogMetadataFromModel(nextModel)
	changes, changed := diffCatalogSource(binding.Source, next)
	responseutil.WriteJSON(w, http.StatusOK, modelCatalogRefreshPreviewResponse{
		Bound:           true,
		ProviderID:      binding.ProviderID,
		CatalogModelID:  binding.CatalogModelID,
		Changed:         changed,
		Changes:         changes,
		CatalogRevision: catalog.ETag,
		FetchedAt:       catalog.FetchedAt,
	})
}

// loadBoundCatalogBinding loads a binding and reports whether the surface may
// proceed; unbound models reject refresh flows instead of silently rebinding.
func loadBoundCatalogBinding(ctx context.Context, exec queryExecutor, profileID, modelConfigID int, out *catalogBindingRecord) (bool, error) {
	binding, found, err := loadCatalogBinding(ctx, exec, profileID, modelConfigID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, newCatalogDomainError(http.StatusConflict, "models_dev_not_bound: bind a catalog offering before refreshing metadata", nil)
	}
	*out = binding
	return true, nil
}

func (s *Service) handleRefreshCatalogCommit(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var requestBody modelCatalogRefreshCommitRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	expectedRevision := strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if expectedRevision == "" {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "expected_catalog_revision is required so stale data cannot commit", map[string]any{"field": "expected_catalog_revision"}))
		return
	}
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if catalog.ETag != expectedRevision {
		writeDomainError(w, r, s.corsSnapshot(), catalogStaleError(expectedRevision, catalog.ETag))
		return
	}
	now := s.nowUTC()
	response, txErr := s.refreshModelCatalogInTransaction(r.Context(), r, modelConfigID, catalog, now)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) refreshModelCatalogInTransaction(ctx context.Context, r *http.Request, modelConfigID int, catalog *modelsdev.Catalog, now time.Time) (modelCatalogResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(ctx, tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		var binding catalogBindingRecord
		if _, boundErr := loadBoundCatalogBinding(ctx, tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return modelCatalogResponse{}, boundErr
		}
		// The coordinates are re-resolved inside the transaction against the
		// same fetched revision, so a concurrent rebind cannot smuggle foreign
		// source values into the new offering's row.
		nextModel, exists := catalog.Find(binding.ProviderID, binding.CatalogModelID)
		if !exists {
			return modelCatalogResponse{}, newCatalogDomainError(http.StatusConflict, "models_dev_offering_missing: the bound offering disappeared from the catalog; nothing was changed", map[string]any{"provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID})
		}
		binding.Source = catalogMetadataFromModel(nextModel)
		binding.CatalogRevision = catalog.ETag
		binding.FetchedAt = catalog.FetchedAt
		binding.UpdatedAt = now
		if upsertErr := upsertCatalogBinding(ctx, tx, binding, now); upsertErr != nil {
			return modelCatalogResponse{}, upsertErr
		}
		saved, _, saveErr := loadCatalogBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
}
