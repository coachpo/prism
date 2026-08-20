package stats

import (
	"time"
)

type RequestLogListParams struct {
	ProfileID             int
	IngressRequestID      *string
	IngressFinalResult    *string
	ConfirmedFailover     *bool
	ModelID               *string
	ResolvedTargetModelID *string
	StatusFamily          *string
	StatusCode            *int
	ErrorText             *string
	PricingStatus         *string
	UnpricedReasons       []string
	FromTime              *time.Time
	ToTime                *time.Time
	EndpointID            *int
	TerminalTargetID      *int
	ProxyAPIKeyID         *int
	ClientRuleID          *int
	ClientRulePattern     *string
	// Observe signed-context deep-link selectors (Observe SPEC §4.3): when
	// any final_* selector is present the query_context must have been
	// validated and the final cohort is resolved through usage_request_events
	// (the authoritative finalized ingress summary), never from retained rows.
	QueryContextFrom      *time.Time
	QueryContextTo        *time.Time
	FinalResult           *string
	FinalModelID          *string
	FinalEndpointID       *int
	FinalTerminalTargetID *int
	FinalPricingStatus    *string
	FinalUnpricedReasons  []string
	FinalReportingEpoch   *string
	// CoverageRequestedFrom/To carry the parsed explicit request bounds (nil
	// when absent) so the coverage projection can be resolved in the same
	// snapshot as the rows.
	CoveragePreset        string
	CoverageRequestedFrom *time.Time
	CoverageRequestedTo   *time.Time
	// CoverageReferenceNow is the server-owned clock used to resolve the
	// selected preset and to read the matching actual-coverage snapshot. HTTP
	// callers pass the service clock; direct domain callers may leave it zero.
	CoverageReferenceNow time.Time
	Coverage             *QueryCoverage
	// SortBy/SortOrder carry the attempt-view sort grammar. HTTP callers pass
	// validated values; an empty SortBy keeps the created_at default.
	SortBy    string
	SortOrder string
	Limit     int
	Offset    int
}

type RequestLogFilterEndpointOption struct {
	EndpointID    int    `json:"endpoint_id"`
	EndpointLabel string `json:"endpoint_label"`
}

type RequestLogFilterModelOption struct {
	ModelID    string `json:"model_id"`
	ModelLabel string `json:"model_label"`
}

type RequestLogFilterResolvedTargetModelOption struct {
	ResolvedTargetModelID string `json:"resolved_target_model_id"`
	ModelLabel            string `json:"model_label"`
}

type RequestLogFilterClientOption struct {
	ClientRuleID int    `json:"client_rule_id"`
	ClientLabel  string `json:"client_label"`
}

type RequestLogListFilterOptions struct {
	Endpoints            []RequestLogFilterEndpointOption            `json:"endpoints"`
	Models               []RequestLogFilterModelOption               `json:"models"`
	ResolvedTargetModels []RequestLogFilterResolvedTargetModelOption `json:"resolved_target_models"`
	Clients              []RequestLogFilterClientOption              `json:"clients"`
}

// ProxyAPIKeyFilterOption is a selectable proxy-key identity. Configured
// identities come from the current key table; deleted identities remain
// selectable when an immutable request-log snapshot survives in the selected
// time window.
type ProxyAPIKeyFilterOption struct {
	ProxyAPIKeyID int     `json:"proxy_api_key_id"`
	Name          string  `json:"proxy_api_key_name"`
	KeyPreview    *string `json:"key_preview"`
	Configured    bool    `json:"configured"`
}

type ProxyAPIKeyFilterOptionsResponse struct {
	Items            []ProxyAPIKeyFilterOption `json:"items"`
	Selected         *ProxyAPIKeyFilterOption  `json:"selected"`
	NextCursor       *string                   `json:"next_cursor"`
	ResolvedFromTime *time.Time                `json:"resolved_from_time"`
	ResolvedToTime   *time.Time                `json:"resolved_to_time"`
}

type ProxyAPIKeyFilterOptionsParams struct {
	ProfileID  int
	Query      *string
	FromTime   *time.Time
	ToTime     *time.Time
	Limit      int
	Cursor     *string
	SelectedID *int
}

