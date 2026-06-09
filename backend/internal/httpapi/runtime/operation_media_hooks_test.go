package runtime

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOpenAIImageHooks(t *testing.T) {
	tests := []struct {
		name             string
		requestPath      string
		rawBody          []byte
		contentType      string
		hookCollectionID string
		requestKind      operationMediaRequestKind
	}{
		{
			name:             "image generations",
			requestPath:      "/v1/images/generations",
			rawBody:          []byte(`{"model":"gpt-image-1","prompt":"cat","stream":true,"temperature":0.7}`),
			contentType:      "application/json",
			hookCollectionID: runtimeHookCollectionOpenAIImagesGeneration,
			requestKind:      operationMediaRequestKindImageGeneration,
		},
	}
	editBody, editContentType := newOpenAIImageEditMultipartBody(t, "gpt-image-1")
	tests = append(tests, struct {
		name             string
		requestPath      string
		rawBody          []byte
		contentType      string
		hookCollectionID string
		requestKind      operationMediaRequestKind
	}{
		name:             "image edits",
		requestPath:      "/v1/images/edits",
		rawBody:          editBody,
		contentType:      editContentType,
		hookCollectionID: runtimeHookCollectionOpenAIImagesEdit,
		requestKind:      operationMediaRequestKindImageEdit,
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			operation := mustResolveRuntimeOperation(t, http.MethodPost, test.requestPath).Operation
			if operation.HookCollectionID != test.hookCollectionID {
				t.Fatalf("expected hook collection %q, got %q", test.hookCollectionID, operation.HookCollectionID)
			}
			mediaHooks, ok := mediaHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected media hooks for %s", operation.Name)
			}
			if mediaHooks.Provider != "openai" || mediaHooks.RequestKind != test.requestKind {
				t.Fatalf("expected openai/%s media hooks, got %+v", test.requestKind, mediaHooks)
			}
			requestHooks, ok := requestHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected request hooks for %s", operation.Name)
			}
			if requestHooks.Provider != "openai" {
				t.Fatalf("expected openai request hooks, got %+v", requestHooks)
			}
			if requestHooks.ExtractBufferedGenerationParams != nil || requestHooks.NewGenerationParamsStreamingObserver != nil {
				t.Fatal("expected media request hooks to omit generation-param extractors")
			}
			if requestWantsStreamForOperation(operation, test.rawBody, test.requestPath) {
				t.Fatal("expected media request hooks to ignore stream-like request fields")
			}
			responseHooks, ok := responseHooksForOperation(operation)
			if !ok {
				t.Fatalf("expected response hooks for %s", operation.Name)
			}
			if responseHooks.Provider != "openai" || responseHooks.Kind != operationResponseKindMedia {
				t.Fatalf("expected openai media response hooks, got %+v", responseHooks)
			}
			if _, ok := streamHooksForOperation(operation); ok {
				t.Fatalf("expected no SSE hooks for media operation %s", operation.Name)
			}
			snapshot := extractBufferedRequestGenerationParams(operation, test.rawBody)
			assertMissingRequestGenerationParams(t, snapshot)
			if got := extractModelFromBodyForOperation(test.rawBody, test.contentType, operation); got != "gpt-image-1" {
				t.Fatalf("expected media model extraction to return gpt-image-1, got %q", got)
			}
		})
	}
}

