package stats

import (
	"context"
	"fmt"
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
		coverageByDomain := make(map[string]statsdomain.ActualCoverageProjection, len(domains))
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
			coverageByDomain[domain] = actual
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
		usageCoverage := coverageFromQueryBounds(usageBounds, usageSource, coverageByDomain["usage_request_events"])
		eventBounds := boundsByDomain["loadbalance_events"]
		eventSource := sourcesByDomain["loadbalance_events"]
		eventCoverage := coverageFromQueryBounds(eventBounds, eventSource, coverageByDomain["loadbalance_events"])
		requestBounds := boundsByDomain["request_logs"]
		requestSource := sourcesByDomain["request_logs"]
		requestCoverage := coverageFromQueryBounds(requestBounds, requestSource, coverageByDomain["request_logs"])
		return statsdomain.QueryContextResponse{
			QueryContext:    signed,
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

func coverageFromQueryBounds(bounds statsdomain.QueryBounds, source statsdomain.RetentionFloorEpochSource, actual statsdomain.ActualCoverageProjection) statsdomain.Coverage {
	var precision *statsdomain.CoveragePrecision
	if bounds.Complete && actual.Complete && actual.Freshness == "fresh" {
		precision = &statsdomain.CoveragePrecision{TTFT: "exact", OutputRate: "exact"}
	}
	return statsdomain.Coverage{
		RequestedPreset:     bounds.RequestedPreset,
		FromTime:            bounds.UsageFrom,
		ToTime:              bounds.UsageTo,
		RetentionFromTime:   bounds.UsageRetentionFrom,
		Source:              bounds.Source,
		Complete:            bounds.Complete,
		Gaps:                bounds.Gaps,
		Precision:           precision,
		RetentionEpoch:      source.RetentionEpoch,
		RetentionGeneration: source.RetentionGeneration,
		PurgeState:          source.PurgeState,
		SourceRevision:      source.SourceRevision,
	}
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
	token, bounds, err := s.resolveQueryContextFromRequest(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
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
		return statsdomain.LoadUsageSummary(r.Context(), tx, profile.ID, bounds, s.nowUTC(), reportCurrencyCode, reportCurrencySymbol)
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
	var requestedFrom, requestedTo *time.Time
	if token.RequestedFrom != nil {
		parsed, parseErr := time.Parse(time.RFC3339, *token.RequestedFrom)
		if parseErr != nil {
			return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "invalid query_context"}
		}
		requestedFrom = &parsed
	}
	if token.RequestedTo != nil {
		parsed, parseErr := time.Parse(time.RFC3339, *token.RequestedTo)
		if parseErr != nil {
			return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "invalid query_context"}
		}
		requestedTo = &parsed
	}
	usageFrom, err := time.Parse(time.RFC3339, token.UsageFrom)
	if err != nil {
		return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	usageTo, err := time.Parse(time.RFC3339, token.UsageTo)
	if err != nil {
		return token, statsdomain.QueryBounds{}, &statsdomain.HTTPError{StatusCode: 422, Detail: "invalid query_context"}
	}
	// The usage snapshot is the only place the frozen retention floor and
	// coverage gaps survive; without them every fragment would report a null
	// gap list and no floor while the query context itself carries both.
	usageSnapshot := token.Domains["usage_request_events"]
	bounds := statsdomain.QueryBounds{
		RequestedPreset:    token.RequestedPreset,
		RequestedFrom:      requestedFrom,
		RequestedTo:        requestedTo,
		UsageFrom:          usageFrom,
		UsageTo:            usageTo,
		UsageRetentionFrom: usageSnapshot.RetentionFromTime,
		Source:             token.Source,
		Complete:           token.Complete,
		Gaps:               append(make([]statsdomain.CoverageGap, 0, len(usageSnapshot.Gaps)), usageSnapshot.Gaps...),
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

// loadStatsRetentionSource consumes the Observe-owned retention source. NULL
// means no configured logical floor; it is not a hidden 30-day default.
func loadStatsRetentionSource(ctx context.Context, tx pgx.Tx, referenceNow time.Time) (statsdomain.RetentionFloorEpochSource, error) {
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, "usage_request_events", referenceNow)
	if err != nil {
		return statsdomain.RetentionFloorEpochSource{}, fmt.Errorf("load usage retention source: %w", err)
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return statsdomain.RetentionFloorEpochSource{}, &statsdomain.HTTPError{StatusCode: http.StatusServiceUnavailable, Code: "usage_purge_in_progress", Detail: "usage data is temporarily unavailable while retention cleanup is publishing"}
	}
	return source, nil
}

func effectiveRetentionFloor(source statsdomain.RetentionFloorEpochSource) *time.Time {
	floor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
		floor = source.PublishedFloor
	}
	if floor == nil {
		return nil
	}
	resolved := floor.UTC()
	return &resolved
}

// observabilitySigningKey returns the domain-separated HMAC subkey used to
// sign query contexts and cursors.
func (s *Service) observabilitySigningKey() []byte {
	return statsdomain.DeriveQuerySigningKey(s.secretEncryptionKey)
}

// handleUsageSeries returns the single main chart for a query context.
func (s *Service) handleUsageSeries(w http.ResponseWriter, r *http.Request) {
	token, bounds, err := s.resolveQueryContextFromRequest(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	metric := strings.TrimSpace(r.URL.Query().Get("metric"))
	if metric == "" {
		metric = "requests"
	}
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
		return statsdomain.LoadUsageSeries(r.Context(), tx, profile.ID, bounds, metric, groupBy, interval, seriesLimit, s.nowUTC(), reportCurrencyCode, reportCurrencySymbol)
	})
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	responseutil.WriteJSON(w, http.StatusOK, response)
}

// handleDashboardNow returns the Now strip (rolling 30m RPM/TPM).
func (s *Service) handleDashboardNow(w http.ResponseWriter, r *http.Request) {
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
	rawQueryContext := strings.TrimSpace(r.URL.Query().Get("query_context"))
	token, bounds, err := s.resolveQueryContextFromRequest(r)
	if err != nil {
		writeDomainError(w, r, s.corsSnapshot(), err)
		return
	}
	params := statsdomain.UsageErrorsParams{
		GroupBy: strings.TrimSpace(r.URL.Query().Get("group_by")),
		Limit:   parsePositiveQueryIntDefault(r, "limit", 20),
	}
	if modelID := strings.TrimSpace(r.URL.Query().Get("model_id")); modelID != "" {
		params.ModelID = &modelID
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
		return statsdomain.LoadUsageErrors(r.Context(), tx, profile.ID, bounds, params, rawQueryContext, s.nowUTC())
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
