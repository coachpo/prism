package runtime

import (
	"context"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider/gemini"
)

func buildGeminiPlannedUpstreamRequest(input requestPlanningInput, operation resolvedRequestOperation, upstreamModelID string) (plannedUpstreamRequest, bool, error) {
	providerOperation := providerOperationFromRuntime(operation.Match.Operation)
	if !gemini.IsOperation(providerOperation) {
		return plannedUpstreamRequest{}, false, nil
	}
	adapter := gemini.New()
	upstream, err := adapter.BuildGenerateContentUpstreamRequest(context.Background(), gemini.GenerateContentUpstreamRequest{
		Operation:       providerOperation,
		RawBody:         input.RawBody,
		ContentType:     operation.ContentType,
		RequestPath:     input.Request.URL.Path,
		UpstreamModelID: upstreamModelID,
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
