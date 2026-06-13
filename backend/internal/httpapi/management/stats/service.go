package stats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/config"
	platformcors "github.com/coachpo/prism/backend/internal/platform/cors"
	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type Options struct {
	CORSOriginProvider platformcors.OriginProvider
	Pool               *pgxpool.Pool
	Now                func() time.Time
	DashboardSnapshots *statsdomain.DashboardAggregateStore
	SideEffects        *managementsideeffects.Dispatcher
}

type Service struct {
	pool               *pgxpool.Pool
	ownsPool           bool
	now                func() time.Time
	corsOriginProvider platformcors.OriginProvider
	dashboardSnapshots *statsdomain.DashboardAggregateStore
	sideEffects        *managementsideeffects.Dispatcher
}

type modelMetricsBatchRequest struct {
	ModelIDs           []string `json:"model_ids"`
	SummaryWindowHours int      `json:"summary_window_hours"`
	SpendingPreset     string   `json:"spending_preset"`
}

func NewService(settings config.Settings, options Options) (*Service, error) {
	pool := options.Pool
	ownsPool := false
	if pool == nil {
		return nil, fmt.Errorf("stats database pool is required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	corsOriginProvider := options.CORSOriginProvider
	if corsOriginProvider == nil {
		corsOriginProvider = platformcors.NewStaticOriginProvider(settings.CORSAllowedOriginsList())
	}
	dashboardSnapshots := options.DashboardSnapshots
	if dashboardSnapshots == nil {
		dashboardSnapshots = statsdomain.NewDashboardAggregateStore()
	}
	service := &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider, dashboardSnapshots: dashboardSnapshots, sideEffects: options.SideEffects}
	if service.sideEffects != nil {
		service.sideEffects.RegisterHandler(managementsideeffects.EventDashboardSnapshotInvalidate, service.handleDashboardSnapshotInvalidation)
	}
	return service, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

const dashboardSnapshotWindowTolerance = 2 * time.Minute

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
}

func (s *Service) resolveEffectiveProfile(ctx context.Context, request *http.Request) (profiledomain.Profile, error) {
	return profiledomain.ResolveEffectiveProfile(ctx, s.pool, request.Header.Get(profiledomain.ProfileIDHeader))
}

func (s *Service) loadOrBuildDashboardAggregateSnapshot(ctx context.Context, profileID int, referenceNow time.Time) (statsdomain.DashboardAggregateSnapshot, error) {
	if snapshot, ok := s.dashboardSnapshots.LoadFreshProfile(profileID, func(snapshot statsdomain.DashboardAggregateSnapshot) bool {
		return dashboardAggregateSnapshotFresh(snapshot, referenceNow)
	}); ok {
		return snapshot, nil
	}
	return pgxutil.InReadOnlyTxValue(ctx, s.pool, "stats dashboard snapshot", func(tx pgx.Tx) (statsdomain.DashboardAggregateSnapshot, error) {
		return s.loadOrBuildDashboardAggregateSnapshotInTx(ctx, tx, profileID, referenceNow)
	})
}

func (s *Service) loadOrBuildDashboardAggregateSnapshotInTx(ctx context.Context, tx pgx.Tx, profileID int, referenceNow time.Time) (statsdomain.DashboardAggregateSnapshot, error) {
	if snapshot, ok := s.dashboardSnapshots.LoadFreshProfile(profileID, func(snapshot statsdomain.DashboardAggregateSnapshot) bool {
		return dashboardAggregateSnapshotFresh(snapshot, referenceNow)
	}); ok {
		return snapshot, nil
	}
	snapshot, err := statsdomain.BuildDashboardAggregateSnapshot(ctx, tx, profileID, referenceNow)
	if err != nil {
		return statsdomain.DashboardAggregateSnapshot{}, err
	}
	s.dashboardSnapshots.StoreProfile(snapshot)
	return snapshot, nil
}

func dashboardAggregateSnapshotFresh(snapshot statsdomain.DashboardAggregateSnapshot, referenceNow time.Time) bool {
	return !statsdomain.NewDashboardSnapshotHealth(snapshot.GeneratedAt, referenceNow).Stale
}

func (s *Service) InvalidateDashboardSnapshot(profileID int) {
	s.evictDashboardAggregateSnapshot(profileID)
}

func (s *Service) InvalidateAllDashboardSnapshots() {
	if s == nil || s.dashboardSnapshots == nil {
		return
	}
	s.dashboardSnapshots.InvalidateAll()
}

func (s *Service) evictDashboardAggregateSnapshot(profileID int) {
	if s == nil || s.dashboardSnapshots == nil {
		return
	}
	s.dashboardSnapshots.InvalidateProfile(profileID)
}

func (s *Service) handleDashboardSnapshotInvalidation(_ context.Context, event managementsideeffects.Event) error {
	var payload managementsideeffects.DashboardSnapshotInvalidatePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return managementsideeffects.PermanentError{Err: fmt.Errorf("decode dashboard snapshot invalidation payload: %w", err)}
	}
	if payload.ProfileID <= 0 {
		return managementsideeffects.PermanentError{Err: fmt.Errorf("dashboard snapshot invalidation profile_id required")}
	}
	s.evictDashboardAggregateSnapshot(payload.ProfileID)
	return nil
}

