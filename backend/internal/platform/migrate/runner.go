package migrate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Outcome string

const (
	OutcomeApply Outcome = "apply"
	OutcomeNoop  Outcome = "noop"
)

type Options struct {
	MigrationsDir string
}

type Result struct {
	Outcome  Outcome
	Versions []string
}

type Runner struct {
	migrationsDir string
}

type fileMigration struct {
	Version string
	Path    string
	SQL     string
}

// supersededMigrationVersions are migration catalog entries whose full DDL
// content is a strict subset of a later canonical migration of the same
// generation, left behind by the pricing/observability renumber at cc1fbb0d:
//
//	000004_pricing_cost_trust_additive      -> 000008_pricing_cost_trust_additive
//	000005_pricing_cost_trust_finalize      -> 000009_pricing_cost_trust_finalize
//	000006_request_logs_audit_observability -> 000010_request_logs_audit_observability
//	000007_audit_bytea_budgets              -> 000010_request_logs_audit_observability
//
// Applying both generations to a fresh database fails on duplicate DDL
// ("column ... already exists"), so the runner never schedules these
// versions: fresh catalogs are produced entirely by the 000008–000011 chain,
// and databases that already recorded the superseded versions keep them as
// stable history identities (they are already applied and therefore never
// pending). The files remain on disk for history continuity; this is the
// single authoritative supersession list.
var supersededMigrationVersions = map[string]struct{}{
	"000004_pricing_cost_trust_additive":      {},
	"000005_pricing_cost_trust_finalize":      {},
	"000006_request_logs_audit_observability": {},
	"000007_audit_bytea_budgets":              {},
}

type statementExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func New(options Options) (Runner, error) {
	migrationsDir := options.MigrationsDir
	if strings.TrimSpace(migrationsDir) == "" {
		migrationsDir = DefaultMigrationsDir()
	}

	if _, err := os.Stat(migrationsDir); err != nil {
		return Runner{}, fmt.Errorf("stat migrations directory: %w", err)
	}

	return Runner{migrationsDir: migrationsDir}, nil
}

