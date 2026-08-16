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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
	"github.com/coachpo/prism/backend/internal/platform/startup"
)

// TestObservabilityV2UpgradeDrainsV1Outbox verifies the exclusive offline v1
// drain: finalized v1 envelopes are scrubbed/capped/split into v2 rows, raw
// unsafe artifacts are wiped with telemetry_orphaned tombstones, and the
// upgrade state advances to v1_drained.
func TestObservabilityV2UpgradeDrainsV1Outbox(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "v2_upgrade_drain"
	databaseURL, pool := upgradeStateDatabase(t, testContext, harness, databaseName)

	profileID := loadUpgradeDefaultProfileID(t, testContext, pool)

	// Simulate a v1 outbox row (schema_version=1) plus a stream_accepted row.
	now := time.Now().UTC()
	ensureUpgradeLogPartitions(t, testContext, pool, "request_logs", now)
	ensureUpgradeLogPartitions(t, testContext, pool, "usage_request_events", now)
	ensureUpgradeLogPartitions(t, testContext, pool, "audit_logs", now)

	v1Envelope := map[string]any{
		"request_logs": []map[string]any{{
			"profile_id":          profileID,
			"model_id":            "v1-model",
			"api_family":          "openai",
			"operation_name":      "openai.chat_completions",
			"ingress_request_id":  "v1-ingress-finalized",
			"attempt_number":      1,
			"status_code":         200,
			"response_time_ms":    120,
			"is_stream":           false,
			"success_flag":        true,
			"request_path":        "/v1/chat/completions?key=sk-secret-query",
			"created_at":          now,
			"caller_user_agent":   "legacy-agent Bearer sk-legacy-token",
			"upstream_user_agent": "upstream-agent",
		}},
		"usage_event": map[string]any{
			"profile_id":              profileID,
			"ingress_request_id":      "v1-ingress-finalized",
			"model_id":                "v1-model",
			"api_family":              "openai",
			"status_code":             200,
			"success_flag":            true,
			"attempt_count":           1,
			"request_path":            "/v1/chat/completions",
			"created_at":              now,
			"endpoint_label_snapshot": "V1 Endpoint",
			"stream_outcome":          "not_streaming",
		},
		"audit_logs": []map[string]any{{
			"request_log_attempt_number":      1,
			"profile_id":                      profileID,
			"model_id":                        "v1-model",
			"endpoint_id":                     0,
			"connection_id":                   0,
			"request_method":                  "POST",
			"request_url":                     "https://user:pass@v1.invalid/v1/chat/completions?api_key=sk-123",
			"request_headers":                 `{"authorization": "Bearer sk-secret", "content-type": "application/json"}`,
			"request_body":                    `{"messages":[{"role":"user","content":"hi"}]}`,
			"response_status":                 200,
			"response_headers":                `{"x-request-id": "req-1"}`,
			"response_body":                   `{"ok": true}`,
			"is_stream":                       false,
			"duration_ms":                     120,
			"created_at":                      now,
			"audit_enabled_at_request":        true,
			"audit_capture_bodies_at_request": true,
		}},
	}
	rawEnvelope, err := json.Marshal(v1Envelope)
	if err != nil {
		t.Fatalf("marshal v1 envelope: %v", err)
	}
	if _, err := pool.Exec(testContext, `INSERT INTO runtime_telemetry_outbox (profile_id, ingress_request_id, payload, schema_version, created_at) VALUES ($1, 'v1-ingress-finalized', $2, 1, $3)`, profileID, rawEnvelope, now); err != nil {
		t.Fatalf("insert v1 finalized outbox row: %v", err)
	}

	v1Accepted := map[string]any{
		"request_logs": []map[string]any{},
		"usage_event": map[string]any{
			"profile_id": profileID, "ingress_request_id": "v1-ingress-accepted", "model_id": "v1-model",
			"api_family": "openai", "status_code": 200, "success_flag": true, "attempt_count": 1,
			"request_path": "/v1/chat/completions", "created_at": now,
		},
		"handoff_phase": "stream_accepted",
	}
	rawAccepted, err := json.Marshal(v1Accepted)
	if err != nil {
		t.Fatalf("marshal v1 accepted envelope: %v", err)
	}
	if _, err := pool.Exec(testContext, `INSERT INTO runtime_telemetry_outbox (profile_id, ingress_request_id, payload, schema_version, created_at) VALUES ($1, 'v1-ingress-accepted', $2, 1, $3)`, profileID, rawAccepted, now); err != nil {
		t.Fatalf("insert v1 accepted outbox row: %v", err)
	}

	// Set upgrade state to draining_v1 (simulating a legacy upgrade).
	if _, err := pool.Exec(testContext, `UPDATE observability_v2_upgrade_state SET state = 'draining_v1', writer_fence_active = true WHERE id = 1`); err != nil {
		t.Fatalf("set upgrade state to draining_v1: %v", err)
	}

	// Rerun the startup sequence to trigger the drain owner.
	runUpgradeStartup(t, testContext, databaseURL)

	// v1 outbox must be empty; finalized row materialized; accepted row tombstoned.
	var remaining int
	if err := pool.QueryRow(testContext, `SELECT COUNT(*) FROM runtime_telemetry_outbox WHERE schema_version = 1 AND lifecycle_state <> 'telemetry_orphaned'`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining v1 rows: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("expected zero remaining v1 finalized rows, got %d", remaining)
	}
	var orphaned int
	if err := pool.QueryRow(testContext, `SELECT COUNT(*) FROM runtime_telemetry_outbox WHERE lifecycle_state = 'telemetry_orphaned'`).Scan(&orphaned); err != nil {
		t.Fatalf("count orphaned rows: %v", err)
	}
	if orphaned != 1 {
		t.Fatalf("expected one telemetry_orphaned tombstone, got %d", orphaned)
	}

	var requestLogID int64
	err = pool.QueryRow(testContext, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1 AND ingress_request_id = 'v1-ingress-finalized'`, profileID).Scan(&requestLogID)
	if err != nil {
		t.Fatalf("count drained request logs: %v", err)
	}
	if requestLogID != 1 {
		t.Fatalf("expected one drained request log, got %d", requestLogID)
	}

	var rowKind string
	var callerUA, requestPath, urlScrubProvenance string
	var metadataRedacted []string
	if err := pool.QueryRow(testContext, `SELECT row_kind, COALESCE(caller_user_agent,''), request_path, url_scrub_provenance, metadata_redacted_fields FROM request_logs WHERE profile_id = $1 AND ingress_request_id = 'v1-ingress-finalized'`, profileID).Scan(&rowKind, &callerUA, &requestPath, &urlScrubProvenance, &metadataRedacted); err != nil {
		t.Fatalf("load drained request log: %v", err)
	}
	if rowKind != "legacy_unknown" {
		t.Fatalf("expected legacy_unknown row kind, got %q", rowKind)
	}
	if strings.Contains(callerUA, "sk-legacy-token") {
		t.Fatalf("legacy caller UA credential leaked: %q", callerUA)
	}
	if strings.Contains(requestPath, "sk-secret-query") {
		t.Fatalf("legacy request path credential leaked: %q", requestPath)
	}
	if urlScrubProvenance != "legacy_rescrubbed" && urlScrubProvenance != "legacy_unknown" {
		t.Fatalf("expected legacy URL provenance, got %q", urlScrubProvenance)
	}

	// Audit row: headers all-values-redacted, body transcoded to BYTEA.
	var requestHeadersJSON, requestScrubProvenance string
	var requestBody []byte
	if err := pool.QueryRow(testContext, `SELECT COALESCE(request_headers::text,''), request_headers_scrub_provenance, request_body FROM audit_logs WHERE profile_id = $1 LIMIT 1`, profileID).Scan(&requestHeadersJSON, &requestScrubProvenance, &requestBody); err != nil {
		t.Fatalf("load drained audit log: %v", err)
	}
	if !strings.Contains(requestHeadersJSON, "[REDACTED-LEGACY]") {
		t.Fatalf("expected legacy all-values-redacted headers, got %q", requestHeadersJSON)
	}
	if strings.Contains(requestHeadersJSON, "sk-secret") {
		t.Fatalf("legacy header credential leaked: %q", requestHeadersJSON)
	}
	if requestScrubProvenance != "legacy_all_values_redacted" {
		t.Fatalf("expected legacy_all_values_redacted provenance, got %q", requestScrubProvenance)
	}
	if len(requestBody) == 0 {
		t.Fatal("expected transcoded BYTEA request body")
	}
}

// TestObservabilityV2BackfillDomainsReady verifies the three-domain backfill
// owner rewrites legacy URLs/headers/metadata, nulls raw shadows, and reaches
// backfill_ready so legacy read routes may activate.
func TestObservabilityV2BackfillDomainsReady(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "v2_upgrade_backfill"
	databaseURL, pool := upgradeStateDatabase(t, testContext, harness, databaseName)

	profileID := loadUpgradeDefaultProfileID(t, testContext, pool)
	now := time.Now().UTC()
	ensureUpgradeLogPartitions(t, testContext, pool, "request_logs", now)
	ensureUpgradeLogPartitions(t, testContext, pool, "audit_logs", now)

	// Seed legacy rows: a request log with raw-ish metadata + base URL, and an
	// audit log with raw header shadows and a query-bearing URL.
	if _, err := pool.Exec(testContext, `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, row_kind, url_scrub_provenance, request_path, is_stream, pricing_status, pricing_evidence_trust, created_at, caller_user_agent, upstream_user_agent, provider_correlation_id, legacy_status_code, legacy_duration_ms) VALUES ($1, 'legacy-model', 'openai', 'legacy-backfill-1', 'legacy_unknown', 'legacy_unknown', '/v1/chat/completions', FALSE, 'unknown', 'legacy_untrusted', $2, 'agent Bearer sk-token-1', 'upstream-agent', 'corr-abc', 200, 100)`,
		profileID, now); err != nil {
		t.Fatalf("seed legacy request log: %v", err)
	}
	if _, err := pool.Exec(testContext, `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, row_kind, url_scrub_provenance, request_path, endpoint_base_url, is_stream, pricing_status, pricing_evidence_trust, created_at, legacy_status_code, legacy_duration_ms) VALUES ($1, 'legacy-model', 'openai', 'legacy-backfill-2', 'legacy_unknown', 'legacy_unknown', '/v1/chat/completions', 'https://user:pass@legacy.invalid/v1?key=secret', FALSE, 'unknown', 'legacy_untrusted', $2, 200, 100)`,
		profileID, now.Add(time.Minute)); err != nil {
		t.Fatalf("seed legacy request log with URL: %v", err)
	}
	if _, err := pool.Exec(testContext, `INSERT INTO audit_logs (profile_id, model_id, request_method, request_url, endpoint_base_url, request_headers_legacy_raw_text, response_headers_legacy_raw_text, request_headers_scrub_provenance, response_headers_scrub_provenance, request_headers_capture_status, response_headers_capture_status, url_scrub_provenance, row_kind, legacy_status_code, legacy_duration_ms, is_stream, created_at) VALUES ($1, 'legacy-model', 'POST', 'https://legacy.invalid/v1?api_key=sk-456', 'https://legacy.invalid', '{"authorization": "Bearer sk-789"}', '{"x-api-key": "secret-key"}', 'legacy_unknown', 'legacy_unknown', 'pending_headers', 'pending_headers', 'legacy_unknown', 'legacy_unknown', 200, 100, FALSE, $2)`,
		profileID, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("seed legacy audit log: %v", err)
	}

	// Mark upgrade state draining_v1 -> v1_drained so backfill can run.
	if _, err := pool.Exec(testContext, `UPDATE observability_v2_upgrade_state SET state = 'v1_drained', writer_fence_active = true WHERE id = 1`); err != nil {
		t.Fatalf("set upgrade state to v1_drained: %v", err)
	}

	runUpgradeStartup(t, testContext, databaseURL)

	// request_urls domain: URL scrubbed, provenance legacy_rescrubbed.
	var endpointBaseURL string
	if err := pool.QueryRow(testContext, `SELECT endpoint_base_url FROM request_logs WHERE profile_id = $1 AND ingress_request_id = 'legacy-backfill-2'`, profileID).Scan(&endpointBaseURL); err != nil {
		t.Fatalf("load backfilled URL: %v", err)
	}
	if strings.Contains(endpointBaseURL, "pass") || strings.Contains(endpointBaseURL, "secret") || strings.Contains(endpointBaseURL, "?") {
		t.Fatalf("legacy endpoint URL not scrubbed: %q", endpointBaseURL)
	}
	var urlProvenance string
	if err := pool.QueryRow(testContext, `SELECT url_scrub_provenance FROM request_logs WHERE profile_id = $1 AND ingress_request_id = 'legacy-backfill-2'`, profileID).Scan(&urlProvenance); err != nil {
		t.Fatalf("load URL provenance: %v", err)
	}
	if urlProvenance != "legacy_rescrubbed" {
		t.Fatalf("expected legacy_rescrubbed URL provenance, got %q", urlProvenance)
	}

	// request_metadata domain: caller UA credential scrubbed + provenance array.
	var callerUA string
	var metadataRedacted []string
	if err := pool.QueryRow(testContext, `SELECT COALESCE(caller_user_agent,''), metadata_redacted_fields FROM request_logs WHERE profile_id = $1 AND ingress_request_id = 'legacy-backfill-1'`, profileID).Scan(&callerUA, &metadataRedacted); err != nil {
		t.Fatalf("load backfilled metadata: %v", err)
	}
	if strings.Contains(callerUA, "sk-token-1") {
		t.Fatalf("legacy metadata credential leaked: %q", callerUA)
	}
	found := false
	for _, field := range metadataRedacted {
		if field == "caller_user_agent" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected caller_user_agent in metadata_redacted_fields, got %v", metadataRedacted)
	}

	// audit_headers_urls domain: shadows null, JSONB target scrubbed, ready.
	var shadowCount int
	if err := pool.QueryRow(testContext, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1 AND (request_headers_legacy_raw_text IS NOT NULL OR response_headers_legacy_raw_text IS NOT NULL)`, profileID).Scan(&shadowCount); err != nil {
		t.Fatalf("count raw shadows: %v", err)
	}
	if shadowCount != 0 {
		t.Fatalf("expected raw header shadows nulled, got %d remaining", shadowCount)
	}
	var requestHeaders string
	if err := pool.QueryRow(testContext, `SELECT request_headers::text FROM audit_logs WHERE profile_id = $1`, profileID).Scan(&requestHeaders); err != nil {
		t.Fatalf("load backfilled headers: %v", err)
	}
	if strings.Contains(requestHeaders, "sk-789") {
		t.Fatalf("legacy header credential leaked: %q", requestHeaders)
	}
	if !strings.Contains(requestHeaders, "[REDACTED-LEGACY]") {
		t.Fatalf("expected legacy redacted header values, got %q", requestHeaders)
	}

	// Upgrade state must be backfill_ready.
	var state string
	if err := pool.QueryRow(testContext, `SELECT state FROM observability_v2_upgrade_state WHERE id = 1`).Scan(&state); err != nil {
		t.Fatalf("load upgrade state: %v", err)
	}
	if state != "backfill_ready" {
		t.Fatalf("expected backfill_ready state, got %q", state)
	}
}

