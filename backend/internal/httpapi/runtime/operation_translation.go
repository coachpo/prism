package runtime

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/platform/config"
)

type TranslationMode string

const (
	TranslationModeNone                             TranslationMode = "none"
	TranslationModeOpenAIResponsesToChatCompletions TranslationMode = "openai_responses_to_chat_completions"
	TranslationModeOpenAIChatCompletionsToResponses TranslationMode = "openai_chat_completions_to_responses"
)

const (
	openAIRequestTranslationUnsupportedErrorCode  = "openai_request_translation_unsupported"
	openAIRequestTranslationUnsupportedDetail     = "Prism cannot translate this OpenAI request shape for the selected target."
	openAIResponseTranslationUnsupportedErrorCode = "openai_response_translation_unsupported"
	openAIResponseTranslationUnsupportedDetail    = "Prism cannot translate this OpenAI response shape for the selected target."
	openAIStreamTranslationUnsupportedErrorCode   = "openai_stream_translation_unsupported"
	openAIStreamTranslationUnsupportedDetail      = "Prism cannot translate this OpenAI stream shape for the selected target."
)

type openAITranslationCapabilityClass string

const (
	openAITranslationCapabilitySafe   openAITranslationCapabilityClass = "safe"
	openAITranslationCapabilityReject openAITranslationCapabilityClass = "reject"
	openAITranslationCapabilityDefer  openAITranslationCapabilityClass = "defer"
)

type runtimeOpenAITranslationCapability struct {
	Mode              TranslationMode
	RequestClass      openAITranslationCapabilityClass
	ResponseClass     openAITranslationCapabilityClass
	StreamClass       openAITranslationCapabilityClass
	UnsupportedReason string
	HTTPStatus        int
}

type requestTranslationEligibility struct {
	Supported  bool
	Capability runtimeOpenAITranslationCapability
	Rejection  *domainError
}

type requestTranslationEligibilitySummary struct {
	byMode map[TranslationMode]requestTranslationEligibility
}

func buildRequestTranslationEligibilitySummary(operation RuntimeOperation, rawBody []byte) requestTranslationEligibilitySummary {
	summary := requestTranslationEligibilitySummary{byMode: map[TranslationMode]requestTranslationEligibility{
		TranslationModeNone: requestTranslationEligibilityFromCapability(newOpenAITranslationCapability(TranslationModeNone)),
	}}
	switch strings.TrimSpace(operation.Name) {
	case openAIUpstreamOperationResponses:
		mode := TranslationModeOpenAIResponsesToChatCompletions
		summary.byMode[mode] = requestTranslationEligibilityFromCapability(classifyOpenAITranslationCapability(operation, rawBody, mode))
	case openAIUpstreamOperationChatCompletions:
		mode := TranslationModeOpenAIChatCompletionsToResponses
		summary.byMode[mode] = requestTranslationEligibilityFromCapability(classifyOpenAITranslationCapability(operation, rawBody, mode))
	}
	return summary
}

func buildRequestTranslationEligibilitySummaryForRollout(operation RuntimeOperation, rawBody []byte, rolloutMode config.OpenAITerminalTranslationMode) requestTranslationEligibilitySummary {
	summary := buildRequestTranslationEligibilitySummary(operation, rawBody)
	if rolloutMode != config.OpenAITerminalTranslationModeOff {
		return summary
	}
	filtered := requestTranslationEligibilitySummary{byMode: map[TranslationMode]requestTranslationEligibility{
		TranslationModeNone: summary.byMode[TranslationModeNone],
	}}
	for _, mode := range []TranslationMode{TranslationModeOpenAIResponsesToChatCompletions, TranslationModeOpenAIChatCompletionsToResponses} {
		if _, ok := summary.byMode[mode]; ok {
			filtered.byMode[mode] = requestTranslationEligibility{Supported: false}
		}
	}
	return filtered
}

