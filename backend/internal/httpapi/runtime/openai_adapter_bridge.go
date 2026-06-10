package runtime

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

type openAITextAttemptCompatibilityResult struct {
	Compatible      bool
	TranslationMode TranslationMode
	Err             error
}

func planOpenAITextAttemptCompatibility(operation RuntimeOperation, rawBody []byte, attempt runtimeTerminalAttempt, mode TranslationMode, rolloutMode config.OpenAITerminalTranslationMode, adapter openai.Adapter) openAITextAttemptCompatibilityResult {
	if mode == TranslationModeNone || strings.TrimSpace(string(mode)) == "" {
		return openAITextAttemptCompatibilityResult{Compatible: true, TranslationMode: TranslationModeNone}
	}
	if rolloutMode != config.OpenAITerminalTranslationModeSafeOnly {
		return openAITextAttemptCompatibilityResult{}
	}
	providerOperation := providerOperationFromRuntime(operation)
	if !openai.IsTextOperation(providerOperation) {
		return openAITextAttemptCompatibilityResult{}
	}
	providerMode := providerTranslationMode(mode)
	capability, err := adapter.ConversionCapability(context.Background(), provider.ConversionRequest{
		Operation:     providerOperation,
		RawBody:       rawBody,
		Mode:          providerMode,
		TargetModelID: attempt.TargetModel.ModelID,
	})
	if err != nil {
		if domainErr := domainErrorFromProviderAdapterError(err); domainErr != nil {
			return openAITextAttemptCompatibilityResult{Err: domainErr}
		}
		return openAITextAttemptCompatibilityResult{Err: err}
	}
	if domainErr := domainErrorFromOpenAITextConversionCapability(capability, mode); domainErr != nil {
		return openAITextAttemptCompatibilityResult{Err: domainErr}
	}
	return openAITextAttemptCompatibilityResult{Compatible: true, TranslationMode: mode}
}

func domainErrorFromOpenAITextConversionCapability(capability provider.ConversionCapability, fallbackMode TranslationMode) *domainError {
	if capability.RequestSupported && capability.ResponseSupported && capability.StreamSupported {
		return nil
	}
	mode := TranslationMode(capability.Mode)
	if strings.TrimSpace(string(mode)) == "" {
		mode = fallbackMode
	}
	reason := strings.TrimSpace(capability.UnsupportedReason)
	if reason == "" {
		reason = "unsupported_request_shape"
	}
	status := capability.HTTPStatus
	if status == 0 {
		status = http.StatusBadRequest
	}
	return openAITranslationUnsupportedDomainError(status, openAIRequestTranslationUnsupportedErrorCode, openAIRequestTranslationUnsupportedDetail, mode, reason)
}

func buildOpenAITextPlannedUpstreamRequest(input requestPlanningInput, operation resolvedRequestOperation, attempt runtimeTerminalAttempt) (plannedUpstreamRequest, bool, error) {
	providerOperation := providerOperationFromRuntime(operation.Match.Operation)
	if !openai.IsTextOperation(providerOperation) {
		return plannedUpstreamRequest{}, false, nil
	}
	adapter := openai.New()
	upstream, err := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
		Operation:       providerOperation,
		RawBody:         input.RawBody,
		ContentType:     operation.ContentType,
		RequestPath:     input.Request.URL.Path,
		TargetModelID:   attempt.TargetModel.ModelID,
		TranslationMode: providerTranslationMode(attempt.TranslationMode),
	})
	if err != nil {
		if domainErr := domainErrorFromProviderAdapterError(err); domainErr != nil {
			return plannedUpstreamRequest{}, true, domainErr
		}
		return plannedUpstreamRequest{}, true, err
	}
	effectiveRequestPath := strings.TrimSpace(upstream.Path)
	if effectiveRequestPath == "" {
		effectiveRequestPath = input.Request.URL.Path
	}
	upstreamBody := upstream.Body
	return plannedUpstreamRequest{
		EffectiveRequestPath:    effectiveRequestPath,
		RawRequestBody:          input.RawBody,
		UpstreamBody:            upstreamBody,
		IsStreamingRequest:      requestWantsStreamForOperation(operation.Match.Operation, input.RawBody, effectiveRequestPath),
		ClientHeaders:           flattenHeaders(input.Request.Header),
		RequestGenerationParams: extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody),
	}, true, nil
}

func providerOperationFromRuntime(operation RuntimeOperation) provider.Operation {
	return provider.Operation{Name: operation.Name, APIFamily: operation.APIFamily, HookCollectionID: operation.HookCollectionID, Streaming: operation.Streaming}
}

func domainErrorFromProviderAdapterError(err error) *domainError {
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) || adapterErr == nil {
		return nil
	}
	status := adapterErr.HTTPStatus
	if status == 0 {
		status = http.StatusBadRequest
	}
	fields := map[string]any{}
	maps.Copy(fields, adapterErr.Fields)
	return &domainError{
		StatusCode: status,
		ErrorCode:  strings.TrimSpace(adapterErr.Code),
		Detail:     strings.TrimSpace(adapterErr.Detail),
		Fields:     fields,
	}
}
