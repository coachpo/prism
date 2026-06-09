package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
)

const (
	OperationImagesGenerations = "openai.images.generations"
	OperationImagesEdits       = "openai.images.edits"
)

type ImageOperationMetadata struct {
	Name       string
	NativePath string
	Edit       bool
}

type ImageUpstreamRequest struct {
	Operation     provider.Operation
	RawBody       []byte
	ContentType   string
	TargetModelID string
}

func ImageOperation(operation provider.Operation) (ImageOperationMetadata, bool) {
	switch strings.TrimSpace(operation.Name) {
	case OperationImagesGenerations:
		return ImageOperationMetadata{Name: OperationImagesGenerations, NativePath: "/v1/images/generations"}, true
	case OperationImagesEdits:
		return ImageOperationMetadata{Name: OperationImagesEdits, NativePath: "/v1/images/edits", Edit: true}, true
	default:
		return ImageOperationMetadata{}, false
	}
}

func IsImageOperation(operation provider.Operation) bool {
	_, ok := ImageOperation(operation)
	return ok
}

func (adapter Adapter) BuildImageUpstreamRequest(_ context.Context, request ImageUpstreamRequest) (provider.UpstreamRequest, error) {
	metadata, ok := ImageOperation(request.Operation)
	if !ok {
		return provider.UpstreamRequest{}, &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: "openai_image_operation_unsupported", Detail: "OpenAI image operation is unsupported by this adapter."}
	}
	body := rewriteImageModel(request.RawBody, request.ContentType, request.TargetModelID, metadata.Edit)
	return provider.UpstreamRequest{Method: http.MethodPost, Path: metadata.NativePath, Body: body}, nil
}

func (adapter Adapter) ExtractImageModel(_ context.Context, request provider.MediaRequest) (string, error) {
	if !IsImageOperation(request.Operation) {
		return "", &provider.AdapterError{HTTPStatus: http.StatusBadRequest, Code: "openai_image_operation_unsupported", Detail: "OpenAI image operation is unsupported by this adapter."}
	}
	return extractImageModel(request.RawBody, request.ContentType), nil
}

func (adapter Adapter) HandleMedia(ctx context.Context, request provider.MediaRequest) (provider.MediaRequest, error) {
	upstream, err := adapter.BuildImageUpstreamRequest(ctx, ImageUpstreamRequest{
		Operation:     request.Operation,
		RawBody:       request.RawBody,
		ContentType:   request.ContentType,
		TargetModelID: request.TargetModelID,
	})
	if err != nil {
		return provider.MediaRequest{}, err
	}
	request.RewrittenBody = append([]byte(nil), upstream.Body...)
	return request, nil
}

func RedactImageRequestAuditBody(rawBody []byte, contentType string) []byte {
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return nil
	}
	if boundary := multipartBoundary(contentType); boundary != "" {
		return redactMultipartImageRequest(rawBody, boundary)
	}
	return redactImageJSONBody(rawBody)
}

func RedactImageResponseAuditBody(rawBody []byte, contentType string) []byte {
	return redactImageJSONBody(rawBody)
}

func extractImageModel(rawBody []byte, contentType string) string {
	if boundary := multipartBoundary(contentType); boundary != "" {
		return extractMultipartFormValue(rawBody, boundary, "model")
	}
	return extractJSONModel(rawBody)
}

func rewriteImageModel(rawBody []byte, contentType string, targetModelID string, edit bool) []byte {
	if edit {
		if boundary := multipartBoundary(contentType); boundary != "" {
			if rewritten, ok := rewriteMultipartFormValue(rawBody, boundary, "model", targetModelID); ok {
				return rewritten
			}
			return append([]byte(nil), rawBody...)
		}
	}
	return rewriteJSONModel(rawBody, targetModelID)
}

func extractJSONModel(rawBody []byte) string {
	if len(rawBody) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return ""
	}
	modelID, _ := payload["model"].(string)
	return strings.TrimSpace(modelID)
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

func redactImageJSONBody(rawBody []byte) []byte {
	if len(bytes.TrimSpace(rawBody)) == 0 {
		return nil
	}
	var payload any
	if err := json.Unmarshal(rawBody, &payload); err != nil {
		return []byte(`{"body":"[redacted image request]"}`)
	}
	redacted := redactImageJSONValue(payload, "")
	body, err := json.Marshal(redacted)
	if err != nil {
		return []byte(`{"body":"[redacted image request]"}`)
	}
	return body
}

func redactImageJSONValue(value any, key string) any {
	if redactsImageKey(key) {
		return "[redacted image bytes]"
	}
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			redacted[childKey] = redactImageJSONValue(childValue, childKey)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for index, childValue := range typed {
			redacted[index] = redactImageJSONValue(childValue, key)
		}
		return redacted
	default:
		return value
	}
}

func redactsImageKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "image", "mask", "b64_json":
		return true
	default:
		return false
	}
}

func redactMultipartImageRequest(rawBody []byte, boundary string) []byte {
	reader := multipart.NewReader(bytes.NewReader(rawBody), boundary)
	fields := map[string]any{}
	files := []map[string]any{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return []byte(`{"body":"[redacted multipart image request]"}`)
		}
		formName := strings.TrimSpace(part.FormName())
		fileName := strings.TrimSpace(part.FileName())
		if fileName != "" || redactsImageKey(formName) {
			files = append(files, map[string]any{"field": formName, "filename": fileName, "body": "[redacted image bytes]"})
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
			continue
		}
		value, readErr := io.ReadAll(part)
		_ = part.Close()
		if readErr != nil {
			return []byte(`{"body":"[redacted multipart image request]"}`)
		}
		if formName != "" {
			fields[formName] = strings.TrimSpace(string(value))
		}
	}
	payload := map[string]any{"fields": fields}
	if len(files) > 0 {
		payload["files"] = files
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return []byte(`{"body":"[redacted multipart image request]"}`)
	}
	return body
}

func MultipartBoundaryForRuntime(contentType string) string {
	return multipartBoundary(contentType)
}
