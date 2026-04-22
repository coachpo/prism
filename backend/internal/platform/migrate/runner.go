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

	pending := pendingMigrations(migrations, appliedVersions)
	if len(pending) == 0 {
		return Result{Outcome: OutcomeNoop}, nil
	}

	applicationTables, err := listApplicationTables(ctx, conn)
	if err != nil {
		return Result{}, err
	}

	if len(appliedVersions) == 0 && len(applicationTables) > 0 {
		return Result{}, fmt.Errorf(
			"database contains existing application tables but %s is missing; reset the database and let Prism reapply Go migrations",
			HistoryTable,
		)
	}

	if err := pgxutil.InTx(ctx, conn, "migration", func(tx pgx.Tx) error {
		if err := ensureHistoryTable(ctx, tx); err != nil {
			return err
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
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		migrations = append(migrations, fileMigration{
			Version: strings.TrimSuffix(entry.Name(), ".sql"),
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

func splitSQLStatements(sql string) []string {
	rawStatements := strings.Split(sql, ";")
	statements := make([]string, 0, len(rawStatements))
	for _, rawStatement := range rawStatements {
		statement := strings.TrimSpace(rawStatement)
		if statement == "" {
			continue
		}
		statements = append(statements, statement)
	}
	return statements
}
