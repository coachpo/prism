package models

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handleGetModelPi serves GET /api/models/{model_config_id}/pi.
//
// This is the single-model Pi management read for the model detail panel.
// Control flow, in order: one best-effort pi.dev fetch outside any
// transaction (fresh, stale last-known-good, or unavailable), then one
// profile-scoped read-only transaction covering the model row and its
// persisted binding, then the pure projection. The route is read-only and
// planning-neutral: it never selects a candidate, writes a binding, or
// touches the runtime snapshot. It never loads export targets, pricing
// plans, source digests, credentials, or render results — a catalog outage
// degrades only the live evidence block and never manufactures an unbound
// binding.
func (s *Service) handleGetModelPi(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	catalog, catalogStatus := s.piCatalogForRead(r.Context())
	// Model identity and binding truth must come from one stable PostgreSQL
	// snapshot. READ COMMITTED could observe a rename/rebind between these two
	// statements and manufacture a model/binding pair that never coexisted.
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (piModelReadResponse, error) {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return piModelReadResponse{}, profileErr
		}
		record, found, loadErr := loadModelRecord(r.Context(), tx, profile.ID, modelConfigID, false)
		if loadErr != nil {
			return piModelReadResponse{}, loadErr
		}
		if !found {
			return piModelReadResponse{}, &domainError{StatusCode: http.StatusNotFound, Detail: "Model configuration not found"}
		}
		binding, _, bindingErr := loadPiBinding(r.Context(), tx, profile.ID, modelConfigID)
		if bindingErr != nil {
			return piModelReadResponse{}, bindingErr
		}
		expectedAPI := modelexport.PiAPIForModel(record.APIFamily, record.OpenAIAcceptedFormat)
		return buildPiModelReadResponse(record, expectedAPI, catalog, catalogStatus, binding), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
