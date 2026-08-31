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

func (s *Service) handleUnbindModelCatalog(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	// An absent body means the caller never confirmed a snapshot; the
	// validation below owns that rejection so the failure is a 422 about the
	// missing coordinate, not a 400 about JSON parsing.
	var requestBody modelCatalogUnbindRequest
	if body, readErr := readJSONBody(r); readErr != nil || len(bytes.TrimSpace(body)) > 0 {
		if err := decodeJSONBytes(body, &requestBody); err != nil {
			responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
			return
		}
	}
	requestBody.ExpectedProviderID = strings.TrimSpace(requestBody.ExpectedProviderID)
	requestBody.ExpectedCatalogModelID = strings.TrimSpace(requestBody.ExpectedCatalogModelID)
	if requestBody.ExpectedProviderID == "" || requestBody.ExpectedCatalogModelID == "" || requestBody.ExpectedBindingUpdatedAt.IsZero() {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "unbind requires the binding coordinate and updated_at snapshot the operator confirmed", nil))
		return
	}
	response, txErr := s.unbindModelCatalogInTransaction(r.Context(), r, modelConfigID, requestBody)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// unbindModelCatalogInTransaction deletes the binding only when the persisted
// coordinate and updated_at still match the snapshot the operator confirmed.
// A binding that is already gone stays an idempotent unbound response; a
// concurrent rebind or refresh keeps the newer row and rejects with 409.
func (s *Service) unbindModelCatalogInTransaction(ctx context.Context, r *http.Request, modelConfigID int, expected modelCatalogUnbindRequest) (modelCatalogResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := lockModelForCatalog(ctx, tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		s.observeCatalogWriteModelLocked(modelConfigID)
		binding, found, loadErr := loadCatalogBindingForUpdate(ctx, tx, profile.ID, modelConfigID)
		if loadErr != nil {
			return modelCatalogResponse{}, loadErr
		}
		if !found {
			return modelCatalogResponse{Bound: false}, nil
		}
		if binding.ProviderID != expected.ExpectedProviderID || binding.CatalogModelID != expected.ExpectedCatalogModelID || !binding.UpdatedAt.Equal(expected.ExpectedBindingUpdatedAt) {
			return modelCatalogResponse{}, newCatalogDomainError(http.StatusConflict, "models_dev_binding_stale: the binding changed after it was read; re-read before deleting", map[string]any{
				"provider_id":        binding.ProviderID,
				"catalog_model_id":   binding.CatalogModelID,
				"binding_updated_at": binding.UpdatedAt.Format(time.RFC3339Nano),
			})
		}
		if deleteErr := deleteCatalogBinding(ctx, tx, modelConfigID); deleteErr != nil {
			return modelCatalogResponse{}, deleteErr
		}
		return modelCatalogResponse{Bound: false}, nil
	})
}
