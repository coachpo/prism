package provider

import (
	"context"
	"io"
	"net/http"
)

const (
	APIFamilyOpenAI    = "openai"
	APIFamilyAnthropic = "anthropic"
	APIFamilyGemini    = "gemini"
)

const (
	TranslationModeNone                             TranslationMode = "none"
	TranslationModeOpenAIResponsesToChatCompletions TranslationMode = "openai_responses_to_chat_completions"
	TranslationModeOpenAIChatCompletionsToResponses TranslationMode = "openai_chat_completions_to_responses"
)

type AdapterError struct {
	HTTPStatus int
	Code       string
	Detail     string
	Fields     map[string]any
}

func (err *AdapterError) Error() string {
	if err == nil {
		return ""
	}
	return err.Detail
}

type TranslationMode string

type Operation struct {
	Name             string
	APIFamily        string
	HookCollectionID string
	Streaming        bool
}

type RequestEnvelope struct {
	Operation   Operation
	RawBody     []byte
	ContentType string
	RequestPath string
	PathParams  map[string]string
}

type ProviderRequest struct {
	Operation   Operation
	Body        []byte
	ContentType string
	NativePath  string
	WantsStream bool
	Metadata    map[string]string
}

type UpstreamTarget struct {
	ModelID    string
	NativePath string
	Header     http.Header
}

type UpstreamRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type UpstreamResponse struct {
	Operation        Operation
	StatusCode       int
	Header           http.Header
	Body             []byte
	BodyReader       io.Reader
	ContentType      string
	TranslationMode  TranslationMode
	RequestedModelID string
}

type ClientResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	Usage      UsageEnvelope
}

type StreamRequest struct {
	Operation        Operation
	Reader           io.Reader
	Writer           io.Writer
	TranslationMode  TranslationMode
	RequestedModelID string
}

type StreamResult struct {
	Usage          UsageEnvelope
	Completed      bool
	TerminalSignal string
}

type UsageEnvelope struct {
	InputTokens              *int
	OutputTokens             *int
	TotalTokens              *int
	CacheReadInputTokens     *int
	CacheCreationInputTokens *int
	ReasoningTokens          *int
	NormalizationRule        string
}

type TokenEstimate struct {
	InputTokens  int
	OutputTokens int
	Source       string
}

type MediaRequest struct {
	Operation     Operation
	RawBody       []byte
	ContentType   string
	TargetModelID string
	RewrittenBody []byte
}

type ConversionRequest struct {
	Operation        Operation
	RawBody          []byte
	Mode             TranslationMode
	TargetModelID    string
	RequestedModelID string
}

type ConversionCapability struct {
	Mode                  TranslationMode
	RequestSupported      bool
	ResponseSupported     bool
	StreamSupported       bool
	UnsupportedReason     string
	HTTPStatus            int
	UpstreamOperationName string
}

type TranslatedRequest struct {
	Path string
	Body []byte
}

type TranslatedResponse struct {
	Body  []byte
	Usage UsageEnvelope
}

type OverflowClassification struct {
	Promotable bool
	ErrorCode  string
	Classifier string
}

type RequestHookBehavior struct {
	Provider                     string
	HasGenerationParamsExtractor bool
	HasStreamingObserver         bool
	HasStreamDetector            bool
}

type ResponseHookBehavior struct {
	Provider           string
	Kind               string
	UsageRule          string
	HasNonStreamParser bool
}

type StreamHookBehavior struct {
	Provider               string
	Kind                   string
	UsageRule              string
	CompleteOnDoneSentinel bool
	HasTerminalClassifier  bool
	HasUsageMerger         bool
}

type MediaHookBehavior struct {
	Provider          string
	Kind              string
	HasModelExtractor bool
	HasModelRewriter  bool
}

type CurrentOperationBehavior struct {
	OperationName    string
	APIFamily        string
	HookCollectionID string
	Request          RequestHookBehavior
	HasRequest       bool
	Response         ResponseHookBehavior
	HasResponse      bool
	Stream           StreamHookBehavior
	HasStream        bool
	Media            MediaHookBehavior
	HasMedia         bool
}

type ProviderAdapter interface {
	APIFamily() string
	ParseRequest(context.Context, RequestEnvelope) (ProviderRequest, error)
	BuildUpstreamRequest(context.Context, ProviderRequest, UpstreamTarget) (UpstreamRequest, error)
	AdaptNonStreamResponse(context.Context, UpstreamResponse) (ClientResponse, error)
	AdaptStream(context.Context, StreamRequest) (StreamResult, error)
	ExtractUsage(context.Context, UpstreamResponse) (UsageEnvelope, error)
	EstimateTokens(context.Context, ProviderRequest) (TokenEstimate, error)
	HandleMedia(context.Context, MediaRequest) (MediaRequest, error)
	ConversionCapability(context.Context, ConversionRequest) (ConversionCapability, error)
	TranslateRequest(context.Context, ConversionRequest) (TranslatedRequest, error)
	TranslateResponse(context.Context, ConversionRequest) (TranslatedResponse, error)
	ClassifyOverflow(context.Context, UpstreamResponse) OverflowClassification
	CurrentBehavior(context.Context, Operation) (CurrentOperationBehavior, bool)
}
