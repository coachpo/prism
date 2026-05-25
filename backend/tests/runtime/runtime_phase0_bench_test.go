package runtime_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
	"github.com/coachpo/prism/backend/internal/platform/config"
)

const runtimeCacheMissStormConcurrency = 8

func BenchmarkRuntimeHotPath(b *testing.B) {
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-hot-path-public-" + randomSuffix(),
		TargetModelID:   "benchmark-hot-path-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/hot-path"),
		EndpointAPIKey:  "benchmark-hot-path-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-0 hot path benchmark")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm runtime hot-path benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime hot-path status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
		if err != nil {
			b.Fatalf("run runtime hot-path benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime hot-path status 200, got %d", statusCode)
		}
	}
}

func BenchmarkRuntimeLargeResponse(b *testing.B) {
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkLargeResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-large-response-public-" + randomSuffix(),
		TargetModelID:   "benchmark-large-response-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/large-response"),
		EndpointAPIKey:  "benchmark-large-response-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-0 large response benchmark")

	statusCode, responseBytes, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm runtime large-response benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime large-response status 200, got %d", statusCode)
	}
	if responseBytes < 512*1024 {
		b.Fatalf("expected warm runtime large-response payload to exceed 512KiB, got %d bytes", responseBytes)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
		if err != nil {
			b.Fatalf("run runtime large-response benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime large-response status 200, got %d", statusCode)
		}
	}
}

func BenchmarkRuntimeLargeRequestBody(b *testing.B) {
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{SettingsMutator: useBenchmarkRuntimeTransportOverrides})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, []byte(`{"responseId":"gemini-benchmark-large-request","usageMetadata":{"promptTokenCount":7,"candidatesTokenCount":13,"totalTokenCount":20}}`))
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "benchmark-large-request-public-" + randomSuffix(),
		TargetModelID:   "benchmark-large-request-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/large-request"),
		EndpointAPIKey:  "benchmark-large-request-key",
	})
	rawBody := runtimeBenchmarkLargeRequestBody(b, "phase-2 large request benchmark")
	if len(rawBody) < 512*1024 {
		b.Fatalf("expected warm runtime large-request payload to exceed 512KiB, got %d bytes", len(rawBody))
	}

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, fmt.Sprintf("%s/v1beta/models/%s:generateContent", harness.url, route.PublicModelID), rawBody)
	if err != nil {
		b.Fatalf("warm runtime large-request benchmark request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime large-request status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		statusCode, _, err = performRuntimeBenchmarkRequest(harness.client, fmt.Sprintf("%s/v1beta/models/%s:generateContent", harness.url, route.PublicModelID), rawBody)
		if err != nil {
			b.Fatalf("run runtime large-request benchmark request: %v", err)
		}
		if statusCode != http.StatusOK {
			b.Fatalf("expected runtime large-request status 200, got %d", statusCode)
		}
	}
}

func BenchmarkRuntimeCacheMissStorm(b *testing.B) {
	cache := runtimeapi.NewSharedCache(time.Minute)
	harness := newRuntimeHarnessWithConfig(b, runtimeHarnessConfig{
		RuntimeOptions:  runtimeapi.Options{Cache: cache},
		SettingsMutator: useBenchmarkRuntimeTransportOverrides,
	})
	profileID := harness.activeProfileID(b)
	upstream := newRuntimeBenchmarkUpstream(b, http.StatusOK, runtimeBenchmarkHotPathResponse())
	route := harness.seedProxyRoute(b, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "benchmark-cache-miss-public-" + randomSuffix(),
		TargetModelID:   "benchmark-cache-miss-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL("/benchmark/cache-miss-storm"),
		EndpointAPIKey:  "benchmark-cache-miss-key",
	})
	rawBody := runtimeBenchmarkRequestBody(b, route.PublicModelID, "phase-0 cache miss storm benchmark")

	statusCode, _, err := performRuntimeBenchmarkRequest(harness.client, harness.url+"/v1/chat/completions", rawBody)
	if err != nil {
		b.Fatalf("warm runtime cache-miss-storm request: %v", err)
	}
	if statusCode != http.StatusOK {
		b.Fatalf("expected warm runtime cache-miss-storm status 200, got %d", statusCode)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.InvalidateActiveProfile()
		cache.InvalidatePlanningProfile(profileID)
		if err := runRuntimeBenchmarkStorm(harness.client, harness.url+"/v1/chat/completions", rawBody, runtimeCacheMissStormConcurrency); err != nil {
			b.Fatalf("run runtime cache-miss-storm benchmark: %v", err)
		}
	}
}

