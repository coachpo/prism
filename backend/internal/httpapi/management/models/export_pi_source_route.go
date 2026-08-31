package models

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handleGetPiExportSource serves GET /api/models/exports/pi/source.
// One consistent DB snapshot plus one best-effort pi.dev fetch outside the transaction.
func (s *Service) handleGetPiExportSource(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	catalog, catalogStatus := s.piCatalogForRead(r.Context())
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "pi export source", func(tx pgx.Tx) (*piSourceResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		modelRows, targetRows, graph, err := loadExportSnapshot(r.Context(), tx, profile.ID)
		if err != nil {
			return nil, err
		}
		piBindings, err := loadPiBindingsForModels(r.Context(), tx, profile.ID, exportModelConfigIDs(modelRows))
		if err != nil {
			return nil, err
		}
		grouped := sortTargetRowsByModel(targetRows)
		facts, templates, err := buildPiSourceFacts(piExportFactsInput{
			ModelRows:     modelRows,
			TargetRows:    grouped,
			PiBindings:    piBindings,
			Catalog:       catalog,
			CatalogStatus: catalogStatus,
			Graph:         graph,
		})
		if err != nil {
			return nil, err
		}
		digest, err := modelexport.ComputeSourceDigest(facts)
		if err != nil {
			return nil, err
		}
		resp, err := assemblePiSourceResponse(facts, templates, catalogStatus, catalog, piBindings)
		if err != nil {
			return nil, err
		}
		resp.SourceDigest = digest
		return resp, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
