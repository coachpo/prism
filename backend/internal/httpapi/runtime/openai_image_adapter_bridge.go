package runtime

import (
	"context"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

func buildOpenAIImagePlannedUpstreamRequest(input requestPlanningInput, operation resolvedRequestOperation, attempt runtimeTerminalAttempt) (plannedUpstreamRequest, bool, error) {
	providerOperation := providerOperationFromRuntime(operation.Match.Operation)
	if !openai.IsImageOperation(providerOperation) {
		return plannedUpstreamRequest{}, false, nil
	}
	adapter := openai.New()
	upstream, err := adapter.BuildImageUpstreamRequest(context.Background(), openai.ImageUpstreamRequest{
		Operation:     providerOperation,
		RawBody:       input.RawBody,
		ContentType:   operation.ContentType,
		TargetModelID: attempt.TargetModel.ModelID,
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
	return plannedUpstreamRequest{
		EffectiveRequestPath:    effectiveRequestPath,
		RawRequestBody:          input.RawBody,
		UpstreamBody:            upstream.Body,
		IsStreamingRequest:      requestWantsStreamForOperation(operation.Match.Operation, input.RawBody, effectiveRequestPath),
		ClientHeaders:           flattenHeaders(input.Request.Header),
		RequestGenerationParams: extractBufferedRequestGenerationParams(operation.Match.Operation, input.RawBody),
	}, true, nil
}