func matchesDashboardSummarySnapshotRequest(params statsdomain.StatsSummaryParams, referenceNow time.Time) bool {
	if params.ModelID != nil || params.APIFamily != nil || params.EndpointID != nil || params.ConnectionID != nil {
		return false
	}
	if !matchesDashboardSummaryWindow(params.FromTime, params.ToTime, referenceNow) {
		return false
	}
	if params.GroupBy == nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(*params.GroupBy), "api_family")
}

func matchesDashboardSummaryWindow(fromTime *time.Time, toTime *time.Time, referenceNow time.Time) bool {
	if fromTime == nil || toTime != nil {
		return false
	}
	window := referenceNow.UTC().Sub(fromTime.UTC())
	return withinDashboardTolerance(window, 24*time.Hour)
}

func matchesDashboardThroughputSnapshotRequest(params statsdomain.ThroughputParams, referenceNow time.Time) bool {
	if params.ModelID != nil || params.APIFamily != nil || params.EndpointID != nil || params.ConnectionID != nil {
		return false
	}
	if params.FromTime == nil || params.ToTime == nil {
		return false
	}
	if !withinDashboardTolerance(params.ToTime.UTC().Sub(params.FromTime.UTC()), 24*time.Hour) {
		return false
	}
	return absDuration(referenceNow.UTC().Sub(params.ToTime.UTC())) <= dashboardSnapshotWindowTolerance
}

func matchesDashboardUsageSnapshotRequest(preset string) bool {
	return strings.EqualFold(strings.TrimSpace(preset), "1h")
}

func withinDashboardTolerance(actual time.Duration, expected time.Duration) bool {
	return absDuration(actual-expected) <= dashboardSnapshotWindowTolerance
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func (s *Service) corsSnapshot() platformcors.Snapshot {
	if s == nil || s.corsOriginProvider == nil {
		return platformcors.Snapshot{}
	}
	return s.corsOriginProvider.CORSSnapshot()
}

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/stats", func(router chi.Router) {
		router.Get("/dashboard", s.handleDashboardStats)
		router.Get("/requests", s.handleListRequestLogs)
		router.Get("/requests/{request_id}", s.handleGetRequestLog)
		router.Get("/summary", s.handleStatsSummary)
		router.Post("/models/metrics", s.handleModelMetrics)
		router.Get("/connection-success-rates", s.handleConnectionSuccessRates)
		router.Get("/throughput", s.handleThroughput)
		router.Get("/spending", s.handleSpending)
		router.Get("/usage-snapshot", s.handleUsageSnapshot)
		router.Get("/endpoints/{endpoint_id}/models", s.handleEndpointModelStatistics)
	})
}

