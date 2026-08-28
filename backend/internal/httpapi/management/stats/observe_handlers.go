package stats

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/httpapi/management/responseutil"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	profiledomain "github.com/coachpo/prism/backend/internal/profiledomain"
)

const observabilitySigningKeyLabel = "prism.observe.query-context.v1"

// handleQueryContext creates the signed query context for the requested
// window. It is the first fragment call of every refresh generation; all
// Window/Breakdown fragments reuse the returned token.
func (s *Service) handleQueryContext(w http.ResponseWriter, r *http.Request) {
	for key := range r.URL.Query() {
		if key != "preset" && key != "from_time" && key != "to_time" && key != "scope" {
			writeDomainError(w, r, s.corsSnapshot(), &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "filter_invalid", Detail: "unknown filter " + key})
			return
		}
	}
	scope, err := statsdomain.NormalizeScope(r.URL.Query().Get("scope"))
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	preset := strings.TrimSpace(r.URL.Query().Get("preset"))
	fromTime := parseOptionalRFC3339(r.URL.Query().Get("from_time"))
	toTime := parseOptionalRFC3339(r.URL.Query().Get("to_time"))
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats query-context", func(tx pgx.Tx) (statsdomain.QueryContextResponse, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.QueryContextResponse{}, err
		}
		referenceNow := s.nowUTC()
		domains := []string{"request_logs", "usage_request_events", "loadbalance_events"}
		boundsByDomain := make(map[string]statsdomain.QueryBounds, len(domains))
		sourcesByDomain := make(map[string]statsdomain.RetentionFloorEpochSource, len(domains))
		domainSnapshots := make(map[string]statsdomain.QueryContextDomainSnapshot, len(domains))
		for _, domain := range domains {
			source, sourceErr := statsdomain.LoadRetentionSourceProjection(r.Context(), tx, domain, referenceNow)
			if sourceErr != nil {
				return statsdomain.QueryContextResponse{}, sourceErr
			}
			if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
				return statsdomain.QueryContextResponse{}, &statsdomain.HTTPError{StatusCode: http.StatusServiceUnavailable, Code: domainPurgeInProgressCode(domain), Detail: "observability data is temporarily unavailable while retention cleanup is publishing"}
			}
			actual, actualErr := statsdomain.LoadActualCoverageProjection(r.Context(), tx, source)
			if actualErr != nil {
				return statsdomain.QueryContextResponse{}, actualErr
			}
			bounds, boundsErr := statsdomain.ResolveQueryBoundsFromActualCoverage(preset, fromTime, toTime, referenceNow, source, actual)
			if boundsErr != nil {
				return statsdomain.QueryContextResponse{}, boundsErr
			}
			boundsByDomain[domain] = bounds
			sourcesByDomain[domain] = source
			domainSnapshots[domain] = statsdomain.QueryContextDomainSnapshot{
				Domain:              domain,
				FromTime:            bounds.UsageFrom.UTC(),
				ToTime:              bounds.UsageTo.UTC(),
				RetentionFromTime:   bounds.UsageRetentionFrom,
				RetentionEpoch:      source.RetentionEpoch,
				RetentionGeneration: source.RetentionGeneration,
				FenceGeneration:     source.FenceGeneration,
				SourceRevision:      source.SourceRevision,
				CoverageRevision:    actual.Revision,
				CoverageHash:        actual.Hash,
				CoverageGeneratedAt: actual.GeneratedAt,
				MaterializationCut:  actual.MaterializationCut,
				Gaps:                append([]statsdomain.CoverageGap(nil), bounds.Gaps...),
				Complete:            actual.Complete && actual.Freshness == "fresh" && bounds.Complete,
				Freshness:           actual.Freshness,
				PurgeState:          source.PurgeState,
			}
		}
		usageBounds := boundsByDomain["usage_request_events"]
		usageSource := sourcesByDomain["usage_request_events"]
		token := statsdomain.QueryContextToken{
			SchemaVersion:   1,
			Scope:           scope,
			ProfileID:       profile.ID,
			RequestedPreset: usageBounds.RequestedPreset,
			UsageFrom:       usageBounds.UsageFrom.UTC().Format(time.RFC3339Nano),
			UsageTo:         usageBounds.UsageTo.UTC().Format(time.RFC3339Nano),
			RetentionEpoch:  usageSource.RetentionEpoch,
			SourceRevision:  usageSource.SourceRevision,
			Source:          usageBounds.Source,
			Complete:        usageBounds.Complete,
			Domains:         domainSnapshots,
			IssuedAt:        referenceNow.UTC(),
			ExpiresAt:       referenceNow.UTC().Add(24 * time.Hour),
		}
		if usageBounds.RequestedFrom != nil {
			value := usageBounds.RequestedFrom.UTC().Format(time.RFC3339Nano)
			token.RequestedFrom = &value
		}
		if usageBounds.RequestedTo != nil {
			value := usageBounds.RequestedTo.UTC().Format(time.RFC3339Nano)
			token.RequestedTo = &value
		}
		signed, err := statsdomain.SignQueryContext(token, s.observabilitySigningKey())
		if err != nil {
			return statsdomain.QueryContextResponse{}, err
		}
		usageCoverage := statsdomain.CoverageFromQueryBounds(usageBounds, domainSnapshots["usage_request_events"])
		eventBounds := boundsByDomain["loadbalance_events"]
		eventCoverage := statsdomain.CoverageFromQueryBounds(eventBounds, domainSnapshots["loadbalance_events"])
		requestBounds := boundsByDomain["request_logs"]
		requestCoverage := statsdomain.CoverageFromQueryBounds(requestBounds, domainSnapshots["request_logs"])
		return statsdomain.QueryContextResponse{
			QueryContext:    signed,
			Scope:           scope,
			Caliber:         statsdomain.CaliberForScope(scope),
			UsageBounds:     statsdomain.TimeBounds{FromTime: usageBounds.UsageFrom, ToTime: usageBounds.UsageTo},
			EventBounds:     statsdomain.TimeBounds{FromTime: eventBounds.UsageFrom, ToTime: eventBounds.UsageTo},
			EventCoverage:   eventCoverage,
			RequestBounds:   statsdomain.TimeBounds{FromTime: requestBounds.UsageFrom, ToTime: requestBounds.UsageTo},
			RequestCoverage: requestCoverage,
			UsageCoverage:   usageCoverage,
			GeneratedAt:     referenceNow.UTC(),
			RequestedBounds: requestedBoundsOrNil(usageBounds),
		}, nil
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func domainPurgeInProgressCode(domain string) string {
	switch domain {
	case "request_logs":
		return "request_purge_in_progress"
	case "loadbalance_events":
		return "loadbalance_purge_in_progress"
	default:
		return "usage_purge_in_progress"
	}
}