type runtimeBenchmarkUpstream struct {
	server *httptest.Server
}

func newRuntimeBenchmarkUpstream(tb testing.TB, statusCode int, responseBody []byte) *runtimeBenchmarkUpstream {
	tb.Helper()
	upstream := &runtimeBenchmarkUpstream{}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			_, _ = io.Copy(io.Discard, r.Body)
			_ = r.Body.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write(responseBody)
	}))
	tb.Cleanup(upstream.server.Close)
	return upstream
}

func (u *runtimeBenchmarkUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func runtimeBenchmarkRequestBody(tb testing.TB, modelID string, prompt string) []byte {
	tb.Helper()
	return mustMarshalBenchmarkJSON(tb, map[string]any{
		"messages": []map[string]any{{"role": "user", "content": prompt}},
		"model":    modelID,
	})
}

func runtimeBenchmarkHotPathResponse() []byte {
	return []byte(`{"id":"chatcmpl-benchmark-hot-path","object":"chat.completion","usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20},"choices":[{"index":0,"message":{"role":"assistant","content":"hot path benchmark response"}}]}`)
}

func runtimeBenchmarkLargeResponse() []byte {
	largeContent := strings.Repeat("phase-0-large-response-", 32768)
	return []byte(fmt.Sprintf(`{"id":"chatcmpl-benchmark-large-response","object":"chat.completion","usage":{"prompt_tokens":7,"completion_tokens":13,"total_tokens":20},"choices":[{"index":0,"message":{"role":"assistant","content":%q}}]}`, largeContent))
}

func runtimeBenchmarkLargeRequestBody(tb testing.TB, prompt string) []byte {
	tb.Helper()
	return mustMarshalBenchmarkJSON(tb, map[string]any{
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": prompt + strings.Repeat("-large-request-body-", 32768)}},
		}},
	})
}

func mustMarshalBenchmarkJSON(tb testing.TB, value any) []byte {
	tb.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		tb.Fatalf("marshal benchmark JSON: %v", err)
	}
	return raw
}

func performRuntimeBenchmarkRequest(client *http.Client, url string, rawBody []byte) (int, int, error) {
	request, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(rawBody))
	if err != nil {
		return 0, 0, fmt.Errorf("build benchmark request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return 0, 0, fmt.Errorf("perform benchmark request: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return response.StatusCode, 0, fmt.Errorf("read benchmark response body: %w", err)
	}
	return response.StatusCode, len(responseBody), nil
}

func runRuntimeBenchmarkStorm(client *http.Client, url string, rawBody []byte, requestCount int) error {
	if requestCount < 1 {
		return fmt.Errorf("runtime benchmark storm request count must be >= 1, got %d", requestCount)
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		firstErr error
	)
	recordErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
	}
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			statusCode, _, err := performRuntimeBenchmarkRequest(client, url, rawBody)
			if err != nil {
				recordErr(err)
				return
			}
			if statusCode != http.StatusOK {
				recordErr(fmt.Errorf("expected storm request status 200, got %d", statusCode))
			}
		}()
	}
	wg.Wait()
	return firstErr
}

func useBenchmarkRuntimeTransportOverrides(settings *config.Settings) {
	settings.RuntimeTransportConfig = config.RuntimeTransportConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   8,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 0,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}