type RequestLogListItem struct {
	RequestLogID                  string    `json:"request_log_id"`
	StatusCode                    *int      `json:"status_code"`
	RowKind                       string    `json:"row_kind"`
	IngressRequestID              *string   `json:"ingress_request_id"`
	AttemptNumber                 *int      `json:"attempt_number"`
	AttemptTrigger                *string   `json:"attempt_trigger"`
	AttemptResult                 *string   `json:"attempt_result"`
	IsWinner                      *bool     `json:"is_winner"`
	CreatedAt                     time.Time `json:"created_at"`
	ModelID                       string    `json:"model_id"`
	ModelLabel                    string    `json:"model_label"`
	ResolvedTargetModelID         *string   `json:"resolved_target_model_id"`
	ResolvedTargetModelLabel      *string   `json:"resolved_target_model_label"`
	APIFamily                     string    `json:"api_family"`
	EndpointID                    *int      `json:"endpoint_id"`
	EndpointLabel                 string    `json:"endpoint_label"`
	TerminalTargetID              *int      `json:"terminal_target_id"`
	ProxyAPIKeyID                 *int      `json:"proxy_api_key_id"`
	ProxyAPIKeyNameSnapshot       *string   `json:"proxy_api_key_name_snapshot"`
	ProxyAPIKeyAttributionState   string    `json:"proxy_api_key_attribution_state"`
	ProxyAPIKeyAuthEnforced       *bool     `json:"proxy_api_key_auth_enforced_at_request"`
	UpstreamStatusCode            *int      `json:"upstream_status_code"`
	GatewayStatusCode             *int      `json:"gateway_status_code"`
	LegacyStatusCode              *int      `json:"legacy_status_code"`
	AttemptDurationMS             *int      `json:"attempt_duration_ms"`
	LegacyDurationMS              *int      `json:"legacy_duration_ms"`
	TTFTMS                        *int      `json:"ttft_ms"`
	CompletionDurationMS          *int      `json:"completion_duration_ms"`
	IsStream                      bool      `json:"is_stream"`
	StreamOutcome                 string    `json:"stream_outcome"`
	StreamErrorKind               *string   `json:"stream_error_kind"`
	ErrorSource                   *string   `json:"error_source"`
	ErrorCode                     *string   `json:"error_code"`
	FailureStage                  *string   `json:"failure_stage"`
	FailureDetailPreview          *string   `json:"failure_detail_preview"`
	FailureDetailSource           string    `json:"failure_detail_source"`
	FailureDetailPreviewTruncated bool      `json:"failure_detail_preview_truncated"`
	FailureDetailRedacted         bool      `json:"failure_detail_redacted"`
	OutputTokens                  *int      `json:"output_tokens"`
	TotalTokens                   *int      `json:"total_tokens"`
	TotalCostUserCurrencyMicros   *int64    `json:"total_cost_user_currency_micros"`
	PricingStatus                 string    `json:"pricing_status"`
	UnpricedReason                *string   `json:"unpriced_reason"`
	PricingResolutionKind         *string   `json:"pricing_resolution_kind"`
	PricingEvidenceTrust          string    `json:"pricing_evidence_trust"`
	// Typed pricing evidence deliberately stays on the list DTO so the table
	// can distinguish family, selector state, and selected role without reading
	// detail-only snapshots.
	PricingTemplateKind            *string `json:"pricing_template_kind"`
	PricingSelectionState          *string `json:"pricing_selection_state"`
	PricingCardRole                *string `json:"pricing_card_role"`
	PricingSelectorThresholdTokens *int    `json:"pricing_selector_threshold_tokens"`
	PricingSelectorBasisTokens     *int64  `json:"pricing_selector_basis_tokens"`
	ReasoningEffort                *string `json:"reasoning_effort"`
	ReportCurrencySymbol           *string `json:"report_currency_symbol"`
	CallerClientDisplay            *string `json:"caller_client_display"`
	UpstreamClientDisplay          *string `json:"upstream_client_display"`
	UserAgentOverridden            bool    `json:"user_agent_overridden"`
	TerminalTargetLabel            *string `json:"terminal_target_label"`
	TerminalTargetConfigured       bool    `json:"terminal_target_configured"`
	TerminalTargetOwnerModelID     *string `json:"terminal_target_owner_model_id"`
}

