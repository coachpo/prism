package provider

import (
	"context"
	"net/http"
	"strings"
)

type DefaultAdapter struct {
	APIFamilyName string
}

func (adapter DefaultAdapter) APIFamily() string {
	return strings.TrimSpace(adapter.APIFamilyName)
}

func (adapter DefaultAdapter) ParseRequest(_ context.Context, envelope RequestEnvelope) (ProviderRequest, error) {
	return ProviderRequest{
		Operation:   envelope.Operation,
		Body:        append([]byte(nil), envelope.RawBody...),
		ContentType: envelope.ContentType,
		NativePath:  envelope.RequestPath,
		WantsStream: envelope.Operation.Streaming,
		Metadata:    map[string]string{},
	}, nil
}

func (adapter DefaultAdapter) BuildUpstreamRequest(_ context.Context, request ProviderRequest, target UpstreamTarget) (UpstreamRequest, error) {
	path := strings.TrimSpace(target.NativePath)
	if path == "" {
		path = strings.TrimSpace(request.NativePath)
	}
	return UpstreamRequest{Method: http.MethodPost, Path: path, Header: target.Header.Clone(), Body: append([]byte(nil), request.Body...)}, nil
}

func (adapter DefaultAdapter) AdaptNonStreamResponse(_ context.Context, response UpstreamResponse) (ClientResponse, error) {
	return ClientResponse{StatusCode: response.StatusCode, Header: response.Header.Clone(), Body: append([]byte(nil), response.Body...)}, nil
}

func (adapter DefaultAdapter) AdaptStream(_ context.Context, _ StreamRequest) (StreamResult, error) {
	return StreamResult{}, nil
}

func (adapter DefaultAdapter) ExtractUsage(_ context.Context, _ UpstreamResponse) (UsageEnvelope, error) {
	return UsageEnvelope{}, nil
}

func (adapter DefaultAdapter) EstimateTokens(_ context.Context, _ ProviderRequest) (TokenEstimate, error) {
	return TokenEstimate{}, nil
}

func (adapter DefaultAdapter) ClassifyOverflow(_ context.Context, _ UpstreamResponse) OverflowClassification {
	return OverflowClassification{}
}

func (adapter DefaultAdapter) CurrentBehavior(_ context.Context, operation Operation) (CurrentOperationBehavior, bool) {
	return CurrentOperationBehavior{OperationName: operation.Name, APIFamily: operation.APIFamily, HookCollectionID: operation.HookCollectionID}, true
}
