package integrationtest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/coachpo/prism/backend/internal/platform/migrate"
)

// rollbackRoutingPolicyMigrationSimulates a pre-000007 database: it unstamps
// the routing-policy migration and drops its schema surfaces so fixture rows
// can be seeded and the migration re-applied.
func rollbackRoutingPolicyMigration(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	for _, statement := range []string{
		`ALTER TABLE public.loadbalance_events DROP CONSTRAINT IF EXISTS chk_loadbalance_events_model_config_id_pair`,
		`ALTER TABLE public.loadbalance_events DROP CONSTRAINT IF EXISTS chk_loadbalance_events_admission_failure_kind_null`,
		`ALTER TABLE public.loadbalance_events DROP CONSTRAINT IF EXISTS chk_loadbalance_events_admission_reason_scope`,
		`ALTER TABLE public.loadbalance_events DROP CONSTRAINT IF EXISTS chk_loadbalance_events_admission_reason`,
		`ALTER TABLE public.loadbalance_events DROP COLUMN IF EXISTS admission_reason`,
		`ALTER TABLE public.loadbalance_events DROP COLUMN IF EXISTS model_config_id`,
		`DROP INDEX IF EXISTS public.loadbalance_strategies_one_default_per_profile_idx`,
		`ALTER TABLE public.loadbalance_strategies DROP COLUMN IF EXISTS is_default`,
		// Restore the pre-000007 column defaults. Later migrations (000019)
		// change them, and fixture rows rely on defaults to reproduce the exact
		// canonical payload that 000007 matches against.
		`ALTER TABLE public.loadbalance_strategies ALTER COLUMN failure_status_codes SET DEFAULT ARRAY[403, 422, 429, 500, 502, 503, 504, 529]`,
		`ALTER TABLE public.loadbalance_strategies ALTER COLUMN retry_base_delay_ms SET DEFAULT 60000`,
		// The fixtures below seed "exact canonical" rows from the column
		// defaults, so simulating the pre-000007 world means restoring the
		// defaults of that era too. Later migrations move them forward
		// (000019 widened the failover set and shortened the retry delay), and
		// without this the seeded row silently stops matching what 000007
		// itself considers canonical.
		`ALTER TABLE public.loadbalance_strategies ALTER COLUMN failure_status_codes SET DEFAULT ARRAY[403, 422, 429, 500, 502, 503, 504, 529]`,
		`ALTER TABLE public.loadbalance_strategies ALTER COLUMN retry_base_delay_ms SET DEFAULT 60000`,
		`DELETE FROM prism_schema_migrations WHERE version = '000007_routing_policy_strategy_defaults_and_event_identity'`,
	} {
		if _, err := conn.Exec(ctx, statement); err != nil {
			t.Fatalf("rollback 000007 surface with %q: %v", statement, err)
		}
	}
}

type migrationStrategyFixture struct {
	name      string
	strategy  string
	profileID int
	override  map[string]any
}

func seedMigrationStrategy(t *testing.T, ctx context.Context, conn *pgx.Conn, fixture migrationStrategyFixture, now time.Time) int {
	t.Helper()
	baseColumns := []string{"profile_id", "name", "legacy_strategy_type", "created_at", "updated_at"}
	columns := append([]string(nil), baseColumns...)
	for key := range fixture.override {
		if !slices.Contains(baseColumns, key) {
			columns = append(columns, key)
		}
	}
	query := "INSERT INTO loadbalance_strategies (" + strings.Join(columns, ", ") + ") VALUES ("
	values := make([]any, 0, len(columns))
	for index, column := range columns {
		if index > 0 {
			query += ", "
		}
		query += fmt.Sprintf("$%d", index+1)
		switch column {
		case "profile_id":
			values = append(values, fixture.profileID)
		case "name":
			values = append(values, fixture.name)
		case "legacy_strategy_type":
			values = append(values, fixture.strategy)
		case "created_at":
			values = append(values, now)
		case "updated_at":
			if value, ok := fixture.override["updated_at"]; ok {
				values = append(values, value)
			} else {
				values = append(values, now)
			}
		default:
			values = append(values, fixture.override[column])
		}
	}
	query += ") RETURNING id"
	var strategyID int
	if err := conn.QueryRow(ctx, query, values...).Scan(&strategyID); err != nil {
		t.Fatalf("seed migration strategy %q: %v", fixture.name, err)
	}
	return strategyID
}

