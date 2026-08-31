package runtimetest

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	runtimeapi "github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

// outputRateEvidenceSSEUpstream serves a scripted SSE body with a delay before
// each chunk, giving the output-evidence capture controllable first-to-last
// visible-output spans.
type outputRateEvidenceSSEUpstream struct {
	server     *httptest.Server
	chunks     []outputRateEvidenceChunk
	statusCode int
	mu         sync.Mutex
}

type outputRateEvidenceChunk struct {
	body        string
	delayBefore time.Duration
}

func newOutputRateEvidenceSSEUpstream(t *testing.T, chunks []outputRateEvidenceChunk) *outputRateEvidenceSSEUpstream {
	return newOutputRateEvidenceSSEUpstreamWithStatus(t, http.StatusOK, chunks)
}

func newOutputRateEvidenceSSEUpstreamWithStatus(t *testing.T, statusCode int, chunks []outputRateEvidenceChunk) *outputRateEvidenceSSEUpstream {
	t.Helper()
	upstream := &outputRateEvidenceSSEUpstream{chunks: chunks, statusCode: statusCode}
	upstream.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("output-rate evidence upstream writer does not support flushing")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(statusCode)
		for _, chunk := range chunks {
			if chunk.delayBefore > 0 {
				time.Sleep(chunk.delayBefore)
			}
			_, _ = io.WriteString(w, chunk.body)
			flusher.Flush()
		}
	}))
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *outputRateEvidenceSSEUpstream) baseURL(path string) string {
	return strings.TrimRight(u.server.URL, "/") + path
}

func outputRateEvidenceRoute(t *testing.T, harness *runtimeHarness, profileID int, upstreamPath string, upstream *outputRateEvidenceSSEUpstream) seededRuntimeRoute {
	t.Helper()
	return harness.seedProxyRoute(t, runtimeRouteSeed{
		ProfileID:       profileID,
		APIFamily:       "openai",
		PublicModelID:   "rate-evidence-public-" + randomSuffix(),
		TargetModelID:   "rate-evidence-target-" + randomSuffix(),
		EndpointBaseURL: upstream.baseURL(upstreamPath),
		EndpointAPIKey:  "rate-evidence-key",
	})
}

const outputRateEvidenceChatUsageChunk = "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":4,\"total_tokens\":9}}\n\n"

