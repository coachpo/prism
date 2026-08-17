package runtime

// Runtime request bodies provide the replay boundary between ingress planning
// and upstream attempts. Buffered bodies can be reopened for failover;
// streaming bodies can be opened once and may observe Gemini generation
// parameters while they pass through.
//
// Consumption is serialized so a hedged or retried attempt cannot silently
// reuse one-shot ingress bytes. Nil bodies remain http.NoBody for operations
// whose provider contract permits an empty request.
//
// The observer is attached only to the streaming fast path and is not a second
// body parser. Buffered planning extracts generation parameters from each
// attempt's final materialized body.
//
// A body source is request-local and is never placed in a planning snapshot.
// Closing a streaming source remains the caller's responsibility after the
// upstream client has finished with it.
// The source also records the one-shot consumption invariant.
// It does not own request-size limits.
//
import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"sync"
)

type runtimeRequestBodySource struct {
	bufferedBody         []byte
	streamingBody        io.ReadCloser
	streamingContentSize int64
	useStreamingBody     bool
	generationObserver   *geminiGenerationParamsStreamingObserver

	mu       sync.Mutex
	consumed bool
}

func newBufferedRuntimeRequestBodySource(body []byte) *runtimeRequestBodySource {
	return &runtimeRequestBodySource{bufferedBody: body}
}

func newStreamingRuntimeRequestBodySource(body io.ReadCloser, contentLength int64) *runtimeRequestBodySource {
	return &runtimeRequestBodySource{
		streamingBody:        body,
		streamingContentSize: contentLength,
		useStreamingBody:     true,
	}
}

func (source *runtimeRequestBodySource) withGenerationParamsObserver(observer *geminiGenerationParamsStreamingObserver) *runtimeRequestBodySource {
	if source != nil {
		source.generationObserver = observer
	}
	return source
}

func (source *runtimeRequestBodySource) Open() (io.ReadCloser, int64, error) {
	if source == nil {
		return http.NoBody, 0, nil
	}
	if !source.useStreamingBody {
		return io.NopCloser(bytes.NewReader(source.bufferedBody)), int64(len(source.bufferedBody)), nil
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.consumed {
		return nil, 0, fmt.Errorf("runtime request body already consumed")
	}
	source.consumed = true
	if source.streamingBody == nil {
		return http.NoBody, 0, nil
	}
	if source.generationObserver != nil {
		return &requestGenerationParamsObservingReadCloser{source: source.streamingBody, observer: source.generationObserver}, source.streamingContentSize, nil
	}
	return source.streamingBody, source.streamingContentSize, nil
}