func seedMigrationProfile(t *testing.T, ctx context.Context, conn *pgx.Conn, label string, now time.Time) int {
	t.Helper()
	var profileID int
	if err := conn.QueryRow(ctx, `INSERT INTO profiles (name, description, is_active, is_default, is_editable, version, deleted_at, created_at, updated_at) VALUES ($1, NULL, FALSE, FALSE, TRUE, 1, NULL, $2, $2) RETURNING id`, "migration-"+label, now).Scan(&profileID); err != nil {
		t.Fatalf("seed migration profile %q: %v", label, err)
	}
	return profileID
}

func assertStrategyDefaultState(t *testing.T, ctx context.Context, conn *pgx.Conn, profileID int, wantDefaultName string, wantCount int) {
	t.Helper()
	var strategyCount int
	var defaultCount int
	var defaultName string
	if err := conn.QueryRow(ctx, `
		SELECT COUNT(id),
		       COUNT(*) FILTER (WHERE is_default),
		       COALESCE(MAX(name) FILTER (WHERE is_default), '')
		FROM loadbalance_strategies
		WHERE profile_id = $1`, profileID).Scan(&strategyCount, &defaultCount, &defaultName); err != nil {
		t.Fatalf("query strategy default state for profile %d: %v", profileID, err)
	}
	if strategyCount != wantCount {
		t.Fatalf("expected %d strategies for profile %d, got %d", wantCount, profileID, strategyCount)
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly one default for profile %d, got %d", profileID, defaultCount)
	}
	if wantDefaultName != "" && defaultName != wantDefaultName {
		t.Fatalf("expected default %q for profile %d, got %q", wantDefaultName, profileID, defaultName)
	}
}

func TestRoutingPolicyStrategyDefaultMigrationFixtures(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "routing_policy_default_migration_fixtures")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply full migration set: %v", err)
	}
	rollbackRoutingPolicyMigration(t, testContext, conn)
	now := time.Now().UTC()

	// Fixture 1: same-second exact canonical rows. Only fill-first may become
	// the default; the other two canonical rows stay non-default.
	exactProfile := seedMigrationProfile(t, testContext, conn, "exact", now)
	for _, spec := range []struct{ name, strategy string }{
		{"Default single routing", "single"},
		{"Default fill-first routing", "fill-first"},
		{"Default round-robin routing", "round-robin"},
	} {
		seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: spec.name, strategy: spec.strategy, profileID: exactProfile}, now)
	}

	// Fixture 2: edited-time drift — the fill-first canonical row was edited
	// most recently; the exact payload still decides, not the timestamp.
	driftProfile := seedMigrationProfile(t, testContext, conn, "drift", now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "Default single routing", strategy: "single", profileID: driftProfile}, now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "Default round-robin routing", strategy: "round-robin", profileID: driftProfile}, now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "Default fill-first routing", strategy: "fill-first", profileID: driftProfile, override: map[string]any{"updated_at": now.Add(2 * time.Hour)}}, now.Add(2*time.Hour))

	// Fixture 3: several custom fill-first rows and no canonical name. The
	// canonical fill-first row must be inserted and become the default; custom
	// rows are untouched and never selected by attachment count.
	customProfile := seedMigrationProfile(t, testContext, conn, "custom", now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "My fill-first A", strategy: "fill-first", profileID: customProfile, override: map[string]any{"retry_base_delay_ms": 1000}}, now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "My fill-first B", strategy: "fill-first", profileID: customProfile, override: map[string]any{"cycle_retry_attempt_limit": 5}}, now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "Default single routing", strategy: "single", profileID: customProfile}, now)

	// Fixture 4: no fill-first of any kind; the canonical row is inserted.
	noFillProfile := seedMigrationProfile(t, testContext, conn, "no-fill", now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "Default single routing", strategy: "single", profileID: noFillProfile}, now)
	seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{name: "Default round-robin routing", strategy: "round-robin", profileID: noFillProfile}, now)

	// Fixture 5: empty profile; the canonical fill-first row is inserted.
	emptyProfile := seedMigrationProfile(t, testContext, conn, "empty", now)

	applyResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("apply 000007 migration: %v", err)
	}
	if applyResult.Outcome != migrate.OutcomeApply {
		t.Fatalf("expected 000007 to apply, got %q", applyResult.Outcome)
	}

	assertStrategyDefaultState(t, testContext, conn, exactProfile, "Default fill-first routing", 3)
	assertStrategyDefaultState(t, testContext, conn, driftProfile, "Default fill-first routing", 3)
	assertStrategyDefaultState(t, testContext, conn, customProfile, "Default fill-first routing", 4)
	assertStrategyDefaultState(t, testContext, conn, noFillProfile, "Default fill-first routing", 3)
	assertStrategyDefaultState(t, testContext, conn, emptyProfile, "Default fill-first routing", 1)

	// Existing model bindings are never rewritten: a model attached to the
	// single strategy keeps its binding after migration.
	var singleStrategyID int
	if err := conn.QueryRow(testContext, `SELECT id FROM loadbalance_strategies WHERE profile_id = $1 AND name = 'Default single routing'`, exactProfile).Scan(&singleStrategyID); err != nil {
		t.Fatalf("load single canonical strategy: %v", err)
	}
	var modelConfigID int
	if err := conn.QueryRow(testContext, `INSERT INTO model_configs (profile_id, api_family, model_id, display_name, loadbalance_strategy_id, openai_accepted_format, is_enabled, created_at, updated_at) VALUES ($1, 'openai', 'migration-model', NULL, $2, 'dual_native', TRUE, $3, $3) RETURNING id`, exactProfile, singleStrategyID, now).Scan(&modelConfigID); err != nil {
		t.Fatalf("seed attached model: %v", err)
	}
	var boundStrategyID int
	if err := conn.QueryRow(testContext, `SELECT loadbalance_strategy_id FROM model_configs WHERE id = $1`, modelConfigID).Scan(&boundStrategyID); err != nil {
		t.Fatalf("load bound strategy id: %v", err)
	}
	if boundStrategyID != singleStrategyID {
		t.Fatalf("expected existing model binding to stay on strategy %d, got %d", singleStrategyID, boundStrategyID)
	}

	// The partial unique index enforces at most one default per profile.
	if _, err := conn.Exec(testContext, `UPDATE loadbalance_strategies SET is_default = TRUE WHERE profile_id = $1 AND name = 'Default single routing'`, exactProfile); err == nil {
		t.Fatalf("expected partial unique index to reject a second default")
	}

	// Idempotent re-run: nothing to apply.
	secondResult, err := runner.Run(testContext, conn)
	if err != nil {
		t.Fatalf("re-run after 000007: %v", err)
	}
	if secondResult.Outcome != migrate.OutcomeNoop {
		t.Fatalf("expected second run to noop, got %q", secondResult.Outcome)
	}
}

