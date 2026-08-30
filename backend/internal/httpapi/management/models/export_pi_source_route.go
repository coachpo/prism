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

// piCatalogForRead resolves the catalog a read-only management surface may
// publish. A successful fetch (including a 304 revalidation of the cached
// revision) is reported as fresh. A failed fetch may still answer from
// last-known-good, but only ever labelled stale: stale evidence is display
// material for source and directory search, and bind/refresh still require
// their own fresh fetch plus a matching revision before they write anything.
func (s *Service) piCatalogForRead(ctx context.Context) (*pidev.Catalog, string) {
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