func (s *Service) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	referenceNow := s.nowUTC()
	snapshot, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats dashboard", func(tx pgx.Tx) (statsdomain.DashboardSnapshot, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.DashboardSnapshot{}, err
		}
		aggregate, err := s.loadOrBuildDashboardAggregateSnapshotInTx(r.Context(), tx, profile.ID, referenceNow)
		if err != nil {
			return statsdomain.DashboardSnapshot{}, err
		}
		return statsdomain.NewDashboardSnapshot(aggregate, referenceNow), nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Service) handleListRequestLogs(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (statsdomain.RequestLogListResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.RequestLogListResponse{}, err
		}
		params, err := parseRequestLogListParams(r, profile.ID)
		if err != nil {
			return statsdomain.RequestLogListResponse{}, err
		}
		return statsdomain.ListRequestLogs(r.Context(), tx, params)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetRequestLog(w http.ResponseWriter, r *http.Request) {
	requestID, err := routeInt(r, "request_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
		return
	}
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (*statsdomain.RequestLogDetailResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		return statsdomain.GetRequestLogDetail(r.Context(), tx, profile.ID, requestID)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if response == nil {
		writeError(w, r, s.corsSnapshot(), http.StatusNotFound, "Request log not found")
		return
	}
	writeJSON(w, http.StatusOK, response)
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
		writeJSON(w, http.StatusOK, response)
		return
	}
	response, err := statsdomain.GetStatsSummary(r.Context(), s.pool, params)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleModelMetrics(w http.ResponseWriter, r *http.Request) {
	body, err := decodeModelMetricsRequest(r)
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleConnectionSuccessRates(w http.ResponseWriter, r *http.Request) {
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
		return statsdomain.GetConnectionSuccessRates(r.Context(), tx, statsdomain.ConnectionSuccessRateParams{ProfileID: profile.ID, FromTime: fromTime, ToTime: toTime})
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleThroughput(w http.ResponseWriter, r *http.Request) {
	profile, err := s.resolveEffectiveProfile(r.Context(), r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	referenceNow := s.nowUTC()
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
	connectionID, err := parseOptionalInt(r, "connection_id")
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	params := statsdomain.ThroughputParams{
		ProfileID:    profile.ID,
		FromTime:     fromTime,
		ToTime:       toTime,
		ModelID:      normalizedQueryString(r, "model_id"),
		APIFamily:    normalizedQueryString(r, "api_family"),
		EndpointID:   endpointID,
		ConnectionID: connectionID,
	}
	if matchesDashboardThroughputSnapshotRequest(params, referenceNow) {
		snapshot, snapshotErr := s.loadOrBuildDashboardAggregateSnapshot(r.Context(), profile.ID, referenceNow)
		if snapshotErr != nil {
			writeDomainError(w, r, s.corsSnapshot(), snapshotErr)
			return
		}
		writeJSON(w, http.StatusOK, snapshot.Throughput24H)
		return
	}
	response, err := statsdomain.GetThroughput(r.Context(), s.pool, params)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleSpending(w http.ResponseWriter, r *http.Request) {
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
		connectionID, err := parseOptionalInt(r, "connection_id")
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
		return statsdomain.GetSpending(r.Context(), tx, statsdomain.SpendingParams{ProfileID: profile.ID, Preset: queryStringOrDefault(r, "preset", ""), FromTime: fromTime, ToTime: toTime, APIFamily: normalizedQueryString(r, "api_family"), ModelID: normalizedQueryString(r, "model_id"), EndpointID: endpointID, ConnectionID: connectionID, GroupBy: queryStringOrDefault(r, "group_by", "none"), Limit: limit, Offset: offset, TopN: topN, ReferenceNow: s.nowUTC()})
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleUsageSnapshot(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, snapshot.UsageSnapshotPreset1)
		return
	}
	response, err := statsdomain.GetUsageSnapshot(r.Context(), s.pool, profile.ID, preset, referenceNow)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleEndpointModelStatistics(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		writeError(w, r, s.corsSnapshot(), http.StatusBadRequest, err.Error())
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
	writeJSON(w, http.StatusOK, response)
}

func parseRequestLogListParams(r *http.Request, profileID int) (statsdomain.RequestLogListParams, error) {
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	endpointID, err := parseOptionalInt(r, "endpoint_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	clientRuleID, err := parseOptionalInt(r, "client_rule_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid client_rule_id"}
	}
	if clientRuleID != nil && *clientRuleID <= 0 {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid client_rule_id"}
	}
	limit, err := parsePositiveIntWithDefault(r, "limit", 50)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	offset, err := parseNonNegativeIntWithDefault(r, "offset", 0)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	statusFamily := normalizedQueryString(r, "status_family")
	if statusFamily != nil {
		normalized := strings.ToLower(strings.TrimSpace(*statusFamily))
		statusFamily = &normalized
	}
	return statsdomain.RequestLogListParams{ProfileID: profileID, IngressRequestID: normalizedQueryString(r, "ingress_request_id"), ModelID: normalizedQueryString(r, "model_id"), ResolvedTargetModelID: normalizedQueryString(r, "resolved_target_model_id"), StatusFamily: statusFamily, FromTime: fromTime, EndpointID: endpointID, ClientRuleID: clientRuleID, Limit: limit, Offset: offset}, nil
}

func parseStatsSummaryParams(r *http.Request, profileID int) (statsdomain.StatsSummaryParams, error) {
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	toTime, err := parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	endpointID, err := parseOptionalInt(r, "endpoint_id")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	connectionID, err := parseOptionalInt(r, "connection_id")
	if err != nil {
		return statsdomain.StatsSummaryParams{}, err
	}
	groupBy := normalizedQueryString(r, "group_by")
	return statsdomain.StatsSummaryParams{ProfileID: profileID, FromTime: fromTime, ToTime: toTime, GroupBy: groupBy, ModelID: normalizedQueryString(r, "model_id"), APIFamily: normalizedQueryString(r, "api_family"), EndpointID: endpointID, ConnectionID: connectionID}, nil
}

func decodeModelMetricsRequest(r *http.Request) (modelMetricsBatchRequest, error) {
	defer func() { _ = r.Body.Close() }()
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var requestBody modelMetricsBatchRequest
	if err := decoder.Decode(&requestBody); err != nil {
		return modelMetricsBatchRequest{}, err
	}
	return requestBody, nil
}

func parseOptionalTime(r *http.Request, key string) (*time.Time, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, fmt.Errorf("invalid %s", key)
		}
	}
	resolved := parsed.UTC()
	return &resolved, nil
}

func parseOptionalInt(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s", key)
	}
	resolved := parsed
	return &resolved, nil
}

func parsePositiveIntWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalInt(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	if *parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return *parsed, nil
}

func parseNonNegativeIntWithDefault(r *http.Request, key string, defaultValue int) (int, error) {
	parsed, err := parseOptionalInt(r, key)
	if err != nil {
		return 0, err
	}
	if parsed == nil {
		return defaultValue, nil
	}
	if *parsed < 0 {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return *parsed, nil
}

func normalizedQueryString(r *http.Request, key string) *string {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}
	return &raw
}

func queryStringOrDefault(r *http.Request, key string, defaultValue string) string {
	if value := strings.TrimSpace(r.URL.Query().Get(key)); value != "" {
		return value
	}
	return defaultValue
}

func routeInt(r *http.Request, name string) (int, error) {
	raw := strings.TrimSpace(chi.URLParam(r, name))
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("invalid %s", name)
	}
	return parsed, nil
}

func writeDomainError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err error) {
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		responseutil.WriteProfileHTTPError(w, r, corsSnapshot, profileErr)
		return
	}
	var statsErr *statsdomain.HTTPError
	if errors.As(err, &statsErr) {
		if statsErr.Code != "" {
			writeStructuredError(w, r, corsSnapshot, statsErr)
			return
		}
		writeError(w, r, corsSnapshot, statsErr.StatusCode, statsErr.Detail)
		return
	}
	writeError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, statusCode int, detail string) {
	writeCORSHeaders(w, r, corsSnapshot)
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}

func writeStructuredError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *statsdomain.HTTPError) {
	writeCORSHeaders(w, r, corsSnapshot)
	payload := map[string]any{"error": map[string]any{"code": err.Code, "message": err.Detail}}
	if len(err.Details) > 0 {
		payload["error"].(map[string]any)["details"] = err.Details
	}
	writeJSON(w, err.StatusCode, payload)
}

func writeCORSHeaders(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot) {
	platformcors.ApplyAllowOriginHeaders(w, r, corsSnapshot)
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
