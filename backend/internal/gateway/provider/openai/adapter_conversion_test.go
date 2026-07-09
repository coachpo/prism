package openai_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
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
