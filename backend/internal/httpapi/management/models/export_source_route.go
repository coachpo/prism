package models

import (
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

// handleGetExportSource serves GET /api/models/exports/{platform}/source.
// One read-only consistent snapshot backs the whole response; models.dev data
// comes from the freshest in-memory snapshot after one best-effort
// revalidation outside any transaction, and a failed fetch or vanished
// offering degrades to stored-only enrichment without failing the read.
func (s *Service) handleGetExportSource(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	platform, ok := parseExportPlatform(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	catalog := s.exportCatalog(r)
	response, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "model export source", func(tx pgx.Tx) (*exportSourceResponse, error) {
		if _, err := resolveEffectiveProfile(r.Context(), tx, r); err != nil {
			return nil, err
		}
		modelRows, targetRows, bindings, graph, err := loadExportSnapshot(r.Context(), tx)
		if err != nil {
			return nil, err
		}
		facts, candidates := buildSourceFacts(platform, exportFactsInput{
			ModelRows:  modelRows,
			TargetRows: sortTargetRowsByModel(targetRows),
			Bindings:   bindings,
			Catalog:    catalog,
			Graph:      graph,
		})
		digest, err := modelexport.ComputeSourceDigest(facts)
		if err != nil {
			return nil, err
		}
		source, err := assembleSourceResponse(platform, facts, candidates)
		if err != nil {
			return nil, err
		}
		source.SourceDigest = digest
		return source, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// exportCatalog returns only a freshly revalidated snapshot. A failed fetch
// never re-labels the stale cache as current enrichment.
func (s *Service) exportCatalog(r *http.Request) *modelsdev.Catalog {
	if s.catalog == nil {
		return nil
	}
	if catalog, err := s.catalog.Fetch(r.Context()); err == nil {
		return catalog
	}
	return nil
}
