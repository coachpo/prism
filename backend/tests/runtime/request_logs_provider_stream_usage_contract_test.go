package runtimetest

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func TestRuntimeUsageEventEndpointLabelSnapshotUsesSelectedEndpointIdentity(t *testing.T) {
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
		RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
			WorkerCount:  1,
			PollInterval: 25 * time.Millisecond,
			Hooks: &runtimeapi.TelemetryOutboxHooks{
				BeforeMaterialize: gate.Wait,
			},
		}},
	})
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "runtime-usage-label-public-" + suffix
	targetModelID := "runtime-usage-label-target-" + suffix
	endpointName := "  Runtime Snapshot Endpoint " + suffix + "  "
	mutatedEndpointName := "mutated endpoint name " + suffix
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{"id": "chatcmpl-runtime-usage-label-" + suffix})
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-usage-label-strategy-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, profileID, endpointName, upstream.baseURL("/request-logs/usage-label/snapshot"), "runtime-usage-label-key", 0)
	connectionID := harness.seedConnection(t, profileID, targetModelConfigID, endpointID, "runtime-usage-label-connection-"+suffix, nil, nil, 0)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "persist endpoint label snapshot from request-time identity"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 0, UsageEvents: 0, OutboxRows: 1}, 5*time.Second)
	if _, err := harness.conn.Exec(context.Background(), `UPDATE endpoints SET name = $2, updated_at = $3 WHERE id = $1`, endpointID, mutatedEndpointName, time.Now().UTC()); err != nil {
		t.Fatalf("mutate endpoint label before durable telemetry materialization: %v", err)
	}

	gate.Release()
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	row := loadLatestRuntimeUsageEndpointLabelSnapshot(t, harness.conn, profileID)
	if !row.EndpointID.Valid || int(row.EndpointID.Int64) != endpointID || !row.ConnectionID.Valid || int(row.ConnectionID.Int64) != connectionID || row.EndpointLabelSnapshot != strings.TrimSpace(endpointName) {
		t.Fatalf("expected usage-event label snapshot from request-time selected endpoint identity, got %+v", row)
	}
	if row.CurrencyAttribution != "identified" {
		t.Fatalf("expected live usage writer to persist identified currency ownership, got %+v", row)
	}
}

func TestRuntimeUsageEventEndpointLabelSnapshotFallsBackToBaseURL(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "runtime-usage-label-base-url-public-" + suffix
	targetModelID := "runtime-usage-label-base-url-target-" + suffix
	endpointBaseURL := harness.upstream.baseURL("/request-logs/usage-label/base-url")
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-usage-label-base-url-strategy-"+suffix, "round-robin")
	targetModelConfigID := harness.seedModel(t, profileID, "openai", targetModelID, "native", &strategyID)
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "proxy", &strategyID)
	harness.seedProxyTarget(t, publicModelConfigID, targetModelConfigID)
	endpointID := harness.seedEndpoint(t, profileID, "   ", endpointBaseURL, "runtime-usage-label-base-url-key", 0)
	harness.seedConnection(t, profileID, targetModelConfigID, endpointID, "runtime-usage-label-base-url-connection-"+suffix, nil, nil, 0)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "persist endpoint label base URL fallback"}},
		"model":    publicModelID,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	row := loadLatestRuntimeUsageEndpointLabelSnapshot(t, harness.conn, profileID)
	if row.EndpointLabelSnapshot != endpointBaseURL {
		t.Fatalf("expected usage-event label snapshot to fall back to endpoint base URL %q, got %+v", endpointBaseURL, row)
	}
}

