package stats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
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
	CORSOriginProvider  platformcors.OriginProvider
	Pool                *pgxpool.Pool
	Now                 func() time.Time
	DashboardSnapshots  *statsdomain.DashboardAggregateStore
	SideEffects         *managementsideeffects.Dispatcher
	SecretEncryptionKey string
}

type Service struct {
	pool                *pgxpool.Pool
	ownsPool            bool
	now                 func() time.Time
	corsOriginProvider  platformcors.OriginProvider
	dashboardSnapshots  *statsdomain.DashboardAggregateStore
	sideEffects         *managementsideeffects.Dispatcher
	secretEncryptionKey string
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
	service := &Service{pool: pool, ownsPool: ownsPool, now: now, corsOriginProvider: corsOriginProvider, dashboardSnapshots: dashboardSnapshots, sideEffects: options.SideEffects, secretEncryptionKey: options.SecretEncryptionKey}
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
		router.Get("/dashboard/recent-activity", s.handleDashboardRecentActivity)
		router.Get("/requests", s.handleListRequestLogs)
		router.Get("/requests/export", s.handleExportRequestLogs)
		router.Get("/request-filter-options/proxy-api-keys", s.handleProxyAPIKeyFilterOptions)
		router.Get("/requests/{request_id}", s.handleGetRequestLog)
		router.Get("/summary", s.handleStatsSummary)
		router.Post("/models/metrics", s.handleModelMetrics)
		router.Get("/connection-success-rates", s.handleConnectionSuccessRates)
		router.Get("/throughput", s.handleThroughput)
		router.Get("/spending", s.handleSpending)
		router.Get("/usage-snapshot", s.handleUsageSnapshot)
		router.Get("/query-context", s.handleQueryContext)
		router.Get("/usage-summary", s.handleUsageSummary)
		router.Get("/usage-series", s.handleUsageSeries)
		router.Get("/usage-errors", s.handleUsageErrors)
		router.Get("/dashboard/now", s.handleDashboardNow)
		router.Get("/observe-activity", s.handleObserveActivity)
		router.Get("/endpoints/{endpoint_id}/models", s.handleEndpointModelStatistics)
		router.Get("/endpoints/{endpoint_id}/terminal-targets", s.handleEndpointTerminalTargetStatistics)
		router.Get("/cost-segments", s.handleCostSegments)
		router.Get("/cost-segments/{segment_key}/symbols", s.handleCostSegmentSymbols)
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
	responseutil.WriteJSON(w, http.StatusOK, snapshot)
}