// TestRuntimeTelemetryOutputRateEvidenceCompatibility proves the output-rate
// evidence drain contract across payload generations:
//
//  1. A current streaming producer writes the same verdict and delivery facts
//     to request_logs and usage_request_events.
//  2. A legacy v2 payload whose serialized envelope predates the evidence
//     fields still materializes as conservative unknown evidence on both
//     tables and is never quarantined, exactly like the currency-attribution
//     precedent.
func TestRuntimeTelemetryOutputRateEvidenceCompatibility(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	t.Run("current producer writes consistent evidence", func(t *testing.T) {
		harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{
			RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
				PollInterval: 25 * time.Millisecond, ShutdownTimeout: 150 * time.Millisecond,
			}},
		})
		profileID := harness.activeProfileID(t)

		// GLM-style buffered burst: a long pre-delivery stall, then the whole
		// visible delivery inside one flush (sub-millisecond span). The writer
		// must classify it unmeasurable while keeping the request facts.
		burstUpstream := newOutputRateEvidenceSSEUpstream(t, []outputRateEvidenceChunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n", delayBefore: 40 * time.Millisecond},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\n"},
			{body: outputRateEvidenceChatUsageChunk},
			{body: "data: [DONE]\n\n"},
		})
		burstRoute := outputRateEvidenceRoute(t, harness, profileID, "/telemetry/rate-burst", burstUpstream)
		response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model":    burstRoute.PublicModelID,
			"stream":   true,
			"messages": []map[string]any{{"role": "user", "content": "buffered burst should not become a rate sample"}},
		}, nil)
		assertStatus(t, response, http.StatusOK)
		waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 5*time.Second)

		assertOutputRateEvidenceRow(t, testContext, harness.conn, profileID, burstRoute.TargetModelID, outputRateEvidenceExpectation{
			state:            "unmeasurable",
			reason:           "unmeasurable_output_span_below_threshold",
			events:           2,
			maxSpanExclusive: 50,
		})

		// A progressive stream: two visible chunks 60ms apart clear the 50ms
		// floor and produce an authoritative measured rate.
		progressiveUpstream := newOutputRateEvidenceSSEUpstream(t, []outputRateEvidenceChunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n", delayBefore: 60 * time.Millisecond},
			{body: outputRateEvidenceChatUsageChunk},
			{body: "data: [DONE]\n\n"},
		})
		progressiveRoute := outputRateEvidenceRoute(t, harness, profileID, "/telemetry/rate-progressive", progressiveUpstream)
		response = harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model":    progressiveRoute.PublicModelID,
			"stream":   true,
			"messages": []map[string]any{{"role": "user", "content": "progressive delivery is measurable"}},
		}, nil)
		assertStatus(t, response, http.StatusOK)
		waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 2, UsageEvents: 2, OutboxRows: 0}, 5*time.Second)

		assertOutputRateEvidenceRow(t, testContext, harness.conn, profileID, progressiveRoute.TargetModelID, outputRateEvidenceExpectation{
			state:     "measured",
			reason:    "",
			events:    2,
			minSpanMS: 50,
		})

		// A non-success SSE can be syntactically complete and carry valid usage,
		// but it is not a successful generation sample.
		nonSuccessUpstream := newOutputRateEvidenceSSEUpstreamWithStatus(t, http.StatusTeapot, []outputRateEvidenceChunk{
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"},
			{body: "data: {\"choices\":[{\"delta\":{\"content\":\"b\"}}]}\n\n", delayBefore: 60 * time.Millisecond},
			{body: outputRateEvidenceChatUsageChunk},
			{body: "data: [DONE]\n\n"},
		})
		nonSuccessRoute := outputRateEvidenceRoute(t, harness, profileID, "/telemetry/rate-non-success", nonSuccessUpstream)
		response = harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
			"model":    nonSuccessRoute.PublicModelID,
			"stream":   true,
			"messages": []map[string]any{{"role": "user", "content": "non-success stream is not a rate sample"}},
		}, nil)
		assertStatus(t, response, http.StatusTeapot)
		waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{RequestLogs: 3, UsageEvents: 3, OutboxRows: 0}, 5*time.Second)

		assertOutputRateEvidenceRow(t, testContext, harness.conn, profileID, nonSuccessRoute.TargetModelID, outputRateEvidenceExpectation{
			state:     "unmeasurable",
			reason:    "unmeasurable_non_success_status",
			events:    2,
			minSpanMS: 50,
		})

		// The authoritative window aggregate counts exactly one measured
		// sample: the burst stays out of the mean.
		var sampleCount, unmeasurableCount int
		var avgRate *float64
		if err := harness.conn.QueryRow(testContext, `
			SELECT COUNT(output_rate_tps)::int,
			       COUNT(*) FILTER (WHERE output_rate_state = 'unmeasurable')::int,
			       AVG(output_rate_tps)
			FROM (
				SELECT CASE WHEN output_rate_state = 'measured' AND output_tokens IS NOT NULL AND output_delivery_span_ms IS NOT NULL AND output_delivery_span_ms > 0
					THEN output_tokens * 1000.0 / output_delivery_span_ms END AS output_rate_tps,
					output_rate_state
				FROM usage_request_events WHERE profile_id = $1
			) rated`, profileID).Scan(&sampleCount, &unmeasurableCount, &avgRate); err != nil {
			t.Fatalf("load observe rate aggregate: %v", err)
		}
		if sampleCount != 1 || unmeasurableCount != 2 || avgRate == nil {
			t.Fatalf("expected exactly one measured sample and two unmeasurable, got samples=%d unmeasurable=%d avg=%v", sampleCount, unmeasurableCount, avgRate)
		}
	})

	t.Run("legacy v2 payload materializes as unknown without quarantine", func(t *testing.T) {
		harness, profileID := enqueueBlockedOutputRatePayload(t)
		// Strip every evidence field from the serialized envelope, simulating a
		// payload written by the pre-000030 producer.
		for _, path := range []string{
			`{envelope,usage_event,OutputRateState}`,
			`{envelope,usage_event,OutputRateReason}`,
			`{envelope,usage_event,OutputDeliveryEventCount}`,
			`{envelope,usage_event,OutputDeliverySpanMS}`,
			`{envelope,request_logs,0,OutputRateState}`,
			`{envelope,request_logs,0,OutputRateReason}`,
			`{envelope,request_logs,0,OutputDeliveryEventCount}`,
			`{envelope,request_logs,0,OutputDeliverySpanMS}`,
		} {
			if _, err := harness.conn.Exec(testContext, `UPDATE runtime_telemetry_outbox SET core_payload = core_payload #- $1 WHERE profile_id = $2`, path, profileID); err != nil {
				t.Fatalf("strip evidence field %s: %v", path, err)
			}
		}

		restarted := restartRuntimeHarnessWithConfig(t, harness.databaseName, runtimeHarnessConfig{
			RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
				PollInterval: 25 * time.Millisecond, ShutdownTimeout: 150 * time.Millisecond,
			}},
		})
		waitForRuntimeTelemetryCounts(t, restarted.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 10*time.Second)

		var quarantined int
		if err := restarted.conn.QueryRow(testContext, `SELECT COUNT(*) FROM runtime_telemetry_quarantine WHERE profile_id = $1`, profileID).Scan(&quarantined); err != nil {
			t.Fatalf("count quarantined rows: %v", err)
		}
		if quarantined != 0 {
			t.Fatalf("expected legacy evidence-less payload to materialize, not quarantine; got %d quarantined rows", quarantined)
		}

		var usageState, usageReason string
		if err := restarted.conn.QueryRow(testContext, `
			SELECT output_rate_state, output_rate_reason FROM usage_request_events WHERE profile_id = $1`, profileID).
			Scan(&usageState, &usageReason); err != nil {
			t.Fatalf("load drained legacy usage event: %v", err)
		}
		if usageState != "unknown" || usageReason != "unknown_missing_evidence" {
			t.Fatalf("expected drained legacy usage event evidence unknown/unknown_missing_evidence, got %q/%q", usageState, usageReason)
		}
		var requestState *string
		if err := restarted.conn.QueryRow(testContext, `
			SELECT output_rate_state FROM request_logs WHERE profile_id = $1 AND row_kind = 'upstream'`, profileID).Scan(&requestState); err != nil {
			t.Fatalf("load drained legacy request log: %v", err)
		}
		if requestState == nil || *requestState != "unknown" {
			t.Fatalf("expected drained legacy final request log evidence unknown, got %v", requestState)
		}
	})

	t.Run("asymmetric v2 evidence degrades both tables without quarantine", func(t *testing.T) {
		harness, profileID := enqueueBlockedOutputRatePayload(t)
		for _, path := range []string{
			`{envelope,request_logs,0,OutputRateState}`,
			`{envelope,request_logs,0,OutputRateReason}`,
			`{envelope,request_logs,0,OutputDeliveryEventCount}`,
			`{envelope,request_logs,0,OutputDeliverySpanMS}`,
		} {
			if _, err := harness.conn.Exec(testContext, `UPDATE runtime_telemetry_outbox SET core_payload = core_payload #- $1 WHERE profile_id = $2`, path, profileID); err != nil {
				t.Fatalf("strip asymmetric evidence field %s: %v", path, err)
			}
		}

		restarted := restartRuntimeHarnessWithConfig(t, harness.databaseName, runtimeHarnessConfig{
			RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
				PollInterval: 25 * time.Millisecond, ShutdownTimeout: 150 * time.Millisecond,
			}},
		})
		waitForRuntimeTelemetryCounts(t, restarted.conn, profileID, runtimeTelemetryCounts{RequestLogs: 1, UsageEvents: 1, OutboxRows: 0}, 10*time.Second)

		var quarantined int
		if err := restarted.conn.QueryRow(testContext, `SELECT COUNT(*) FROM runtime_telemetry_quarantine WHERE profile_id = $1`, profileID).Scan(&quarantined); err != nil {
			t.Fatalf("count asymmetric quarantined rows: %v", err)
		}
		if quarantined != 0 {
			t.Fatalf("expected asymmetric evidence to degrade, not quarantine; got %d rows", quarantined)
		}
		var requestState, requestReason, usageState, usageReason string
		if err := restarted.conn.QueryRow(testContext, `
			SELECT rl.output_rate_state, rl.output_rate_reason, ue.output_rate_state, ue.output_rate_reason
			FROM request_logs rl JOIN usage_request_events ue
			  ON ue.profile_id = rl.profile_id AND ue.ingress_request_id = rl.ingress_request_id
			WHERE rl.profile_id = $1 AND rl.row_kind = 'upstream'`, profileID).
			Scan(&requestState, &requestReason, &usageState, &usageReason); err != nil {
			t.Fatalf("load asymmetric normalized evidence: %v", err)
		}
		if requestState != "unknown" || usageState != "unknown" || requestReason != "unknown_inconsistent_evidence" || usageReason != requestReason {
			t.Fatalf("expected symmetric unknown/inconsistent evidence, got request=%q/%q usage=%q/%q", requestState, requestReason, usageState, usageReason)
		}
	})
}

