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

type catalogModelIdentity struct {
	apiFamily      string
	runtimeModelID string
}

func (s *Service) handleBindModelCatalog(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var requestBody modelCatalogBindRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	providerID := strings.TrimSpace(requestBody.ProviderID)
	catalogModelID := strings.TrimSpace(requestBody.CatalogModelID)
	expectedRevision := strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if expectedRevision == "" {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "expected_catalog_revision is required so stale previews cannot commit", map[string]any{"field": "expected_catalog_revision"}))
		return
	}
	if (providerID == "") != (catalogModelID == "") {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "provider_id and catalog_model_id must be provided together for manual binding", map[string]any{"field": "provider_id"}))
		return
	}

	identity, err := s.loadCatalogModelIdentity(r.Context(), r, modelConfigID)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	// Remote I/O happens here, outside the binding transaction below.
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if catalog.ETag != expectedRevision {
		writeDomainError(w, r, s.corsSnapshot(), catalogStaleError(expectedRevision, catalog.ETag))
		return
	}

	matchSource := catalogMatchSourceUnique
	if providerID != "" {
		matchSource = catalogMatchSourceManual
		model, found := catalog.Find(providerID, catalogModelID)
		if !found {
			s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "models_dev_offering_unknown: the requested provider/model pair does not exist in the catalog", map[string]any{"field": "provider_id", "provider_id": providerID, "catalog_model_id": catalogModelID}))
			return
		}
		_ = model
	} else {
		matches := modelsdev.ExactMatches(catalog, identity.apiFamily, identity.runtimeModelID)
		switch len(matches) {
		case 1:
			providerID, catalogModelID = matches[0].ProviderID, matches[0].ModelID
		case 0:
			s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusConflict, "models_dev_match_missing: no exact catalog id matches this model id; bind explicitly", map[string]any{"reason": "no_match", "candidates": []modelsdev.Candidate{}, "model_id": identity.runtimeModelID}))
			return
		default:
			s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusConflict, "models_dev_match_ambiguous: the model id matches multiple providers; bind explicitly", map[string]any{"reason": "ambiguous", "candidates": matches}))
			return
		}
	}

	sourceMetadata := catalogMetadataFromCoordinates(catalog, providerID, catalogModelID)
	now := s.nowUTC()
	response, err := s.bindModelCatalogInTransaction(r.Context(), r, modelConfigID, providerID, catalogModelID, matchSource, catalog, sourceMetadata, now)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) loadCatalogModelIdentity(ctx context.Context, r *http.Request, modelConfigID int) (catalogModelIdentity, error) {
	var identity catalogModelIdentity
	err := pgxutil.InTx(ctx, s.pool, "model", func(tx pgx.Tx) error {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return profileErr
		}
		record, recordErr := loadModelForCatalog(ctx, tx, profile.ID, modelConfigID)
		if recordErr != nil {
			return recordErr
		}
		identity.apiFamily, identity.runtimeModelID = record.APIFamily, record.ModelID
		return nil
	})
	return identity, err
}

// bindModelCatalogInTransaction is the only database write phase of a bind.
// The catalog has already been fetched and matched before this transaction
// begins; this function never performs remote I/O.
func (s *Service) bindModelCatalogInTransaction(ctx context.Context, r *http.Request, modelConfigID int, providerID, catalogModelID, matchSource string, catalog *modelsdev.Catalog, sourceMetadata modelCatalogMetadata, now time.Time) (modelCatalogResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return modelCatalogResponse{}, profileErr
		}
		if _, loadErr := loadModelForCatalog(ctx, tx, profile.ID, modelConfigID); loadErr != nil {
			return modelCatalogResponse{}, loadErr
		}
		existing, _, loadBindingErr := loadCatalogBinding(ctx, tx, profile.ID, modelConfigID)
		if loadBindingErr != nil {
			return modelCatalogResponse{}, loadBindingErr
		}
		sameOffering := existing.ProviderID == providerID && existing.CatalogModelID == catalogModelID
		record := catalogBindingRecord{
			ModelConfigID:   modelConfigID,
			ProviderID:      providerID,
			CatalogModelID:  catalogModelID,
			MatchSource:     matchSource,
			CatalogRevision: catalog.ETag,
			FetchedAt:       catalog.FetchedAt,
			UpdatedAt:       now,
			Source:          sourceMetadata,
			Override:        existing.Override,
		}
		// Rebinding to a different offering invalidates prior overrides: they
		// described another provider's metadata. Same-offering rebinds keep
		// both overrides and the original match_source.
		if !sameOffering {
			record.Override = modelCatalogMetadata{}
		} else if existing.MatchSource != "" {
			record.MatchSource = existing.MatchSource
		}
		if upsertErr := upsertCatalogBinding(ctx, tx, record, now); upsertErr != nil {
			return modelCatalogResponse{}, upsertErr
		}
		saved, _, saveErr := loadCatalogBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return modelCatalogResponse{}, saveErr
		}
		return catalogResponseFromBinding(saved), nil
	})
}

func catalogMetadataFromCoordinates(catalog *modelsdev.Catalog, providerID, catalogModelID string) modelCatalogMetadata {
	model, _ := catalog.Find(providerID, catalogModelID)
	if model == nil {
		return modelCatalogMetadata{}
	}
	return catalogMetadataFromModel(model)
}