func requestTranslationEligibilityFromCapability(capability runtimeOpenAITranslationCapability) requestTranslationEligibility {
	rejection := capability.rejection()
	return requestTranslationEligibility{Supported: rejection == nil, Capability: capability, Rejection: rejection}
}

func (summary requestTranslationEligibilitySummary) resolveTranslationMode(operation RuntimeOperation, connection runtimeConnection) (TranslationMode, bool, *domainError) {
	mode := resolveTranslationMode(operation, connection.OpenAIUpstreamOperation, connection.OpenAIProbeEndpointVariant)
	if mode == TranslationModeNone {
		return TranslationModeNone, true, nil
	}
	eligibility, ok := summary.byMode[mode]
	if ok && eligibility.Supported {
		return mode, true, nil
	}
	if ok && eligibility.Rejection == nil {
		return "", false, nil
	}
	if ok && eligibility.Rejection != nil {
		return "", false, cloneDomainError(eligibility.Rejection)
	}
	return "", false, openAIRequestTranslationUnsupportedDomainError(mode, "unsupported_request_shape")
}

func newOpenAITranslationCapability(mode TranslationMode) runtimeOpenAITranslationCapability {
	return runtimeOpenAITranslationCapability{
		Mode:          mode,
		RequestClass:  openAITranslationCapabilitySafe,
		ResponseClass: openAITranslationCapabilitySafe,
		StreamClass:   openAITranslationCapabilitySafe,
		HTTPStatus:    http.StatusBadRequest,
	}
}

func classifyOpenAITranslationCapability(operation RuntimeOperation, rawBody []byte, mode TranslationMode) runtimeOpenAITranslationCapability {
	capability := newOpenAITranslationCapability(mode)
	if mode == TranslationModeNone {
		return capability
	}
	payload, err := decodeOpenAITranslationPayload(rawBody, mode)
	if err != nil {
		return capability.withRequestRejection(requestTranslationUnsupportedReason(err, "invalid_translation_payload"))
	}
	if requestWantsStreamForOperation(operation, rawBody, "") {
		switch mode {
		case TranslationModeOpenAIResponsesToChatCompletions:
			if responsesTranslationStreamUsesTools(payload) {
				return capability.withStreamRejection("responses_stream_tools")
			}
		case TranslationModeOpenAIChatCompletionsToResponses:
			if chatTranslationStreamUsesTools(payload) {
				return capability.withStreamRejection("chat_stream_tools")
			}
		}
	}
	if _, _, err := translateOpenAIRequest(rawBody, mode, "translation-preview-model"); err != nil {
		return capability.withRequestRejection(requestTranslationUnsupportedReason(err, "invalid_translation_payload"))
	}
	return capability
}

func (capability runtimeOpenAITranslationCapability) supported() bool {
	if capability.RequestClass == openAITranslationCapabilityDefer || capability.ResponseClass == openAITranslationCapabilityDefer || capability.StreamClass == openAITranslationCapabilityDefer {
		return false
	}
	return capability.RequestClass == openAITranslationCapabilitySafe && capability.ResponseClass == openAITranslationCapabilitySafe && capability.StreamClass == openAITranslationCapabilitySafe
}

func (capability runtimeOpenAITranslationCapability) rejection() *domainError {
	if capability.supported() {
		return nil
	}
	reason := strings.TrimSpace(capability.UnsupportedReason)
	if reason == "" {
		reason = "unsupported_request_shape"
	}
	rejection := openAIRequestTranslationUnsupportedDomainError(capability.Mode, reason)
	if capability.HTTPStatus > 0 {
		rejection.StatusCode = capability.HTTPStatus
	}
	return rejection
}

func (capability runtimeOpenAITranslationCapability) withRequestRejection(reason string) runtimeOpenAITranslationCapability {
	capability.RequestClass = openAITranslationCapabilityReject
	capability.UnsupportedReason = normalizedTranslationUnsupportedReason(reason)
	return capability
}

