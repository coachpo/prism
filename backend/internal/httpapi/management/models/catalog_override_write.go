package models

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handlePutCatalogOverride(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	var requestBody modelCatalogOverrideWriteRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	requestBody.ExpectedProviderID = strings.TrimSpace(requestBody.ExpectedProviderID)
	requestBody.ExpectedCatalogModelID = strings.TrimSpace(requestBody.ExpectedCatalogModelID)
	if requestBody.ExpectedProviderID == "" || requestBody.ExpectedCatalogModelID == "" {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "override requires the binding coordinate the operator confirmed", nil))
		return
	}
	values, err := decodeOverrideFields(requestBody.Override)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	now := s.nowUTC()
	response, txErr := s.putCatalogOverrideInTransaction(r.Context(), r, modelConfigID, requestBody, values, now)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) putCatalogOverrideInTransaction(ctx context.Context, r *http.Request, modelConfigID int, expected modelCatalogOverrideWriteRequest, values map[string]any, now time.Time) (modelCatalogResponse, error) {
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
		if binding.ProviderID != expected.ExpectedProviderID || binding.CatalogModelID != expected.ExpectedCatalogModelID {
			return modelCatalogResponse{}, newCatalogDomainError(http.StatusConflict, "models_dev_binding_stale: the binding changed after it was read; re-read before applying overrides", map[string]any{
				"provider_id":      binding.ProviderID,
				"catalog_model_id": binding.CatalogModelID,
			})
		}
		// The operator's edit merges over the locked current row, so two
		// concurrent sparse overrides of different fields both survive.
		for _, spec := range overrideFieldSpecs {
			value, present := values[spec.field]
			if !present {
				continue
			}
			if value == nil {
				spec.setNull(&binding.Override)
				continue
			}
			spec.setValue(&binding.Override, value)
		}
		updatedAt := nextCatalogBindingUpdatedAt(binding.UpdatedAt, now)
		// Override-only UPDATE: source columns, revision, and match source
		// stay exactly as the locked row carries them.
		if updateErr := updateCatalogBindingOverride(ctx, tx, modelConfigID, binding.Override, updatedAt); updateErr != nil {
			return modelCatalogResponse{}, updateErr
		}
		saved, _, saveErr := loadCatalogBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
}

func (s *Service) handleClearCatalogOverride(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	var requestBody modelCatalogOverrideClearRequest
	if body, readErr := readJSONBody(r); readErr != nil || len(bytes.TrimSpace(body)) > 0 {
		if err := decodeJSONBytes(body, &requestBody); err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
			return
		}
	}
	requestBody.ExpectedProviderID = strings.TrimSpace(requestBody.ExpectedProviderID)
	requestBody.ExpectedCatalogModelID = strings.TrimSpace(requestBody.ExpectedCatalogModelID)
	if requestBody.ExpectedProviderID == "" || requestBody.ExpectedCatalogModelID == "" || requestBody.ExpectedBindingUpdatedAt.IsZero() {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "override clear requires the binding coordinate and updated_at snapshot the operator confirmed", nil))
		return
	}
	now := s.nowUTC()
	response, txErr := s.clearCatalogOverrideInTransaction(r.Context(), r, modelConfigID, requestBody, now)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) clearCatalogOverrideInTransaction(ctx context.Context, r *http.Request, modelConfigID int, expected modelCatalogOverrideClearRequest, now time.Time) (modelCatalogResponse, error) {
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
			return modelCatalogResponse{}, newCatalogDomainError(http.StatusConflict, "models_dev_binding_stale: the binding changed after it was read; re-read before clearing overrides", map[string]any{
				"provider_id":        binding.ProviderID,
				"catalog_model_id":   binding.CatalogModelID,
				"binding_updated_at": binding.UpdatedAt.Format(time.RFC3339Nano),
			})
		}
		updatedAt := nextCatalogBindingUpdatedAt(binding.UpdatedAt, now)
		if updateErr := updateCatalogBindingOverride(ctx, tx, modelConfigID, modelCatalogMetadata{}, updatedAt); updateErr != nil {
			return modelCatalogResponse{}, updateErr
		}
		saved, _, saveErr := loadCatalogBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
}