func (s *Service) handleDashboardRecentActivity(w http.ResponseWriter, r *http.Request) {
	generatedAt := s.nowUTC()
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats dashboard recent activity", func(tx pgx.Tx) (statsdomain.DashboardRecentActivityResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.DashboardRecentActivityResponse{}, err
		}
		limit, err := parseDashboardRecentActivityLimit(r)
		if err != nil {
			return statsdomain.DashboardRecentActivityResponse{}, err
		}
		return statsdomain.GetDashboardRecentActivity(r.Context(), tx, profile.ID, limit, generatedAt)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func (s *Service) handleListRequestLogs(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	response, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (any, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return nil, err
		}
		view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
		if view != "" && view != "ingress_chains" && view != "attempts" {
			return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "view must be ingress_chains or attempts"}
		}
		var signedRequestBounds *statsdomain.QueryBounds
		// Final-result deep links on the flat request-log list bind a signed
		// query context (Requests SPEC §4.3). The retained ingress-chain view
		// has its own server-side cohort and does not require that token.
		if view != "ingress_chains" && requestLogHasSignedCohortSelector(r) {
			if strings.TrimSpace(r.URL.Query().Get("query_context")) == "" {
				return nil, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_required", Detail: "query_context is required with final filters"}
			}
			token, _, err := s.resolveQueryContextFromRequest(r)
			if err != nil {
				return nil, err
			}
			if token.ProfileID != profile.ID {
				return nil, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_scope_mismatch", Detail: "query_context scope mismatch"}
			}
			requestBounds, boundsErr := statsdomain.QueryBoundsForDomain(token, "request_logs")
			if boundsErr != nil {
				return nil, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_query_context", Detail: "invalid query_context"}
			}
			signedRequestBounds = &requestBounds
		}
		if view == "ingress_chains" {
			params, parseErr := parseChainQueryParams(r, profile.ID)
			if parseErr != nil {
				return nil, parseErr
			}
			params.CoverageReferenceNow = s.nowUTC()
			return statsdomain.ListIngressChains(r.Context(), tx, params)
		}
		params, err := parseRequestLogListParams(r, profile.ID, s.observabilitySigningKey(), s.nowUTC())
		if err != nil {
			return nil, err
		}

		// The signed context is authoritative for final-result deep links. Do
		// not trust browser-supplied from_time/to_time values to widen or shift
		// that cohort; the owner resolves the signed bounds against its actual
		// coverage projection in the same transaction.
		if signedRequestBounds != nil {
			fromTime := signedRequestBounds.UsageFrom.UTC()
			toTime := signedRequestBounds.UsageTo.UTC()
			params.CoveragePreset = "custom"
			params.CoverageRequestedFrom = &fromTime
			params.CoverageRequestedTo = &toTime
			params.FromTime = &fromTime
			params.ToTime = &toTime
		}
		return statsdomain.ListRequestLogs(r.Context(), tx, params)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func requestLogHasSignedCohortSelector(r *http.Request) bool {
	query := r.URL.Query()
	return query.Get("ingress_final_result") != "" ||
		query.Get("confirmed_failover") != "" ||
		query.Get("final_result") != "" ||
		query.Get("final_model_id") != "" ||
		query.Get("final_endpoint_id") != "" ||
		query.Get("final_terminal_target_id") != "" ||
		query.Get("final_pricing_status") != "" ||
		len(query["final_unpriced_reason"]) > 0 ||
		query.Get("reporting_currency_epoch") != ""
}

func (s *Service) handleProxyAPIKeyFilterOptions(w http.ResponseWriter, r *http.Request) {
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats proxy API key filter options", func(tx pgx.Tx) (statsdomain.ProxyAPIKeyFilterOptionsResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.ProxyAPIKeyFilterOptionsResponse{}, err
		}
		params, err := parseProxyAPIKeyFilterOptionsParams(r, profile.ID)
		if err != nil {
			return statsdomain.ProxyAPIKeyFilterOptionsResponse{}, err
		}
		return statsdomain.ListProxyAPIKeyFilterOptions(r.Context(), tx, params)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func parseProxyAPIKeyFilterOptionsParams(r *http.Request, profileID int) (statsdomain.ProxyAPIKeyFilterOptionsParams, error) {
	params := statsdomain.ProxyAPIKeyFilterOptionsParams{ProfileID: profileID}
	if raw := strings.TrimSpace(r.URL.Query().Get("q")); raw != "" {
		params.Query = &raw
	}
	var err error
	params.FromTime, err = parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.ProxyAPIKeyFilterOptionsParams{}, err
	}
	params.ToTime, err = parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.ProxyAPIKeyFilterOptionsParams{}, err
	}
	params.Limit, err = parsePositiveIntWithDefault(r, "limit", 50)
	if err != nil {
		return statsdomain.ProxyAPIKeyFilterOptionsParams{}, err
	}
	if cursor := strings.TrimSpace(r.URL.Query().Get("cursor")); cursor != "" {
		params.Cursor = &cursor
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("selected_id")); raw != "" {
		selectedID, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || selectedID <= 0 || selectedID > math.MaxInt32 {
			return statsdomain.ProxyAPIKeyFilterOptionsParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid selected_id"}
		}
		selected := int(selectedID)
		params.SelectedID = &selected
	}
	return params, nil
}