type outputRateEvidenceExpectation struct {
	state            string
	reason           string
	events           int
	minSpanMS        int
	maxSpanExclusive int
}

// assertOutputRateEvidenceRow verifies the request_logs final row and the
// usage event carry the same verdict and delivery facts for one model's
// single-attempt ingress, and that the request facts survive alongside.
func assertOutputRateEvidenceRow(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, targetModelID string, expected outputRateEvidenceExpectation) {
	t.Helper()
	var usageState string
	var usageReason *string
	var usageCount, usageSpan *int
	if err := conn.QueryRow(ctx, `
		SELECT output_rate_state, output_rate_reason, output_delivery_event_count, output_delivery_span_ms
		FROM usage_request_events WHERE profile_id = $1 AND resolved_target_model_id = $2`, profileID, targetModelID).
		Scan(&usageState, &usageReason, &usageCount, &usageSpan); err != nil {
		t.Fatalf("load usage event evidence: %v", err)
	}
	if usageState != expected.state {
		t.Fatalf("expected usage event state %q, got %q", expected.state, usageState)
	}
	if expected.reason == "" {
		if usageReason != nil {
			t.Fatalf("expected no reason for state %q, got %q", expected.state, *usageReason)
		}
	} else if usageReason == nil || *usageReason != expected.reason {
		t.Fatalf("expected usage event reason %q, got %v", expected.reason, usageReason)
	}
	if usageCount == nil || *usageCount != expected.events {
		t.Fatalf("expected %d delivery events on the usage event, got %v", expected.events, usageCount)
	}

	var requestState string
	var requestReason *string
	var requestCount, requestSpan *int
	var requestOutputTokens *int
	if err := conn.QueryRow(ctx, `
		SELECT output_rate_state, output_rate_reason, output_delivery_event_count, output_delivery_span_ms, output_tokens
		FROM request_logs WHERE profile_id = $1 AND resolved_target_model_id = $2 AND row_kind = 'upstream'`, profileID, targetModelID).
		Scan(&requestState, &requestReason, &requestCount, &requestSpan, &requestOutputTokens); err != nil {
		t.Fatalf("load request log evidence: %v", err)
	}
	if requestState != usageState {
		t.Fatalf("expected identical evidence state on both tables, got request %q usage %q", requestState, usageState)
	}
	if (requestReason == nil) != (usageReason == nil) || (requestReason != nil && usageReason != nil && *requestReason != *usageReason) {
		t.Fatalf("expected identical evidence reasons on both tables, got request %v usage %v", requestReason, usageReason)
	}
	if requestCount == nil || usageCount == nil || *requestCount != *usageCount {
		t.Fatalf("expected identical delivery event counts on both tables, got request %v usage %v", requestCount, usageCount)
	}
	if (requestSpan == nil) != (usageSpan == nil) || (requestSpan != nil && usageSpan != nil && *requestSpan != *usageSpan) {
		t.Fatalf("expected identical delivery spans on both tables, got request %v usage %v", requestSpan, usageSpan)
	}
	if requestOutputTokens == nil || *requestOutputTokens != 4 {
		t.Fatalf("expected output token fact preserved on the final row, got %v", requestOutputTokens)
	}
	if expected.minSpanMS > 0 && (usageSpan == nil || *usageSpan < expected.minSpanMS) {
		t.Fatalf("expected a span at or above %dms, got %v", expected.minSpanMS, usageSpan)
	}
	if expected.maxSpanExclusive > 0 && (usageSpan == nil || *usageSpan >= expected.maxSpanExclusive) {
		t.Fatalf("expected a span below %dms, got %v", expected.maxSpanExclusive, usageSpan)
	}
}

