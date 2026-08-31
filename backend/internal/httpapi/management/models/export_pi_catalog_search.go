package models

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/pidev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
)

// maxPiSearchQueryLength bounds the literal model-id fragment a caller may
// submit. pi.dev model ids are far shorter than this, so the cap only stops a
// request from turning the search into an unbounded text probe.
const maxPiSearchQueryLength = 200

// handleSearchPiCatalog serves POST /api/models/{model_config_id}/pi/search.
//
// This is the single directory-discovery entry for every model whose final Pi
// API is determinable, including models the default exact-id search reports as
// not_in_catalog or api_mismatch. Prism's backend is the only reader of the
// trusted pi.dev catalog: the browser sends a model-id fragment and receives a
// bounded page of same-API coordinates with their safe evidence, so no
// pi.dev request ever leaves the browser.
//
// The route writes nothing. It reads the model row in one read-only
// transaction, then performs the catalog fetch outside any transaction, and
// every failure path returns before a statement could persist.
func (s *Service) handleSearchPiCatalog(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requirePiCatalogClient(w, r) {
		return
	}
	var requestBody piCatalogSearchRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&requestBody); err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "Invalid request body")
		return
	}
	query := strings.TrimSpace(requestBody.ModelIDQuery)
	if query == "" {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "model_id_query is required: the directory search is model-id-only", map[string]any{"field": "model_id_query"}))
		return
	}
	if len(query) > maxPiSearchQueryLength {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "model_id_query is too long", map[string]any{"field": "model_id_query", "max_length": maxPiSearchQueryLength}))
		return
	}
	limit := requestBody.Limit
	switch {
	case limit == 0:
		limit = pidev.SearchDefaultLimit
	case limit < 0:
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "limit must be a positive whole number", map[string]any{"field": "limit"}))
		return
	case limit > pidev.SearchMaxLimit:
		limit = pidev.SearchMaxLimit
	}
	if requestBody.Offset < 0 {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusUnprocessableEntity, "offset must be a non-negative whole number", map[string]any{"field": "offset"}))
		return
	}

	record, expectedAPI, err := s.loadModelForPi(r.Context(), r, modelConfigID)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}

	catalog, catalogStatus := s.piCatalogForRead(r.Context())
	if catalog == nil {
		writeDomainError(w, r, s.corsSnapshot(), newPiDomainError(http.StatusServiceUnavailable, "pi_catalog_unavailable: no fetched or last-known-good pi.dev catalog is available to search", nil))
		return
	}
	page, total := catalog.SearchModelIDs(query, expectedAPI, limit, requestBody.Offset)
	responseutil.WriteJSON(w, http.StatusOK, piCatalogSearchResponse{
		Query:     query,
		API:       expectedAPI,
		Limit:     limit,
		Offset:    requestBody.Offset,
		Total:     total,
		Returned:  len(page),
		Truncated: requestBody.Offset+len(page) < total,
		Selected:  false,
		Catalog: piCatalogWire{
			Revision:       catalog.Revision,
			Status:         catalogStatus,
			MinimumVersion: catalog.MinimumVersion,
			ETag:           catalog.ETag,
		},
		FetchedAt: catalog.FetchedAt,
		// CheckedAt is when this revision was last revalidated (304 included);
		// FetchedAt is when the content was originally fetched. Keeping both
		// visible stops a revalidation from looking like a fresh download.
		CheckedAt: catalog.CheckedAt,
		ExportIdentity: piExportIdentityWire{
			ModelConfigID:    record.ID,
			ModelID:          record.ModelID,
			API:              expectedAPI,
			ProviderIDSource: "operator_input",
		},
		Results: piCandidateWiresFromModels(page),
	})
}