func (s *Service) handleGetRequestLog(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	rawRequestLogID := strings.TrimSpace(chi.URLParam(r, "request_id"))
	if rawRequestLogID == "" || !regexp.MustCompile(`^[0-9]+$`).MatchString(rawRequestLogID) {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "request_id must be a positive decimal string")
		return
	}
	requestLogID, err := strconv.ParseInt(rawRequestLogID, 10, 64)
	if err != nil {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "request_id must be a positive decimal string")
		return
	}
	type detailResult struct {
		response *statsdomain.RequestLogDetailResponseV2
		found    bool
	}
	result, err := pgxutil.InTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (detailResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return detailResult{}, err
		}
		response, found, err := statsdomain.GetRequestLogDetailV2(r.Context(), tx, profile.ID, requestLogID)
		if err != nil {
			return detailResult{}, err
		}
		if found && response != nil {
			source, sourceErr := statsdomain.LoadRetentionSourceProjection(r.Context(), tx, "request_logs", s.nowUTC())
			if sourceErr != nil {
				return detailResult{}, sourceErr
			}
			if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
				return detailResult{}, &statsdomain.HTTPError{StatusCode: http.StatusServiceUnavailable, Code: "request_log_purge_in_progress", Detail: "request logs are temporarily unavailable while retention cleanup is publishing"}
			}
			floor := source.ConfiguredCutoff
			if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
				floor = source.PublishedFloor
			}
			if floor != nil && response.Summary.CreatedAt.Before(*floor) {
				found = false
				response = nil
			}
		}
		return detailResult{response: response, found: found}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	if !result.found {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusNotFound, "Request log not found")
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, result.response)
}

// handleExportRequestLogs streams the full filtered CSV export (Requests SPEC
// §6.8). The snapshot, preflight count, spool, and digest happen before any
// response body bytes are sent; typed rejections never produce a partial file.
func (s *Service) handleExportRequestLogs(w http.ResponseWriter, r *http.Request) {
	responseutil.SetPrivateNoStoreHeaders(w)
	// Reject pagination keys up front.
	if strings.TrimSpace(r.URL.Query().Get("limit")) != "" || strings.TrimSpace(r.URL.Query().Get("offset")) != "" ||
		strings.TrimSpace(r.URL.Query().Get("cursor")) != "" || strings.TrimSpace(r.URL.Query().Get("chain_cursor")) != "" ||
		strings.TrimSpace(r.URL.Query().Get("row_cursor")) != "" {
		responseutil.WriteError(w, r, s.corsSnapshot(), http.StatusBadRequest, "export_pagination_unsupported")
		return
	}
	// A bounded export must state its range before the handler resolves the
	// interactive default (24h). Exact ingress selection is the sole
	// range-free exception; otherwise a preset or both explicit bounds are
	// required so a download can never silently become a browser-window dump.
	query := r.URL.Query()
	hasExactIngress := strings.TrimSpace(query.Get("ingress_request_id")) != ""
	hasPreset := strings.TrimSpace(query.Get("time_range")) != ""
	hasFrom := strings.TrimSpace(query.Get("from_time")) != ""
	hasTo := strings.TrimSpace(query.Get("to_time")) != ""
	if !hasExactIngress && !hasPreset && (!hasFrom || !hasTo) {
		writeDomainError(w, r, s.corsSnapshot(), &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "export_range_required", Detail: "Export requires an explicit time range."})
		return
	}
	if strings.TrimSpace(r.Header.Get("Accept")) != "text/csv" {
		// Allow Accept: text/csv or */*; missing Accept still proceeds (browsers
		// trigger downloads via the endpoint URL).
	}
	result, err := pgxutil.InRepeatableReadTxValue(r.Context(), s.pool, "stats", func(tx pgx.Tx) (statsdomain.ExportResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.ExportResult{}, err
		}
		view := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("view")))
		if view == "" {
			view = "ingress_chains"
		}
		if view != "ingress_chains" && view != "attempts" {
			return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "view must be ingress_chains or attempts"}
		}
		var signedRequestBounds *statsdomain.QueryBounds
		if view != "ingress_chains" && requestLogHasSignedCohortSelector(r) {
			if strings.TrimSpace(r.URL.Query().Get("query_context")) == "" {
				return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_required", Detail: "query_context is required with final filters"}
			}
			token, _, resolveErr := s.resolveQueryContextFromRequest(r)
			if resolveErr != nil {
				return statsdomain.ExportResult{}, resolveErr
			}
			if token.ProfileID != profile.ID {
				return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "query_context_scope_mismatch", Detail: "query_context scope mismatch"}
			}
			requestBounds, boundsErr := statsdomain.QueryBoundsForDomain(token, "request_logs")
			if boundsErr != nil {
				return statsdomain.ExportResult{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_query_context", Detail: "invalid query_context"}
			}
			signedRequestBounds = &requestBounds
		}
		params, err := parseRequestLogListParams(r, profile.ID, s.observabilitySigningKey(), s.nowUTC())
		if err != nil {
			return statsdomain.ExportResult{}, err
		}
		// Signed final-result selectors bind the export to the same per-domain
		// owner window as the JSON attempt list; browser bounds cannot widen it.
		if signedRequestBounds != nil {
			fromTime := signedRequestBounds.UsageFrom.UTC()
			toTime := signedRequestBounds.UsageTo.UTC()
			params.CoveragePreset = "custom"
			params.CoverageRequestedFrom = &fromTime
			params.CoverageRequestedTo = &toTime
			params.FromTime = &fromTime
			params.ToTime = &toTime
		}
		exportParams := statsdomain.ExportParams{RequestLogListParams: params, View: view}
		return statsdomain.ExportCSV(r.Context(), tx, exportParams)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	// Stream the exact verified spool bytes.
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"prism-requests-%s.csv\"", time.Now().UTC().Format("20060102-150405")))
	w.Header().Set("X-Prism-Export-Row-Count", fmt.Sprintf("%d", result.RowCount))
	w.Header().Set("X-Prism-Export-View", result.View)
	w.Header().Set("X-Prism-Export-Coverage", "retained")
	w.Header().Set("Digest", "sha-256="+result.DigestSHA256)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(result.Content)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := w.Write(result.Content); err != nil {
		slog.Warn("export stream interrupted", "error", err)
	}
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
	responseutil.WriteJSON(w, http.StatusOK, response)
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
	responseutil.WriteJSON(w, http.StatusOK, response)
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

