package runtime

import (
	"bytes"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
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

type operationMediaModelExtractor func([]byte, string) string
type operationMediaModelRewriter func([]byte, string, string) []byte

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
		ExtractModel: extractOpenAIImageGenerationModel,
		RewriteModel: rewriteModelInBodyForMediaJSON,
	},
	runtimeHookCollectionOpenAIImagesEdit: {
		Provider:     "openai",
		RequestKind:  operationMediaRequestKindImageEdit,
		ExtractModel: extractOpenAIImageEditModel,
		RewriteModel: rewriteOpenAIImageEditModel,
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
		return hooks.ExtractModel(rawBody, contentType)
	}
	return extractModelFromBody(rawBody)
}

func rewriteModelInBodyForOperation(rawBody []byte, contentType string, operation RuntimeOperation, targetModelID string) []byte {
	if hooks, ok := mediaHooksForOperation(operation); ok && hooks.RewriteModel != nil {
		return hooks.RewriteModel(rawBody, contentType, targetModelID)
	}
	return rewriteModelInBody(rawBody, targetModelID)
}

func extractOpenAIImageGenerationModel(rawBody []byte, _ string) string {
	return extractModelFromBody(rawBody)
}

func rewriteModelInBodyForMediaJSON(rawBody []byte, _ string, targetModelID string) []byte {
	return rewriteModelInBody(rawBody, targetModelID)
}

func extractOpenAIImageEditModel(rawBody []byte, contentType string) string {
	if boundary := multipartBoundary(contentType); boundary != "" {
		return extractMultipartFormValue(rawBody, boundary, "model")
	}
	return extractModelFromBody(rawBody)
}

func rewriteOpenAIImageEditModel(rawBody []byte, contentType string, targetModelID string) []byte {
	boundary := multipartBoundary(contentType)
	if boundary == "" {
		return rewriteModelInBody(rawBody, targetModelID)
	}
	rewritten, ok := rewriteMultipartFormValue(rawBody, boundary, "model", targetModelID)
	if !ok {
		return rawBody
	}
	return rewritten
}

func multipartBoundary(contentType string) string {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(strings.ToLower(mediaType), "multipart/") {
		return ""
	}
	return strings.TrimSpace(params["boundary"])
}

func extractMultipartFormValue(rawBody []byte, boundary string, fieldName string) string {
	reader := multipart.NewReader(bytes.NewReader(rawBody), boundary)
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			return ""
		}
		if err != nil {
			return ""
		}
		if part.FormName() != fieldName {
			_ = part.Close()
			continue
		}
		value, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(value))
	}
}

func cloneMultipartHeader(header textproto.MIMEHeader) textproto.MIMEHeader {
	cloned := make(textproto.MIMEHeader, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

func rewriteMultipartFormValue(rawBody []byte, boundary string, fieldName string, value string) ([]byte, bool) {
	reader := multipart.NewReader(bytes.NewReader(rawBody), boundary)
	var rewritten bytes.Buffer
	writer := multipart.NewWriter(&rewritten)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, false
	}
	replaced := false
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, false
		}
		partBody, err := io.ReadAll(part)
		if err != nil {
			_ = part.Close()
			return nil, false
		}
		header := cloneMultipartHeader(part.Header)
		if part.FormName() == fieldName {
			partBody = []byte(value)
			replaced = true
		}
		_ = part.Close()
		outPart, err := writer.CreatePart(header)
		if err != nil {
			return nil, false
		}
		if _, err := outPart.Write(partBody); err != nil {
			return nil, false
		}
	}
	if err := writer.Close(); err != nil {
		return nil, false
	}
	if !replaced {
		return rawBody, false
	}
	return rewritten.Bytes(), true
}
