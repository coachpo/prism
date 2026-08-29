package models

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/pidev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handleGetPiExportSource serves GET /api/models/export/source.
// One consistent DB snapshot plus one best-effort pi.dev fetch outside the transaction.
func (s *Service) handleGetPiExportSource(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	catalog, catalogStatus := s.piCatalogForSource(r.Context())
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "pi export source", func(tx pgx.Tx) (*piSourceResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return nil, err
		}
		modelRows, targetRows, bindings, graph, err := loadExportSnapshot(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		piBindings, err := loadPiBindingsForModels(r.Context(), tx, profile.ID, exportModelConfigIDs(modelRows))
		if err != nil {
			return nil, err
		}
		grouped := sortTargetRowsByModel(targetRows)
		facts, candidates, err := buildPiSourceFacts(piExportFactsInput{
			ModelRows:     modelRows,
			TargetRows:    grouped,
			Bindings:      bindings,
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
		resp, err := assemblePiSourceResponse(facts, candidates, catalogStatus, catalog, piBindings)
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

func (s *Service) piCatalogForSource(ctx context.Context) (*pidev.Catalog, string) {
	if s.piCatalog == nil {
		return nil, "unavailable"
	}
	// Fetch does its own singleflight/ETag/timeout handling outside any DB transaction.
	cat, err := s.piCatalog.Fetch(ctx)
	if err == nil {
		return cat, "fresh"
	}
	if snap := s.piCatalog.Snapshot(); snap != nil {
		return snap, "stale"
	}
	return nil, "unavailable"
}

// piCatalogSnapshot backs render, which must stay network-free: it only ever reads the
// last-known-good snapshot and reports "stale" for it, since a live freshness check would
// require the Fetch this function deliberately never makes.
func (s *Service) piCatalogSnapshot() (*pidev.Catalog, string) {
	if s.piCatalog == nil {
		return nil, "unavailable"
	}
	if snap := s.piCatalog.Snapshot(); snap != nil {
		return snap, "stale"
	}
	return nil, "unavailable"
}
