package core

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