func loadUpgradeDefaultProfileID(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var profileID int
	if err := pool.QueryRow(ctx, `SELECT id FROM profiles WHERE is_default = TRUE ORDER BY id ASC LIMIT 1`).Scan(&profileID); err != nil {
		t.Fatalf("load default profile: %v", err)
	}
	return profileID
}

// upgradeStateDatabase applies migrations 000001 through 000005 only (no
// 000011), producing a database in the upgrade state with legacy raw shadow
// columns still present and pricing/observability columns NOT NULL. This is
// the state the offline drain/backfill owners run against. Profile/settings
// seeds run through the same upgrade startup so the DB has a Default profile.
func upgradeStateDatabase(t *testing.T, ctx context.Context, harness postgresHarness, databaseName string) (string, *pgxpool.Pool) {
	t.Helper()
	databaseURL := harness.connectionString(databaseName)
	conn := harness.openEmptyDatabase(t, ctx, databaseName)

	tmpDir := t.TempDir()
	for _, version := range []string{
		"000001_initial_schema.sql",
		"000002_connection_custom_request_parameters.sql",
		"000003_runtime_latency_semantics.sql",
		"000004_endpoint_reference_metadata.sql",
		"000005_proxy_api_key_immutable_attribution.sql",
		"000006_proxy_api_key_immutable_attribution_finalize.sql",
		"000007_routing_policy_strategy_defaults_and_event_identity.sql",
		"000008_pricing_cost_trust_additive.sql",
		"000009_pricing_cost_trust_finalize.sql",
		"000010_request_logs_audit_observability.sql",
	} {
		raw, err := os.ReadFile(filepath.Join("../../migrations", version))
		if err != nil {
			t.Fatalf("read migration %s: %v", version, err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, version), raw, 0o600); err != nil {
			t.Fatalf("write migration %s: %v", version, err)
		}
	}
	runner, err := migrate.New(migrate.Options{MigrationsDir: tmpDir})
	if err != nil {
		t.Fatalf("build upgrade migration runner: %v", err)
	}
	if result, err := runner.Run(ctx, conn); err != nil {
		t.Fatalf("apply upgrade migrations: %v", err)
	} else if result.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected upgrade migrations to apply, got %q", result.Outcome)
	}

	service, err := startup.New(startup.Options{
		DatabaseURL:         databaseURL,
		SecretEncryptionKey: "startup-upgrade-secret",
		MigrationsDir:       tmpDir,
		TimeNow:             func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("build upgrade seed service: %v", err)
	}
	if _, err := service.RunWithConn(ctx, conn); err != nil {
		t.Fatalf("run upgrade seeds: %v", err)
	}
	_ = conn.Close(ctx)

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open upgrade pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return databaseURL, pool
}