func TestRuntimeUsageEventEndpointLabelSnapshotForSelectedEndpoint(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	publicModelID := "gpt-4o-runtime-usage-label-unknown-public-" + suffix
	strategyID := harness.seedLegacyStrategy(t, profileID, "runtime-usage-label-unknown-strategy-"+suffix, "fill-first")
	publicModelConfigID := harness.seedModel(t, profileID, "openai", publicModelID, "native", &strategyID)
	smallEndpointID := harness.seedEndpoint(t, profileID, "runtime-usage-label-unknown-small-"+suffix, harness.upstream.baseURL("/request-logs/usage-label/unknown/small"), "runtime-usage-label-unknown-small-key", 0)
	largeEndpointID := harness.seedEndpoint(t, profileID, "runtime-usage-label-unknown-large-"+suffix, harness.upstream.baseURL("/request-logs/usage-label/unknown/large"), "runtime-usage-label-unknown-large-key", 1)
	smallConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, smallEndpointID, "runtime-usage-label-unknown-small-connection-"+suffix, nil, nil, 0)
	largeConnectionID := harness.seedConnection(t, profileID, publicModelConfigID, largeEndpointID, "runtime-usage-label-unknown-large-connection-"+suffix, nil, nil, 1)
	_ = largeConnectionID
	harness.refreshRuntimeSnapshot(t, runtimeapi.RefreshRequest{PlanningProfileIDs: []int{profileID}})

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"messages":              []map[string]any{{"role": "user", "content": "force no endpoint attribution"}},
		"model":                 publicModelID,
		"max_completion_tokens": 600,
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	row := loadLatestRuntimeUsageEndpointLabelSnapshot(t, harness.conn, profileID)
	if !row.EndpointID.Valid || int(row.EndpointID.Int64) != smallEndpointID || !row.ConnectionID.Valid || int(row.ConnectionID.Int64) != smallConnectionID {
		t.Fatalf("expected usage event to persist selected endpoint/connection, got %+v", row)
	}
	if row.EndpointLabelSnapshot != "runtime-usage-label-unknown-small-"+suffix {
		t.Fatalf("expected selected endpoint label snapshot, got %+v", row)
	}
}

func TestRuntimeRequestLogPersistsStreamedResponsesUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.created\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.created\",\"response\":{\"usage\":{\"input_tokens\":999,\"output_tokens\":999,\"total_tokens\":1998}}}\n\n")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens_details\":{\"reasoning_tokens\":5}}}}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "stream-public-" + randomSuffix(),
		TargetModelID:   "stream-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-stream-key",
	})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/responses",
		map[string]any{
			"model": route.PublicModelID,
			"input": []map[string]any{{
				"type": "message",
				"role": "user",
				"content": []map[string]any{{
					"type": "input_text",
					"text": "你好",
				}},
			}},
			"stream": true,
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:          runtimeNullInt64(5),
		OutputTokens:         runtimeNullInt64(8),
		TotalTokens:          runtimeNullInt64(20),
		CacheReadInputTokens: runtimeNullInt64(2),
		ReasoningTokens:      runtimeNullInt64(5),
	})
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || row.StreamErrorKind.Valid || row.StreamErrorDetail.Valid || !row.TotalCostUserCurrencyMicros.Valid || row.TotalCostUserCurrencyMicros.Int64 != 50 || !row.PricingStatus.Valid || row.PricingStatus.String != "priced" || row.UnpricedReason.Valid || !row.CompletionDurationMS.Valid {
		t.Fatalf("expected completed streamed request log to persist priced stream telemetry, got %+v", row)
	}
	usageEventRow := loadLatestRuntimeUsageEventStreamTelemetryRow(t, harness.conn, profileID)
	if usageEventRow.StreamOutcome != "completed" || usageEventRow.StreamErrorKind.Valid || !usageEventRow.TotalCostUserCurrencyMicros.Valid || usageEventRow.TotalCostUserCurrencyMicros.Int64 != 50 || !usageEventRow.PricingStatus.Valid || usageEventRow.PricingStatus.String != "priced" || usageEventRow.UnpricedReason.Valid || !usageEventRow.CompletionDurationMS.Valid {
		t.Fatalf("expected completed streamed usage event to persist priced stream telemetry, got %+v", usageEventRow)
	}
}

func TestRuntimeRequestLogPersistsProviderIncompleteStreamOutcome(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
		_, _ = io.WriteString(w, "event: response.incomplete\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.incomplete\",\"response\":{\"status\":\"incomplete\"}}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "stream-incomplete-public-" + randomSuffix(), TargetModelID: "stream-incomplete-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-stream-incomplete-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-incomplete-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "provider incomplete stream", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "provider_incomplete" || row.StreamErrorKind.Valid || row.StreamErrorDetail.Valid || !row.CompletionDurationMS.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected provider-incomplete stream telemetry with terminal duration and unavailable usage reason, got %+v", row)
	}
}