// enqueueBlockedOutputRatePayload streams one successful chat completion
// through the proxy while the materialize gate blocks the outbox, then stops
// the service so the test can mutate the queued payload.
func enqueueBlockedOutputRatePayload(t *testing.T) (*runtimeHarness, int) {
	t.Helper()
	gate := newRuntimeTelemetryMaterializeGate()
	harness := newRuntimeHarnessWithConfig(t, runtimeHarnessConfig{RuntimeOptions: runtimeapi.Options{TelemetryOutbox: runtimeapi.TelemetryOutboxOptions{
		PollInterval: 25 * time.Millisecond, ShutdownTimeout: 150 * time.Millisecond,
		Hooks: &runtimeapi.TelemetryOutboxHooks{BeforeMaterialize: gate.Wait},
	}}})
	profileID := harness.activeProfileID(t)
	upstream := newOutputRateEvidenceSSEUpstream(t, []outputRateEvidenceChunk{
		{body: "data: {\"choices\":[{\"delta\":{\"content\":\"legacy\"}}]}\n\n"},
		{body: outputRateEvidenceChatUsageChunk},
		{body: "data: [DONE]\n\n"},
	})
	route := outputRateEvidenceRoute(t, harness, profileID, "/telemetry/rate-legacy", upstream)

	response := harness.requestJSON(t, http.MethodPost, "/v1/chat/completions", map[string]any{
		"model":    route.PublicModelID,
		"stream":   true,
		"messages": []map[string]any{{"role": "user", "content": "legacy payload without evidence fields"}},
	}, nil)
	assertStatus(t, response, http.StatusOK)
	waitForRuntimeTelemetryCounts(t, harness.conn, profileID, runtimeTelemetryCounts{OutboxRows: 1}, 5*time.Second)
	harness.runtimeService.Close()
	return harness, profileID
}
