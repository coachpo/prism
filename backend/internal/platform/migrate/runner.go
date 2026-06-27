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

const modelAccessTargetsConnectionOwnerIndexName = "uq_model_access_targets_connection_owner"

const modelAccessTargetsConnectionOwnerIndexSQL = `CREATE UNIQUE INDEX uq_model_access_targets_connection_owner ON public.model_access_targets USING btree (target_connection_id) WHERE (target_connection_id IS NOT NULL)`
const ensureRequestLogsUpstreamOperationNameColumnSQL = `ALTER TABLE public.request_logs ADD COLUMN IF NOT EXISTS upstream_operation_name character varying(120)`
const ensureRequestLogsOperationTranslationModeColumnSQL = `ALTER TABLE public.request_logs ADD COLUMN IF NOT EXISTS operation_translation_mode character varying(80)`
const ensureRequestLogsUpstreamRequestPathColumnSQL = `ALTER TABLE public.request_logs ADD COLUMN IF NOT EXISTS upstream_request_path character varying(500)`
const ensureUsageEventsUpstreamOperationNameColumnSQL = `ALTER TABLE public.usage_request_events ADD COLUMN IF NOT EXISTS upstream_operation_name character varying(120)`
const ensureUsageEventsOperationTranslationModeColumnSQL = `ALTER TABLE public.usage_request_events ADD COLUMN IF NOT EXISTS operation_translation_mode character varying(80)`
const ensureUsageEventsUpstreamRequestPathColumnSQL = `ALTER TABLE public.usage_request_events ADD COLUMN IF NOT EXISTS upstream_request_path character varying(500)`
const ensureUsageEventsEndpointLabelSnapshotColumnSQL = `ALTER TABLE public.usage_request_events ADD COLUMN IF NOT EXISTS endpoint_label_snapshot text`
const backfillUsageEventsEndpointLabelSnapshotSQL = `WITH ranked_request_logs AS (
	SELECT
		profile_id,
		ingress_request_id,
		endpoint_id,
		NULLIF(BTRIM(endpoint_description), '') AS endpoint_description,
		NULLIF(BTRIM(endpoint_base_url), '') AS endpoint_base_url,
		ROW_NUMBER() OVER (
			PARTITION BY profile_id, ingress_request_id, endpoint_id
			ORDER BY attempt_number DESC NULLS LAST, created_at DESC, id DESC
		) AS row_number
	FROM public.request_logs
	WHERE ingress_request_id IS NOT NULL
	  AND endpoint_id IS NOT NULL
), selected_request_logs AS (
	SELECT profile_id, ingress_request_id, endpoint_id, endpoint_description, endpoint_base_url
	FROM ranked_request_logs
	WHERE row_number = 1
), backfill AS (
	SELECT
		usage_events.created_at,
		usage_events.id,
		COALESCE(
			request_logs.endpoint_description,
			request_logs.endpoint_base_url,
			NULLIF(BTRIM(endpoints.name), ''),
			NULLIF(BTRIM(endpoints.base_url), ''),
			CASE
				WHEN usage_events.endpoint_id IS NOT NULL THEN 'Endpoint ' || usage_events.endpoint_id::text
				ELSE NULL
			END,
			'Unknown Endpoint'
		) AS endpoint_label_snapshot
	FROM public.usage_request_events usage_events
	LEFT JOIN selected_request_logs request_logs
	  ON request_logs.profile_id = usage_events.profile_id
	 AND request_logs.ingress_request_id = usage_events.ingress_request_id
	 AND request_logs.endpoint_id = usage_events.endpoint_id
	LEFT JOIN public.endpoints endpoints
	  ON endpoints.id = usage_events.endpoint_id
	WHERE usage_events.endpoint_label_snapshot IS NULL
)
UPDATE public.usage_request_events usage_events
SET endpoint_label_snapshot = backfill.endpoint_label_snapshot
FROM backfill
WHERE usage_events.created_at = backfill.created_at
  AND usage_events.id = backfill.id
  AND usage_events.endpoint_label_snapshot IS NULL`
const ensureUsageEventsEndpointLabelSnapshotNotNullSQL = `ALTER TABLE public.usage_request_events ALTER COLUMN endpoint_label_snapshot SET NOT NULL`

const duplicateModelAccessTargetConnectionOwnersSQL = `SELECT target_connection_id, COUNT(*) AS owner_count, ARRAY_AGG(source_model_config_id ORDER BY source_model_config_id) AS source_model_config_ids FROM model_access_targets WHERE target_connection_id IS NOT NULL GROUP BY target_connection_id HAVING COUNT(*) > 1`

type duplicateModelAccessTargetConnectionOwner struct {
	targetConnectionID   int
	ownerCount           int64
	sourceModelConfigIDs []int32
}