func TestRuntimeRequestLogPersistsStreamEOFWithoutTerminalOutcome(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "stream-missing-terminal-public-" + randomSuffix(), TargetModelID: "stream-missing-terminal-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-stream-missing-terminal-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-missing-terminal-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "missing terminal stream", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "upstream_ended_without_terminal" || !row.StreamErrorKind.Valid || row.StreamErrorKind.String != "missing_terminal_event" || row.StreamErrorDetail.Valid || row.InputTokens.Valid || row.OutputTokens.Valid || row.TotalTokens.Valid || row.TotalCostUserCurrencyMicros.Valid || row.CompletionDurationMS.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected missing-terminal stream telemetry with null usage/cost and unavailable reason, got %+v", row)
	}
}

func TestRuntimeRequestLogCompletedStreamWithMissingUsageKeepsMissingTokenUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.completed\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\",\"response\":{\"usage\":null}}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "stream-missing-usage-public-" + randomSuffix(), TargetModelID: "stream-missing-usage-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-stream-missing-usage-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-stream-missing-usage-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "completed stream without usage", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || row.StreamErrorKind.Valid || row.TotalCostUserCurrencyMicros.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "MISSING_TOKEN_USAGE" {
		t.Fatalf("expected completed missing-usage stream to keep missing-token reason, got %+v", row)
	}
}

func TestRuntimeRequestLogUsageDiscardInvalidUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	suffix := randomSuffix()
	upstream := newScriptedUpstream(t, http.StatusOK, map[string]any{
		"id":    "chatcmpl-invalid-usage-" + suffix,
		"usage": map[string]any{"prompt_tokens": 7, "completion_tokens": 13, "total_tokens": 19},
	})
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "invalid-usage-public-" + suffix, TargetModelID: "invalid-usage-target-" + suffix, EndpointBaseURL: upstream.baseURL("/request-logs/invalid-usage"), EndpointAPIKey: "runtime-invalid-usage-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-invalid-usage-pricing-"+suffix, loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{"model": route.PublicModelID, "messages": []map[string]any{{"role": "user", "content": "invalid usage should discard"}}}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	assertRuntimePricingRowDiscardedUsage(t, loadLatestRuntimePricingRowForTable(t, harness.conn, profileID, "request_logs"), "MISSING_TOKEN_USAGE")
	assertRuntimePricingRowDiscardedUsage(t, loadLatestRuntimePricingRowForTable(t, harness.conn, profileID, "usage_request_events"), "MISSING_TOKEN_USAGE")
}

func TestRuntimeRequestLogMissingTerminalUsageDiscard(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: response.output_text.delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"response\":{\"usage\":{\"input_tokens\":7,\"output_tokens\":13,\"total_tokens\":20}},\"delta\":\"partial\"}\n\n")
	}))
	defer upstream.Close()
	route := harness.seedProxyRoute(t, runtimeRouteSeed{ProfileID: profileID, APIFamily: "openai", PublicModelID: "missing-terminal-usage-public-" + randomSuffix(), TargetModelID: "missing-terminal-usage-target-" + randomSuffix(), EndpointBaseURL: upstream.URL, EndpointAPIKey: "runtime-missing-terminal-usage-key"})
	pricingTemplateID := insertRuntimePricingTemplate(t, harness.conn, profileID, "runtime-missing-terminal-usage-pricing-"+randomSuffix(), loadRuntimeReportCurrencyCode(t, harness.conn, profileID), "2", "5", "0", "0", "0")
	attachRuntimeConnectionPricingTemplate(t, harness, route.ConnectionID, pricingTemplateID)

	response := harness.requestJSON(t, http.MethodPost, "/v1/responses", map[string]any{"model": route.PublicModelID, "input": "missing terminal with usage", "stream": true}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "upstream_ended_without_terminal" || !row.StreamErrorKind.Valid || row.StreamErrorKind.String != "missing_terminal_event" || row.InputTokens.Valid || row.OutputTokens.Valid || row.TotalTokens.Valid || row.TotalCostUserCurrencyMicros.Valid || !row.UnpricedReason.Valid || row.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected missing-terminal usage to be discarded in request log, got %+v", row)
	}
	usageEventRow := loadLatestRuntimeUsageEventStreamTelemetryRow(t, harness.conn, profileID)
	if usageEventRow.StreamOutcome != "upstream_ended_without_terminal" || usageEventRow.InputTokens.Valid || usageEventRow.OutputTokens.Valid || usageEventRow.TotalTokens.Valid || usageEventRow.TotalCostUserCurrencyMicros.Valid || !usageEventRow.UnpricedReason.Valid || usageEventRow.UnpricedReason.String != "STREAM_USAGE_UNAVAILABLE" {
		t.Fatalf("expected missing-terminal usage to be discarded in usage event, got %+v", usageEventRow)
	}
}