func requestedBoundsOrNil(bounds statsdomain.QueryBounds) *statsdomain.TimeBounds {
	if bounds.RequestedFrom == nil || bounds.RequestedTo == nil {
		return nil
	}
	return &statsdomain.TimeBounds{FromTime: *bounds.RequestedFrom, ToTime: *bounds.RequestedTo}
}

// handleUsageSummary returns the Window KPI aggregate for a query context.
func (s *Service) handleUsageSummary(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "query_context"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	token, bounds, err := s.resolveQueryContextFromRequest(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	usageBounds, _ := statsdomain.QueryBoundsForDomain(token, "usage_request_events")
	requestBounds, _ := statsdomain.QueryBoundsForDomain(token, "request_logs")
	usageCoverage := statsdomain.CoverageFromQueryBounds(usageBounds, token.Domains["usage_request_events"])
	requestCoverage := statsdomain.CoverageFromQueryBounds(requestBounds, token.Domains["request_logs"])
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats usage-summary", func(tx pgx.Tx) (statsdomain.UsageSummaryResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.UsageSummaryResult{}, err
		}
		if profile.ID != token.ProfileID {
			return statsdomain.UsageSummaryResult{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "query_context scope mismatch"}
		}
		reportCurrencyCode, reportCurrencySymbol, err := statsdomain.LoadReportCurrencyPreferences(r.Context(), tx, profile.ID)
		if err != nil {
			return statsdomain.UsageSummaryResult{}, err
		}
		if token.Scope == statsdomain.ScopeIngress {
			return statsdomain.LoadUsageSummary(r.Context(), tx, profile.ID, usageBounds, usageCoverage, s.nowUTC(), reportCurrencyCode, reportCurrencySymbol)
		}
		return statsdomain.LoadScopedUsageSummary(r.Context(), tx, profile.ID, token.Scope, bounds, usageCoverage, requestCoverage, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// resolveQueryContextFromRequest verifies the signed token and re-derives the
// effective bounds from it (the token is the single time/consistency input).
func (s *Service) resolveQueryContextFromRequest(r *http.Request) (statsdomain.QueryContextToken, statsdomain.QueryBounds, error) {
	raw := strings.TrimSpace(r.URL.Query().Get("query_context"))
	if raw == "" {
		return statsdomain.QueryContextToken{}, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "query_context is required"}
	}
	token, err := statsdomain.VerifyQueryContext(raw, s.observabilitySigningKey(), s.nowUTC())
	if err != nil {
		return statsdomain.QueryContextToken{}, statsdomain.QueryBounds{}, err
	}
	if strings.TrimSpace(token.RetentionEpoch) == "" || len(token.Domains) == 0 {
		return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "invalid_query_context", Detail: "invalid query_context"}
	}
	currentSources, sourceErr := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats query-context protection", func(tx pgx.Tx) (map[string]statsdomain.RetentionFloorEpochSource, error) {
		result := make(map[string]statsdomain.RetentionFloorEpochSource, len(token.Domains))
		for _, domain := range []string{"request_logs", "usage_request_events", "loadbalance_events"} {
			source, err := statsdomain.LoadRetentionSourceProjection(r.Context(), tx, domain, s.nowUTC())
			if err != nil {
				return nil, err
			}
			result[domain] = source
		}
		return result, nil
	})
	if sourceErr != nil {
		return token, statsdomain.QueryBounds{}, sourceErr
	}
	for _, domain := range []string{"request_logs", "usage_request_events", "loadbalance_events"} {
		source := currentSources[domain]
		snapshot, ok := token.Domains[domain]
		if !ok || snapshot.Domain != domain || snapshot.RetentionEpoch != source.RetentionEpoch || snapshot.SourceRevision != source.SourceRevision || snapshot.FenceGeneration != source.FenceGeneration {
			return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: http.StatusGone, Code: "dataset_snapshot_revoked", Detail: "query_context snapshot has been revoked"}
		}
		if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
			return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: http.StatusServiceUnavailable, Code: domainPurgeInProgressCode(domain), Detail: "observability data is temporarily unavailable while retention cleanup is publishing"}
		}
	}
	scope, err := statsdomain.NormalizeScope(token.Scope)
	if err != nil {
		return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: 422, Code: "invalid_query_context", Detail: "invalid query_context scope"}
	}
	token.Scope = scope
	domain := "usage_request_events"
	if scope == statsdomain.ScopeRouteAttempt {
		domain = "request_logs"
	}
	bounds, err := statsdomain.QueryBoundsForDomain(token, domain)
	if err != nil {
		return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: 422, Code: "invalid_query_context", Detail: "invalid query_context"}
	}
	return token, bounds, nil
}