func parseRequestLogListParams(r *http.Request, profileID int, observabilitySigningKey []byte, referenceNow time.Time) (statsdomain.RequestLogListParams, error) {
	if err := rejectUnsupportedRequestLogQueryKeys(r); err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	// Observe signed-context deep link: query_context is required whenever any
	// final_* selector is present, and the final cohort is always resolved
	// through the authoritative finalized usage summary (never translated
	// into ordinary filters).
	rawContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
	hasFinalSelector := r.URL.Query().Get("ingress_final_result") != "" ||
		r.URL.Query().Get("confirmed_failover") != "" ||
		r.URL.Query().Get("final_result") != "" ||
		r.URL.Query().Get("final_model_id") != "" ||
		r.URL.Query().Get("final_endpoint_id") != "" ||
		r.URL.Query().Get("final_terminal_target_id") != "" ||
		r.URL.Query().Get("final_pricing_status") != "" ||
		len(r.URL.Query()["final_unpriced_reason"]) > 0 ||
		r.URL.Query().Get("reporting_currency_epoch") != ""
	var queryContextFrom, queryContextTo *time.Time
	if rawContext != "" || hasFinalSelector {
		if rawContext == "" {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "query_context_required"}
		}
		token, err := statsdomain.VerifyQueryContext(rawContext, observabilitySigningKey, referenceNow)
		if err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
		if token.ProfileID != profileID {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "query_context scope mismatch"}
		}
		from, err := time.Parse(time.RFC3339, token.UsageFrom)
		if err != nil {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "invalid query_context"}
		}
		to, err := time.Parse(time.RFC3339, token.UsageTo)
		if err != nil {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Detail: "invalid query_context"}
		}
		queryContextFrom = &from
		queryContextTo = &to
		// The two pricing cohort grammars must never select the same cohort
		// (Requests SPEC §6.x): final_* pricing selectors are exclusive with
		// the ordinary pricing_status/unpriced_reason filters.
		if (r.URL.Query().Get("final_pricing_status") != "" || len(r.URL.Query()["final_unpriced_reason"]) > 0) &&
			(r.URL.Query().Get("pricing_status") != "" || len(r.URL.Query()["unpriced_reason"]) > 0) {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "conflicting_pricing_cohort", Detail: "final pricing selectors cannot be combined with ordinary pricing filters"}
		}
	}
	fromTime, err := parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	toTime, err := parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	endpointID, err := parseOptionalInt(r, "endpoint_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	terminalTargetID, err := parseOptionalInt(r, "terminal_target_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	if terminalTargetID != nil && *terminalTargetID <= 0 {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_terminal_target_id", Detail: "invalid terminal_target_id"}
	}
	var proxyAPIKeyID *int
	if rawValues, present := r.URL.Query()["proxy_api_key_id"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
		}
		proxyAPIKeyID, err = parseOptionalInt(r, "proxy_api_key_id")
	}
	if err != nil || (proxyAPIKeyID != nil && *proxyAPIKeyID <= 0) {
		return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
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
	statusCode, err := parseOptionalInt(r, "status_code")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	pricingStatus := normalizedQueryString(r, "pricing_status")
	if pricingStatus != nil {
		normalized := strings.ToLower(strings.TrimSpace(*pricingStatus))
		switch normalized {
		case "priced", "unpriced", "ineligible", "unknown":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown pricing_status value: " + normalized}
		}
		pricingStatus = &normalized
	}
	unpricedReasons := repeatableQueryValues(r, "unpriced_reason")
	for _, reason := range unpricedReasons {
		if _, err := parseUnpricedReasonValue(reason); err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
	}
	ingressFinalResult := normalizedQueryString(r, "ingress_final_result")
	if ingressFinalResult != nil {
		normalized := strings.ToLower(strings.TrimSpace(*ingressFinalResult))
		switch normalized {
		case "completed", "failed", "client_disconnected":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown ingress_final_result value: " + normalized}
		}
		ingressFinalResult = &normalized
	}
	confirmedFailover, err := parseOptionalBool(r, "confirmed_failover")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalResult := normalizedQueryString(r, "final_result")
	if finalResult != nil {
		switch *finalResult {
		case "completed", "failed", "client_disconnected":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown final_result value: " + *finalResult}
		}
	}
	finalModelID := normalizedQueryString(r, "final_model_id")
	finalEndpointID, err := parseOptionalInt(r, "final_endpoint_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalTerminalTargetID, err := parseOptionalInt(r, "final_terminal_target_id")
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	finalPricingStatus := normalizedQueryString(r, "final_pricing_status")
	if finalPricingStatus != nil {
		switch *finalPricingStatus {
		case "priced", "unpriced", "ineligible", "unknown":
		default:
			return statsdomain.RequestLogListParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown final_pricing_status value: " + *finalPricingStatus}
		}
	}
	finalUnpricedReasons := repeatableQueryValues(r, "final_unpriced_reason")
	for _, reason := range finalUnpricedReasons {
		if _, err := parseUnpricedReasonValue(reason); err != nil {
			return statsdomain.RequestLogListParams{}, err
		}
	}
	reportingEpoch := normalizedQueryString(r, "reporting_currency_epoch")
	sortBy, sortOrder, err := parseRequestLogSort(r)
	if err != nil {
		return statsdomain.RequestLogListParams{}, err
	}
	coveragePreset := strings.TrimSpace(r.URL.Query().Get("time_range"))
	return statsdomain.RequestLogListParams{ProfileID: profileID, IngressFinalResult: ingressFinalResult, ConfirmedFailover: confirmedFailover, IngressRequestID: normalizedQueryString(r, "ingress_request_id"), ModelID: normalizedQueryString(r, "model_id"), ResolvedTargetModelID: normalizedQueryString(r, "resolved_target_model_id"), StatusFamily: statusFamily, StatusCode: statusCode, ErrorText: normalizedQueryString(r, "error_text"), PricingStatus: pricingStatus, UnpricedReasons: unpricedReasons, FromTime: fromTime, ToTime: toTime, EndpointID: endpointID, TerminalTargetID: terminalTargetID, ProxyAPIKeyID: proxyAPIKeyID, ClientRuleID: clientRuleID, QueryContextFrom: queryContextFrom, QueryContextTo: queryContextTo, FinalResult: finalResult, FinalModelID: finalModelID, FinalEndpointID: finalEndpointID, FinalTerminalTargetID: finalTerminalTargetID, FinalPricingStatus: finalPricingStatus, FinalUnpricedReasons: finalUnpricedReasons, FinalReportingEpoch: reportingEpoch, CoveragePreset: coveragePreset, CoverageRequestedFrom: fromTime, CoverageRequestedTo: toTime, CoverageReferenceNow: referenceNow.UTC(), SortBy: sortBy, SortOrder: sortOrder, Limit: limit, Offset: offset}, nil
}

// parseRequestLogSort resolves the attempt-view sort grammar: `sort_by` over
// created_at|display_status|ttft_ms|total_tokens|total_cost_user_currency_micros
// and `sort_order` asc|desc. An unsupported value is rejected instead of
// falling back to created_at, so a sorted column header can never claim an
// order the returned rows do not have. The ingress-chain view keeps its own
// created_at-only restriction in parseChainQueryParams.
func parseRequestLogSort(r *http.Request) (string, string, error) {
	sortBy := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_by")))
	if sortBy == "" {
		sortBy = "created_at"
	}
	switch sortBy {
	case "created_at", "display_status", "ttft_ms", "total_tokens", "total_cost_user_currency_micros":
	default:
		return "", "", &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "sort_unsupported", Detail: "Unsupported sort_by: " + sortBy}
	}
	sortOrder := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_order")))
	if sortOrder == "" {
		sortOrder = "desc"
	}
	if sortOrder != "asc" && sortOrder != "desc" {
		return "", "", &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "sort_unsupported", Detail: "Unsupported sort_order: " + sortOrder}
	}
	return sortBy, sortOrder, nil
}