func TestRuntimeAnthropicStreamRequestLogPersistsSplitUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"content\":[],\"model\":\"claude-sonnet-4-6\",\"usage\":{\"input_tokens\":7,\"cache_read_input_tokens\":2,\"cache_creation_input_tokens\":3,\"output_tokens\":1}}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":13}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\n")
		_, _ = io.WriteString(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "anthropic",
		PublicModelID:   "stream-anthropic-public-" + randomSuffix(),
		TargetModelID:   "stream-anthropic-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-anthropic-stream-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		"/v1/messages",
		map[string]any{
			"model":      route.PublicModelID,
			"max_tokens": 16,
			"stream":     true,
			"messages":   []map[string]any{{"role": "user", "content": "你好"}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:              runtimeNullInt64(7),
		OutputTokens:             runtimeNullInt64(13),
		TotalTokens:              runtimeNullInt64(25),
		CacheReadInputTokens:     runtimeNullInt64(2),
		CacheCreationInputTokens: runtimeNullInt64(3),
	})
	assertLatestRuntimeWinningRequestLogTiming(t, harness.conn, profileID, false)
}

func TestRuntimeRequestLogPersistsStreamedGeminiUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好\"}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好！有什么我可以帮你的吗？\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":25,\"cachedContentTokenCount\":3,\"thoughtsTokenCount\":5}}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "stream-gemini-public-" + randomSuffix(),
		TargetModelID:   "stream-gemini-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-gemini-stream-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID),
		map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": "你好"}},
			}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:          runtimeNullInt64(4),
		OutputTokens:         runtimeNullInt64(13),
		TotalTokens:          runtimeNullInt64(25),
		CacheReadInputTokens: runtimeNullInt64(3),
		ReasoningTokens:      runtimeNullInt64(5),
	})
	row := loadLatestRuntimeRequestLogStreamTelemetryRow(t, harness.conn, profileID)
	if row.StreamOutcome != "completed" || row.StreamErrorKind.Valid || row.StreamErrorDetail.Valid || !row.CompletionDurationMS.Valid {
		t.Fatalf("expected Gemini streamGenerateContent response to persist completed stream telemetry, got %+v", row)
	}
	usageEventRow := loadLatestRuntimeUsageEventStreamTelemetryRow(t, harness.conn, profileID)
	if usageEventRow.StreamOutcome != "completed" || usageEventRow.StreamErrorKind.Valid || !usageEventRow.CompletionDurationMS.Valid {
		t.Fatalf("expected Gemini streamGenerateContent usage event to persist completed stream telemetry, got %+v", usageEventRow)
	}
}