func parseOptionalRFC3339(value string) *time.Time {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil
	}
	return &parsed
}

// observabilitySigningKey returns the domain-separated HMAC subkey used to
// sign query contexts and cursors.
func (s *Service) observabilitySigningKey() []byte {
	return statsdomain.DeriveQuerySigningKey(s.secretEncryptionKey)
}

// handleUsageSeries returns the single main chart for a query context.
func (s *Service) handleUsageSeries(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "query_context", "metric", "group_by", "interval", "series_limit"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	token, bounds, err := s.resolveQueryContextFromRequest(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	usageBounds, _ := statsdomain.QueryBoundsForDomain(token, "usage_request_events")
	requestBounds, _ := statsdomain.QueryBoundsForDomain(token, "request_logs")
	usageCoverage := statsdomain.CoverageFromQueryBounds(usageBounds, token.Domains["usage_request_events"])
	requestCoverage := statsdomain.CoverageFromQueryBounds(requestBounds, token.Domains["request_logs"])
	metric := strings.TrimSpace(r.URL.Query().Get("metric"))
	groupBy := strings.TrimSpace(r.URL.Query().Get("group_by"))
	if groupBy == "" {
		groupBy = "none"
	}
	interval := strings.TrimSpace(r.URL.Query().Get("interval"))
	if interval == "" {
		interval = "auto"
	}
	seriesLimit, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("series_limit")))
	if err != nil || seriesLimit <= 0 {
		seriesLimit = 6
	}
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats usage-series", func(tx pgx.Tx) (statsdomain.UsageSeriesResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.UsageSeriesResult{}, err
		}
		if profile.ID != token.ProfileID {
			return statsdomain.UsageSeriesResult{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "query_context scope mismatch"}
		}
		reportCurrencyCode, reportCurrencySymbol, err := statsdomain.LoadReportCurrencyPreferences(r.Context(), tx, profile.ID)
		if err != nil {
			return statsdomain.UsageSeriesResult{}, err
		}
		return statsdomain.LoadUsageSeries(r.Context(), tx, profile.ID, token.Scope, bounds, usageCoverage, requestCoverage, metric, groupBy, interval, seriesLimit, s.nowUTC(), reportCurrencyCode, reportCurrencySymbol)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleDashboardNow returns the Now strip (rolling 30m RPM/TPM).
