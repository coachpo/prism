package stats

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

func (s *Service) handleEndpointTerminalTargetStatistics(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
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
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats terminal targets", func(tx pgx.Tx) (statsdomain.TerminalTargetStatisticsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.TerminalTargetStatisticsResponse{}, err
		}
		fromTime, err := parseOptionalTime(r, "from_time")
		if err != nil {
			return statsdomain.TerminalTargetStatisticsResponse{}, err
		}
		toTime, err := parseOptionalTime(r, "to_time")
		if err != nil {
			return statsdomain.TerminalTargetStatisticsResponse{}, err
		}
		return statsdomain.GetEndpointTerminalTargetStatistics(r.Context(), tx, statsdomain.TerminalTargetStatisticsParams{
			ProfileID:      profile.ID,
			EndpointID:     endpointID,
			Preset:         queryStringOrDefault(r, "preset", "1h"),
			FromTime:       fromTime,
			ToTime:         toTime,
			CostSegmentKey: strings.TrimSpace(r.URL.Query().Get("cost_segment_key")),
			Limit:          limit,
			Offset:         offset,
			ReferenceNow:   s.nowUTC(),
		})
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleObserveActivity(w http.ResponseWriter, r *http.Request) {
	token, bounds, err := s.resolveQueryContextFromRequest(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	coverage := statsdomain.CoverageFromQueryBounds(bounds, token.Domains["usage_request_events"])
	params := statsdomain.ActivityParams{Limit: parsePositiveQueryIntDefault(r, "limit", 20)}
	if raw := strings.TrimSpace(r.URL.Query().Get("before")); raw != "" {
		if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
			params.Before = &value
		}
	}
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats observe activity", func(tx pgx.Tx) (statsdomain.ActivityFeedResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.ActivityFeedResponse{}, err
		}
		if profile.ID != token.ProfileID {
			return statsdomain.ActivityFeedResponse{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "query_context scope mismatch"}
		}
		return statsdomain.LoadFinalizedActivity(r.Context(), tx, profile.ID, bounds, coverage, params, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
