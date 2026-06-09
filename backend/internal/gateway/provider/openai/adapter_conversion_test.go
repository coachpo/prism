package openai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coachpo/prism/backend/internal/gateway/provider"
	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

func TestAdapterBuildTextUpstreamRequestUsesGoldenConversion(t *testing.T) {
	adapter := openai.New()
	upstream, err := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
		Operation:       provider.Operation{Name: openai.OperationResponses, APIFamily: provider.APIFamilyOpenAI},
		RawBody:         []byte(`{"model":"responses-public","instructions":"system note","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}],"max_output_tokens":64,"temperature":0.2}`),
		TargetModelID:   "chat-target",
		TranslationMode: provider.TranslationModeOpenAIResponsesToChatCompletions,
	})
	if err != nil {
		t.Fatalf("build translated upstream request: %v", err)
	}
	if upstream.Method != http.MethodPost || upstream.Path != "/v1/chat/completions" {
		t.Fatalf("expected POST /v1/chat/completions, got %+v", upstream)
	}
	assertAdapterGoldenJSON(t, "request_responses_to_chat.json", upstream.Body)
}

func TestAdapterRejectsAdjunctConversionBeforeTranslation(t *testing.T) {
	adapter := openai.New()
	_, err := adapter.BuildTextUpstreamRequest(context.Background(), openai.TextUpstreamRequest{
		Operation:       provider.Operation{Name: openai.OperationResponsesInputTokens, APIFamily: provider.APIFamilyOpenAI},
		RawBody:         []byte(`{"model":"responses-public","input":"hello"}`),
		TargetModelID:   "chat-target",
		TranslationMode: provider.TranslationModeOpenAIResponsesToChatCompletions,
	})
	var adapterErr *provider.AdapterError
	if !errors.As(err, &adapterErr) {
		t.Fatalf("expected adapter error, got %v", err)
	}
	if adapterErr.HTTPStatus != http.StatusBadRequest || adapterErr.Code != "openai_request_translation_unsupported" || adapterErr.Fields["unsupported_reason"] != "operation_translation_unsupported" {
		t.Fatalf("expected typed adjunct conversion rejection, got %+v", adapterErr)
	}
}

func TestAdapterBuildImageUpstreamRequestRewritesJSONAndMultipart(t *testing.T) {
	adapter := openai.New()
	generation, err := adapter.BuildImageUpstreamRequest(context.Background(), openai.ImageUpstreamRequest{
		Operation:     provider.Operation{Name: openai.OperationImagesGenerations, APIFamily: provider.APIFamilyOpenAI},
		RawBody:       []byte(`{"model":"public-image","prompt":"cat"}`),
		ContentType:   "application/json",
		TargetModelID: "target-image",
	})
	if err != nil {
		t.Fatalf("build image generation request: %v", err)
	}
	if generation.Method != http.MethodPost || generation.Path != "/v1/images/generations" || adapterTestJSONModel(t, generation.Body) != "target-image" {
		t.Fatalf("unexpected image generation upstream request: %+v body=%s", generation, string(generation.Body))
	}

	editBody, editContentType := adapterImageEditMultipartBody(t, "public-image")
	edit, err := adapter.BuildImageUpstreamRequest(context.Background(), openai.ImageUpstreamRequest{Operation: provider.Operation{Name: openai.OperationImagesEdits, APIFamily: provider.APIFamilyOpenAI}, RawBody: editBody, ContentType: editContentType, TargetModelID: "target-image"})
	if err != nil {
		t.Fatalf("build image edit request: %v", err)
	}
	if edit.Method != http.MethodPost || edit.Path != "/v1/images/edits" || adapterMultipartValue(t, edit.Body, editContentType, "model") != "target-image" || adapterMultipartValue(t, edit.Body, editContentType, "image") != "fake-png-bytes" {
		t.Fatalf("unexpected image edit upstream request: %+v body=%s", edit, string(edit.Body))
	}
}

func TestImageAuditRedactionRemovesImageBytes(t *testing.T) {
	request := openai.RedactImageRequestAuditBody([]byte(`{"model":"gpt-image-1","prompt":"draw","image":"raw-image-bytes"}`), "application/json")
	if strings.Contains(string(request), "raw-image-bytes") || !strings.Contains(string(request), "gpt-image-1") {
		t.Fatalf("expected JSON request redaction to keep metadata only, got %s", string(request))
	}
	response := openai.RedactImageResponseAuditBody([]byte(`{"data":[{"b64_json":"raw-base64-image"},{"url":"https://example.test/image.png"}]}`), "application/json")
	if strings.Contains(string(response), "raw-base64-image") || !strings.Contains(string(response), "image.png") {
		t.Fatalf("expected JSON response redaction to remove b64 image bytes, got %s", string(response))
	}

	editBody, editContentType := adapterImageEditMultipartBody(t, "gpt-image-1")
	multipartRequest := openai.RedactImageRequestAuditBody(editBody, editContentType)
	if strings.Contains(string(multipartRequest), "fake-png-bytes") || !strings.Contains(string(multipartRequest), "gpt-image-1") || !strings.Contains(string(multipartRequest), "input.png") {
		t.Fatalf("expected multipart request redaction to remove file bytes only, got %s", string(multipartRequest))
	}
}

func adapterTestJSONModel(t *testing.T, body []byte) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode adapter JSON body: %v", err)
	}
	model, _ := payload["model"].(string)
	return model
}

func adapterImageEditMultipartBody(t *testing.T, model string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", model); err != nil {
		t.Fatalf("write image edit model field: %v", err)
	}
	if err := writer.WriteField("prompt", "make the image brighter"); err != nil {
		t.Fatalf("write image edit prompt field: %v", err)
	}
	imagePart, err := writer.CreateFormFile("image", "input.png")
	if err != nil {
		t.Fatalf("create image edit file field: %v", err)
	}
	if _, err := imagePart.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatalf("write image edit file bytes: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close image edit multipart writer: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func adapterMultipartValue(t *testing.T, body []byte, contentType string, field string) string {
	t.Helper()
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		t.Fatalf("parse multipart content type: %v", err)
	}
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read multipart part: %v", err)
		}
		if part.FormName() != field {
			_ = part.Close()
			continue
		}
		value, err := io.ReadAll(part)
		_ = part.Close()
		if err != nil {
			t.Fatalf("read multipart field %s: %v", field, err)
		}
		return string(value)
	}
	t.Fatalf("multipart field %s not found", field)
	return ""
}

func assertAdapterGoldenJSON(t *testing.T, name string, actual []byte) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(actual, &decoded); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}
	canonical, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("canonicalize actual JSON: %v", err)
	}
	goldenPath := filepath.Join("..", "..", "..", "httpapi", "runtime", "testdata", "openai_translation", name)
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v", goldenPath, err)
	}
	if !bytes.Equal(bytes.TrimSpace(canonical), bytes.TrimSpace(expected)) {
		t.Fatalf("golden mismatch\nexpected: %s\nactual:   %s", bytes.TrimSpace(expected), bytes.TrimSpace(canonical))
	}
}