// rejectUnsupportedRequestLogQueryKeys enforces the strict request-log query
// grammar (Requests SPEC §6.1/§6.3): the legacy `priced=true|false` alias is
// not a compatibility branch — any unknown query key returns a typed
// 422 unknown_query_key with no migration hint. The Observe signed-context
// deep-link family (query_context + final_*) is part of the grammar and is
// never translated into ordinary filters.
func rejectUnsupportedRequestLogQueryKeys(r *http.Request) error {
	supported := map[string]struct{}{
		"ingress_request_id":       {},
		"ingress_final_result":     {},
		"confirmed_failover":       {},
		"model_id":                 {},
		"resolved_target_model_id": {},
		"status_family":            {},
		"status_code":              {},
		"error_text":               {},
		"pricing_status":           {},
		"unpriced_reason":          {},
		"from_time":                {},
		"to_time":                  {},
		"time_range":               {},
		"endpoint_id":              {},
		"terminal_target_id":       {},
		"proxy_api_key_id":         {},
		"client_rule_id":           {},
		"limit":                    {},
		"offset":                   {},
		"query_context":            {},
		"final_result":             {},
		"final_model_id":           {},
		"final_endpoint_id":        {},
		"final_terminal_target_id": {},
		"final_pricing_status":     {},
		"final_unpriced_reason":    {},
		"reporting_currency_epoch": {},
		"view":                     {},
		"observe_return":           {},
		"sort_by":                  {},
		"sort_order":               {},
	}
	for key := range r.URL.Query() {
		if _, ok := supported[key]; !ok {
			return &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "unknown_query_key", Detail: "Unknown query key: " + key}
		}
	}
	return nil
}

