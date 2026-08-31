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
		Bound:            true,
		ProviderID:       binding.ProviderID,
		CatalogModelID:   binding.CatalogModelID,
		Changed:          changed,
		Changes:          changes,
		CatalogRevision:  catalog.ETag,
		FetchedAt:        catalog.FetchedAt,
		BindingUpdatedAt: binding.UpdatedAt,
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

// loadBoundCatalogBindingForUpdate is the write-phase counterpart: it takes
// the binding row lock (after the caller already holds the model row lock)
// and rejects unbound models the same way.
func loadBoundCatalogBindingForUpdate(ctx context.Context, tx pgx.Tx, profileID, modelConfigID int, out *catalogBindingRecord) (bool, error) {
	binding, found, err := loadCatalogBindingForUpdate(ctx, tx, profileID, modelConfigID)
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
	requestBody.ExpectedProviderID = strings.TrimSpace(requestBody.ExpectedProviderID)
	requestBody.ExpectedCatalogModelID = strings.TrimSpace(requestBody.ExpectedCatalogModelID)
	requestBody.ExpectedCatalogRevision = strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if requestBody.ExpectedCatalogRevision == "" || requestBody.ExpectedProviderID == "" || requestBody.ExpectedCatalogModelID == "" || requestBody.ExpectedBindingUpdatedAt.IsZero() {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "refresh commit requires the previewed binding coordinate, updated_at token, and catalog revision", nil))
		return
	}
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if catalog.ETag != requestBody.ExpectedCatalogRevision {
		writeDomainError(w, r, s.corsSnapshot(), catalogStaleError(requestBody.ExpectedCatalogRevision, catalog.ETag))
		return
	}
	now := s.nowUTC()
	response, txErr := s.refreshModelCatalogInTransaction(r.Context(), r, modelConfigID, requestBody, catalog, now)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) refreshModelCatalogInTransaction(ctx context.Context, r *http.Request, modelConfigID int, expected modelCatalogRefreshCommitRequest, catalog *modelsdev.Catalog, now time.Time) (modelCatalogResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := lockModelForCatalog(ctx, tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		s.observeCatalogWriteModelLocked(modelConfigID)
		var binding catalogBindingRecord
		if _, boundErr := loadBoundCatalogBindingForUpdate(ctx, tx, profile.ID, modelConfigID, &binding); boundErr != nil {
			return modelCatalogResponse{}, boundErr
		}
		if binding.ProviderID != expected.ExpectedProviderID || binding.CatalogModelID != expected.ExpectedCatalogModelID || !binding.UpdatedAt.Equal(expected.ExpectedBindingUpdatedAt) {
			return modelCatalogResponse{}, newCatalogDomainError(http.StatusConflict, "models_dev_binding_stale: the binding changed after refresh preview; preview again", map[string]any{
				"provider_id":        binding.ProviderID,
				"catalog_model_id":   binding.CatalogModelID,
				"binding_updated_at": binding.UpdatedAt.Format(time.RFC3339Nano),
			})
		}
		// The coordinates are re-resolved inside the transaction against the
		// same fetched revision, so a concurrent rebind cannot smuggle foreign
		// source values into the new offering's row.
		nextModel, exists := catalog.Find(binding.ProviderID, binding.CatalogModelID)
		if !exists {
			return modelCatalogResponse{}, newCatalogDomainError(http.StatusConflict, "models_dev_offering_missing: the bound offering disappeared from the catalog; nothing was changed", map[string]any{"provider_id": binding.ProviderID, "catalog_model_id": binding.CatalogModelID})
		}
		updatedAt := nextCatalogBindingUpdatedAt(binding.UpdatedAt, now)
		// Source-only UPDATE: overrides and match source stay exactly as the
		// locked current row carries them.
		if updateErr := updateCatalogBindingSource(ctx, tx, modelConfigID, catalogMetadataFromModel(nextModel), catalog.ETag, catalog.FetchedAt, updatedAt); updateErr != nil {
			return modelCatalogResponse{}, updateErr
		}
		saved, _, saveErr := loadCatalogBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
}