type RequestLogListResponse struct {
	FilterOptions RequestLogListFilterOptions `json:"filter_options"`
	Items         []RequestLogListItem        `json:"items"`
	Total         int                         `json:"total"`
	// TotalIsExact is false when the browse count hit requestLogCountCap and
	// Total is the capped value rather than the exact row count.
	TotalIsExact bool `json:"total_is_exact"`
	// HasMore is true when the requested page is not the last one; the list
	// query fetches limit+1 rows to decide this without an extra count pass.
	HasMore bool `json:"has_more"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	// Coverage is the non-null ordinary query-scoped retention projection
	// (Requests SPEC: every successful ordinary attempts envelope carries it).
	Coverage QueryCoverage `json:"coverage"`
}

type DashboardRecentActivityItem struct {
	RequestLogID                int       `json:"request_log_id"`
	CreatedAt                   time.Time `json:"created_at"`
	ModelID                     string    `json:"model_id"`
	ModelLabel                  string    `json:"model_label"`
	ResolvedTargetModelID       *string   `json:"resolved_target_model_id"`
	ResolvedTargetModelLabel    *string   `json:"resolved_target_model_label"`
	EndpointID                  *int      `json:"endpoint_id"`
	EndpointLabel               string    `json:"endpoint_label"`
	StatusCode                  *int      `json:"status_code"`
	ResponseTimeMS              *int      `json:"response_time_ms"`
	TTFTMS                      *int      `json:"ttft_ms"`
	CompletionDurationMS        *int      `json:"completion_duration_ms"`
	IsStream                    bool      `json:"is_stream"`
	StreamOutcome               string    `json:"stream_outcome"`
	TotalTokens                 *int      `json:"total_tokens"`
	TotalCostUserCurrencyMicros *int64    `json:"total_cost_user_currency_micros"`
	PricingStatus               string    `json:"pricing_status"`
	UnpricedReason              *string   `json:"unpriced_reason"`
	ReportCurrencySymbol        *string   `json:"report_currency_symbol"`
}

type DashboardRecentActivityWatermark struct {
	LatestRequestLogCreatedAt *time.Time `json:"latest_request_log_created_at"`
	LatestRequestLogID        *int       `json:"latest_request_log_id"`
}

type DashboardRecentActivityResponse struct {
	GeneratedAt       time.Time                        `json:"generated_at"`
	ActivityWatermark DashboardRecentActivityWatermark `json:"activity_watermark"`
	Items             []DashboardRecentActivityItem    `json:"items"`
}

type StatsSummaryParams struct {
	ProfileID    int
	FromTime     *time.Time
	ToTime       *time.Time
	GroupBy      *string
	ModelID      *string
	APIFamily    *string
	EndpointID   *int
	ConnectionID *int
}

type StatGroup struct {
	Key               string  `json:"key"`
	TotalRequests     int     `json:"total_requests"`
	SuccessCount      int     `json:"success_count"`
	ErrorCount        int     `json:"error_count"`
	AvgResponseTimeMS float64 `json:"avg_response_time_ms"`
	TotalTokens       int     `json:"total_tokens"`
}

type StatsSummaryResponse struct {
	TotalRequests     int         `json:"total_requests"`
	SuccessCount      int         `json:"success_count"`
	ErrorCount        int         `json:"error_count"`
	SuccessRate       float64     `json:"success_rate"`
	AvgResponseTimeMS float64     `json:"avg_response_time_ms"`
	P95ResponseTimeMS int         `json:"p95_response_time_ms"`
	TotalInputTokens  int         `json:"total_input_tokens"`
	TotalOutputTokens int         `json:"total_output_tokens"`
	TotalTokens       int         `json:"total_tokens"`
	Groups            []StatGroup `json:"groups"`
	// Granularity is always "request": the summary is built from the
	// request-level usage-event records, not attempt rows.
	Granularity string `json:"granularity"`
	// LatencyBasis is always "end_to_end": stream durations include the
	// finalization time, not just the response-header time.
	LatencyBasis string `json:"latency_basis"`
}

type ModelMetricsBatchItem struct {
	ModelID         string   `json:"model_id"`
	SuccessRate     *float64 `json:"success_rate"`      // null when the window has no samples
	RequestCount24H int      `json:"request_count_24h"` // 0 is a fact; stays non-pointer
	P95LatencyMS    *int     `json:"p95_latency_ms"`    // null without latency samples
	Spend30DMicros  *int64   `json:"spend_30d_micros"`  // null without trusted pricing evidence
}

type ModelMetricsBatchResponse struct {
	Items []ModelMetricsBatchItem `json:"items"`
}

type ConnectionSuccessRate struct {
	ConnectionID  int      `json:"connection_id"`
	TotalRequests int      `json:"total_requests"`
	SuccessCount  int      `json:"success_count"`
	ErrorCount    int      `json:"error_count"`
	SuccessRate   *float64 `json:"success_rate"`
}

type ThroughputBucket struct {
	Timestamp    time.Time `json:"timestamp"`
	RequestCount int       `json:"request_count"`
	RPM          float64   `json:"rpm"`
}

type ThroughputStatsResponse struct {
	AverageRPM        float64            `json:"average_rpm"`
	PeakRPM           float64            `json:"peak_rpm"`
	CurrentRPM        float64            `json:"current_rpm"`
	TotalRequests     int                `json:"total_requests"`
	TimeWindowSeconds float64            `json:"time_window_seconds"`
	Buckets           []ThroughputBucket `json:"buckets"`
}

type SpendingSummary struct {
	TotalCostMicros                   int64 `json:"total_cost_micros"`
	SuccessfulRequestCount            int   `json:"successful_request_count"`
	PricedRequestCount                int   `json:"priced_request_count"`
	UnpricedRequestCount              int   `json:"unpriced_request_count"`
	TotalInputTokens                  int   `json:"total_input_tokens"`
	TotalOutputTokens                 int   `json:"total_output_tokens"`
	TotalCacheReadInputTokens         int   `json:"total_cache_read_input_tokens"`
	TotalCacheCreationInputTokens     int   `json:"total_cache_creation_input_tokens"`
	TotalReasoningTokens              int   `json:"total_reasoning_tokens"`
	TotalTokens                       int   `json:"total_tokens"`
	AvgCostPerSuccessfulRequestMicros int64 `json:"avg_cost_per_successful_request_micros"`
}

type SpendingGroupRow struct {
	Key              string `json:"key"`
	TotalCostMicros  int64  `json:"total_cost_micros"`
	TotalRequests    int    `json:"total_requests"`
	PricedRequests   int    `json:"priced_requests"`
	UnpricedRequests int    `json:"unpriced_requests"`
	TotalTokens      int    `json:"total_tokens"`
}

type SpendingTopModel struct {
	ModelID         string `json:"model_id"`
	ModelLabel      string `json:"model_label"`
	TotalCostMicros int64  `json:"total_cost_micros"`
}

type SpendingTopEndpoint struct {
	EndpointID      *int   `json:"endpoint_id"`
	EndpointLabel   string `json:"endpoint_label"`
	TotalCostMicros int64  `json:"total_cost_micros"`
}

type SpendingReportResponse struct {
	Summary              SpendingSummary       `json:"summary"`
	Groups               []SpendingGroupRow    `json:"groups"`
	GroupsTotal          int                   `json:"groups_total"`
	TopSpendingModels    []SpendingTopModel    `json:"top_spending_models"`
	TopSpendingEndpoints []SpendingTopEndpoint `json:"top_spending_endpoints"`
	UnpricedBreakdown    map[string]int        `json:"unpriced_breakdown"`
	ReportCurrencyCode   string                `json:"report_currency_code"`
	ReportCurrencySymbol string                `json:"report_currency_symbol"`
}

type UsageSnapshotTimeRange struct {
	Preset  string     `json:"preset"`
	StartAt *time.Time `json:"start_at"`
	EndAt   time.Time  `json:"end_at"`
}

type UsageSnapshotCurrency struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol"`
}