func (capability runtimeOpenAITranslationCapability) withStreamRejection(reason string) runtimeOpenAITranslationCapability {
	capability.StreamClass = openAITranslationCapabilityReject
	capability.UnsupportedReason = normalizedTranslationUnsupportedReason(reason)
	return capability
}

func requestTranslationUnsupportedReason(err error, fallback string) string {
	if rejection, ok := isRequestTranslationUnsupportedError(err); ok && rejection != nil {
		if reason := stringValue(rejection.Fields["unsupported_reason"]); strings.TrimSpace(reason) != "" {
			return reason
		}
	}
	return fallback
}

func normalizedTranslationUnsupportedReason(reason string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}
	return "unsupported_request_shape"
}

func chatTranslationStreamUsesTools(payload map[string]any) bool {
	if fieldHasValue(payload, "tools") || fieldHasValue(payload, "tool_choice") {
		return true
	}
	messages, _ := payload["messages"].([]any)
	for _, rawMessage := range messages {
		message, _ := rawMessage.(map[string]any)
		if fieldHasValue(message, "tool_calls") || fieldHasValue(message, "function_call") || fieldHasValue(message, "tool_call_id") {
			return true
		}
		if strings.TrimSpace(stringValue(message["role"])) == "tool" {
			return true
		}
	}
	return false
}

func responsesTranslationStreamUsesTools(payload map[string]any) bool {
	if fieldHasValue(payload, "tools") || fieldHasValue(payload, "tool_choice") {
		return true
	}
	return responsesInputContainsFunctionItems(payload["input"])
}

func responsesInputContainsFunctionItems(value any) bool {
	items, ok := value.([]any)
	if !ok {
		return false
	}
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		switch strings.TrimSpace(stringValue(item["type"])) {
		case "function_call", "function_call_output":
			return true
		}
	}
	return false
}

func resolveTranslationMode(operation RuntimeOperation, upstreamOperation *string, probeEndpointVariant *string) TranslationMode {
	ingressOperation := strings.TrimSpace(operation.Name)
	if ingressOperation == "" || upstreamOperation == nil {
		return TranslationModeNone
	}
	if probeEndpointVariant == nil || strings.TrimSpace(*probeEndpointVariant) == "" {
		return TranslationModeNone
	}
	upstream := strings.TrimSpace(*upstreamOperation)
	if upstream == "" || upstream == ingressOperation {
		return TranslationModeNone
	}
	switch ingressOperation {
	case openAIUpstreamOperationResponses:
		if upstream == openAIUpstreamOperationChatCompletions {
			return TranslationModeOpenAIResponsesToChatCompletions
		}
	case openAIUpstreamOperationChatCompletions:
		if upstream == openAIUpstreamOperationResponses {
			return TranslationModeOpenAIChatCompletionsToResponses
		}
	}
	return TranslationModeNone
}

func cloneDomainError(err *domainError) *domainError {
	if err == nil {
		return nil
	}
	cloned := &domainError{StatusCode: err.StatusCode, ErrorCode: err.ErrorCode, Detail: err.Detail}
	if len(err.Fields) > 0 {
		cloned.Fields = make(map[string]any, len(err.Fields))
		for key, value := range err.Fields {
			cloned.Fields[key] = value
		}
	}
	cloned.ContextRouting = cloneRuntimeContextRoutingDecision(err.ContextRouting)
	cloned.SelectedTerminalTargetID = cloneRuntimeIntPointer(err.SelectedTerminalTargetID)
	cloned.ResolvedTargetModelID = cloneRuntimeStringPointer(err.ResolvedTargetModelID)
	if err.PlanningFailure != nil {
		planningFailure := *err.PlanningFailure
		planningFailure.RequestGenerationParams = err.PlanningFailure.RequestGenerationParams.clone()
		planningFailure.SelectedTerminalTargetID = cloneRuntimeIntPointer(err.PlanningFailure.SelectedTerminalTargetID)
		planningFailure.ContextRouting = cloneRuntimeContextRoutingDecision(err.PlanningFailure.ContextRouting)
		cloned.PlanningFailure = &planningFailure
	}
	return cloned
}

