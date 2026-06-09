package core

type APIFamily string

const (
	APIFamilyOpenAI    APIFamily = "openai"
	APIFamilyAnthropic APIFamily = "anthropic"
	APIFamilyGemini    APIFamily = "gemini"
)

type ModelBindingSource string

const (
	ModelBindingSourceBody ModelBindingSource = "body"
	ModelBindingSourcePath ModelBindingSource = "path"
)

type EndpointShape string

const (
	EndpointShapeTextGeneration  EndpointShape = "text_generation"
	EndpointShapeTokenCount      EndpointShape = "token_count"
	EndpointShapeImageGeneration EndpointShape = "image_generation"
	EndpointShapeImageEdit       EndpointShape = "image_edit"
)

type OperationDescriptor struct {
	Name               string             `json:"name"`
	Method             string             `json:"method"`
	APIFamily          APIFamily          `json:"api_family"`
	PathTemplate       string             `json:"path_template"`
	Shape              EndpointShape      `json:"shape,omitempty"`
	Streaming          bool               `json:"streaming"`
	ModelBindingSource ModelBindingSource `json:"model_binding_source"`
}

type RequestEnvelope struct {
	Context    RequestContext      `json:"context"`
	Operation  OperationDescriptor `json:"operation"`
	Method     string              `json:"method"`
	Path       string              `json:"path"`
	RawQuery   string              `json:"raw_query,omitempty"`
	PathParams map[string]string   `json:"path_params,omitempty"`
	Headers    map[string][]string `json:"headers,omitempty"`
	Body       []byte              `json:"body,omitempty"`
	Metadata   map[string]string   `json:"metadata,omitempty"`
}

type RequestEnvelopeInput struct {
	Context    RequestContext
	Operation  OperationDescriptor
	Method     string
	Path       string
	RawQuery   string
	PathParams map[string]string
	Headers    map[string][]string
	Body       []byte
	Metadata   map[string]string
}

func NewRequestEnvelope(input RequestEnvelopeInput) RequestEnvelope {
	return RequestEnvelope{
		Context:    input.Context,
		Operation:  input.Operation,
		Method:     input.Method,
		Path:       input.Path,
		RawQuery:   input.RawQuery,
		PathParams: cloneStringMap(input.PathParams),
		Headers:    cloneStringSliceMap(input.Headers),
		Body:       cloneBytes(input.Body),
		Metadata:   cloneStringMap(input.Metadata),
	}
}
