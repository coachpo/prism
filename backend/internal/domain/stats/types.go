package stats

import (
	"encoding/json"
	"time"
)

type RequestLogListParams struct {
	ProfileID        int
	IngressRequestID *string
	ModelID          *string
	StatusFamily     *string
	FromTime         *time.Time
	ToTime           *time.Time
	EndpointID       *int
	Limit            int
	Offset           int
}

type RequestLogFilterEndpointOption struct {
	EndpointID    int    `json:"endpoint_id"`
	EndpointLabel string `json:"endpoint_label"`
}

type RequestLogFilterModelOption struct {
	ModelID    string `json:"model_id"`
	ModelLabel string `json:"model_label"`
}

type RequestLogListFilterOptions struct {
	Endpoints []RequestLogFilterEndpointOption `json:"endpoints"`
	Models    []RequestLogFilterModelOption    `json:"models"`
}

type RequestLogListItem struct {
	ID                          int       `json:"id"`
	CreatedAt                   time.Time `json:"created_at"`
	ModelID                     string    `json:"model_id"`
	ModelLabel                  string    `json:"model_label"`
	ResolvedTargetModelID       *string   `json:"resolved_target_model_id"`
	ResolvedTargetModelLabel    *string   `json:"resolved_target_model_label"`
	APIFamily                   string    `json:"api_family"`
	VendorID                    *int      `json:"vendor_id"`
	VendorKey                   *string   `json:"vendor_key"`
	VendorName                  *string   `json:"vendor_name"`
	EndpointID                  *int      `json:"endpoint_id"`
	EndpointLabel               string    `json:"endpoint_label"`
	ConnectionID                *int      `json:"connection_id"`
	StatusCode                  int       `json:"status_code"`
	ResponseTimeMS              int       `json:"response_time_ms"`
	TTFTMS                      *int      `json:"ttft_ms"`
	CompletionDurationMS        *int      `json:"completion_duration_ms"`
	IsStream                    bool      `json:"is_stream"`
	StreamOutcome               string    `json:"stream_outcome"`
	StreamErrorKind             *string   `json:"stream_error_kind"`
	OutputTokens                *int      `json:"output_tokens"`
	TotalTokens                 *int      `json:"total_tokens"`
	TotalCostUserCurrencyMicros *int64    `json:"total_cost_user_currency_micros"`
	PricedFlag                  *bool     `json:"priced_flag"`
	UnpricedReason              *string   `json:"unpriced_reason"`
	ReasoningEffort             *string   `json:"reasoning_effort"`
	ReportCurrencySymbol        *string   `json:"report_currency_symbol"`
	CallerClientDisplay         *string   `json:"caller_client_display"`
	UpstreamClientDisplay       *string   `json:"upstream_client_display"`
	UserAgentOverridden         bool      `json:"user_agent_overridden"`
}

type RequestLogListResponse struct {
	FilterOptions RequestLogListFilterOptions `json:"filter_options"`
	Items         []RequestLogListItem        `json:"items"`
	Total         int                         `json:"total"`
	Limit         int                         `json:"limit"`
	Offset        int                         `json:"offset"`
}

type RequestLogDetailSummary struct {
	ID                       int       `json:"id"`
	CreatedAt                time.Time `json:"created_at"`
	ModelID                  string    `json:"model_id"`
	ModelLabel               string    `json:"model_label"`
	ResolvedTargetModelID    *string   `json:"resolved_target_model_id"`
	ResolvedTargetModelLabel *string   `json:"resolved_target_model_label"`
	APIFamily                string    `json:"api_family"`
	VendorID                 *int      `json:"vendor_id"`
	VendorKey                *string   `json:"vendor_key"`
	VendorName               *string   `json:"vendor_name"`
	StatusCode               int       `json:"status_code"`
	ResponseTimeMS           int       `json:"response_time_ms"`
	TTFTMS                   *int      `json:"ttft_ms"`
	CompletionDurationMS     *int      `json:"completion_duration_ms"`
	IsStream                 bool      `json:"is_stream"`
	StreamOutcome            string    `json:"stream_outcome"`
	StreamErrorKind          *string   `json:"stream_error_kind"`
	StreamErrorDetail        *string   `json:"stream_error_detail"`
}

type RequestLogDetailRequest struct {
	RequestPath                   string           `json:"request_path"`
	IngressRequestID              *string          `json:"ingress_request_id"`
	AttemptNumber                 *int             `json:"attempt_number"`
	ProviderCorrelationID         *string          `json:"provider_correlation_id"`
	ProxyAPIKeyID                 *int             `json:"proxy_api_key_id"`
	ProxyAPIKeyNameSnapshot       *string          `json:"proxy_api_key_name_snapshot"`
	CallerUserAgent               *string          `json:"caller_user_agent"`
	UpstreamUserAgent             *string          `json:"upstream_user_agent"`
	CallerClientDisplay           *string          `json:"caller_client_display"`
	UpstreamClientDisplay         *string          `json:"upstream_client_display"`
	UserAgentOverridden           bool             `json:"user_agent_overridden"`
	ErrorDetail                   *string          `json:"error_detail"`
	RequestGenerationParams       *json.RawMessage `json:"request_generation_params"`
	RequestGenerationParamsStatus *string          `json:"request_generation_params_status"`
}

