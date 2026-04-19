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
	"github.com/coachpo/prism/backend/internal/platform/config"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

type Options struct {
	Pool *pgxpool.Pool
	Now  func() time.Time
}

type Service struct {
	pool           *pgxpool.Pool
	ownsPool       bool
	now            func() time.Time
	allowedOrigins map[string]struct{}
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
	return &Service{pool: pool, ownsPool: ownsPool, now: now, allowedOrigins: allowedOrigins}, nil
}

func (s *Service) Close() {
	if s != nil && s.ownsPool && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Service) nowUTC() time.Time {
	return s.now().UTC()
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (statsdomain.RequestLogListResponse, error) {
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (*statsdomain.RequestLogDetailResponse, error) {
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (statsdomain.StatsSummaryResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.StatsSummaryResponse{}, err
		}
		params, err := parseStatsSummaryParams(r, profile.ID)
		if err != nil {
			return statsdomain.StatsSummaryResponse{}, err
		}
		return statsdomain.GetStatsSummary(r.Context(), tx, params)
	})
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (statsdomain.ModelMetricsBatchResponse, error) {
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) ([]statsdomain.ConnectionSuccessRate, error) {
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (statsdomain.ThroughputStatsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.ThroughputStatsResponse{}, err
		}
		fromTime, err := parseOptionalTime(r, "from_time")
		if err != nil {
			return statsdomain.ThroughputStatsResponse{}, err
		}
		toTime, err := parseOptionalTime(r, "to_time")
		if err != nil {
			return statsdomain.ThroughputStatsResponse{}, err
		}
		modelID := normalizedQueryString(r, "model_id")
		apiFamily := normalizedQueryString(r, "api_family")
		endpointID, err := parseOptionalInt(r, "endpoint_id")
		if err != nil {
			return statsdomain.ThroughputStatsResponse{}, err
		}
		connectionID, err := parseOptionalInt(r, "connection_id")
		if err != nil {
			return statsdomain.ThroughputStatsResponse{}, err
		}
		return statsdomain.GetThroughput(r.Context(), tx, statsdomain.ThroughputParams{ProfileID: profile.ID, FromTime: fromTime, ToTime: toTime, ModelID: modelID, APIFamily: apiFamily, EndpointID: endpointID, ConnectionID: connectionID})
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Service) handleSpending(w http.ResponseWriter, r *http.Request) {
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (statsdomain.SpendingReportResponse, error) {
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (statsdomain.UsageSnapshotResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.UsageSnapshotResponse{}, err
		}
		preset := queryStringOrDefault(r, "preset", "1h")
		return statsdomain.GetUsageSnapshot(r.Context(), tx, profile.ID, preset, s.nowUTC())
	})
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
	response, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) ([]statsdomain.EndpointModelStatistic, error) {
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
	_, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (struct{}, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return struct{}{}, err
		}
		olderThanDays, err := parseOptionalInt(r, "older_than_days")
		if err != nil {
			return struct{}{}, err
		}
		deleteAll, err := parseOptionalBool(r, "delete_all")
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, statsdomain.DeleteRequestLogs(r.Context(), tx, profile.ID, olderThanDays, deleteAll, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Service) handleDeleteStatistics(w http.ResponseWriter, r *http.Request) {
	_, err := withTxValue(r.Context(), s.pool, func(tx pgx.Tx) (struct{}, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return struct{}{}, err
		}
		olderThanDays, err := parseOptionalInt(r, "older_than_days")
		if err != nil {
			return struct{}{}, err
		}
		deleteAll, err := parseOptionalBool(r, "delete_all")
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, statsdomain.DeleteStatistics(r.Context(), tx, profile.ID, olderThanDays, deleteAll, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.allowedOrigins, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
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
	defer r.Body.Close()
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

func withTxValue[T any](ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return zero, fmt.Errorf("begin stats transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	value, err := fn(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(ctx); err != nil {
		return zero, fmt.Errorf("commit stats transaction: %w", err)
	}
	return value, nil
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
