package stats

import (
	"context"
	"database/sql"
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
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/config"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type Options struct {
	Pool               *pgxpool.Pool
	Now                func() time.Time
	DashboardSnapshots *statsdomain.DashboardAggregateStore
}

type Service struct {
	pool               *pgxpool.Pool
	ownsPool           bool
	now                func() time.Time
	allowedOrigins     map[string]struct{}
	dashboardSnapshots *statsdomain.DashboardAggregateStore
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
		if strings.TrimSpace(settings.DatabaseURL) == "" {
			return nil, fmt.Errorf("database URL is required")
		}
		createdPool, err := pgxpool.New(context.Background(), settings.DatabaseURL)
		if err != nil {
			return nil, fmt.Errorf("create stats database pool: %w", err)
		}
		pool = createdPool
		ownsPool = true
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	allowedOrigins := map[string]struct{}{}
	for _, origin := range settings.CORSAllowedOriginsList() {
		allowedOrigins[origin] = struct{}{}
	}
	dashboardSnapshots := options.DashboardSnapshots
	if dashboardSnapshots == nil {
		dashboardSnapshots = statsdomain.NewDashboardAggregateStore()
	}
	return &Service{pool: pool, ownsPool: ownsPool, now: now, allowedOrigins: allowedOrigins, dashboardSnapshots: dashboardSnapshots}, nil
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
	if snapshot, ok := s.dashboardSnapshots.LoadProfile(profileID); ok {
		return snapshot, nil
	}
	snapshot, err := statsdomain.BuildDashboardAggregateSnapshot(ctx, s.pool, profileID, referenceNow)
	if err != nil {
		return statsdomain.DashboardAggregateSnapshot{}, err
	}
	s.dashboardSnapshots.StoreProfile(snapshot)
	return snapshot, nil
}