func (r Runner) Run(ctx context.Context, conn *pgx.Conn) (Result, error) {
	migrations, err := r.loadMigrations()
	if err != nil {
		return Result{}, err
	}

	appliedVersions, err := loadAppliedVersions(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	if ahead := versionsAheadOfBinary(migrations, appliedVersions); len(ahead) > 0 {
		return Result{}, fmt.Errorf(
			"database schema is ahead of this binary: %s recorded in %s but absent from %s; restore a database backup taken before the upgrade, or run an image at or newer than the recorded schema",
			strings.Join(ahead, ", "), HistoryTable, r.migrationsDir)
	}

	pending := pendingMigrations(migrations, appliedVersions)
	applicationTables, err := listApplicationTables(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	if len(applicationTables) > 0 {
		if len(appliedVersions) == 0 {
			return Result{}, fmt.Errorf(
				"database contains existing application tables but %s is missing; reset the database and let Prism reapply Go migrations",
				HistoryTable,
			)
		}
		if migrationVersionPending(pending, DefaultBaselineVersion) {
			return Result{}, fmt.Errorf(
				"database contains existing application tables but current baseline %s is not recorded in %s; reset the database and let Prism reapply Go migrations",
				DefaultBaselineVersion,
				HistoryTable,
			)
		}
	}

	if len(pending) == 0 {
		if len(applicationTables) > 0 {
			if err := ensurePostBaselineSchemaGuards(ctx, conn); err != nil {
				return Result{}, err
			}
		}
		return Result{Outcome: OutcomeNoop}, nil
	}

	// Trusted bootstrap provenance: `fresh_from_zero` is true only when no
	// application migration has ever been recorded and no application table
	// exists. The marker is transaction-local (SET LOCAL inside the migration
	// transaction) and is read by 000003; it cannot be forged via environment
	// variables, user settings or table contents, and migrations executed
	// outside the runner fail closed when the setting is absent.
	freshFromZero := len(appliedVersions) == 0 && len(applicationTables) == 0

	if err := pgxutil.InTx(ctx, conn, "migration", func(tx pgx.Tx) error {
		if err := ensureHistoryTable(ctx, tx); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, fmt.Sprintf(`SET LOCAL prism.migration_fresh_from_zero = %s`, boolLiteral(freshFromZero))); err != nil {
			return fmt.Errorf("set transaction-local migration provenance: %w", err)
		}

		for _, migration := range pending {
			if err := executeSQL(ctx, tx, migration.SQL); err != nil {
				return fmt.Errorf("apply migration %s: %w", migration.Version, err)
			}
			if err := recordMigration(ctx, tx, migration.Version); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return Result{}, err
	}

	return Result{Outcome: OutcomeApply, Versions: migrationVersions(pending)}, nil
}

func boolLiteral(value bool) string {
	if value {
		return "'true'"
	}
	return "'false'"
}

func (r Runner) SnapshotApplicationSchema(ctx context.Context, conn *pgx.Conn) (string, error) {
	return SnapshotApplicationSchema(ctx, conn)
}

func (r Runner) loadMigrations() ([]fileMigration, error) {
	entries, err := os.ReadDir(r.migrationsDir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	migrations := make([]fileMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		path := filepath.Join(r.migrationsDir, entry.Name())
		version := strings.TrimSuffix(entry.Name(), ".sql")
		if _, superseded := supersededMigrationVersions[version]; superseded {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, fileMigration{
			Version: version,
			Path:    path,
			SQL:     string(raw),
		})
	}

	sort.Slice(migrations, func(left, right int) bool {
		return migrations[left].Version < migrations[right].Version
	})

	if len(migrations) == 0 {
		return nil, fmt.Errorf("no SQL migrations found in %s", r.migrationsDir)
	}

	return migrations, nil
}

func pendingMigrations(migrations []fileMigration, applied map[string]struct{}) []fileMigration {
	pending := make([]fileMigration, 0, len(migrations))
	for _, migration := range migrations {
		if _, ok := applied[migration.Version]; ok {
			continue
		}
		pending = append(pending, migration)
	}
	return pending
}

// versionsAheadOfBinary reports applied history versions this binary does not
// know: either never shipped, or superseded catalog entries whose DDL was
// folded into a later migration. Superseded versions are intentionally part
// of the known set because live databases record them even though
// loadMigrations skips the files.
func versionsAheadOfBinary(migrations []fileMigration, applied map[string]struct{}) []string {
	known := make(map[string]struct{}, len(migrations)+len(supersededMigrationVersions)+1)
	known[DefaultBaselineVersion] = struct{}{}
	for _, migration := range migrations {
		known[migration.Version] = struct{}{}
	}
	for version := range supersededMigrationVersions {
		known[version] = struct{}{}
	}
	// "Ahead" means the database carries a migration this binary does not have
	// yet, which is what happens after rolling an image back. An unrecognised
	// version that sorts *before* the newest known one is not that: it is a
	// legacy or renamed stamp (the v1.0.0 squash renamed the baseline), and the
	// baseline-mismatch check below reports it with a far more actionable
	// message. Flagging those here told operators to restore a backup when the
	// real answer was that the history predates the current baseline.
	newestKnown := ""
	for _, migration := range migrations {
		if migration.Version > newestKnown {
			newestKnown = migration.Version
		}
	}
	ahead := make([]string, 0)
	for version := range applied {
		if _, ok := known[version]; ok {
			continue
		}
		if version > newestKnown {
			ahead = append(ahead, version)
		}
	}
	sort.Strings(ahead)
	return ahead
}

func migrationVersionPending(migrations []fileMigration, version string) bool {
	for _, migration := range migrations {
		if migration.Version == version {
			return true
		}
	}
	return false
}

func migrationVersions(migrations []fileMigration) []string {
	versions := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		versions = append(versions, migration.Version)
	}
	return versions
}

func ensureHistoryTable(ctx context.Context, execer statementExecutor) error {
	const historyTableSQL = `CREATE TABLE IF NOT EXISTS prism_schema_migrations (
		version VARCHAR(255) PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`

	if _, err := execer.Exec(ctx, historyTableSQL); err != nil {
		return fmt.Errorf("ensure prism schema migration history: %w", err)
	}

	return nil
}

func recordMigration(ctx context.Context, execer statementExecutor, version string) error {
	_, err := execer.Exec(
		ctx,
		`INSERT INTO prism_schema_migrations (version, applied_at)
		VALUES ($1, $2)
		ON CONFLICT (version) DO NOTHING`,
		version,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("record prism migration %s: %w", version, err)
	}

	return nil
}

func loadAppliedVersions(ctx context.Context, conn *pgx.Conn) (map[string]struct{}, error) {
	hasHistoryTable, err := tableExists(ctx, conn, HistoryTable)
	if err != nil {
		return nil, err
	}
	if !hasHistoryTable {
		return map[string]struct{}{}, nil
	}

	rows, err := conn.Query(ctx, `SELECT version FROM prism_schema_migrations ORDER BY version ASC`)
	if err != nil {
		return nil, fmt.Errorf("query prism migration history: %w", err)
	}
	defer rows.Close()

	versions := map[string]struct{}{}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan prism migration history row: %w", err)
		}
		versions[version] = struct{}{}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prism migration history rows: %w", err)
	}

	return versions, nil
}

func listApplicationTables(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	rows, err := conn.Query(
		ctx,
		`SELECT tablename
		FROM pg_tables
		WHERE schemaname = $1
		ORDER BY tablename ASC`,
		publicSchema,
	)
	if err != nil {
		return nil, fmt.Errorf("query public table inventory: %w", err)
	}
	defer rows.Close()

	tables := []string{}
	for rows.Next() {
		var tableName string
		if err := rows.Scan(&tableName); err != nil {
			return nil, fmt.Errorf("scan public table inventory: %w", err)
		}
		if tableName == HistoryTable {
			continue
		}
		tables = append(tables, tableName)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate public table inventory: %w", err)
	}

	return tables, nil
}

func tableExists(ctx context.Context, conn *pgx.Conn, tableName string) (bool, error) {
	var exists bool
	if err := conn.QueryRow(
		ctx,
		`SELECT EXISTS (
			SELECT 1
			FROM pg_tables
			WHERE schemaname = $1 AND tablename = $2
		)`,
		publicSchema,
		tableName,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check table existence for %s: %w", tableName, err)
	}

	return exists, nil
}

func executeSQL(ctx context.Context, execer statementExecutor, sql string) error {
	for _, statement := range splitSQLStatements(sql) {
		if _, err := execer.Exec(ctx, statement); err != nil {
			return err
		}
	}

	return nil
}