func TestRuntimeRequestLogPersistsGeminiStreamGenerateContentUsage(t *testing.T) {
	harness := newRuntimeHarness(t)
	profileID := harness.activeProfileID(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好\"}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你好！有什么我可以帮你的吗？\"}]}}],\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":13,\"totalTokenCount\":99,\"cachedContentTokenCount\":3,\"thoughtsTokenCount\":5}}\n\n")
	}))
	defer upstream.Close()

	route := harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "gemini",
		PublicModelID:   "stream-gemini-authoritative-public-" + randomSuffix(),
		TargetModelID:   "stream-gemini-authoritative-target-" + randomSuffix(),
		EndpointBaseURL: upstream.URL,
		EndpointAPIKey:  "runtime-gemini-stream-authoritative-key",
	})

	response := harness.requestJSON(
		t,
		http.MethodPost,
		fmt.Sprintf("/v1beta/models/%s:streamGenerateContent", route.PublicModelID),
		map[string]any{
			"contents": []map[string]any{{
				"role":  "user",
				"parts": []map[string]any{{"text": "你好"}},
			}},
		},
		nil,
	)
	assertStatus(t, response, http.StatusOK)
	assertLatestRuntimeUsageRows(t, harness.conn, profileID, true, runtimePersistedUsageRow{
		InputTokens:          runtimeNullInt64(4),
		OutputTokens:         runtimeNullInt64(13),
		TotalTokens:          runtimeNullInt64(99),
		CacheReadInputTokens: runtimeNullInt64(3),
		ReasoningTokens:      runtimeNullInt64(5),
	})
}

type runtimeUsageEndpointLabelSnapshotRow struct {
	EndpointID            sql.NullInt64
	ConnectionID          sql.NullInt64
	EndpointLabelSnapshot string
	CurrencyAttribution   string
}

func loadLatestRuntimeUsageEndpointLabelSnapshot(t *testing.T, conn *pgx.Conn, profileID int) runtimeUsageEndpointLabelSnapshotRow {
	t.Helper()
	var row runtimeUsageEndpointLabelSnapshotRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT endpoint_id, connection_id, endpoint_label_snapshot, currency_attribution FROM usage_request_events WHERE profile_id = $1 ORDER BY id DESC LIMIT 1`,
		profileID,
	).Scan(&row.EndpointID, &row.ConnectionID, &row.EndpointLabelSnapshot, &row.CurrencyAttribution); err != nil {
		t.Fatalf("load latest runtime usage endpoint label snapshot: %v", err)
	}
	return row
}

type runtimePersistedStreamTelemetryRow struct {
	StreamOutcome               string
	StreamErrorKind             sql.NullString
	StreamErrorDetail           sql.NullString
	InputTokens                 sql.NullInt64
	OutputTokens                sql.NullInt64
	TotalTokens                 sql.NullInt64
	TotalCostUserCurrencyMicros sql.NullInt64
	PricingStatus               sql.NullString
	UnpricedReason              sql.NullString
	CompletionDurationMS        sql.NullInt64
}

func loadLatestRuntimeRequestLogStreamTelemetryRow(t *testing.T, conn *pgx.Conn, profileID int) runtimePersistedStreamTelemetryRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var row runtimePersistedStreamTelemetryRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT stream_outcome, stream_error_kind, stream_error_detail, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, completion_duration_ms FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&row.StreamOutcome, &row.StreamErrorKind, &row.StreamErrorDetail, &row.InputTokens, &row.OutputTokens, &row.TotalTokens, &row.TotalCostUserCurrencyMicros, &row.PricingStatus, &row.UnpricedReason, &row.CompletionDurationMS); err != nil {
		t.Fatalf("load latest runtime request-log stream telemetry row: %v", err)
	}
	return row
}

func loadLatestRuntimeUsageEventStreamTelemetryRow(t *testing.T, conn *pgx.Conn, profileID int) runtimePersistedStreamTelemetryRow {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var row runtimePersistedStreamTelemetryRow
	if err := conn.QueryRow(
		context.Background(),
		`SELECT stream_outcome, stream_error_kind, input_tokens, output_tokens, total_tokens, total_cost_user_currency_micros, pricing_status, unpriced_reason, completion_duration_ms FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&row.StreamOutcome, &row.StreamErrorKind, &row.InputTokens, &row.OutputTokens, &row.TotalTokens, &row.TotalCostUserCurrencyMicros, &row.PricingStatus, &row.UnpricedReason, &row.CompletionDurationMS); err != nil {
		t.Fatalf("load latest runtime usage-event stream telemetry row: %v", err)
	}
	return row
}

type runtimePersistedUsageRow struct {
	InputTokens              sql.NullInt64
	OutputTokens             sql.NullInt64
	TotalTokens              sql.NullInt64
	CacheReadInputTokens     sql.NullInt64
	CacheCreationInputTokens sql.NullInt64
	ReasoningTokens          sql.NullInt64
}