func ensurePostBaselineSchemaGuards(ctx context.Context, conn *pgx.Conn) error {
	if err := ensureTranslatedObservabilitySchema(ctx, conn); err != nil {
		return err
	}
	if err := ensureModelAccessTargetsConnectionOwnerIndex(ctx, conn); err != nil {
		return err
	}
	return nil
}

func ensureTranslatedObservabilitySchema(ctx context.Context, conn *pgx.Conn) error {
	for _, step := range []struct {
		name string
		sql  string
	}{
		{name: "request_logs upstream operation column", sql: ensureRequestLogsUpstreamOperationNameColumnSQL},
		{name: "request_logs translation mode column", sql: ensureRequestLogsOperationTranslationModeColumnSQL},
		{name: "request_logs upstream request path column", sql: ensureRequestLogsUpstreamRequestPathColumnSQL},
		{name: "usage_request_events upstream operation column", sql: ensureUsageEventsUpstreamOperationNameColumnSQL},
		{name: "usage_request_events translation mode column", sql: ensureUsageEventsOperationTranslationModeColumnSQL},
		{name: "usage_request_events upstream request path column", sql: ensureUsageEventsUpstreamRequestPathColumnSQL},
		{name: "usage_request_events endpoint label snapshot column", sql: ensureUsageEventsEndpointLabelSnapshotColumnSQL},
		{name: "usage_request_events endpoint label snapshot backfill", sql: backfillUsageEventsEndpointLabelSnapshotSQL},
		{name: "usage_request_events endpoint label snapshot not-null constraint", sql: ensureUsageEventsEndpointLabelSnapshotNotNullSQL},
	} {
		if _, err := conn.Exec(ctx, step.sql); err != nil {
			return fmt.Errorf("ensure %s: %w", step.name, err)
		}
	}
	return nil
}

