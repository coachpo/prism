package models

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
)

// handleBindModelPi serves POST /api/models/{model_config_id}/pi/bind.
//
// Two authoring paths share this route and never weaken each other:
//
//   - Default exact path (no provider_id/catalog_model_id): the unchanged
//     complete-Prism-model_id case-sensitive plus final-Pi-API candidate set
//     decides. Exactly one candidate binds automatically; zero rejects with
//     pi_candidate_not_in_catalog/pi_candidate_api_mismatch; more than one
//     rejects with 409 pi_candidate_ambiguous. Nothing is ever chosen by
//     fuzzy match, lexical order, provider preference, or template equality.
//   - Explicit directory path (both fields present): the operator names one
//     real pi.dev coordinate taken from the bounded directory search or from
//     the default candidate list. The only hard gate is that the coordinate
//     exists in the fetched revision and carries exactly the model's current
//     final Pi API, so a cross-directory model_id is allowed while a
//     cross-API coordinate never is.
//
// Both paths carry the caller's expected catalog revision plus the expected
// Prism full model id and final Pi API, and all three are re-verified before
// any write. Every rejection happens before the write transaction or aborts
// it, so a failed bind leaves the stored binding untouched.
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
	expectedPrismModelID := strings.TrimSpace(requestBody.ExpectedPrismModelID)
	expectedPiAPI := strings.TrimSpace(requestBody.ExpectedPiAPI)
	if expectedRevision == "" {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "expected_catalog_revision is required so stale previews cannot commit", map[string]any{"field": "expected_catalog_revision"}))
		return
	}
	if expectedPrismModelID == "" {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "expected_prism_model_id is required so a confirmed identity cannot commit against a drifted model", map[string]any{"field": "expected_prism_model_id"}))
		return
	}
	if expectedPiAPI == "" {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "expected_pi_api is required so a confirmed API cannot commit against a drifted model", map[string]any{"field": "expected_pi_api"}))
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
	if expectedPrismModelID != record.ModelID || expectedPiAPI != expectedAPI {
		writeDomainError(w, r, s.corsSnapshot(), piModelChangedError(expectedPrismModelID, record.ModelID, expectedPiAPI, expectedAPI))
		return
	}

	// Remote I/O happens here, outside the binding transaction below. A failed
	// or stale fetch never reaches a write, so a last-known-good catalog is
	// never committed as fresh binding evidence.
	catalog, err := s.piCatalog.Fetch(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), piCatalogFetchFailed(err))
		return
	}
	if catalog.Revision != expectedRevision {
		writeDomainError(w, r, s.corsSnapshot(), piCatalogStaleError(expectedRevision, catalog.Revision))
		return
	}

	bindSource := piBindSourceManual
	var model *pidev.Model
	if providerID != "" {
		found, exists := catalog.Find(providerID, catalogModelID)
		if exists && found.API != expectedAPI {
			writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "pi_candidate_api_mismatch: that directory coordinate is published for a different Pi API, so it cannot be bound to this model", map[string]any{
				"provider_id": providerID, "catalog_model_id": catalogModelID, "coordinate_pi_api": found.API, "expected_pi_api": expectedAPI,
			}))
			return
		}
		if !exists {
			writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "pi_candidate_unknown: the requested provider/model pair is not a real coordinate in the fetched pi.dev revision", map[string]any{"provider_id": providerID, "catalog_model_id": catalogModelID}))
			return
		}
		model = found
	} else {
		candidates := catalog.Candidates(record.ModelID, expectedAPI)
		switch len(candidates) {
		case 1:
			providerID, catalogModelID = candidates[0].ProviderID, candidates[0].ModelID
			bindSource = piBindSourceSingleCandidate
			model = candidates[0]
		case 0:
			status := piZeroCandidateStatus(catalog, record.ModelID)
			writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "pi_candidate_"+status+": no compatible pi.dev candidate exists for this model; search the directory and bind an explicit coordinate", map[string]any{"reason": status}))
			return
		default:
			writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusConflict, "pi_candidate_ambiguous: multiple pi.dev candidates match this model id; bind explicitly", map[string]any{"reason": "ambiguous", "candidates": piCandidateWiresFromModels(candidates)}))
			return
		}
	}

	sourceMetadata := piBindingMetadataFromModel(model)
	now := s.nowUTC()
	response, err := s.bindPiInTransaction(r.Context(), r, modelConfigID, providerID, catalogModelID, expectedAPI, expectedPrismModelID, bindSource, catalog.Revision, catalog.FetchedAt, sourceMetadata, model.DroppedFields, now)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// bindPiInTransaction is the only database write phase of a bind. The
// catalog has already been fetched and matched before this transaction
// begins; this function never performs remote I/O, and every guard below
// returns before any statement writes.
func (s *Service) bindPiInTransaction(ctx context.Context, r *http.Request, modelConfigID int, providerID, catalogModelID, api, expectedPrismModelID, bindSource, catalogRevision string, fetchedAt time.Time, sourceMetadata piBindingMetadata, droppedFields []string, now time.Time) (piBindingResponse, error) {
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
		currentAPI := modelexport.PiAPIForModel(currentModel.APIFamily, currentModel.OpenAIAcceptedFormat)
		if currentModel.ModelID != expectedPrismModelID || currentAPI != api {
			return piBindingResponse{}, piModelChangedError(expectedPrismModelID, currentModel.ModelID, api, currentAPI)
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
			if existing.PrismModelIDAtBind == currentModel.ModelID {
				return existing.response(), nil
			}
			// Same directory coordinate, newly confirmed Prism identity: the
			// operator is re-asserting this freeze against a renamed model, so
			// only the identity snapshot moves. Frozen source, overrides,
			// revision, fetched_at, bind source, and dropped evidence all stay;
			// updated_at advances because this is a real identity write and must
			// invalidate any refresh preview built against the old snapshot.
			existing.PrismModelIDAtBind = currentModel.ModelID
			existing.UpdatedAt = nextPiBindingUpdatedAt(existing.UpdatedAt, now)
			if upsertErr := upsertPiBinding(ctx, tx, existing, existing.UpdatedAt); upsertErr != nil {
				return piBindingResponse{}, upsertErr
			}
			saved, _, saveErr := loadPiBinding(ctx, tx, profile.ID, modelConfigID)
			if saveErr != nil {
				return piBindingResponse{}, saveErr
			}
			return saved.response(), nil
		}
		record := piBindingRecord{
			ModelConfigID:      modelConfigID,
			ProviderID:         providerID,
			CatalogModelID:     catalogModelID,
			API:                api,
			PrismModelIDAtBind: currentModel.ModelID,
			BindSource:         bindSource,
			CatalogRevision:    catalogRevision,
			FetchedAt:          fetchedAt,
			UpdatedAt:          nextPiBindingUpdatedAt(existing.UpdatedAt, now),
			Source:             sourceMetadata,
			Override:           existing.Override,
			DroppedFields:      normalizePiDroppedFields(droppedFields),
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

// piModelChangedError is the shared fail-closed identity/API drift rejection
// used both before the catalog fetch and inside the write transaction.
func piModelChangedError(expectedPrismModelID, currentPrismModelID, expectedAPI, currentAPI string) error {
	return newPiDomainError(http.StatusConflict, "pi_model_changed: the Prism model id or final Pi API changed after the preview was confirmed; fetch source and bind again", map[string]any{
		"expected_prism_model_id": expectedPrismModelID,
		"current_prism_model_id":  currentPrismModelID,
		"expected_pi_api":         expectedAPI,
		"current_pi_api":          currentAPI,
	})
}
