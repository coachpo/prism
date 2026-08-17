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
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (statsdomain.CostSegmentPage, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.CostSegmentPage{}, err
		}
		limit, err := parsePositiveIntWithDefault(r, "limit", 50)
		if err != nil {
			return statsdomain.CostSegmentPage{}, err
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
	limit, err := parsePositiveIntWithDefault(r, "limit", 50)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) ([]statsdomain.EndpointModelStatistic, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		fromTime, err := parseOptionalTime(r, "from_time")
		if err != nil {
			return nil, err
		}
		toTime, err := parseOptionalTime(r, "to_time")
		if err != nil {
			return nil, err
		}
		preset := queryStringOrDefault(r, "preset", "1h")
		return statsdomain.GetEndpointModelStatistics(r.Context(), tx, statsdomain.EndpointModelStatisticsParams{ProfileID: profile.ID, EndpointID: endpointID, Preset: preset, FromTime: fromTime, ToTime: toTime}, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