func decorateRequestTranslationRejection(err *domainError, selectedTerminalTargetID *int, contextRouting *runtimeContextRoutingDecision) *domainError {
	if err == nil {
		return nil
	}
	decorated := cloneDomainError(err)
	if decorated.SelectedTerminalTargetID == nil {
		decorated.SelectedTerminalTargetID = cloneRuntimeIntPointer(selectedTerminalTargetID)
	}
	if contextRouting != nil {
		if decorated.ContextRouting == nil {
			decorated.ContextRouting = cloneRuntimeContextRoutingDecision(contextRouting)
		} else if contextRouting.FacadeSelection != nil && decorated.ContextRouting.FacadeSelection == nil {
			mergedContextRouting := cloneRuntimeContextRoutingDecision(decorated.ContextRouting)
			mergedContextRouting.FacadeSelection = cloneRuntimeFacadeSelectionDecision(contextRouting.FacadeSelection)
			decorated.ContextRouting = mergedContextRouting
		}
	}
	if decorated.SelectedTerminalTargetID == nil && decorated.ContextRouting != nil {
		decorated.SelectedTerminalTargetID = cloneRuntimeIntPointer(decorated.ContextRouting.SelectedTerminalTargetID)
	}
	return decorated
}

func isRequestTranslationUnsupportedError(err error) (*domainError, bool) {
	return translationUnsupportedDomainError(err, openAIRequestTranslationUnsupportedErrorCode)
}

func translationUnsupportedDomainError(err error, errorCode string) (*domainError, bool) {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil {
		return nil, false
	}
	if strings.TrimSpace(domainErr.ErrorCode) != errorCode {
		return nil, false
	}
	return domainErr, true
}

func translationUnsupportedReasonFromError(err error, errorCode string, fallback string) string {
	if domainErr, ok := translationUnsupportedDomainError(err, errorCode); ok && domainErr != nil {
		if reason := stringValue(domainErr.Fields["unsupported_reason"]); strings.TrimSpace(reason) != "" {
			return reason
		}
	}
	return normalizedTranslationUnsupportedReason(fallback)
}

func openAIRequestTranslationUnsupportedDomainError(mode TranslationMode, reason string) *domainError {
	return openAITranslationUnsupportedDomainError(http.StatusBadRequest, openAIRequestTranslationUnsupportedErrorCode, openAIRequestTranslationUnsupportedDetail, mode, reason)
}

func openAIResponseTranslationUnsupportedDomainError(mode TranslationMode, reason string) *domainError {
	return openAITranslationUnsupportedDomainError(http.StatusBadGateway, openAIResponseTranslationUnsupportedErrorCode, openAIResponseTranslationUnsupportedDetail, mode, reason)
}

func openAIStreamTranslationUnsupportedDomainError(mode TranslationMode, reason string) *domainError {
	return openAITranslationUnsupportedDomainError(http.StatusBadGateway, openAIStreamTranslationUnsupportedErrorCode, openAIStreamTranslationUnsupportedDetail, mode, reason)
}

func openAITranslationUnsupportedDomainError(statusCode int, errorCode string, detail string, mode TranslationMode, reason string) *domainError {
	fields := map[string]any{}
	if strings.TrimSpace(string(mode)) != "" {
		fields["translation_mode"] = string(mode)
	}
	if strings.TrimSpace(reason) != "" {
		fields["unsupported_reason"] = strings.TrimSpace(reason)
	}
	return &domainError{
		StatusCode: statusCode,
		ErrorCode:  errorCode,
		Detail:     detail,
		Fields:     fields,
	}
}

func unsupportedTranslationModeError(mode TranslationMode) error {
	return fmt.Errorf("unsupported translation mode %q", mode)
}