func (s *Service) handleDashboardNow(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats dashboard now", func(tx pgx.Tx) (statsdomain.DashboardNowResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.DashboardNowResult{}, err
		}
		return statsdomain.LoadDashboardNow(r.Context(), tx, profile.ID, s.nowUTC(), 30)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleUsageErrors returns the error aggregation panel for a query context.
func (s *Service) handleUsageErrors(w http.ResponseWriter, r *http.Request) {
	if err := rejectQueryKeys(r, "query_context", "group_by", "limit", "ingress_model_id", "final_target_model_id", "attempt_target_model_id", "endpoint_id", "terminal_target_id", "final_result", "outcome_detail", "status_code", "stream_outcome", "stream_error_kind"); err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	rawQueryContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
	token, bounds, err := s.resolveQueryContextFromRequest(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	usageBounds, _ := statsdomain.QueryBoundsForDomain(token, "usage_request_events")
	requestBounds, _ := statsdomain.QueryBoundsForDomain(token, "request_logs")
	usageCoverage := statsdomain.CoverageFromQueryBounds(usageBounds, token.Domains["usage_request_events"])
	requestCoverage := statsdomain.CoverageFromQueryBounds(requestBounds, token.Domains["request_logs"])
	params := statsdomain.UsageErrorsParams{
		GroupBy: strings.TrimSpace(r.URL.Query().Get("group_by")),
		Scope:   token.Scope,
		Limit:   parsePositiveQueryIntDefault(r, "limit", 20),
	}
	if modelID := strings.TrimSpace(r.URL.Query().Get("ingress_model_id")); modelID != "" {
		params.IngressModelID = &modelID
	}
	if modelID := strings.TrimSpace(r.URL.Query().Get("final_target_model_id")); modelID != "" {
		params.FinalTargetModelID = &modelID
	}
	if modelID := strings.TrimSpace(r.URL.Query().Get("attempt_target_model_id")); modelID != "" {
		params.AttemptTargetModelID = &modelID
	}
	if endpointID := parseOptionalPositiveQueryInt(r, "endpoint_id"); endpointID != nil {
		params.EndpointID = endpointID
	}
	if targetID := parseOptionalPositiveQueryInt(r, "terminal_target_id"); targetID != nil {
		params.TerminalTargetID = targetID
	}
	params.FinalResult = r.URL.Query()["final_result"]
	params.OutcomeDetail = r.URL.Query()["outcome_detail"]
	params.StreamOutcome = r.URL.Query()["stream_outcome"]
	params.StreamErrorKind = r.URL.Query()["stream_error_kind"]
	for _, raw := range r.URL.Query()["status_code"] {
		if value, err := strconv.Atoi(raw); err == nil {
			params.StatusCode = append(params.StatusCode, value)
		}
	}
	response, err := pgxutil.InReadOnlyTxValue(r.Context(), s.pool, "stats usage-errors", func(tx pgx.Tx) (statsdomain.UsageErrorsResult, error) {
		profile, err := profiledomain.ResolveEffectiveProfile(r.Context(), tx, r.Header.Get(profiledomain.ProfileIDHeader))
		if err != nil {
			return statsdomain.UsageErrorsResult{}, err
		}
		if profile.ID != token.ProfileID {
			return statsdomain.UsageErrorsResult{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "query_context scope mismatch"}
		}
		return statsdomain.LoadUsageErrors(r.Context(), tx, profile.ID, bounds, usageCoverage, requestCoverage, params, rawQueryContext, s.nowUTC())
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

func parsePositiveQueryIntDefault(r *http.Request, key string, defaultValue int) int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return defaultValue
	}
	return parsed
}

func parseOptionalPositiveQueryInt(r *http.Request, key string) *int {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	if raw == "" {
		return nil
	}
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil
	}
	return &parsed
}

func rejectQueryKeys(r *http.Request, allowed ...string) error {
	allow := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allow[key] = struct{}{}
	}
	for key := range r.URL.Query() {
		if _, ok := allow[key]; !ok {
			return &statsdomain.HTTPError{StatusCode: http.StatusUnprocessableEntity, Code: "filter_invalid", Detail: "unknown filter " + key}
		}
	}
	return nil
}
