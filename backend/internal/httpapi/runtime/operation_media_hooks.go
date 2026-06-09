package runtime

import (
	"context"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

const (
	runtimeHookCollectionOpenAIImagesGeneration = "media.openai_images_generation"
	runtimeHookCollectionOpenAIImagesEdit       = "media.openai_images_edit"
)

type operationMediaRequestKind string

const (
	operationMediaRequestKindImageGeneration operationMediaRequestKind = "image_generation"
	operationMediaRequestKindImageEdit       operationMediaRequestKind = "image_edit"
)

type operationMediaModelExtractor func([]byte, string, RuntimeOperation) string
type operationMediaModelRewriter func([]byte, string, RuntimeOperation, string) []byte

type operationMediaHooks struct {
	Provider     string
	RequestKind  operationMediaRequestKind
	ExtractModel operationMediaModelExtractor
	RewriteModel operationMediaModelRewriter
}

var operationMediaHooksByCollectionID = map[string]operationMediaHooks{
	runtimeHookCollectionOpenAIImagesGeneration: {
		Provider:     "openai",
		RequestKind:  operationMediaRequestKindImageGeneration,
		ExtractModel: extractOpenAIImageModelViaAdapter,
		RewriteModel: rewriteOpenAIImageModelViaAdapter,
	},
	runtimeHookCollectionOpenAIImagesEdit: {
		Provider:     "openai",
		RequestKind:  operationMediaRequestKindImageEdit,
		ExtractModel: extractOpenAIImageModelViaAdapter,
		RewriteModel: rewriteOpenAIImageModelViaAdapter,
	},
}

func mediaHooksForOperation(operation RuntimeOperation) (operationMediaHooks, bool) {
	hookCollectionID := operation.HookCollectionID
	if hookCollectionID == "" {
		hookCollectionID = operation.Name
	}
	hooks, ok := operationMediaHooksByCollectionID[hookCollectionID]
	return hooks, ok
}

func extractModelFromBodyForOperation(rawBody []byte, contentType string, operation RuntimeOperation) string {
	if hooks, ok := mediaHooksForOperation(operation); ok && hooks.ExtractModel != nil {
		return hooks.ExtractModel(rawBody, contentType, operation)
	}
	return extractModelFromBody(rawBody)
}

func rewriteModelInBodyForOperation(rawBody []byte, contentType string, operation RuntimeOperation, targetModelID string) []byte {
	if hooks, ok := mediaHooksForOperation(operation); ok && hooks.RewriteModel != nil {
		return hooks.RewriteModel(rawBody, contentType, operation, targetModelID)
	}
	return rewriteModelInBody(rawBody, targetModelID)
}

func extractOpenAIImageModelViaAdapter(rawBody []byte, contentType string, operation RuntimeOperation) string {
	adapter := openai.New()
	modelID, err := adapter.ExtractImageModel(context.Background(), provider.MediaRequest{
		Operation:   providerOperationFromRuntime(operation),
		RawBody:     rawBody,
		ContentType: contentType,
	})
	if err != nil {
		return ""
	}
	return modelID
}

func rewriteOpenAIImageModelViaAdapter(rawBody []byte, contentType string, operation RuntimeOperation, targetModelID string) []byte {
	adapter := openai.New()
	media, err := adapter.HandleMedia(context.Background(), provider.MediaRequest{
		Operation:     providerOperationFromRuntime(operation),
		RawBody:       rawBody,
		ContentType:   contentType,
		TargetModelID: targetModelID,
	})
	if err != nil || len(media.RewrittenBody) == 0 {
		return append([]byte(nil), rawBody...)
	}
	return media.RewrittenBody
}

func auditRequestBodyForOperation(rawBody []byte, contentType string, operation RuntimeOperation) []byte {
	if hooks, ok := mediaHooksForOperation(operation); ok && hooks.Provider == "openai" {
		return openai.RedactImageRequestAuditBody(rawBody, contentType)
	}
	return append([]byte(nil), rawBody...)
}

func multipartBoundary(contentType string) string {
	return openai.MultipartBoundaryForRuntime(contentType)
}