func assertLatestRequestLogUsage(t *testing.T, conn *pgx.Conn, profileID int, expectStream bool, wantInput int64, wantOutput int64, wantTotal int64) {
	t.Helper()
	assertLatestRuntimeUsageRows(t, conn, profileID, expectStream, runtimePersistedUsageRow{
		InputTokens:  runtimeNullInt64(wantInput),
		OutputTokens: runtimeNullInt64(wantOutput),
		TotalTokens:  runtimeNullInt64(wantTotal),
	})
}

func assertLatestRuntimeUsageRows(t *testing.T, conn *pgx.Conn, profileID int, expectStream bool, want runtimePersistedUsageRow) {
	t.Helper()
	waitForRuntimeTelemetryCounts(t, conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)
	var isStream bool
	requestLogRow := runtimePersistedUsageRow{}
	if err := conn.QueryRow(
		context.Background(),
		`SELECT is_stream, input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&isStream, &requestLogRow.InputTokens, &requestLogRow.OutputTokens, &requestLogRow.TotalTokens, &requestLogRow.CacheReadInputTokens, &requestLogRow.CacheCreationInputTokens, &requestLogRow.ReasoningTokens); err != nil {
		t.Fatalf("load latest request_logs usage row: %v", err)
	}
	if isStream != expectStream {
		t.Fatalf("expected request_logs is_stream=%t, got %t", expectStream, isStream)
	}
	if requestLogRow != want {
		t.Fatalf("expected request_logs canonical usage row %+v, got %+v", want, requestLogRow)
	}
	usageEventRow := runtimePersistedUsageRow{}
	if err := conn.QueryRow(
		context.Background(),
		`SELECT input_tokens, output_tokens, total_tokens, cache_read_input_tokens, cache_creation_input_tokens, reasoning_tokens FROM usage_request_events WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&usageEventRow.InputTokens, &usageEventRow.OutputTokens, &usageEventRow.TotalTokens, &usageEventRow.CacheReadInputTokens, &usageEventRow.CacheCreationInputTokens, &usageEventRow.ReasoningTokens); err != nil {
		t.Fatalf("load latest usage_request_events usage row: %v", err)
	}
	if usageEventRow != want {
		t.Fatalf("expected usage_request_events canonical usage row %+v, got %+v", want, usageEventRow)
	}
}

func assertLatestRuntimeWinningRequestLogTiming(t *testing.T, conn *pgx.Conn, profileID int, expectTTFT bool) {
	t.Helper()
	ingressRequestID := loadLatestRuntimeIngressRequestID(t, conn, profileID)

	var ttftMs sql.NullInt64
	var completionDurationMs sql.NullInt64
	if err := conn.QueryRow(
		context.Background(),
		`SELECT ttft_ms, completion_duration_ms FROM request_logs WHERE profile_id = $1 AND ingress_request_id = $2 ORDER BY attempt_number DESC, id DESC LIMIT 1`,
		profileID,
		ingressRequestID,
	).Scan(&ttftMs, &completionDurationMs); err != nil {
		t.Fatalf("load runtime winning request-log timing: %v", err)
	}
	if expectTTFT {
		if !ttftMs.Valid || ttftMs.Int64 < 1 {
			t.Fatalf("expected streamed winning request log to persist positive ttft_ms, got %+v", ttftMs)
		}
		if !completionDurationMs.Valid || completionDurationMs.Int64 < ttftMs.Int64 {
			t.Fatalf("expected streamed winning request log to persist completion_duration_ms >= ttft_ms, got ttft=%+v completion=%+v", ttftMs, completionDurationMs)
		}
		return
	}
	if ttftMs.Valid {
		t.Fatalf("expected streamed winning request log without meaningful payload to keep ttft_ms NULL, got %+v", ttftMs)
	}
	if !completionDurationMs.Valid || completionDurationMs.Int64 < 1 {
		t.Fatalf("expected streamed winning request log to persist positive completion_duration_ms, got %+v", completionDurationMs)
	}
}
