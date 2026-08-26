package connections

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func normalizeConnectionIDs(raw []int) ([]int, error) {
	seen := map[int]struct{}{}
	ids := make([]int, 0, len(raw))
	for _, id := range raw {
		if id <= 0 {
			return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "connection_ids must carry positive connection ids"}
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	if len(ids) > catalogPricingTargetLimit {
		return nil, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: fmt.Sprintf("connection_ids must not exceed %d targets", catalogPricingTargetLimit)}
	}
	return ids, nil
}

// resolveCatalogOffering turns the request into offering coordinates: either
// explicit provider+model or the model's persisted binding.
func resolveCatalogOffering(ctx context.Context, tx pgx.Tx, profileID int, requestBody catalogPricingPreviewRequest) (modelsdev.Offering, error) {
	providerID := strings.TrimSpace(requestBody.ProviderID)
	catalogModelID := strings.TrimSpace(requestBody.CatalogModelID)
	if providerID != "" || catalogModelID != "" {
		if providerID == "" || catalogModelID == "" {
			return modelsdev.Offering{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "provider_id and catalog_model_id must be provided together"}
		}
		return modelsdev.Offering{ProviderID: providerID, ModelID: catalogModelID}, nil
	}
	if requestBody.ModelConfigID == nil || *requestBody.ModelConfigID <= 0 {
		return modelsdev.Offering{}, &domainError{StatusCode: http.StatusUnprocessableEntity, Detail: "either model_config_id or an explicit provider_id + catalog_model_id pair is required"}
	}
	binding, found, err := loadCatalogBindingForProfile(ctx, tx, profileID, *requestBody.ModelConfigID)
	if err != nil {
		return modelsdev.Offering{}, err
	}
	if !found {
		return modelsdev.Offering{}, &domainError{StatusCode: http.StatusConflict, Detail: "models_dev_not_bound: bind the model to a catalog offering before generating prices"}
	}
	return modelsdev.Offering{ProviderID: binding.ProviderID, ModelID: binding.CatalogModelID}, nil
}

type catalogBindingCoordinates struct {
	ProviderID     string
	CatalogModelID string
}

type catalogPricingPreviewScope struct {
	profileID int
	offering  modelsdev.Offering
}

func (s *Service) resolveCatalogPricingPreviewScope(ctx context.Context, r *http.Request, requestBody catalogPricingPreviewRequest) (catalogPricingPreviewScope, error) {
	return pgxutil.InReadOnlyTxValue(ctx, s.pool, "connection", func(tx pgx.Tx) (catalogPricingPreviewScope, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return catalogPricingPreviewScope{}, profileErr
		}
		offering, offerErr := resolveCatalogOffering(ctx, tx, profile.ID, requestBody)
		if offerErr != nil {
			return catalogPricingPreviewScope{}, offerErr
		}
		return catalogPricingPreviewScope{profileID: profile.ID, offering: offering}, nil
	})
}

func loadCatalogBindingForProfile(ctx context.Context, exec queryExecutor, profileID, modelConfigID int) (catalogBindingCoordinates, bool, error) {
	var binding catalogBindingCoordinates
	err := exec.QueryRow(ctx,
		`SELECT bindings.provider_id, bindings.catalog_model_id
		   FROM model_catalog_bindings AS bindings
		   JOIN model_configs AS configs ON configs.id = bindings.model_config_id
		  WHERE bindings.model_config_id = $1 AND configs.profile_id = $2`,
		modelConfigID, profileID).Scan(&binding.ProviderID, &binding.CatalogModelID)
	if err == pgx.ErrNoRows {
		return catalogBindingCoordinates{}, false, nil
	}
	if err != nil {
		return catalogBindingCoordinates{}, false, fmt.Errorf("load catalog binding for model %d: %w", modelConfigID, err)
	}
	return binding, true, nil
}