type UsageSnapshotOverview struct {
	TotalRequests   int     `json:"total_requests"`
	SuccessRequests int     `json:"success_requests"`
	FailedRequests  int     `json:"failed_requests"`
	SuccessRate     float64 `json:"success_rate"`
	TotalTokens     int     `json:"total_tokens"`
	InputTokens     int     `json:"input_tokens"`
	OutputTokens    int     `json:"output_tokens"`
	// CachedTokens is derived from cache-read plus cache-creation input tokens for aggregate presentation only.
	CachedTokens    int `json:"cached_tokens"`
	ReasoningTokens int `json:"reasoning_tokens"`
	// TokenComponentBasis names the caliber of the component fields above.
	// "disjoint" means InputTokens excludes cache-read and cache-creation
	// input and OutputTokens excludes reasoning output.
	TokenComponentBasis string `json:"token_component_basis"`
	// UncategorizedTokens is TotalTokens minus the sum of the disjoint
	// components, clamped at zero. A positive value means upstreams reported
	// provider totals the components cannot reconstruct - typically usage
	// payloads that carry only a total.
	UncategorizedTokens  int     `json:"uncategorized_tokens"`
	AverageRPM           float64 `json:"average_rpm"`
	AverageTPM           float64 `json:"average_tpm"`
	TotalCostMicros      int64   `json:"total_cost_micros"`
	RollingWindowMinutes int     `json:"rolling_window_minutes"`
	RollingRequestCount  int     `json:"rolling_request_count"`
	RollingTokenCount    int     `json:"rolling_token_count"`
	RollingRPM           float64 `json:"rolling_rpm"`
	RollingTPM           float64 `json:"rolling_tpm"`
}

