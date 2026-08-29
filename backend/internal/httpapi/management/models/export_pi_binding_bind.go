package models

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// handleBindModelPi serves POST /api/models/{model_config_id}/pi/bind. A
// single exact compatible candidate may be applied implicitly; multiple
// candidates - even sharing one template - always require an explicit
// provider_id/catalog_model_id choice. Binding never selects by name, slug,
// lexical order, or host.
func (s *Service) handleBindModelPi(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requirePiCatalogClient(w, r) {
		return
	}
	var requestBody piBindRequest
	if err := decodeJSONBody(r, &requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	providerID := strings.TrimSpace(requestBody.ProviderID)
	catalogModelID := strings.TrimSpace(requestBody.CatalogModelID)
	expectedRevision := strings.TrimSpace(requestBody.ExpectedCatalogRevision)
	if expectedRevision == "" {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "expected_catalog_revision is required so stale previews cannot commit", map[string]any{"field": "expected_catalog_revision"}))
		return
	}
	if (providerID == "") != (catalogModelID == "") {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "provider_id and catalog_model_id must be provided together", map[string]any{"field": "provider_id"}))
		return
	}

	record, expectedAPI, err := s.loadModelForPi(r.Context(), r, modelConfigID)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	// Remote I/O happens here, outside the binding transaction below.
	catalog, err := s.piCatalog.Fetch(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), piCatalogFetchFailed(err))
		return
	}
	if catalog.Revision != expectedRevision {
		writeDomainError(w, r, s.corsSnapshot(), piCatalogStaleError(expectedRevision, catalog.Revision))
		return
	}

	candidates := catalog.Candidates(record.ModelID, expectedAPI)
	bindSource := piBindSourceManual
	if providerID != "" {
		found := false
		for _, candidate := range candidates {
			if candidate.ProviderID == providerID && candidate.ModelID == catalogModelID {
				found = true
				break
			}
		}
		if !found {
			writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "pi_candidate_unknown: the requested provider/model pair is not a current candidate for this model", map[string]any{"provider_id": providerID, "catalog_model_id": catalogModelID}))
			return
		}
	} else {
		switch len(candidates) {
		case 1:
			providerID, catalogModelID = candidates[0].ProviderID, candidates[0].ModelID
			bindSource = piBindSourceSingleCandidate
		case 0:
			status := piZeroCandidateStatus(catalog, record.ModelID)
			writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "pi_candidate_"+status+": no compatible pi.dev candidate exists for this model; bind explicitly once one exists", map[string]any{"reason": status}))
			return
		default:
			writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusConflict, "pi_candidate_ambiguous: multiple pi.dev candidates match this model id; bind explicitly", map[string]any{"reason": "ambiguous", "candidates": piCandidateWiresFromModels(candidates)}))
			return
		}
	}
	model, found := catalog.Find(providerID, catalogModelID)
	if !found {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusConflict, "pi_candidate_unknown: the candidate no longer resolves in the fetched catalog", nil))
		return
	}

	sourceMetadata := piBindingMetadataFromModel(model)
	now := s.nowUTC()
	response, err := s.bindPiInTransaction(r.Context(), r, modelConfigID, providerID, catalogModelID, expectedAPI, bindSource, catalog.Revision, catalog.FetchedAt, sourceMetadata, model.DroppedFields, now)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// bindPiInTransaction is the only database write phase of a bind. The
// catalog has already been fetched and matched before this transaction
// begins; this function never performs remote I/O.
func (s *Service) bindPiInTransaction(ctx context.Context, r *http.Request, modelConfigID int, providerID, catalogModelID, api, bindSource, catalogRevision string, fetchedAt time.Time, sourceMetadata piBindingMetadata, droppedFields []string, now time.Time) (piBindingResponse, error) {
	return pgxutil.InTxValue(ctx, s.pool, "model", func(tx pgx.Tx) (piBindingResponse, error) {
		profile, profileErr := resolveEffectiveProfile(ctx, tx, r)
		if profileErr != nil {
			return piBindingResponse{}, profileErr
		}
		currentModel, found, loadErr := loadModelRecord(ctx, tx, profile.ID, modelConfigID, true)
		if loadErr != nil {
			return piBindingResponse{}, loadErr
		}
		if !found {
			return piBindingResponse{}, newPiDomainError(http.StatusNotFound, "Model configuration not found", nil)
		}
		if currentModel.ModelID != catalogModelID || modelexport.PiAPIForModel(currentModel.APIFamily, currentModel.OpenAIAcceptedFormat) != api {
			return piBindingResponse{}, newPiDomainError(http.StatusConflict, "pi_model_changed: the Prism model id or final Pi API changed while the catalog was fetched; fetch source and bind again", nil)
		}
		existing, _, loadBindingErr := loadPiBindingForUpdate(ctx, tx, profile.ID, modelConfigID)
		if loadBindingErr != nil {
			return piBindingResponse{}, loadBindingErr
		}
		sameCoordinate := existing.ProviderID == providerID && existing.CatalogModelID == catalogModelID && existing.API == api
		if sameCoordinate {
			// Binding is an explicit freeze. Repeating bind for the same
			// coordinate is idempotent; only refresh may replace its source
			// snapshot, dropped-field evidence, or catalog revision.
			return existing.response(), nil
		}
		record := piBindingRecord{
			ModelConfigID:   modelConfigID,
			ProviderID:      providerID,
			CatalogModelID:  catalogModelID,
			API:             api,
			BindSource:      bindSource,
			CatalogRevision: catalogRevision,
			FetchedAt:       fetchedAt,
			UpdatedAt:       nextPiBindingUpdatedAt(existing.UpdatedAt, now),
			Source:          sourceMetadata,
			Override:        existing.Override,
			DroppedFields:   normalizePiDroppedFields(droppedFields),
		}
		// Rebinding to a different coordinate invalidates prior overrides: they
		// described another candidate's metadata.
		record.Override = piBindingMetadata{}
		if upsertErr := upsertPiBinding(ctx, tx, record, record.UpdatedAt); upsertErr != nil {
			return piBindingResponse{}, upsertErr
		}
		saved, _, saveErr := loadPiBinding(ctx, tx, profile.ID, modelConfigID)
		if saveErr != nil {
			return piBindingResponse{}, saveErr
		}
		return saved.response(), nil
	})
}