type RequestLogDetailRouting struct {
	ProfileID                   int     `json:"profile_id"`
	EndpointLabel               string  `json:"endpoint_label"`
	EndpointID                  *int    `json:"endpoint_id"`
	ConnectionID                *int    `json:"connection_id"`
	EndpointBaseURL             *string `json:"endpoint_base_url"`
	EndpointDescription         *string `json:"endpoint_description"`
	AuditEnabledAtRequest       bool    `json:"audit_enabled_at_request"`
	AuditCaptureBodiesAtRequest bool    `json:"audit_capture_bodies_at_request"`
}

type RequestLogDetailUsage struct {
	InputTokens              *int    `json:"input_tokens"`
	OutputTokens             *int    `json:"output_tokens"`
	TotalTokens              *int    `json:"total_tokens"`
	SuccessFlag              *bool   `json:"success_flag"`
	BillableFlag             *bool   `json:"billable_flag"`
	PricedFlag               *bool   `json:"priced_flag"`
	UnpricedReason           *string `json:"unpriced_reason"`
	CacheReadInputTokens     *int    `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int    `json:"cache_creation_input_tokens"`
	ReasoningTokens          *int    `json:"reasoning_tokens"`
}

type RequestLogDetailCosting struct {
	InputCostMicros              *int64  `json:"input_cost_micros"`
	OutputCostMicros             *int64  `json:"output_cost_micros"`
	CacheReadInputCostMicros     *int64  `json:"cache_read_input_cost_micros"`
	CacheCreationInputCostMicros *int64  `json:"cache_creation_input_cost_micros"`
	ReasoningCostMicros          *int64  `json:"reasoning_cost_micros"`
	TotalCostOriginalMicros      *int64  `json:"total_cost_original_micros"`
	TotalCostUserCurrencyMicros  *int64  `json:"total_cost_user_currency_micros"`
	CurrencyCodeOriginal         *string `json:"currency_code_original"`
	ReportCurrencyCode           *string `json:"report_currency_code"`
	ReportCurrencySymbol         *string `json:"report_currency_symbol"`
	FXRateUsed                   *string `json:"fx_rate_used"`
	FXRateSource                 *string `json:"fx_rate_source"`
}

type RequestLogDetailPricing struct {
	PricingSnapshotUnit               *string `json:"pricing_snapshot_unit"`
	PricingSnapshotInput              *string `json:"pricing_snapshot_input"`
	PricingSnapshotOutput             *string `json:"pricing_snapshot_output"`
	PricingSnapshotCacheReadInput     *string `json:"pricing_snapshot_cache_read_input"`
	PricingSnapshotCacheCreationInput *string `json:"pricing_snapshot_cache_creation_input"`
	PricingSnapshotReasoning          *string `json:"pricing_snapshot_reasoning"`
	PricingConfigVersionUsed          *int    `json:"pricing_config_version_used"`
}

type RequestLogDetailResponse struct {
	Summary RequestLogDetailSummary `json:"summary"`
	Request RequestLogDetailRequest `json:"request"`
	Routing RequestLogDetailRouting `json:"routing"`
	Usage   RequestLogDetailUsage   `json:"usage"`
	Costing RequestLogDetailCosting `json:"costing"`
	Pricing RequestLogDetailPricing `json:"pricing"`
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
}

type ModelMetricsBatchItem struct {
	ModelID         string  `json:"model_id"`
	SuccessRate     float64 `json:"success_rate"`
	RequestCount24H int     `json:"request_count_24h"`
	P95LatencyMS    int     `json:"p95_latency_ms"`
	Spend30DMicros  int64   `json:"spend_30d_micros"`
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
	CachedTokens         int     `json:"cached_tokens"`
	ReasoningTokens      int     `json:"reasoning_tokens"`
	AverageRPM           float64 `json:"average_rpm"`
	AverageTPM           float64 `json:"average_tpm"`
	TotalCostMicros      int64   `json:"total_cost_micros"`
	RollingWindowMinutes int     `json:"rolling_window_minutes"`
	RollingRequestCount  int     `json:"rolling_request_count"`
	RollingTokenCount    int     `json:"rolling_token_count"`
	RollingRPM           float64 `json:"rolling_rpm"`
	RollingTPM           float64 `json:"rolling_tpm"`
}

type UsageServiceHealthCell struct {
	BucketStart            time.Time `json:"bucket_start"`
	RequestCount           int       `json:"request_count"`
	SuccessCount           int       `json:"success_count"`
	FailedCount            int       `json:"failed_count"`
	AvailabilityPercentage *float64  `json:"availability_percentage"`
	Status                 string    `json:"status"`
}

type UsageServiceHealth struct {
	AvailabilityPercentage *float64                 `json:"availability_percentage"`
	RequestCount           int                      `json:"request_count"`
	SuccessCount           int                      `json:"success_count"`
	FailedCount            int                      `json:"failed_count"`
	IntervalMinutes        int                      `json:"interval_minutes"`
	Cells                  []UsageServiceHealthCell `json:"cells"`
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
	ServiceHealth         UsageServiceHealth          `json:"service_health"`
	RequestTrends         UsageRequestTrends          `json:"request_trends"`
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