func TestRoutingPolicyStrategyDefaultMigrationConflictFailsFast(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	for _, conflict := range []struct {
		name        string
		label       string
		strategy    string
		override    map[string]any
		wantMessage string
	}{
		{
			name:        "subtype conflict",
			label:       "subtype-conflict",
			strategy:    "single",
			override:    nil,
			wantMessage: "canonical_default_strategy_conflict",
		},
		{
			name:        "payload conflict",
			label:       "payload-conflict",
			strategy:    "fill-first",
			override:    map[string]any{"retry_base_delay_ms": 5000},
			wantMessage: "canonical_default_strategy_conflict",
		},
	} {
		t.Run(conflict.name, func(t *testing.T) {
			harness := newPostgresHarness(t)
			runner := newRunner(t)
			conn := harness.openEmptyDatabase(t, testContext, "routing_policy_default_conflict_"+strings.ReplaceAll(conflict.label, "-", "_"))
			defer func() { _ = conn.Close(testContext) }()

			if _, err := runner.Run(testContext, conn); err != nil {
				t.Fatalf("apply full migration set: %v", err)
			}
			rollbackRoutingPolicyMigration(t, testContext, conn)
			now := time.Now().UTC()
			profileID := seedMigrationProfile(t, testContext, conn, conflict.label, now)
			seedMigrationStrategy(t, testContext, conn, migrationStrategyFixture{
				name:      "Default fill-first routing",
				strategy:  conflict.strategy,
				profileID: profileID,
				override:  conflict.override,
			}, now)

			applyResult, err := runner.Run(testContext, conn)
			if err == nil {
				t.Fatalf("expected 000007 to fail on canonical conflict, got outcome %q", applyResult.Outcome)
			}
			if !strings.Contains(err.Error(), conflict.wantMessage) {
				t.Fatalf("expected conflict error to contain %q, got %v", conflict.wantMessage, err)
			}
			if !strings.Contains(err.Error(), fmt.Sprintf("profile_id=%d", profileID)) {
				t.Fatalf("expected conflict error to name profile %d, got %v", profileID, err)
			}

			// The failed migration must roll back completely: the is_default
			// column and migration history stay absent, no row was modified.
			var columnExists bool
			if err := conn.QueryRow(testContext, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'loadbalance_strategies' AND column_name = 'is_default')`).Scan(&columnExists); err != nil {
				t.Fatalf("check is_default rollback: %v", err)
			}
			if columnExists {
				t.Fatalf("expected failed migration to roll back is_default column")
			}
			var versionExists bool
			if err := conn.QueryRow(testContext, `SELECT EXISTS (SELECT 1 FROM prism_schema_migrations WHERE version = '000007_routing_policy_strategy_defaults_and_event_identity')`).Scan(&versionExists); err != nil {
				t.Fatalf("check migration history rollback: %v", err)
			}
			if versionExists {
				t.Fatalf("expected failed migration to roll back history stamp")
			}
		})
	}
}

func TestRoutingPolicyEventsIdentityMigration(t *testing.T) {
	testContext, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	harness := newPostgresHarness(t)
	runner := newRunner(t)
	conn := harness.openEmptyDatabase(t, testContext, "routing_policy_events_identity")
	defer func() { _ = conn.Close(testContext) }()

	if _, err := runner.Run(testContext, conn); err != nil {
		t.Fatalf("apply full migration set: %v", err)
	}

	// Columns are nullable on the parent and propagate to existing partitions.
	assertColumnNullable(t, testContext, conn, "loadbalance_events", "admission_reason")
	assertColumnNullable(t, testContext, conn, "loadbalance_events", "model_config_id")
	partition := ensureDailyLogPartition(t, testContext, conn, "loadbalance_events", time.Now().UTC(), "identity")
	assertColumnNullable(t, testContext, conn, partition, "admission_reason")
	assertColumnNullable(t, testContext, conn, partition, "model_config_id")

	now := time.Now().UTC()
	profileID := seedMigrationProfile(t, testContext, conn, "events", now)

	// Writer invariants: admission rows carry allowlisted reason and NULL
	// failure kind; non-admission rows carry NULL admission reason.
	admissionEventID, err := insertMigrationEvent(t, testContext, conn, partition, profileID, "admission_rejected", now)
	if err != nil {
		t.Fatalf("insert admission event: %v", err)
	}
	if _, err := conn.Exec(testContext, `UPDATE loadbalance_events SET admission_reason = 'qps_limit', failure_kind = NULL WHERE id = $1`, admissionEventID); err != nil {
		t.Fatalf("set admission reason: %v", err)
	}
	retryEventID, err := insertMigrationEvent(t, testContext, conn, partition, profileID, "retry_scheduled", now)
	if err != nil {
		t.Fatalf("insert retry event: %v", err)
	}
	if _, err := conn.Exec(testContext, `UPDATE loadbalance_events SET failure_kind = 'timeout' WHERE id = $1`, retryEventID); err != nil {
		t.Fatalf("set retry failure kind: %v", err)
	}

	rejections := []struct {
		label    string
		sql      string
		targetID int64
	}{
		{"unknown admission reason", `UPDATE loadbalance_events SET admission_reason = 'mystery' WHERE id = $1`, admissionEventID},
		{"admission reason on non-admission row", `UPDATE loadbalance_events SET admission_reason = 'qps_limit' WHERE id = $1`, retryEventID},
		{"failure kind on admission row", `UPDATE loadbalance_events SET failure_kind = 'timeout' WHERE id = $1`, admissionEventID},
		{"model_config_id without public model_id", `UPDATE loadbalance_events SET model_config_id = 42 WHERE id = $1`, retryEventID},
		{"non-positive model_config_id", `UPDATE loadbalance_events SET model_config_id = 0, model_id = 'm' WHERE id = $1`, retryEventID},
	}
	for _, rejection := range rejections {
		if _, err := conn.Exec(testContext, rejection.sql, rejection.targetID); err == nil {
			t.Fatalf("expected CHECK to reject %q", rejection.label)
		}
	}
	if _, err := conn.Exec(testContext, `UPDATE loadbalance_events SET model_config_id = 42, model_id = 'model-a' WHERE id = $1`, retryEventID); err != nil {
		t.Fatalf("expected positive model_config_id with public model_id to be accepted: %v", err)
	}
}

func insertMigrationEvent(t *testing.T, ctx context.Context, conn *pgx.Conn, partition string, profileID int, eventType string, now time.Time) (int64, error) {
	t.Helper()
	var eventID int64
	err := conn.QueryRow(ctx, fmt.Sprintf(`
		INSERT INTO public.%s (
			profile_id, connection_id, event_type, failure_kind,
			cycle_retry_attempts, cumulative_retry_attempts,
			next_retry_at, last_retry_delay_ms, model_id, endpoint_id,
			ban_mode, policy_cycle_retry_attempt_limit,
			policy_ban_cumulative_retry_attempt_threshold,
			banned_until_at, last_success_at, created_at
		) VALUES ($1, 1, $2, NULL, 0, 0, NULL, 0, NULL, NULL, NULL, NULL, NULL, NULL, NULL, $3)
		RETURNING id`,
		quoteIdentifier(partition),
	), profileID, eventType, now).Scan(&eventID)
	if err != nil {
		return 0, err
	}
	return eventID, nil
}