func ensureModelAccessTargetsConnectionOwnerIndex(ctx context.Context, conn *pgx.Conn) error {
	exists, err := indexExists(ctx, conn, "model_access_targets", modelAccessTargetsConnectionOwnerIndexName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	duplicates, err := loadDuplicateModelAccessTargetConnectionOwners(ctx, conn)
	if err != nil {
		return err
	}
	if len(duplicates) > 0 {
		return fmt.Errorf(
			"model access target connection owner invariant violation blocks %s creation: %s",
			modelAccessTargetsConnectionOwnerIndexName,
			formatDuplicateModelAccessTargetConnectionOwners(duplicates),
		)
	}

	if _, err := conn.Exec(ctx, modelAccessTargetsConnectionOwnerIndexSQL); err != nil {
		return fmt.Errorf("create %s: %w", modelAccessTargetsConnectionOwnerIndexName, err)
	}
	return nil
}

func loadDuplicateModelAccessTargetConnectionOwners(ctx context.Context, conn *pgx.Conn) ([]duplicateModelAccessTargetConnectionOwner, error) {
	rows, err := conn.Query(ctx, duplicateModelAccessTargetConnectionOwnersSQL)
	if err != nil {
		return nil, fmt.Errorf("query duplicate model access target connection owners: %w", err)
	}
	defer rows.Close()

	duplicates := []duplicateModelAccessTargetConnectionOwner{}
	for rows.Next() {
		var duplicate duplicateModelAccessTargetConnectionOwner
		if err := rows.Scan(&duplicate.targetConnectionID, &duplicate.ownerCount, &duplicate.sourceModelConfigIDs); err != nil {
			return nil, fmt.Errorf("scan duplicate model access target connection owner: %w", err)
		}
		duplicates = append(duplicates, duplicate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate duplicate model access target connection owners: %w", err)
	}

	sort.Slice(duplicates, func(left, right int) bool {
		return duplicates[left].targetConnectionID < duplicates[right].targetConnectionID
	})
	return duplicates, nil
}

func formatDuplicateModelAccessTargetConnectionOwners(duplicates []duplicateModelAccessTargetConnectionOwner) string {
	parts := make([]string, 0, len(duplicates))
	for _, duplicate := range duplicates {
		parts = append(parts, fmt.Sprintf(
			"target_connection_id=%d owner_count=%d source_model_config_ids=%s",
			duplicate.targetConnectionID,
			duplicate.ownerCount,
			formatInt32List(duplicate.sourceModelConfigIDs),
		))
	}
	return strings.Join(parts, "; ")
}

func formatInt32List(values []int32) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, fmt.Sprintf("%d", value))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func indexExists(ctx context.Context, conn *pgx.Conn, tableName string, indexName string) (bool, error) {
	var exists bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_index idx
			JOIN pg_class index_class ON index_class.oid = idx.indexrelid
			JOIN pg_class table_class ON table_class.oid = idx.indrelid
			JOIN pg_namespace n ON n.oid = table_class.relnamespace
			WHERE n.nspname = $1 AND table_class.relname = $2 AND index_class.relname = $3
		)`, publicSchema, tableName, indexName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check index %s on %s: %w", indexName, tableName, err)
	}
	return exists, nil
}

func recreateConstraint(ctx context.Context, conn *pgx.Conn, tableName string, constraintName string, addSQL string) error {
	exists, err := constraintExists(ctx, conn, tableName, constraintName)
	if err != nil {
		return err
	}
	if exists {
		if _, err := conn.Exec(ctx, fmt.Sprintf(`ALTER TABLE public.%s DROP CONSTRAINT %s`, tableName, constraintName)); err != nil {
			return fmt.Errorf("drop constraint %s on %s: %w", constraintName, tableName, err)
		}
	}
	if _, err := conn.Exec(ctx, addSQL); err != nil {
		return fmt.Errorf("add constraint %s on %s: %w", constraintName, tableName, err)
	}
	return nil
}

func constraintExists(ctx context.Context, conn *pgx.Conn, tableName string, constraintName string) (bool, error) {
	var exists bool
	if err := conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_constraint con
			JOIN pg_class table_class ON table_class.oid = con.conrelid
			JOIN pg_namespace n ON n.oid = table_class.relnamespace
			WHERE n.nspname = $1 AND table_class.relname = $2 AND con.conname = $3
		)`, publicSchema, tableName, constraintName).Scan(&exists); err != nil {
		return false, fmt.Errorf("check constraint %s on %s: %w", constraintName, tableName, err)
	}
	return exists, nil
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
	statements := make([]string, 0, 8)
	var current strings.Builder
	inSingleQuote := false
	inDoubleQuote := false
	lineComment := false
	blockComment := false
	dollarQuoteTag := ""

	for index := 0; index < len(sql); index++ {
		char := sql[index]

		if lineComment {
			current.WriteByte(char)
			if char == '\n' {
				lineComment = false
			}
			continue
		}

		if blockComment {
			current.WriteByte(char)
			if char == '*' && index+1 < len(sql) && sql[index+1] == '/' {
				current.WriteByte(sql[index+1])
				index++
				blockComment = false
			}
			continue
		}

		if dollarQuoteTag != "" {
			if strings.HasPrefix(sql[index:], dollarQuoteTag) {
				current.WriteString(dollarQuoteTag)
				index += len(dollarQuoteTag) - 1
				dollarQuoteTag = ""
				continue
			}
			current.WriteByte(char)
			continue
		}

		if inSingleQuote {
			current.WriteByte(char)
			if char == '\'' {
				if index+1 < len(sql) && sql[index+1] == '\'' {
					current.WriteByte(sql[index+1])
					index++
				} else {
					inSingleQuote = false
				}
			}
			continue
		}

		if inDoubleQuote {
			current.WriteByte(char)
			if char == '"' {
				if index+1 < len(sql) && sql[index+1] == '"' {
					current.WriteByte(sql[index+1])
					index++
				} else {
					inDoubleQuote = false
				}
			}
			continue
		}

		if char == '-' && index+1 < len(sql) && sql[index+1] == '-' {
			current.WriteByte(char)
			current.WriteByte(sql[index+1])
			index++
			lineComment = true
			continue
		}

		if char == '/' && index+1 < len(sql) && sql[index+1] == '*' {
			current.WriteByte(char)
			current.WriteByte(sql[index+1])
			index++
			blockComment = true
			continue
		}

		if char == '\'' {
			current.WriteByte(char)
			inSingleQuote = true
			continue
		}

		if char == '"' {
			current.WriteByte(char)
			inDoubleQuote = true
			continue
		}

		if char == '$' {
			if tag, ok := readDollarQuoteTag(sql[index:]); ok {
				current.WriteString(tag)
				index += len(tag) - 1
				dollarQuoteTag = tag
				continue
			}
		}

		if char == ';' {
			statement := strings.TrimSpace(current.String())
			if statement != "" {
				statements = append(statements, statement)
			}
			current.Reset()
			continue
		}

		current.WriteByte(char)
	}

	statement := strings.TrimSpace(current.String())
	if statement != "" {
		statements = append(statements, statement)
	}

	return statements
}

func readDollarQuoteTag(sql string) (string, bool) {
	if !strings.HasPrefix(sql, "$") {
		return "", false
	}
	for index := 1; index < len(sql); index++ {
		char := sql[index]
		if char == '$' {
			return sql[:index+1], true
		}
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_' {
			return "", false
		}
	}
	return "", false
}
