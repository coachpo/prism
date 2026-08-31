package integrationtest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

// TestOutputDeliveryRateEvidenceUpgradePath proves the 000030 output-rate
// evidence migration is purely additive on a copy of a pre-000030 database
// that carries historical partitioned rows and a legacy v2-shaped outbox
// payload: no row is deleted or rewritten, historical rows read as unknown,
// and the legacy outbox item still materializes instead of being quarantined.
func TestOutputDeliveryRateEvidenceUpgradePath(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)

	// Stage a pre-000030 migration directory: every current migration except
	// the new 000030 file, so the staged database stops at 000029.
	pre030Dir := t.TempDir()
	entries, err := os.ReadDir(migrate.DefaultMigrationsDir())
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" || strings.HasPrefix(entry.Name(), "000030_") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(migrate.DefaultMigrationsDir(), entry.Name()))
		if err != nil {
			t.Fatalf("read pre-000030 migration %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(pre030Dir, entry.Name()), contents, 0o644); err != nil {
			t.Fatalf("stage pre-000030 migration %s: %v", entry.Name(), err)
		}
	}
	pre030Runner, err := migrate.New(migrate.Options{MigrationsDir: pre030Dir})
	if err != nil {
		t.Fatalf("build pre-000030 migration runner: %v", err)
	}

	conn := harness.openEmptyDatabase(t, testContext, "output_rate_evidence_upgrade")
	defer func() { _ = conn.Close(testContext) }()

	staged, err := pre030Runner.Run(testContext, conn)
	if err != nil || staged.Outcome != migrate.OutcomeApply {
		t.Fatalf("apply migrations through 000029: result=%+v err=%v", staged, err)
	}

	// Historical partitioned rows: one retained request_logs/usage_request_events
	// pair from before the evidence columns existed.
	historyAt := time.Date(2026, 1, 15, 8, 0, 0, 0, time.UTC)
	requestPartition := ensureDailyLogPartition(t, testContext, conn, "request_logs", historyAt, "rate-history")
	usagePartition := ensureDailyLogPartition(t, testContext, conn, "usage_request_events", historyAt, "rate-history")

	var profileID int
	if err := conn.QueryRow(testContext, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ('output-rate-history', NULL, FALSE, FALSE, TRUE, 1, NULL, $1, $1) RETURNING id`, historyAt).Scan(&profileID); err != nil {
		t.Fatalf("seed history profile: %v", err)
	}
	if _, err := conn.Exec(testContext, `
		INSERT INTO request_logs (profile_id, model_id, api_family, endpoint_id, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, upstream_status_code, attempt_duration_ms, is_stream, pricing_status, pricing_evidence_trust, request_path, created_at,
			input_tokens, output_tokens, total_tokens, ttft_ms, completion_duration_ms,
			input_cost_micros, output_cost_micros, reasoning_cost_micros,
			cache_read_input_cost_micros, cache_creation_input_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros)
		VALUES ($1, 'rate-history-model', 'openai', NULL, 'rate-history-ingress', 1, 'upstream', 'runtime_scrubbed', 200, 42000, TRUE, 'priced', 'trusted', '/v1/chat/completions', $2,
			10, 53, 63, 1, 2,
			100, 100, 0, 0, 0, 200, 200)`,
		profileID, historyAt); err != nil {
		t.Fatalf("seed historical request log: %v", err)
	}
	if _, err := conn.Exec(testContext, `
		INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, status_code, success_flag, pricing_status, pricing_evidence_trust, attempt_count, request_path, created_at, endpoint_label_snapshot,
			input_tokens, output_tokens, total_tokens, ttft_ms, completion_duration_ms,
			input_cost_micros, output_cost_micros, reasoning_cost_micros,
			cache_read_input_cost_micros, cache_creation_input_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros)
		VALUES ($1, 'rate-history-ingress', 'rate-history-model', 'openai', 200, TRUE, 'priced', 'trusted', 1, '/v1/chat/completions', $2, 'History Endpoint',
			10, 53, 63, 1, 2,
			100, 100, 0, 0, 0, 200, 200)`,
		profileID, historyAt); err != nil {
		t.Fatalf("seed historical usage event: %v", err)
	}

	// A legacy v2 outbox metadata item whose serialized envelope predates the
	// evidence fields. The drain itself is regression-backed in the runtime
	// suite (TestRuntimeTelemetryOutputRateEvidenceCompatibility); here we
	// prove the upgraded schema accepts a legacy payload row without any
	// evidence columns and without the CHECK constraints rejecting it.
	outboxAt := historyAt.Add(time.Hour)
	legacyEnvelope := map[string]any{
		"request_logs": []any{map[string]any{
			"profile_id":             profileID,
			"model_id":               "rate-legacy-model",
			"api_family":             "openai",
			"operation_name":         "openai.chat_completions",
			"ingress_request_id":     "rate-legacy-ingress",
			"attempt_number":         1,
			"status_code":            200,
			"response_time_ms":       120,
			"is_stream":              true,
			"success_flag":           true,
			"stream_outcome":         "completed",
			"row_kind":               "upstream",
			"url_scrub_provenance":   "runtime_scrubbed",
			"request_path":           "/v1/chat/completions",
			"created_at":             outboxAt.Format(time.RFC3339Nano),
			"pricing_status":         "priced",
			"pricing_evidence_trust": "trusted",
			"input_tokens":           7,
			"output_tokens":          20,
			"total_tokens":           27,
		}},
		"usage_event": map[string]any{
			"profile_id":             profileID,
			"model_id":               "rate-legacy-model",
			"api_family":             "openai",
			"operation_name":         "openai.chat_completions",
			"ingress_request_id":     "rate-legacy-ingress",
			"status_code":            200,
			"success_flag":           true,
			"stream_outcome":         "completed",
			"attempt_count":          1,
			"request_path":           "/v1/chat/completions",
			"created_at":             outboxAt.Format(time.RFC3339Nano),
			"response_time_ms":       120,
			"pricing_status":         "priced",
			"pricing_evidence_trust": "trusted",
			"input_tokens":           7,
			"output_tokens":          20,
			"total_tokens":           27,
			"currency_attribution":   "identified",
		},
	}
	legacyPayload, err := json.Marshal(legacyEnvelope)
	if err != nil {
		t.Fatalf("serialize legacy outbox payload: %v", err)
	}
	if _, err := conn.Exec(testContext, `
		INSERT INTO runtime_telemetry_outbox (profile_id, ingress_request_id, schema_version, lifecycle_state, payload, core_payload, created_at)
		VALUES ($1, 'rate-legacy-ingress', 2, 'finalized', '{}'::jsonb, $2, $3)`,
		profileID, legacyPayload, outboxAt); err != nil {
		t.Fatalf("seed legacy outbox row: %v", err)
	}

	// The pre-000030 database must not carry any evidence surface yet.
	for _, table := range []string{"request_logs", "usage_request_events"} {
		assertColumnsAbsent(t, testContext, conn, table, "output_rate_state", "output_rate_reason", "output_delivery_event_count", "output_delivery_span_ms")
	}

	upgrade, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000030 upgrade: %v", err)
	}
	if upgrade.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected upgrade run to apply 000030, got %q", upgrade.Outcome)
	}
	assertHistoryVersions(t, testContext, conn, expectedMigrationVersionsFrom(t, migrate.DefaultBaselineVersion))

	// The historical rows still hold every pre-existing fact (no rewrite) and
	// read as unknown through the evidence columns.
	var inputTokens, outputTokens, ttftMS, completionMS int
	var rateState *string
	var rateReason *string
	var eventCount *int
	var spanMS *int
	if err := conn.QueryRow(testContext, `
		SELECT input_tokens, output_tokens, ttft_ms, completion_duration_ms, output_rate_state, output_rate_reason, output_delivery_event_count, output_delivery_span_ms
		FROM `+quoteIdentifier(requestPartition)+`
		WHERE ingress_request_id = 'rate-history-ingress'`).Scan(&inputTokens, &outputTokens, &ttftMS, &completionMS, &rateState, &rateReason, &eventCount, &spanMS); err != nil {
		t.Fatalf("load historical request log after upgrade: %v", err)
	}
	if inputTokens != 10 || outputTokens != 53 || ttftMS != 1 || completionMS != 2 {
		t.Fatalf("historical request log facts were rewritten: input=%d output=%d ttft=%d completion=%d", inputTokens, outputTokens, ttftMS, completionMS)
	}
	if rateState != nil || rateReason != nil || eventCount != nil || spanMS != nil {
		t.Fatalf("historical request log evidence columns are not NULL: %+v %+v %+v %+v", rateState, rateReason, eventCount, spanMS)
	}
	if err := conn.QueryRow(testContext, `
		SELECT output_tokens, ttft_ms, completion_duration_ms, output_rate_state, output_rate_reason, output_delivery_event_count, output_delivery_span_ms
		FROM `+quoteIdentifier(usagePartition)+`
		WHERE ingress_request_id = 'rate-history-ingress'`).Scan(&outputTokens, &ttftMS, &completionMS, &rateState, &rateReason, &eventCount, &spanMS); err != nil {
		t.Fatalf("load historical usage event after upgrade: %v", err)
	}
	if outputTokens != 53 || ttftMS != 1 || completionMS != 2 {
		t.Fatalf("historical usage event facts were rewritten: output=%d ttft=%d completion=%d", outputTokens, ttftMS, completionMS)
	}
	if rateState != nil || rateReason != nil || eventCount != nil || spanMS != nil {
		t.Fatalf("historical usage event evidence columns are not NULL: %+v %+v %+v %+v", rateState, rateReason, eventCount, spanMS)
	}

	// The evidence CHECK constraints reject malformed states on both tables.
	assertOutputRateEvidenceConstraints(t, testContext, conn)
}

// assertOutputRateEvidenceConstraints proves the 000030 CHECK constraints
// guard the four-state domain on both evidence-bearing tables. Rows use the
// seeded history partition date so the daily partition already exists.
func assertOutputRateEvidenceConstraints(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `SELECT id FROM profiles ORDER BY id DESC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load profile for constraint checks: %v", err)
	}
	constraintAt := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	for _, table := range []struct {
		name             string
		reasonConstraint string
		factsConstraint  string
	}{
		{
			name:             "request_logs",
			reasonConstraint: "ck_request_logs_output_rate_reason",
			factsConstraint:  "ck_request_logs_output_rate_delivery_facts",
		},
		{
			name:             "usage_request_events",
			reasonConstraint: "ck_usage_request_events_output_rate_reason",
			factsConstraint:  "ck_usage_request_events_output_rate_delivery_facts",
		},
	} {
		_, err := conn.Exec(ctx, `UPDATE `+quoteIdentifier(table.name)+`
			SET output_rate_reason = 'partial-evidence'
			WHERE ingress_request_id = 'rate-history-ingress'`)
		if err == nil || !strings.Contains(err.Error(), table.reasonConstraint) {
			t.Fatalf("expected %s to reject a NULL state with a non-NULL reason, got %v", table.reasonConstraint, err)
		}
		_, err = conn.Exec(ctx, `UPDATE `+quoteIdentifier(table.name)+`
			SET output_delivery_event_count = 2
			WHERE ingress_request_id = 'rate-history-ingress'`)
		if err == nil || !strings.Contains(err.Error(), table.factsConstraint) {
			t.Fatalf("expected %s to reject a NULL state with non-NULL delivery facts, got %v", table.factsConstraint, err)
		}
	}
	insertUsageEventEvidence := func(ingress string, state any, reason any, outputTokens any, eventCount any, spanMS any) error {
		_, err := conn.Exec(ctx, `
			INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, status_code, success_flag, pricing_status, pricing_evidence_trust, attempt_count, request_path, created_at, endpoint_label_snapshot,
				output_tokens, output_rate_state, output_rate_reason, output_delivery_event_count, output_delivery_span_ms,
				input_cost_micros, output_cost_micros, reasoning_cost_micros,
				cache_read_input_cost_micros, cache_creation_input_cost_micros,
				total_cost_original_micros, total_cost_user_currency_micros)
			VALUES ($1, $2, 'rate-constraint-model', 'openai', 200, TRUE, 'priced', 'trusted', 1, '/v1/chat/completions', $3, 'Constraint Endpoint',
				$4, $5, $6, $7, $8,
				100, 100, 0, 0, 0, 200, 200)`,
			profileID, ingress, constraintAt, outputTokens, state, reason, eventCount, spanMS)
		return err
	}
	if err := insertUsageEventEvidence("rate-constraint-measured-zero", "measured", nil, 0, 2, 1); err != nil {
		t.Fatalf("insert valid measured state: %v", err)
	}
	if err := insertUsageEventEvidence("rate-constraint-unmeasurable", "unmeasurable", "unmeasurable_single_output_event", 1, 1, nil); err != nil {
		t.Fatalf("insert valid unmeasurable state: %v", err)
	}
	if err := insertUsageEventEvidence("rate-constraint-unknown", "unknown", "unknown_missing_evidence", nil, nil, nil); err != nil {
		t.Fatalf("insert valid unknown state: %v", err)
	}
	for index, test := range []struct {
		name         string
		outputTokens any
		eventCount   any
		spanMS       any
		reason       any
	}{
		{name: "missing output tokens", eventCount: 2, spanMS: 50},
		{name: "negative output tokens", outputTokens: -1, eventCount: 2, spanMS: 50},
		{name: "missing event count", outputTokens: 1, spanMS: 50},
		{name: "single event", outputTokens: 1, eventCount: 1, spanMS: 50},
		{name: "missing span", outputTokens: 1, eventCount: 2},
		{name: "zero span", outputTokens: 1, eventCount: 2, spanMS: 0},
		{name: "measured reason", outputTokens: 1, eventCount: 2, spanMS: 50, reason: "unexpected_reason"},
	} {
		t.Run("usage measured "+test.name, func(t *testing.T) {
			err := insertUsageEventEvidence(fmt.Sprintf("rate-invalid-%d", index), "measured", test.reason, test.outputTokens, test.eventCount, test.spanMS)
			if err == nil || (!strings.Contains(err.Error(), "ck_usage_request_events_output_rate_delivery_facts") && !strings.Contains(err.Error(), "ck_usage_request_events_output_rate_reason")) {
				t.Fatalf("expected malformed measured evidence to be rejected, got %v", err)
			}
		})
	}
	// A fabricated state without a reason is rejected by the guardrail
	// constraints (the reason CHECK may fire first — PostgreSQL constraint
	// order is unspecified).
	if err := insertUsageEventEvidence("rate-constraint-fabricated", "fabricated", nil, nil, nil, nil); err == nil ||
		(!strings.Contains(err.Error(), "ck_usage_request_events_output_rate_state") &&
			!strings.Contains(err.Error(), "ck_usage_request_events_output_rate_reason") &&
			!strings.Contains(err.Error(), "ck_usage_request_events_output_rate_delivery_facts")) {
		t.Fatalf("expected the output-rate evidence CHECKs to reject 'fabricated', got %v", err)
	}
	// With a reason present, the state-domain CHECK is the one that fires.
	_, reasonErr := conn.Exec(ctx, `
		INSERT INTO usage_request_events (profile_id, ingress_request_id, model_id, api_family, status_code, success_flag, pricing_status, pricing_evidence_trust, attempt_count, request_path, created_at, endpoint_label_snapshot, output_rate_state, output_rate_reason,
			input_cost_micros, output_cost_micros, reasoning_cost_micros,
			cache_read_input_cost_micros, cache_creation_input_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros)
		VALUES ($1, 'rate-constraint-state', 'rate-constraint-model', 'openai', 200, TRUE, 'priced', 'trusted', 1, '/v1/chat/completions', $2, 'Constraint Endpoint', 'fabricated', 'some_reason',
			100, 100, 0, 0, 0, 200, 200)`,
		profileID, constraintAt)
	if reasonErr == nil || (!strings.Contains(reasonErr.Error(), "ck_usage_request_events_output_rate_state") &&
		!strings.Contains(reasonErr.Error(), "ck_usage_request_events_output_rate_delivery_facts")) {
		t.Fatalf("expected output_rate_state CHECK to reject 'fabricated' even with a reason, got %v", reasonErr)
	}
	_, err := conn.Exec(ctx, `
		INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, is_stream, pricing_status, pricing_evidence_trust, request_path, created_at, output_rate_state,
			input_cost_micros, output_cost_micros, reasoning_cost_micros,
			cache_read_input_cost_micros, cache_creation_input_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros)
		VALUES ($1, 'rate-constraint-model', 'openai', 'rate-constraint-request', 1, 'upstream', 'runtime_scrubbed', TRUE, 'priced', 'trusted', '/v1/chat/completions', $2, 'unmeasurable',
			100, 100, 0, 0, 0, 200, 200)`,
		profileID, constraintAt)
	if err == nil || !strings.Contains(err.Error(), "ck_request_logs_output_rate_reason") {
		t.Fatalf("expected output_rate_reason CHECK to require a reason for unmeasurable state, got %v", err)
	}
	_, measuredErr := conn.Exec(ctx, `
		INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, attempt_number, row_kind, url_scrub_provenance, is_stream, pricing_status, pricing_evidence_trust, request_path, created_at, output_rate_state,
			input_cost_micros, output_cost_micros, reasoning_cost_micros,
			cache_read_input_cost_micros, cache_creation_input_cost_micros,
			total_cost_original_micros, total_cost_user_currency_micros)
		VALUES ($1, 'rate-constraint-model', 'openai', 'rate-constraint-request-measured', 1, 'upstream', 'runtime_scrubbed', TRUE, 'priced', 'trusted', '/v1/chat/completions', $2, 'measured',
			100, 100, 0, 0, 0, 200, 200)`,
		profileID, constraintAt)
	if measuredErr == nil || !strings.Contains(measuredErr.Error(), "ck_request_logs_output_rate_delivery_facts") {
		t.Fatalf("expected request-log measured evidence without output facts to be rejected, got %v", measuredErr)
	}
}
