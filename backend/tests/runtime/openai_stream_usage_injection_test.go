package runtimetest

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

const openaiStreamUsageChatChunks = "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":100,\"completion_tokens\":20,\"total_tokens\":120}}\n\ndata: [DONE]\n\n"

// openaiStreamNoUsageChatChunks models an upstream honoring include_usage=false:
// content chunks terminate without any usage event.
const openaiStreamNoUsageChatChunks = "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n"

// TestOpenAIStreamingChatCompletionsInjectsIncludeUsageAndPrices is the
// value-proposition end of the stream-usage instrumentation: the adapter adds
// stream_options.include_usage to the upstream body, the upstream emits the
// final usage chunk, and the row lands priced instead of MISSING_TOKEN_USAGE.
func TestOpenAIStreamingChatCompletionsInjectsIncludeUsageAndPrices(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "text/event-stream", []byte(openaiStreamUsageChatChunks))

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:            profileID,
		APIFamily:            "openai",
		PublicModelID:        "stream-usage-public-" + randomSuffix(),
		TargetModelID:        "stream-usage-target-" + randomSuffix(),
		EndpointBaseURL:      upstream.baseURL("/stream-usage/chat"),
		EndpointAPIKey:       "stream-usage-key",
		OpenAITextCapability: runtimeStringPtr("chat_completions_only"),
	})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "stream-usage-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "1000", "2000", "", "", "")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"model":    route.PublicModelID,
			"messages": []map[string]any{{"role": "user", "content": "stream usage"}},
			"stream":   true,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	// (a) The upstream body carries the injected stream_options.
	upstreamRequest := upstream.lastRequest(t)
	var body map[string]any
	if err := json.Unmarshal(upstreamRequest.Body, &body); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	streamOptions, present := body["stream_options"]
	if !present {
		t.Fatalf("expected injected stream_options in upstream body, got %s", upstreamRequest.Body)
	}
	options, ok := streamOptions.(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Fatalf("expected stream_options.include_usage=true, got %v", streamOptions)
	}

	// (b) Tokens persisted from the merged usage chunk.
	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || !row.InputTokens.Valid || row.InputTokens.Int64 != 100 || !row.OutputTokens.Valid || row.OutputTokens.Int64 != 20 || !row.TotalTokens.Valid || row.TotalTokens.Int64 != 120 {
		t.Fatalf("expected streamed request log to persist merged usage tokens, got %+v", row)
	}

	// (c) Priced at 100*1000 + 20*2000 micros.
	if !row.TotalCostUserCurrencyMicros.Valid || row.TotalCostUserCurrencyMicros.Int64 != 140000 {
		t.Fatalf("expected priced cost 140000 micros, got %+v", row.TotalCostUserCurrencyMicros)
	}
	if !row.PricingStatus.Valid || row.PricingStatus.String != "priced" || row.UnpricedReason.Valid {
		t.Fatalf("expected pricing_status=priced without unpriced_reason, got %+v", row)
	}
	usageEventRow := loadLatestRuntimeUsageEventStreamTelemetryRow(t, harness.conn, profileID)
	if usageEventRow.StreamOutcome != "completed" || !usageEventRow.PricingStatus.Valid || usageEventRow.PricingStatus.String != "priced" || usageEventRow.UnpricedReason.Valid {
		t.Fatalf("expected priced usage event, got %+v", usageEventRow)
	}
}

// TestOpenAIStreamingChatCompletionsPreservesClientIncludeUsageFalse pins the
// caller-intent-wins decision: an explicit include_usage=false is forwarded
// untouched and the row lands MISSING_TOKEN_USAGE instead of being overridden.
func TestOpenAIStreamingChatCompletionsPreservesClientIncludeUsageFalse(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "text/event-stream", []byte(openaiStreamNoUsageChatChunks))

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:            profileID,
		APIFamily:            "openai",
		PublicModelID:        "stream-usage-false-public-" + randomSuffix(),
		TargetModelID:        "stream-usage-false-target-" + randomSuffix(),
		EndpointBaseURL:      upstream.baseURL("/stream-usage/chat-false"),
		EndpointAPIKey:       "stream-usage-false-key",
		OpenAITextCapability: runtimeStringPtr("chat_completions_only"),
	})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "stream-usage-false-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "1000", "2000", "", "", "")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"model":          route.PublicModelID,
			"messages":       []map[string]any{{"role": "user", "content": "stream usage false"}},
			"stream":         true,
			"stream_options": map[string]any{"include_usage": false},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	upstreamRequest := upstream.lastRequest(t)
	var body map[string]any
	if err := json.Unmarshal(upstreamRequest.Body, &body); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	streamOptions, present := body["stream_options"]
	if !present {
		t.Fatalf("expected stream_options in upstream body, got %s", upstreamRequest.Body)
	}
	options, ok := streamOptions.(map[string]any)
	if !ok || options["include_usage"] != false {
		t.Fatalf("expected client include_usage=false to be preserved, got %v", streamOptions)
	}

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || row.InputTokens.Valid || row.OutputTokens.Valid || row.TotalTokens.Valid {
		t.Fatalf("expected no usage without include_usage, got %+v", row)
	}
	if !row.PricingStatus.Valid || row.PricingStatus.String != "unpriced" || !row.UnpricedReason.Valid || row.UnpricedReason.String != "MISSING_TOKEN_USAGE" {
		t.Fatalf("expected unpriced MISSING_TOKEN_USAGE, got %+v", row)
	}
}

// TestConnectionCustomRequestParametersOverrideInjectedStreamOptions pins the
// documented escape hatch: a connection overlay that sets stream_options to
// null wins over the injected include_usage, so an upstream that rejects the
// field can be served without code changes.
func TestConnectionCustomRequestParametersOverrideInjectedStreamOptions(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := newRouteMatrixUpstream(t, "text/event-stream", []byte(openaiStreamUsageChatChunks))

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:               profileID,
		APIFamily:               "openai",
		PublicModelID:           "stream-usage-overlay-public-" + randomSuffix(),
		TargetModelID:           "stream-usage-overlay-target-" + randomSuffix(),
		EndpointBaseURL:         upstream.baseURL("/stream-usage/chat-overlay"),
		EndpointAPIKey:          "stream-usage-overlay-key",
		OpenAITextCapability:    runtimeStringPtr("chat_completions_only"),
		CustomRequestParameters: runtimeStringPtr(`{"stream_options": null}`),
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/chat/completions",
		map[string]any{
			"model":    route.PublicModelID,
			"messages": []map[string]any{{"role": "user", "content": "stream usage overlay"}},
			"stream":   true,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)

	upstreamRequest := upstream.lastRequest(t)
	var body map[string]any
	if err := json.Unmarshal(upstreamRequest.Body, &body); err != nil {
		t.Fatalf("decode upstream body: %v", err)
	}
	streamOptions, present := body["stream_options"]
	if !present {
		t.Fatalf("expected stream_options key from overlay, got %s", upstreamRequest.Body)
	}
	if streamOptions != nil {
		t.Fatalf("expected connection overlay to null stream_options, got %v", streamOptions)
	}
}