func parseOptionalBool(r *http.Request, key string) (*bool, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "true":
		parsed := true
		return &parsed, nil
	case "false":
		parsed := false
		return &parsed, nil
	default:
		return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid " + key}
	}
}

// parseChainQueryParams parses the canonical ingress-chain query (Requests
// SPEC §6.1). Chain view only accepts created_at sort; row-scoped filters
// select the ingress cohort server-side before pagination.
func parseChainQueryParams(r *http.Request, profileID int) (statsdomain.ChainQueryParams, error) {
	params := statsdomain.ChainQueryParams{
		ProfileID:              profileID,
		View:                   "ingress_chains",
		IngressRequestID:       normalizedQueryString(r, "ingress_request_id"),
		IngressFinalResult:     normalizedQueryString(r, "ingress_final_result"),
		RowResult:              normalizedQueryString(r, "row_result"),
		PricingStatus:          normalizedQueryString(r, "pricing_status"),
		ReportingCurrencyEpoch: normalizedQueryString(r, "reporting_currency_epoch"),
		CostSegmentKey:         normalizedQueryString(r, "cost_segment_key"),
		ModelID:                normalizedQueryString(r, "model_id"),
		ResolvedTargetModelID:  normalizedQueryString(r, "resolved_target_model_id"),
		StatusFamily:           normalizedQueryString(r, "status_family"),
		ErrorText:              normalizedQueryString(r, "error_text"),
	}
	var err error
	params.FromTime, err = parseOptionalTime(r, "from_time")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.CoveragePreset = strings.TrimSpace(r.URL.Query().Get("time_range"))
	params.ToTime, err = parseOptionalTime(r, "to_time")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.EndpointID, err = parseOptionalInt(r, "endpoint_id")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.TerminalTargetID, err = parseOptionalInt(r, "terminal_target_id")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	if params.TerminalTargetID != nil && *params.TerminalTargetID <= 0 {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_terminal_target_id", Detail: "invalid terminal_target_id"}
	}
	params.StatusCode, err = parseOptionalInt(r, "status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	var proxyAPIKeyID *int
	if rawValues, present := r.URL.Query()["proxy_api_key_id"]; present {
		if len(rawValues) != 1 || strings.TrimSpace(rawValues[0]) == "" {
			return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
		}
		proxyAPIKeyID, err = parseOptionalInt(r, "proxy_api_key_id")
	}
	if err != nil || (proxyAPIKeyID != nil && *proxyAPIKeyID <= 0) {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_proxy_api_key_id", Detail: "invalid proxy_api_key_id"}
	}
	params.ProxyAPIKeyID = proxyAPIKeyID
	params.ConfirmedFailover, err = parseOptionalBool(r, "confirmed_failover")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.IsStream, err = parseOptionalBool(r, "is_stream")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.UnpricedReasons = repeatableQueryValues(r, "unpriced_reason")
	params.StreamOutcomes = repeatableQueryValues(r, "stream_outcome")
	params.StreamErrorKinds = repeatableQueryValues(r, "stream_error_kind")
	params.UpstreamStatusCodes, err = repeatableQueryInts(r, "upstream_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.GatewayStatusCodes, err = repeatableQueryInts(r, "gateway_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.LegacyStatusCodes, err = repeatableQueryInts(r, "legacy_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.IngressFinalStatusCodes, err = repeatableQueryInts(r, "ingress_final_status_code")
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	params.SortBy = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_by")))
	if params.SortBy == "" {
		params.SortBy = "created_at"
	}
	if params.SortBy != "created_at" {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "chain_sort_unsupported", Detail: "Ingress chain view only supports created_at sorting."}
	}
	params.SortOrder = strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort_order")))
	params.ChainLimit, err = parsePositiveIntWithDefault(r, "chain_limit", 20)
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	if params.ChainLimit > 50 {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "chain_limit_exceeded", Detail: "chain_limit must be between 1 and 50."}
	}
	params.ChainRowLimit, err = parsePositiveIntWithDefault(r, "chain_row_limit", 50)
	if err != nil {
		return statsdomain.ChainQueryParams{}, err
	}
	if params.ChainRowLimit > 200 {
		return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "chain_row_limit_exceeded", Detail: "chain_row_limit must be between 1 and 200."}
	}
	params.ChainCursor = normalizedQueryString(r, "chain_cursor")
	params.RowCursor = normalizedQueryString(r, "row_cursor")
	if rawAnchor := strings.TrimSpace(r.URL.Query().Get("anchor_request_log_id")); rawAnchor != "" {
		var anchor int64
		if _, err := fmt.Sscanf(rawAnchor, "%d", &anchor); err != nil || anchor <= 0 {
			return statsdomain.ChainQueryParams{}, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Code: "anchor_invalid", Detail: "anchor_request_log_id must be a positive decimal string."}
		}
		params.AnchorRequestLogID = &anchor
	}
	return params, nil
}

