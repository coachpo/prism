package stats

import (
	"net/http"
	"strings"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func (s *Service) handleCostSegments(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "limit", "cursor"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats cost segments", func(tx pgx.Tx) (statsdomain.CostSegmentPage, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.CostSegmentPage{}, err
		}
		limit, err := parsePositiveIntWithDefault(r, "limit", 50)
		if err != nil {
			return statsdomain.CostSegmentPage{}, err
		}
		if limit > 100 {
			return statsdomain.CostSegmentPage{}, invalidQueryParameter("limit", "must be within [1, 100]")
		}
		params := statsdomain.CostSegmentParams{ProfileID: profile.ID, Limit: limit, Cursor: normalizedQueryString(r, "cursor")}
		cursorSigningKey := statsdomain.DeriveCostSegmentCursorSigningKey(s.secretEncryptionKey)
		return statsdomain.ListCostSegments(r.Context(), tx, params, cursorSigningKey)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleCostSegmentSymbols(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "limit", "offset"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	limit, err := parsePositiveIntWithDefault(r, "limit", 50)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	if limit > 100 {
		writeDomainError(w, r, s.corsSnapshot(), invalidQueryParameter("limit", "must be within [1, 100]"))
		return
	}
	offset, err := parseNonNegativeIntWithDefault(r, "offset", 0)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	segmentKey := strings.TrimSpace(chi.URLParam(r, "segment_key"))
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "cost segment symbols", func(tx pgx.Tx) (statsdomain.CostSegmentSymbolsPage, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.CostSegmentSymbolsPage{}, err
		}
		return statsdomain.ListCostSegmentSymbols(r.Context(), tx, statsdomain.CostSegmentSymbolsParams{
			ProfileID: profile.ID, SegmentKey: segmentKey, Limit: limit, Offset: offset,
		})
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleEndpointModelStatistics(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "scope", "preset", "from_time", "to_time"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	scope, err := statsdomain.NormalizeScope(queryStringOrDefault(r, "scope", statsdomain.ScopeFinal))
	if err != nil || scope == statsdomain.ScopeIngress {
		if err == nil {
			err = &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "scope_invalid", Detail: "endpoint model statistics support final_execution or route_attempt"}
		}
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats endpoint model statistics", func(tx pgx.Tx) (statsdomain.EndpointModelStatisticsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.EndpointModelStatisticsResponse{}, err
		}
		fromTime, err := parseOptionalTime(r, "from_time")
		if err != nil {
			return statsdomain.EndpointModelStatisticsResponse{}, err
		}
		toTime, err := parseOptionalTime(r, "to_time")
		if err != nil {
			return statsdomain.EndpointModelStatisticsResponse{}, err
		}
		preset := queryStringOrDefault(r, "preset", "1h")
		return statsdomain.GetEndpointModelStatistics(r.Context(), tx, statsdomain.EndpointModelStatisticsParams{ProfileID: profile.ID, EndpointID: endpointID, Preset: preset, FromTime: fromTime, ToTime: toTime, Scope: scope, ReferenceNow: s.nowUTC()}, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
