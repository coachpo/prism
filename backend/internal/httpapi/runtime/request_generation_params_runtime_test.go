package runtime

import (
	"io"
	"strings"
	"testing"
)

func TestRequestGenerationParamsObservingReadCloserFinishesOnEOF(t *testing.T) {
	observer := newGeminiGenerationParamsStreamingObserver()
	reader := &requestGenerationParamsObservingReadCloser{source: io.NopCloser(strings.NewReader(`{"generationConfig":{"maxOutputTokens":42}}`)), observer: observer}
	_, err := io.Copy(io.Discard, reader)
	if err != nil {
		t.Fatalf("copy observed body: %v", err)
	}
	snapshot := observer.Snapshot()
	if snapshot.Status != requestGenerationParamsStatusComplete || snapshot.Params == nil || snapshot.Params.MaxOutputTokens == nil || *snapshot.Params.MaxOutputTokens != 42 {
		t.Fatalf("expected EOF to finalize observed generation params, got %+v", snapshot)
	}
}
