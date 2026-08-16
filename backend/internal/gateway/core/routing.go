package core

type RouteReason string

const (
	RouteReasonDirectMatch         RouteReason = "direct_match"
	RouteReasonModelRedirect       RouteReason = "model_redirect"
	RouteReasonUpstreamRedirect    RouteReason = "upstream_redirect"
	RouteReasonQPSOverflow         RouteReason = "qps_overflow"
	RouteReasonRPMOverflow         RouteReason = "rpm_overflow"
	RouteReasonTPMOverflow         RouteReason = "tpm_overflow"
	RouteReasonIPMOverflow         RouteReason = "ipm_overflow"
	RouteReasonConcurrencyOverflow RouteReason = "concurrency_overflow"
	RouteReasonRetry429            RouteReason = "retry_429"
	RouteReasonRetry5xx            RouteReason = "retry_5xx"
	RouteReasonRetryHTTP           RouteReason = "retry_http"
	RouteReasonRetryConnectTimeout RouteReason = "retry_connect_timeout"
	RouteReasonRetryTransport      RouteReason = "retry_transport"
	RouteReasonCircuitOpenSkip     RouteReason = "circuit_open_skip"
	RouteReasonNoHealthyUpstream   RouteReason = "no_healthy_upstream"
	RouteReasonPolicyReject        RouteReason = "policy_reject"
)

type UsageSource string

const (
	UsageSourceProvider               UsageSource = "provider"
	UsageSourceProviderStreamTerminal UsageSource = "provider_stream_terminal"
	UsageSourceLocalEstimate          UsageSource = "local_estimate"
	UsageSourceMissing                UsageSource = "missing"
)

type RoutePlan struct {
	OperationName     string         `json:"operation_name"`
	RequestedModelID  string         `json:"requested_model_id"`
	EffectiveModelID  string         `json:"effective_model_id"`
	RouteReason       RouteReason    `json:"route_reason"`
	CandidateAttempts []RouteAttempt `json:"candidate_attempts,omitempty"`
}

type RouteAttempt struct {
	AttemptNumber int         `json:"attempt_number"`
	UpstreamID    string      `json:"upstream_id"`
	EndpointID    *int        `json:"endpoint_id,omitempty"`
	Reason        RouteReason `json:"reason"`
}

type AccountingEvent struct {
	Context          RequestContext `json:"context"`
	OperationName    string         `json:"operation_name"`
	RequestedModelID string         `json:"requested_model_id,omitempty"`
	EffectiveModelID string         `json:"effective_model_id,omitempty"`
	RouteReason      RouteReason    `json:"route_reason,omitempty"`
	UsageSource      UsageSource    `json:"usage_source,omitempty"`
	AttemptNumber    int            `json:"attempt_number,omitempty"`
	Final            bool           `json:"final"`
	StatusCode       int            `json:"status_code,omitempty"`
	Error            *GatewayError  `json:"error,omitempty"`
}
