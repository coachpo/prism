package stats

import (
	"net/http"
	"strings"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
	"github.com/jackc/pgx/v5"
)

type modelMetricsBatchRequest struct {
	ModelIDs           []string `json:"model_ids"`
	SummaryWindowHours int      `json:"summary_window_hours"`
	SpendingPreset     string   `json:"spending_preset"`
}

func (s *Service) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	profile, err := s.resolveEffectiveProfile(r.Context(), r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	referenceNow := s.nowUTC()
	params, err := parseStatsSummaryParams(r, profile.ID)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	params.ReferenceNow = referenceNow
	if matchesDashboardSummarySnapshotRequest(params, referenceNow) {
		snapshot, snapshotErr := s.loadOrBuildDashboardAggregateSnapshot(r.Context(), profile.ID, referenceNow)
		if snapshotErr != nil {
			writeDomainError(w, r, s.corsSnapshot(), snapshotErr)
			return
		}
		response := snapshot.StatsSummary24H
		if params.GroupBy != nil && strings.EqualFold(strings.TrimSpace(*params.GroupBy), "api_family") {
			response = snapshot.APIFamilySummary24H
		}
		responseutil.WriteJSON(w, http.StatusOK, response)
		return
	}
	response, err := statsdomain.GetStatsSummary(r.Context(), s.pool, params)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleModelMetrics(w http.ResponseWriter, r *http.Request) {
	body, err := decodeModelMetricsRequest(r)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (statsdomain.ModelMetricsBatchResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.ModelMetricsBatchResponse{}, err
		}
		if body.SummaryWindowHours <= 0 {
			body.SummaryWindowHours = 24
		}
		if strings.TrimSpace(body.SpendingPreset) == "" {
			body.SpendingPreset = "last_30_days"
		}
		return statsdomain.GetModelMetrics(r.Context(), tx, statsdomain.ModelMetricsParams{ProfileID: profile.ID, ModelIDs: body.ModelIDs, SummaryWindowHours: body.SummaryWindowHours, SpendingPreset: body.SpendingPreset, ReferenceNow: s.nowUTC()})
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleConnectionSuccessRates(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "from_time", "to_time"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) ([]statsdomain.ConnectionSuccessRate, error) {
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
		return statsdomain.GetConnectionSuccessRates(r.Context(), tx, statsdomain.ConnectionSuccessRateParams{ProfileID: profile.ID, FromTime: fromTime, ToTime: toTime, ReferenceNow: s.nowUTC()})
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleThroughput(w http.ResponseWriter, r *http.Request) {
	profile, err := s.resolveEffectiveProfile(r.Context(), r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	referenceNow := s.nowUTC()
	scope, err := statsdomain.NormalizeScope(r.URL.Query().Get("scope"))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	keys := make([]string, 0, len(r.URL.Query()))
	for key := range r.URL.Query() {
		keys = append(keys, key)
	}
	if err := statsdomain.ValidateScopeQueryKeys(scope, keys); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	toTime, err := parseOptionalTime(r, "to_time")
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	endpointID, err := parseOptionalInt(r, "endpoint_id")
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	connectionID, err := parseOptionalInt(r, "terminal_target_id")
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	params := statsdomain.ThroughputParams{
		ProfileID: profile.ID, FromTime: fromTime, ToTime: toTime, Preset: queryStringOrDefault(r, "preset", "24h"), ReferenceNow: referenceNow,
		IngressModelID: normalizedQueryString(r, "ingress_model_id"), FinalTargetModelID: normalizedQueryString(r, "final_target_model_id"), AttemptTargetModelID: normalizedQueryString(r, "attempt_target_model_id"),
		APIFamily: normalizedQueryString(r, "api_family"), EndpointID: endpointID, ConnectionID: connectionID, Scope: scope,
	}
	if matchesDashboardThroughputSnapshotRequest(params, referenceNow) {
		snapshot, snapshotErr := s.loadOrBuildDashboardAggregateSnapshot(r.Context(), profile.ID, referenceNow)
		if snapshotErr != nil {
			writeDomainError(w, r, s.corsSnapshot(), snapshotErr)
			return
		}
		responseutil.WriteJSON(w, http.StatusOK, snapshot.Throughput24H)
		return
	}
	response, err := statsdomain.GetThroughput(r.Context(), s.pool, params)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleSpending(w http.ResponseWriter, r *http.Request) {
	scope, err := statsdomain.NormalizeScope(r.URL.Query().Get("scope"))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	keys := make([]string, 0, len(r.URL.Query()))
	for key := range r.URL.Query() {
		keys = append(keys, key)
	}
	if err := statsdomain.ValidateScopeQueryKeys(scope, keys); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (statsdomain.SpendingReportResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		fromTime, err := parseOptionalTime(r, "from_time")
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		toTime, err := parseOptionalTime(r, "to_time")
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		endpointID, err := parseOptionalInt(r, "endpoint_id")
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		connectionID, err := parseOptionalInt(r, "terminal_target_id")
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		limit, err := parsePositiveIntWithDefault(r, "limit", 50)
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		offset, err := parseNonNegativeIntWithDefault(r, "offset", 0)
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		topN, err := parsePositiveIntWithDefault(r, "top_n", 5)
		if err != nil {
			return statsdomain.SpendingReportResponse{}, err
		}
		return statsdomain.GetSpending(r.Context(), tx, statsdomain.SpendingParams{ProfileID: profile.ID, Preset: queryStringOrDefault(r, "preset", "24h"), FromTime: fromTime, ToTime: toTime, APIFamily: normalizedQueryString(r, "api_family"), IngressModelID: normalizedQueryString(r, "ingress_model_id"), FinalTargetModelID: normalizedQueryString(r, "final_target_model_id"), EndpointID: endpointID, ConnectionID: connectionID, GroupBy: queryStringOrDefault(r, "group_by", "none"), Limit: limit, Offset: offset, TopN: topN, ReferenceNow: s.nowUTC(), Scope: scope})
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleUsageSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "preset"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	profile, err := s.resolveEffectiveProfile(r.Context(), r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	preset := queryStringOrDefault(r, "preset", "1h")
	referenceNow := s.nowUTC()
	if matchesDashboardUsageSnapshotRequest(preset) {
		snapshot, snapshotErr := s.loadOrBuildDashboardAggregateSnapshot(r.Context(), profile.ID, referenceNow)
		if snapshotErr != nil {
			writeDomainError(w, r, s.corsSnapshot(), snapshotErr)
			return
		}
		responseutil.WriteJSON(w, http.StatusOK, snapshot.UsageSnapshotPreset1)
		return
	}
	response, err := statsdomain.GetUsageSnapshot(r.Context(), s.pool, profile.ID, preset, referenceNow)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}