type UsageRequestTrendPoint struct {
	BucketStart  time.Time `json:"bucket_start"`
	RequestCount int       `json:"request_count"`
	SuccessCount int       `json:"success_count"`
	FailedCount  int       `json:"failed_count"`
	RPM          float64   `json:"rpm"`
}

type UsageRequestTrendSeries struct {
	Key           string                   `json:"key"`
	Label         string                   `json:"label"`
	TotalRequests int                      `json:"total_requests"`
	Points        []UsageRequestTrendPoint `json:"points"`
}

type UsageRequestTrends struct {
	Hourly []UsageRequestTrendSeries `json:"hourly"`
	Daily  []UsageRequestTrendSeries `json:"daily"`
}

type UsageLatencyTrendPoint struct {
	BucketStart time.Time `json:"bucket_start"`
	P50MS       *int      `json:"p50_ms"`
	P95MS       *int      `json:"p95_ms"`
}

type UsageLatencyTrendSeries struct {
	Key    string                   `json:"key"`
	Label  string                   `json:"label"`
	Points []UsageLatencyTrendPoint `json:"points"`
}

type UsageLatencyTrends struct {
	Hourly []UsageLatencyTrendSeries `json:"hourly"`
	Daily  []UsageLatencyTrendSeries `json:"daily"`
}

type UsageTokenTrendPoint struct {
	BucketStart  time.Time `json:"bucket_start"`
	TotalTokens  int       `json:"total_tokens"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	// CachedTokens is derived from cache-read plus cache-creation input tokens for aggregate presentation only.
	CachedTokens    int     `json:"cached_tokens"`
	ReasoningTokens int     `json:"reasoning_tokens"`
	TPM             float64 `json:"tpm"`
}

type UsageTokenTrendSeries struct {
	Key         string                 `json:"key"`
	Label       string                 `json:"label"`
	TotalTokens int                    `json:"total_tokens"`
	Points      []UsageTokenTrendPoint `json:"points"`
}

type UsageTokenUsageTrends struct {
	Hourly []UsageTokenTrendSeries `json:"hourly"`
	Daily  []UsageTokenTrendSeries `json:"daily"`
}

type UsageTokenTypeBreakdownPoint struct {
	BucketStart  time.Time `json:"bucket_start"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	// CachedTokens is derived from cache-read plus cache-creation input tokens for aggregate presentation only.
	CachedTokens    int `json:"cached_tokens"`
	ReasoningTokens int `json:"reasoning_tokens"`
}

type UsageTokenTypeBreakdown struct {
	Hourly []UsageTokenTypeBreakdownPoint `json:"hourly"`
	Daily  []UsageTokenTypeBreakdownPoint `json:"daily"`
}

type UsageCostOverviewPoint struct {
	BucketStart     time.Time `json:"bucket_start"`
	TotalCostMicros int64     `json:"total_cost_micros"`
}

type UsageCostOverview struct {
	TotalCostMicros      int64                    `json:"total_cost_micros"`
	PricedRequestCount   int                      `json:"priced_request_count"`
	UnpricedRequestCount int                      `json:"unpriced_request_count"`
	Hourly               []UsageCostOverviewPoint `json:"hourly"`
	Daily                []UsageCostOverviewPoint `json:"daily"`
}