func repeatableQueryValues(r *http.Request, key string) []string {
	values, ok := r.URL.Query()[key]
	if !ok {
		return nil
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, duplicate := seen[trimmed]; duplicate {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func repeatableQueryInts(r *http.Request, key string) ([]int, error) {
	values, ok := r.URL.Query()[key]
	if !ok {
		return nil, nil
	}
	result := make([]int, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		var parsed int
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err != nil {
			return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid " + key}
		}
		result = append(result, parsed)
	}
	return result, nil
}

func parseOptionalUnpricedReason(r *http.Request, key string) (*string, error) {
	value := strings.TrimSpace(r.URL.Query().Get(key))
	if value == "" {
		return nil, nil
	}
	switch value {
	case "PRICING_DISABLED", "MISSING_TOKEN_USAGE", "STREAM_USAGE_UNAVAILABLE", "MISSING_PRICE_DATA":
		return &value, nil
	default:
		return nil, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid " + key}
	}
}

func parseUnpricedReasonValue(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	switch trimmed {
	case "PRICING_DISABLED", "MISSING_TOKEN_USAGE", "STREAM_USAGE_UNAVAILABLE", "MISSING_PRICE_DATA":
		return trimmed, nil
	default:
		return "", &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid unpriced_reason"}
	}
}

func parseDashboardRecentActivityLimit(r *http.Request) (int, error) {
	limit, err := parseOptionalInt(r, "limit")
	if err != nil {
		return 0, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid limit"}
	}
	if limit == nil {
		return 12, nil
	}
	if *limit <= 0 {
		return 0, &statsdomain.HTTPError{StatusCode: http.StatusBadRequest, Detail: "invalid limit"}
	}
	if *limit > 50 {
		return 50, nil
	}
	return *limit, nil
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
		return modelMetricsBatchRequest{}, responseutil.SanitizeDecodeError(err)
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
			return nil, invalidQueryParameter(key, "must be an RFC3339 timestamp")
		}
	}
	resolved := parsed.UTC()
	return &resolved, nil
}

func invalidQueryParameter(key string, reason string) error {
	return &statsdomain.HTTPError{
		StatusCode: http.StatusUnprocessableEntity,
		Code:       "invalid_query_parameter",
		Detail:     fmt.Sprintf("%s %s", key, reason),
		Details:    map[string]any{"parameter": key},
	}
}

func parseOptionalInt(r *http.Request, key string) (*int, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil {
		return nil, invalidQueryParameter(key, "must be an integer")
	}
	if parsed > math.MaxInt32 || parsed < math.MinInt32 {
		return nil, invalidQueryParameter(key, fmt.Sprintf("must be within [%d, %d]", math.MinInt32, math.MaxInt32))
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
		return 0, invalidQueryParameter(key, "must be >= 1")
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
		return 0, invalidQueryParameter(key, "must be >= 0")
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
		return 0, invalidQueryParameter(name, "must be a positive integer")
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
		responseutil.WriteError(w, r, corsSnapshot, statsErr.StatusCode, statsErr.Detail)
		return
	}
	if strings.Contains(r.URL.Path, "/stats/requests") {
		slog.Error("stats requests handler error", "path", r.URL.Path, "error", err)
	}
	responseutil.WriteError(w, r, corsSnapshot, http.StatusInternalServerError, "Internal server error")
}

func writeStructuredError(w http.ResponseWriter, r *http.Request, corsSnapshot platformcors.Snapshot, err *statsdomain.HTTPError) {
	var details any
	if len(err.Details) > 0 {
		details = err.Details
	}
	responseutil.WriteProblem(w, r, corsSnapshot, err.StatusCode, err.Code, err.Detail, map[string]any{}, details)
}