func (s *Service) invalidateDashboardAggregateSnapshot(profileID int) {
	if s == nil || s.dashboardSnapshots == nil {
		return
	}
	s.dashboardSnapshots.InvalidateProfile(profileID)
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

func (s *Service) MountManagementRoutes(api chi.Router) {
	api.Route("/stats", func(router chi.Router) {
		router.Get("/requests", s.handleListRequestLogs)
		router.Delete("/requests", s.handleDeleteRequestLogs)
		router.Get("/requests/{request_id}", s.handleGetRequestLog)
		router.Get("/summary", s.handleStatsSummary)
		router.Post("/models/metrics", s.handleModelMetrics)
		router.Get("/connection-success-rates", s.handleConnectionSuccessRates)
		router.Get("/throughput", s.handleThroughput)
		router.Get("/spending", s.handleSpending)
		router.Get("/usage-snapshot", s.handleUsageSnapshot)
		router.Get("/endpoints/{endpoint_id}/models", s.handleEndpointModelStatistics)
		router.Delete("/statistics", s.handleDeleteStatistics)
	})
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleGetRequestLog(w http.ResponseWriter, r *http.Request) {
	requestID, err := routeInt(r, "request_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	if response == nil {
		writeError(w, r, s.allowedOrigins, http.StatusNotFound, "Request log not found")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleStatsSummary(w http.ResponseWriter, r *http.Request) {
	profile, err := s.resolveEffectiveProfile(r.Context(), r)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	referenceNow := s.nowUTC()
	params, err := parseStatsSummaryParams(r, profile.ID)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	if matchesDashboardSummarySnapshotRequest(params, referenceNow) {
		snapshot, snapshotErr := s.loadOrBuildDashboardAggregateSnapshot(r.Context(), profile.ID, referenceNow)
		if snapshotErr != nil {
			writeDomainError(w, r, s.allowedOrigins, snapshotErr)
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleModelMetrics(w http.ResponseWriter, r *http.Request) {
	body, err := decodeModelMetricsRequest(r)
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
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
		writeDomainError(w, r, s.allowedOrigins, err)
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleThroughput(w http.ResponseWriter, r *http.Request) {
	profile, err := s.resolveEffectiveProfile(r.Context(), r)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	referenceNow := s.nowUTC()
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	toTime, err := parseOptionalTime(r, "to_time")
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	endpointID, err := parseOptionalInt(r, "endpoint_id")
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	connectionID, err := parseOptionalInt(r, "connection_id")
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
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
			writeDomainError(w, r, s.allowedOrigins, snapshotErr)
			return
		}
		writeJSON(w, http.StatusOK, snapshot.Throughput24H)
		return
	}
	response, err := statsdomain.GetThroughput(r.Context(), s.pool, params)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleUsageSnapshot(w http.ResponseWriter, r *http.Request) {
	profile, err := s.resolveEffectiveProfile(r.Context(), r)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	preset := queryStringOrDefault(r, "preset", "1h")
	referenceNow := s.nowUTC()
	if matchesDashboardUsageSnapshotRequest(preset) {
		snapshot, snapshotErr := s.loadOrBuildDashboardAggregateSnapshot(r.Context(), profile.ID, referenceNow)
		if snapshotErr != nil {
			writeDomainError(w, r, s.allowedOrigins, snapshotErr)
			return
		}
		writeJSON(w, http.StatusOK, snapshot.UsageSnapshotPreset1)
		return
	}
	response, err := statsdomain.GetUsageSnapshot(r.Context(), s.pool, profile.ID, preset, referenceNow)
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleEndpointModelStatistics(w http.ResponseWriter, r *http.Request) {
	endpointID, err := routeInt(r, "endpoint_id")
	if err != nil {
		writeError(w, r, s.allowedOrigins, http.StatusBadRequest, err.Error())
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
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleDeleteRequestLogs(w http.ResponseWriter, r *http.Request) {
	profileID, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (int, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return 0, err
		}
		olderThanDays, err := parseOptionalInt(r, "older_than_days")
		if err != nil {
			return 0, err
		}
		deleteAll, err := parseOptionalBool(r, "delete_all")
		if err != nil {
			return 0, err
		}
		if olderThanDays == nil && !deleteAll {
			olderThanDays, err = loadRequestLogRetentionDays(r.Context(), tx, profile.ID)
			if err != nil {
				return 0, err
			}
			if olderThanDays == nil {
				return 0, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "No request log retention policy configured; provide 'older_than_days' or 'delete_all=true', or configure request_logs_retention_days in /api/settings/retention"}
			}
		}
		if err := statsdomain.DeleteRequestLogs(r.Context(), tx, profile.ID, olderThanDays, deleteAll, s.nowUTC()); err != nil {
			return 0, err
		}
		return profile.ID, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	s.invalidateDashboardAggregateSnapshot(profileID)
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Service) handleDeleteStatistics(w http.ResponseWriter, r *http.Request) {
	profileID, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (int, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return 0, err
		}
		olderThanDays, err := parseOptionalInt(r, "older_than_days")
		if err != nil {
			return 0, err
		}
		deleteAll, err := parseOptionalBool(r, "delete_all")
		if err != nil {
			return 0, err
		}
		if olderThanDays == nil && !deleteAll {
			olderThanDays, err = loadStatisticsRetentionDays(r.Context(), tx, profile.ID)
			if err != nil {
				return 0, err
			}
			if olderThanDays == nil {
				return 0, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "No statistics retention policy configured; provide 'older_than_days' or 'delete_all=true', or configure statistics_retention_days in /api/settings/retention"}
			}
		}
		if err := statsdomain.DeleteStatistics(r.Context(), tx, profile.ID, olderThanDays, deleteAll, s.nowUTC()); err != nil {
			return 0, err
		}
		return profile.ID, nil
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	s.invalidateDashboardAggregateSnapshot(profileID)
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func loadRequestLogRetentionDays(ctx context.Context, tx pgx.Tx, profileID int) (*int, error) {
	requestLogsRetentionDays, _, err := loadRetentionSettings(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	return requestLogsRetentionDays, nil
}

func loadStatisticsRetentionDays(ctx context.Context, tx pgx.Tx, profileID int) (*int, error) {
	_, statisticsRetentionDays, err := loadRetentionSettings(ctx, tx, profileID)
	if err != nil {
		return nil, err
	}
	return statisticsRetentionDays, nil
}

func loadRetentionSettings(ctx context.Context, tx pgx.Tx, profileID int) (*int, *int, error) {
	var requestLogsRetentionDays sql.NullInt32
	var statisticsRetentionDays sql.NullInt32
	if err := tx.QueryRow(ctx, `SELECT request_logs_retention_days, statistics_retention_days FROM user_settings WHERE profile_id = $1 LIMIT 1`, profileID).Scan(&requestLogsRetentionDays, &statisticsRetentionDays); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("load stats retention settings for profile %d: %w", profileID, err)
	}
	return nullableIntFromNullInt32(requestLogsRetentionDays), nullableIntFromNullInt32(statisticsRetentionDays), nil
}

func nullableIntFromNullInt32(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
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
	return statsdomain.RequestLogListParams{ProfileID: profileID, IngressRequestID: normalizedQueryString(r, "ingress_request_id"), ModelID: normalizedQueryString(r, "model_id"), StatusFamily: statusFamily, FromTime: fromTime, EndpointID: endpointID, Limit: limit, Offset: offset}, nil
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

func parseOptionalBool(r *http.Request, key string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("invalid %s", key)
	}
	return parsed, nil
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

func writeDomainError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, err error) {
	var profileErr *profiledomain.HTTPError
	if errors.As(err, &profileErr) {
		writeError(w, r, allowedOrigins, profileErr.StatusCode, profileErr.Detail)
		return
	}
	var statsErr *statsdomain.HTTPError
	if errors.As(err, &statsErr) {
		writeError(w, r, allowedOrigins, statsErr.StatusCode, statsErr.Detail)
		return
	}
	writeError(w, r, allowedOrigins, http.StatusInternalServerError, "Internal server error")
}

func writeError(w http.ResponseWriter, r *http.Request, allowedOrigins map[string]struct{}, statusCode int, detail string) {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin != "" {
		if _, ok := allowedOrigins[origin]; ok {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
	}
	writeJSON(w, statusCode, map[string]string{"detail": detail})
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(payload)
}