type UsageEndpointStatistic struct {
	EndpointID       *int     `json:"endpoint_id"`
	EndpointLabel    string   `json:"endpoint_label"`
	RequestCount     int      `json:"request_count"`
	SuccessRate      float64  `json:"success_rate"`
	P50TTFTMS        *int     `json:"p50_ttft_ms"`
	P95TTFTMS        *int     `json:"p95_ttft_ms"`
	AvgOutputRateTPS *float64 `json:"avg_output_rate_tps"`
	TotalTokens      int      `json:"total_tokens"`
	TotalCostMicros  int64    `json:"total_cost_micros"`
}

type UsageModelStatistic struct {
	ModelID              string  `json:"model_id"`
	ModelLabel           string  `json:"model_label"`
	RequestCount         int     `json:"request_count"`
	SuccessCount         *int    `json:"success_count"`
	FailedCount          *int    `json:"failed_count"`
	PricedRequestCount   *int    `json:"priced_request_count"`
	UnpricedRequestCount *int    `json:"unpriced_request_count"`
	SuccessRate          float64 `json:"success_rate"`
	P50TTFTMS            *int    `json:"p50_ttft_ms"`
	P95TTFTMS            *int    `json:"p95_ttft_ms"`
	InputTokens          *int    `json:"input_tokens"`
	OutputTokens         *int    `json:"output_tokens"`
	// CachedTokens is derived from cache-read plus cache-creation input tokens for aggregate presentation only.
	CachedTokens     *int     `json:"cached_tokens"`
	ReasoningTokens  *int     `json:"reasoning_tokens"`
	TotalTokens      int      `json:"total_tokens"`
	TotalCostMicros  int64    `json:"total_cost_micros"`
	AvgOutputRateTPS *float64 `json:"avg_output_rate_tps"`
}

type UsageProxyAPIKeyStatistic struct {
	ProxyAPIKeyID    *int    `json:"proxy_api_key_id"`
	ProxyAPIKeyLabel string  `json:"proxy_api_key_label"`
	RequestCount     int     `json:"request_count"`
	SuccessRate      float64 `json:"success_rate"`
	TotalTokens      int     `json:"total_tokens"`
	TotalCostMicros  int64   `json:"total_cost_micros"`
}

type UsageSnapshotResponse struct {
	GeneratedAt           time.Time                   `json:"generated_at"`
	TimeRange             UsageSnapshotTimeRange      `json:"time_range"`
	Currency              UsageSnapshotCurrency       `json:"currency"`
	Overview              UsageSnapshotOverview       `json:"overview"`
	RequestTrends         UsageRequestTrends          `json:"request_trends"`
	LatencyTrends         UsageLatencyTrends          `json:"latency_trends"`
	TokenUsageTrends      UsageTokenUsageTrends       `json:"token_usage_trends"`
	TokenTypeBreakdown    UsageTokenTypeBreakdown     `json:"token_type_breakdown"`
	CostOverview          UsageCostOverview           `json:"cost_overview"`
	EndpointStatistics    []UsageEndpointStatistic    `json:"endpoint_statistics"`
	ModelStatistics       []UsageModelStatistic       `json:"model_statistics"`
	ProxyAPIKeyStatistics []UsageProxyAPIKeyStatistic `json:"proxy_api_key_statistics"`
}

type EndpointModelStatistic struct {
	ModelID              string   `json:"model_id"`
	ModelLabel           string   `json:"model_label"`
	RequestCount         int      `json:"request_count"`
	SuccessCount         *int     `json:"success_count"`
	FailedCount          *int     `json:"failed_count"`
	PricedRequestCount   *int     `json:"priced_request_count"`
	UnpricedRequestCount *int     `json:"unpriced_request_count"`
	SuccessRate          float64  `json:"success_rate"`
	P50TTFTMS            *int     `json:"p50_ttft_ms"`
	P95TTFTMS            *int     `json:"p95_ttft_ms"`
	TotalTokens          int      `json:"total_tokens"`
	TotalCostMicros      int64    `json:"total_cost_micros"`
	AvgOutputRateTPS     *float64 `json:"avg_output_rate_tps"`
}
