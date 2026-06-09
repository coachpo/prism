package core

import "context"

type Pipeline interface {
	Execute(context.Context, RequestEnvelope) (ClientResponse, error)
}

type Phase[I any, O any] interface {
	Run(context.Context, I) (O, error)
}

type RequestParser interface {
	ParseRequest(context.Context, RequestEnvelope) (ProviderRequest, error)
}

type RoutePlanner interface {
	Plan(context.Context, RequestEnvelope, ProviderRequest) (RoutePlan, error)
}

type AccountingSink interface {
	RecordAttempt(context.Context, AccountingEvent) error
	RecordFinal(context.Context, AccountingEvent) error
}

type ProviderRequest struct {
	Envelope RequestEnvelope   `json:"envelope"`
	Body     []byte            `json:"body,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type ClientResponse struct {
	StatusCode int                 `json:"status_code"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       []byte              `json:"body,omitempty"`
	Usage      UsageEnvelope       `json:"usage"`
}

type UsageEnvelope struct {
	InputTokens  *int        `json:"input_tokens,omitempty"`
	OutputTokens *int        `json:"output_tokens,omitempty"`
	TotalTokens  *int        `json:"total_tokens,omitempty"`
	Source       UsageSource `json:"source,omitempty"`
}
