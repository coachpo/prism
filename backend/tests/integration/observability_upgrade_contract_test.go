package integrationtest

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

// TestObservabilityUpgradeFinalizeFailsClosedWhenUndrained verifies that
// applying 000011 while the v1 drain is incomplete fails closed and is not
// recorded as applied (Requests SPEC §5.6).
func TestObservabilityUpgradeFinalizeFailsClosedWhenUndrained(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "observability_finalize_fail_closed"
	_, pool := upgradeStateDatabase(t, testContext, harness, databaseName)
	defer pool.Close()

	// Seed a v1 outbox row and force draining_v1 so 000011 preconditions fail.
	now := time.Now().UTC()
	if _, err := pool.Exec(testContext, `INSERT INTO runtime_telemetry_outbox (profile_id, ingress_request_id, payload, schema_version, created_at)
		VALUES (1, 'v1-undrained', '{"usage_event":{}}', 1, $1)`, now); err != nil {
		t.Fatalf("seed undrained v1 row: %v", err)
	}
	if _, err := pool.Exec(testContext, `UPDATE observability_v2_upgrade_state SET state = 'draining_v1', writer_fence_active = false WHERE id = 1`); err != nil {
		t.Fatalf("set undrained upgrade state: %v", err)
	}

	// Attempt to apply the full migration set including 000011.
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
		"000011_request_logs_audit_observability_finalize.sql",
	} {
		raw, readErr := readRepoFile("../../migrations", version)
		if readErr != nil {
			t.Fatalf("read migration %s: %v", version, readErr)
		}
		if err := writeRepoFile(tmpDir, version, raw); err != nil {
			t.Fatalf("write migration %s: %v", version, err)
		}
	}
	runner, err := migrate.New(migrate.Options{MigrationsDir: tmpDir})
	if err != nil {
		t.Fatalf("build finalize runner: %v", err)
	}
	conn, err := pgx.Connect(testContext, harness.connectionString(databaseName))
	if err != nil {
		t.Fatalf("connect upgrade database: %v", err)
	}
	defer func() { _ = conn.Close(testContext) }()
	_, err = runner.Run(testContext, conn)
	if err == nil {
		t.Fatal("expected 000011 to fail closed on an undrained upgrade database")
	}
	if !strings.Contains(err.Error(), "not drained") && !strings.Contains(err.Error(), "v1 telemetry outbox") {
		t.Fatalf("expected v1-drain fail-closed error, got %v", err)
	}
	var applied int
	if err := pool.QueryRow(testContext, `SELECT COUNT(*) FROM prism_schema_migrations WHERE version LIKE '000011%'`).Scan(&applied); err != nil {
		t.Fatalf("count applied 000011: %v", err)
	}
	if applied != 0 {
		t.Fatalf("expected 000011 not recorded as applied, got %d", applied)
	}
}

// TestObservabilityUpgradeBackfillInterruptedResume verifies the backfill owner is
// crash-resumable: after a partial batch commit, a restart continues from the
// durable cursor and eventually reaches backfill_ready.
func TestObservabilityUpgradeBackfillInterruptedResume(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	databaseName := "observability_backfill_resume"
	databaseURL, pool := upgradeStateDatabase(t, testContext, harness, databaseName)
	profileID := loadUpgradeDefaultProfileID(t, testContext, pool)

	now := time.Now().UTC()
	ensureUpgradeLogPartitions(t, testContext, pool, "request_logs", now)
	// Seed two legacy request logs; simulate a partial first batch by
	// committing a scrubbed first row plus a cursor past it with the domain
	// still running, so a restart must resume from the second row.
	if _, err := pool.Exec(testContext, `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, row_kind, url_scrub_provenance, request_path, endpoint_base_url, is_stream, pricing_status, pricing_evidence_trust, created_at, legacy_status_code, legacy_duration_ms)
		VALUES ($1, 'legacy-model', 'openai', 'resume-a', 'legacy_unknown', 'legacy_rescrubbed', '/v1/chat/completions', 'https://legacy.invalid/v1', FALSE, 'unknown', 'legacy_untrusted', $2, 200, 100)`,
		profileID, now); err != nil {
		t.Fatalf("seed scrubbed resume request log: %v", err)
	}
	if _, err := pool.Exec(testContext, `INSERT INTO request_logs (profile_id, model_id, api_family, ingress_request_id, row_kind, url_scrub_provenance, request_path, endpoint_base_url, is_stream, pricing_status, pricing_evidence_trust, created_at, legacy_status_code, legacy_duration_ms)
		VALUES ($1, 'legacy-model', 'openai', 'resume-b', 'legacy_unknown', 'legacy_unknown', '/v1/chat/completions', 'https://user:pass@legacy.invalid/v1?key=secret', FALSE, 'unknown', 'legacy_untrusted', $2, 200, 100)`,
		profileID, now.Add(time.Minute)); err != nil {
		t.Fatalf("seed unscrubbed resume request log: %v", err)
	}
	firstCreatedAt := now
	firstID := loadFirstResumeRequestLogID(t, testContext, pool, profileID)
	if _, err := pool.Exec(testContext, `INSERT INTO observability_v2_backfill_state (profile_id, domain, status, last_created_at, last_id, updated_at)
		VALUES ($1, 'request_urls', 'running', $2, $3, now())
		ON CONFLICT (profile_id, domain) DO UPDATE SET status = 'running', last_created_at = EXCLUDED.last_created_at, last_id = EXCLUDED.last_id, updated_at = now()`,
		profileID, firstCreatedAt, firstID); err != nil {
		t.Fatalf("seed partial backfill cursor: %v", err)
	}
	if _, err := pool.Exec(testContext, `UPDATE observability_v2_upgrade_state SET state = 'v1_drained', writer_fence_active = true WHERE id = 1`); err != nil {
		t.Fatalf("set v1_drained state: %v", err)
	}

	// Rerun startup: the backfill must resume from the cursor and finish.
	runUpgradeStartup(t, testContext, databaseURL)

	var state string
	if err := pool.QueryRow(testContext, `SELECT state FROM observability_v2_upgrade_state WHERE id = 1`).Scan(&state); err != nil {
		t.Fatalf("load upgrade state: %v", err)
	}
	if state != "backfill_ready" {
		t.Fatalf("expected backfill_ready after interrupted resume, got %q", state)
	}
	var scrubbed int
	if err := pool.QueryRow(testContext, `SELECT COUNT(*) FROM request_logs WHERE profile_id = $1 AND url_scrub_provenance = 'legacy_rescrubbed'`, profileID).Scan(&scrubbed); err != nil {
		t.Fatalf("count scrubbed URLs: %v", err)
	}
	if scrubbed != 2 {
		t.Fatalf("expected both legacy URLs rescrubbed after resume, got %d", scrubbed)
	}
}

func loadFirstResumeRequestLogID(t *testing.T, ctx context.Context, pool *pgxpool.Pool, profileID int) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(ctx, `SELECT id FROM request_logs WHERE profile_id = $1 ORDER BY created_at ASC, id ASC LIMIT 1`, profileID).Scan(&id); err != nil {
		t.Fatalf("load first resume request log: %v", err)
	}
	return id
}

func readRepoFile(dir string, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, name))
}

func writeRepoFile(dir string, name string, raw []byte) error {
	return os.WriteFile(filepath.Join(dir, name), raw, 0o600)
}