func TestOpenAIImageEditsMultipartModelBinding(t *testing.T) {
	t.Run("native edit request keeps original multipart body replayable", func(t *testing.T) {
		rawBody, contentType := newOpenAIImageEditMultipartBody(t, "gpt-image-1")
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/images/edits")
		if got := extractModelFromBody(rawBody); got != "" {
			t.Fatalf("expected generic JSON body extraction to ignore multipart, got %q", got)
		}
		if got, err := resolveModelIDForOperation(rawBody, contentType, operationMatch); err != nil || got != "gpt-image-1" {
			t.Fatalf("expected multipart model id, got model=%q err=%v", got, err)
		}

		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "gpt-image-1"})
		request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
		request.Header.Set("Content-Type", contentType)
		plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		if err != nil {
			t.Fatalf("build image edit plan: %v", err)
		}
		if plan.RequestedModelID != "gpt-image-1" || plan.EffectiveRequestPath != "/v1/images/edits" {
			t.Fatalf("expected image edit plan to bind model/path, got model=%q path=%q", plan.RequestedModelID, plan.EffectiveRequestPath)
		}
		if plan.IsStreamingRequest {
			t.Fatal("expected image edit plan to stay non-streaming")
		}
		assertMissingRequestGenerationParams(t, plan.RequestGenerationParams)
		if !bytes.Equal(plan.UpstreamBody, rawBody) {
			t.Fatal("expected native image edit body to forward unchanged")
		}
		assertReplayableBodySource(t, newBufferedRuntimeRequestBodySource(plan.UpstreamBody), rawBody)
	})

	t.Run("proxy edit request rewrites multipart model and remains replayable", func(t *testing.T) {
		rawBody, contentType := newOpenAIImageEditMultipartBody(t, "public-image")
		operationMatch := mustResolveRuntimeOperation(t, http.MethodPost, "/v1/images/edits")
		service := newRequestPlanUnitService()
		snapshot := newRequestPlanSnapshot(
			runtimeModelRecord{ID: 1, APIFamily: "openai", ModelID: "public-image"},
			runtimeModelRecord{ID: 2, APIFamily: "openai", ModelID: "target-image"},
		)
		addRequestPlanProxyTarget(snapshot, "public-image", "target-image")
		request := httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
		request.Header.Set("Content-Type", contentType)
		plan, err := service.buildRequestPlanFromSnapshot(request, rawBody, RuntimeProxyConfigSnapshot{}, operationMatch, requestPlanTestProfileID, snapshot)
		if err != nil {
			t.Fatalf("build proxied image edit plan: %v", err)
		}
		if plan.RequestedModelID != "public-image" || plan.ResolvedTargetModelID == nil || *plan.ResolvedTargetModelID != "target-image" {
			t.Fatalf("expected proxy model to resolve target-image, got requested=%q target=%v", plan.RequestedModelID, plan.ResolvedTargetModelID)
		}
		if bytes.Equal(plan.UpstreamBody, rawBody) {
			t.Fatal("expected proxy image edit body to rewrite the multipart model field")
		}
		if got := extractModelFromBodyForOperation(plan.UpstreamBody, contentType, operationMatch.Operation); got != "target-image" {
			t.Fatalf("expected rewritten multipart model target-image, got %q", got)
		}
		assertMissingRequestGenerationParams(t, plan.RequestGenerationParams)
		assertReplayableBodySource(t, newBufferedRuntimeRequestBodySource(plan.UpstreamBody), plan.UpstreamBody)
	})
}

func newOpenAIImageEditMultipartBody(t *testing.T, model string) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("model", model); err != nil {
		t.Fatalf("write model field: %v", err)
	}
	if err := writer.WriteField("prompt", "make the image brighter"); err != nil {
		t.Fatalf("write prompt field: %v", err)
	}
	imagePart, err := writer.CreateFormFile("image", "input.png")
	if err != nil {
		t.Fatalf("create image part: %v", err)
	}
	if _, err := imagePart.Write([]byte("fake-png-bytes")); err != nil {
		t.Fatalf("write image part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func assertMissingRequestGenerationParams(t *testing.T, snapshot requestGenerationParamsSnapshot) {
	t.Helper()
	if snapshot.Status != requestGenerationParamsStatusMissing || snapshot.Params != nil {
		t.Fatalf("expected missing request-generation params, got %+v", snapshot)
	}
}

func assertReplayableBodySource(t *testing.T, source *runtimeRequestBodySource, want []byte) {
	t.Helper()
	for attempt := range 2 {
		reader, size, err := source.Open()
		if err != nil {
			t.Fatalf("open body source attempt %d: %v", attempt+1, err)
		}
		got, err := io.ReadAll(reader)
		_ = reader.Close()
		if err != nil {
			t.Fatalf("read body source attempt %d: %v", attempt+1, err)
		}
		if size != int64(len(want)) || !bytes.Equal(got, want) {
			t.Fatalf("attempt %d expected replayable body len=%d bytes=%q, got len=%d bytes=%q", attempt+1, len(want), string(want), size, string(got))
		}
	}
}
