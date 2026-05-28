package realtime

import (
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

type RequestLogEntry struct {
	ID                                int       `json:"id"`
	ProfileID                         int       `json:"profile_id"`
	ModelID                           string    `json:"model_id"`
	ModelLabel                        string    `json:"model_label"`
	ResolvedTargetModelID             *string   `json:"resolved_target_model_id"`
	ResolvedTargetModelLabel          *string   `json:"resolved_target_model_label"`
	APIFamily                         string    `json:"api_family"`
	VendorID                          *int      `json:"vendor_id"`
	VendorKey                         *string   `json:"vendor_key"`
	VendorName                        *string   `json:"vendor_name"`
	EndpointID                        *int      `json:"endpoint_id"`
	ConnectionID                      *int      `json:"connection_id"`
	ProxyAPIKeyID                     *int      `json:"proxy_api_key_id"`
	ProxyAPIKeyNameSnapshot           *string   `json:"proxy_api_key_name_snapshot"`
	IngressRequestID                  *string   `json:"ingress_request_id"`
	AttemptNumber                     *int      `json:"attempt_number"`
	ProviderCorrelationID             *string   `json:"provider_correlation_id"`
	EndpointBaseURL                   *string   `json:"endpoint_base_url"`
	StatusCode                        int       `json:"status_code"`
	ResponseTimeMS                    int       `json:"response_time_ms"`
	TTFTMS                            *int      `json:"ttft_ms"`
	CompletionDurationMS              *int      `json:"completion_duration_ms"`
	IsStream                          bool      `json:"is_stream"`
	StreamOutcome                     string    `json:"stream_outcome"`
	StreamErrorKind                   *string   `json:"stream_error_kind"`
	InputTokens                       *int      `json:"input_tokens"`
	OutputTokens                      *int      `json:"output_tokens"`
	TotalTokens                       *int      `json:"total_tokens"`
	SuccessFlag                       *bool     `json:"success_flag"`
	BillableFlag                      *bool     `json:"billable_flag"`
	PricedFlag                        *bool     `json:"priced_flag"`
	UnpricedReason                    *string   `json:"unpriced_reason"`
	CacheReadInputTokens              *int      `json:"cache_read_input_tokens"`
	CacheCreationInputTokens          *int      `json:"cache_creation_input_tokens"`
	ReasoningTokens                   *int      `json:"reasoning_tokens"`
	InputCostMicros                   *int64    `json:"input_cost_micros"`
	OutputCostMicros                  *int64    `json:"output_cost_micros"`
	CacheReadInputCostMicros          *int64    `json:"cache_read_input_cost_micros"`
	CacheCreationInputCostMicros      *int64    `json:"cache_creation_input_cost_micros"`
	ReasoningCostMicros               *int64    `json:"reasoning_cost_micros"`
	TotalCostOriginalMicros           *int64    `json:"total_cost_original_micros"`
	TotalCostUserCurrencyMicros       *int64    `json:"total_cost_user_currency_micros"`
	CurrencyCodeOriginal              *string   `json:"currency_code_original"`
	ReportCurrencyCode                *string   `json:"report_currency_code"`
	ReportCurrencySymbol              *string   `json:"report_currency_symbol"`
	FXRateUsed                        *string   `json:"fx_rate_used"`
	FXRateSource                      *string   `json:"fx_rate_source"`
	PricingSnapshotUnit               *string   `json:"pricing_snapshot_unit"`
	PricingSnapshotInput              *string   `json:"pricing_snapshot_input"`
	PricingSnapshotOutput             *string   `json:"pricing_snapshot_output"`
	PricingSnapshotCacheReadInput     *string   `json:"pricing_snapshot_cache_read_input"`
	PricingSnapshotCacheCreationInput *string   `json:"pricing_snapshot_cache_creation_input"`
	PricingSnapshotReasoning          *string   `json:"pricing_snapshot_reasoning"`
	PricingConfigVersionUsed          *int      `json:"pricing_config_version_used"`
	RequestPath                       string    `json:"request_path"`
	ErrorDetail                       *string   `json:"error_detail"`
	EndpointDescription               *string   `json:"endpoint_description"`
	CreatedAt                         time.Time `json:"created_at"`
}

type DashboardUpdateMessage struct {
	Type       string                        `json:"type"`
	RequestLog RequestLogEntry               `json:"request_log"`
	Snapshot   statsdomain.DashboardSnapshot `json:"snapshot"`
}

type AnalyticsSnapshotMessage struct {
	Type                                string                                       `json:"type"`
	Channel                             string                                       `json:"channel"`
	ProfileID                           int                                          `json:"profile_id"`
	Preset                              string                                       `json:"preset"`
	Sequence                            int64                                        `json:"sequence"`
	GeneratedAt                         time.Time                                    `json:"generated_at"`
	Snapshot                            statsdomain.UsageSnapshotResponse            `json:"snapshot"`
	EndpointModelStatisticsByEndpointID map[string][]statsdomain.UsageModelStatistic `json:"endpoint_model_statistics_by_endpoint_id"`
}

type AnalyticsErrorMessage struct {
	Type      string  `json:"type"`
	Channel   string  `json:"channel"`
	ProfileID *int    `json:"profile_id,omitempty"`
	Preset    *string `json:"preset,omitempty"`
	Code      string  `json:"code"`
	Message   string  `json:"message"`
}
