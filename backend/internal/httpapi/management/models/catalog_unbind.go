package models

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handleUnbindModelCatalog(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	response, txErr := s.unbindModelCatalogInTransaction(r.Context(), r, modelConfigID)
	if txErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), txErr)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) unbindModelCatalogInTransaction(ctx context.Context, r *http.Request, modelConfigID int) (modelCatalogResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, modelErr := loadModelForCatalog(ctx, tx, profile.ID, modelConfigID); modelErr != nil {
			return modelCatalogResponse{}, modelErr
		}
		if _, err := tx.Exec(ctx, `DELETE FROM model_catalog_bindings WHERE model_config_id = $1`, modelConfigID); err != nil {
			return modelCatalogResponse{}, fmt.Errorf("unbind catalog binding for model %d: %w", modelConfigID, err)
		}
		return catalogResponseFromBinding(catalogBindingRecord{}), nil
	})
}
