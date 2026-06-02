package runtime

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type TranslationMode string

const (
	TranslationModeNone                             TranslationMode = "none"
	TranslationModeOpenAIResponsesToChatCompletions TranslationMode = "openai_responses_to_chat_completions"
	TranslationModeOpenAIChatCompletionsToResponses TranslationMode = "openai_chat_completions_to_responses"
)

const (
	openAIRequestTranslationUnsupportedErrorCode = "openai_request_translation_unsupported"
	openAIRequestTranslationUnsupportedDetail    = "Prism cannot translate this OpenAI request shape for the selected target."
)

type requestTranslationEligibility struct {
	Supported bool
	Rejection *domainError
}

type requestTranslationEligibilitySummary struct {
	byMode map[TranslationMode]requestTranslationEligibility
}

func buildRequestTranslationEligibilitySummary(operation RuntimeOperation, rawBody []byte) requestTranslationEligibilitySummary {
	summary := requestTranslationEligibilitySummary{byMode: map[TranslationMode]requestTranslationEligibility{
		TranslationModeNone: {Supported: true},
	}}
	switch strings.TrimSpace(operation.Name) {
	case openAIUpstreamOperationResponses:
		_, _, err := translateOpenAIRequest(rawBody, TranslationModeOpenAIResponsesToChatCompletions, "translation-preview-model")
		summary.byMode[TranslationModeOpenAIResponsesToChatCompletions] = requestTranslationEligibilityFromError(err)
	case openAIUpstreamOperationChatCompletions:
		_, _, err := translateOpenAIRequest(rawBody, TranslationModeOpenAIChatCompletionsToResponses, "translation-preview-model")
		summary.byMode[TranslationModeOpenAIChatCompletionsToResponses] = requestTranslationEligibilityFromError(err)
	}
	return summary
}

func requestTranslationEligibilityFromError(err error) requestTranslationEligibility {
	if err == nil {
		return requestTranslationEligibility{Supported: true}
	}
	if rejection, ok := isRequestTranslationUnsupportedError(err); ok {
		return requestTranslationEligibility{Rejection: rejection}
	}
	return requestTranslationEligibility{Rejection: openAIRequestTranslationUnsupportedDomainError(TranslationModeNone, "invalid_translation_payload")}
}

func (summary requestTranslationEligibilitySummary) resolveTranslationMode(operation RuntimeOperation, connection runtimeConnection) (TranslationMode, *domainError) {
	mode := resolveTranslationMode(operation, connection.OpenAIUpstreamOperation, connection.OpenAIProbeEndpointVariant)
	if mode == TranslationModeNone {
		return TranslationModeNone, nil
	}
	eligibility, ok := summary.byMode[mode]
	if ok && eligibility.Supported {
		return mode, nil
	}
	if ok && eligibility.Rejection != nil {
		return "", cloneDomainError(eligibility.Rejection)
	}
	return "", openAIRequestTranslationUnsupportedDomainError(mode, "unsupported_request_shape")
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
	if decorated.ContextRouting == nil && contextRouting != nil {
		decorated.ContextRouting = cloneRuntimeContextRoutingDecision(contextRouting)
	}
	if decorated.SelectedTerminalTargetID == nil && decorated.ContextRouting != nil {
		decorated.SelectedTerminalTargetID = cloneRuntimeIntPointer(decorated.ContextRouting.SelectedTerminalTargetID)
	}
	return decorated
}

func isRequestTranslationUnsupportedError(err error) (*domainError, bool) {
	var domainErr *domainError
	if !errors.As(err, &domainErr) || domainErr == nil {
		return nil, false
	}
	if strings.TrimSpace(domainErr.ErrorCode) != openAIRequestTranslationUnsupportedErrorCode {
		return nil, false
	}
	return domainErr, true
}

func openAIRequestTranslationUnsupportedDomainError(mode TranslationMode, reason string) *domainError {
	fields := map[string]any{}
	if strings.TrimSpace(string(mode)) != "" {
		fields["translation_mode"] = string(mode)
	}
	if strings.TrimSpace(reason) != "" {
		fields["unsupported_reason"] = strings.TrimSpace(reason)
	}
	return &domainError{
		StatusCode: http.StatusBadRequest,
		ErrorCode:  openAIRequestTranslationUnsupportedErrorCode,
		Detail:     openAIRequestTranslationUnsupportedDetail,
		Fields:     fields,
	}
}

func unsupportedTranslationModeError(mode TranslationMode) error {
	return fmt.Errorf("unsupported translation mode %q", mode)
}
