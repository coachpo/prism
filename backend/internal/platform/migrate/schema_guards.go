package migrate

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

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