// runUpgradeStartup runs the startup service against an upgrade-state
// database using the same temp migrations dir (000001-000005) so the offline
// drain/backfill owners run before 000011 is ever applied.
func runUpgradeStartup(t *testing.T, ctx context.Context, databaseURL string) startup.Service {
	t.Helper()
	tmpDir := t.TempDir()
	for _, version := range []string{
		"000001_initial_schema.sql",
		"000002_connection_custom_request_parameters.sql",
		"000003_runtime_latency_semantics.sql",
		"000004_endpoint_reference_metadata.sql",
		"000005_proxy_api_key_immutable_attribution.sql",
		"000006_proxy_api_key_immutable_attribution_finalize.sql",
		"000007_routing_policy_strategy_defaults_and_event_identity.sql",
		"000008_pricing_cost_trust_additive.sql",
		"000009_pricing_cost_trust_finalize.sql",
		"000010_request_logs_audit_observability.sql",
	} {
		raw, err := os.ReadFile(filepath.Join("../../migrations", version))
		if err != nil {
			t.Fatalf("read migration %s: %v", version, err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, version), raw, 0o600); err != nil {
			t.Fatalf("write migration %s: %v", version, err)
		}
	}
	service, err := startup.New(startup.Options{
		DatabaseURL:         databaseURL,
		SecretEncryptionKey: "startup-upgrade-secret",
		MigrationsDir:       tmpDir,
		TimeNow:             func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		t.Fatalf("build upgrade startup service: %v", err)
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect upgrade startup: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	if _, err := service.RunWithConn(ctx, conn); err != nil {
		t.Fatalf("run upgrade startup: %v", err)
	}
	return service
}

func ensureUpgradeLogPartitions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, tableName string, timestamp time.Time) {
	t.Helper()
	day := timestamp.UTC().Truncate(24 * time.Hour)
	start := day.Format("2006-01-02")
	end := day.Add(24 * time.Hour).Format("2006-01-02")
	partitionName := fmt.Sprintf("%s_%s", tableName, strings.ReplaceAll(start, "-", ""))
	if _, err := pool.Exec(ctx, fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s')`, partitionName, tableName, start, end)); err != nil {
		t.Fatalf("ensure partition %s for %s: %v", partitionName, tableName, err)
	}
}
