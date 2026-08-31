package models

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

func (s *Service) handleGetModelCatalog(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "model", func(tx pgx.Tx) (modelCatalogResponse, error) {
		profile, err := resolveEffectiveProfile(r.Context(), tx, r)
		if err != nil {
			return modelCatalogResponse{}, err
		}
		record, err := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return modelCatalogResponse{}, err
		}
		binding, _, err := loadCatalogBinding(r.Context(), tx, profile.ID, modelConfigID)
		if err != nil {
			return modelCatalogResponse{}, err
		}
		payload := catalogResponseFromBinding(binding)
		if !payload.Bound && s.catalog != nil {
			if snapshot := s.catalog.Snapshot(); snapshot != nil {
				payload.AutoMatch = autoMatchHint(snapshot, record.APIFamily, record.ModelID)
			}
		}
		return payload, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetCatalogCandidates(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))
	if scope == "" {
		scope = "family"
	}
	if scope != "family" && scope != "all" {
		s.writeCatalogDomainError(w, r, newCatalogDomainError(http.StatusUnprocessableEntity, "scope must be family or all", nil))
		return
	}
	limit := parseBoundedLimit(r.URL.Query().Get("limit"))
	offset, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
	if err != nil || offset < 0 {
		offset = 0
	}
	var apiFamily string
	listErr := pgxutil.InTx(r.Context(), s.pool, "model", func(tx pgx.Tx) error {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return profileErr
		}
		record, recordErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID)
		if recordErr != nil {
			return recordErr
		}
		apiFamily = record.APIFamily
		return nil
	})
	if listErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), listErr)
		return
	}
	catalog, err := s.currentCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	items, total := modelsdev.SearchCandidates(catalog, apiFamily, query, scope, limit, offset)
	candidatesResponse := modelCatalogCandidatesResponse{
		Items: items, Total: total, Limit: limit, Offset: offset, Scope: scope, Query: query,
		// Every page publishes the snapshot it was computed from, so the
		// operator can see which catalog evidence a candidate list carries.
		CatalogRevision: catalog.ETag,
	}
	if !catalog.FetchedAt.IsZero() {
		fetchedAt := catalog.FetchedAt
		candidatesResponse.FetchedAt = &fetchedAt
	}
	responseutil.WriteJSON(w, http.StatusOK, candidatesResponse)
}

func (s *Service) handleMatchCatalogPreview(w http.ResponseWriter, r *http.Request) {
	modelConfigID, ok := routeIntOrBadRequest(w, r, s.corsSnapshot())
	if !ok {
		return
	}
	if !s.requireCatalogClient(w, r) {
		return
	}
	var apiFamily, modelID string
	loadErr := pgxutil.InTx(r.Context(), s.pool, "model", func(tx pgx.Tx) error {
		profile, profileErr := resolveEffectiveProfile(r.Context(), tx, r)
		if profileErr != nil {
			return profileErr
		}
		record, recordErr := loadModelForCatalog(r.Context(), tx, profile.ID, modelConfigID)
		if recordErr != nil {
			return recordErr
		}
		apiFamily, modelID = record.APIFamily, record.ModelID
		return nil
	})
	if loadErr != nil {
		writeDomainError(w, r, s.corsSnapshot(), loadErr)
		return
	}
	catalog, err := s.fetchValidatedCatalog(r.Context())
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	matches := modelsdev.ExactMatches(catalog, apiFamily, modelID)
	preview := modelCatalogMatchPreviewResponse{
		Candidates:      matches,
		CatalogRevision: catalog.ETag,
		FetchedAt:       catalog.FetchedAt,
	}
	switch len(matches) {
	case 1:
		preview.Committable = true
		preview.ProviderID = matches[0].ProviderID
		preview.CatalogModelID = matches[0].ModelID
		preview.Reason = "unique_match"
	case 0:
		preview.Reason = "no_match"
	default:
		preview.Reason = "ambiguous"
	}
	responseutil.WriteJSON(w, http.StatusOK, preview)
}

func parseBoundedLimit(raw string) int {
	limit, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}
