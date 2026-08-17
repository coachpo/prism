package settings

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type retentionRow struct {
	RequestLogsRetentionDays       *int
	AuditLogsRetentionDays         *int
	StatisticsRetentionDays        *int
	LoadbalanceEventsRetentionDays *int
	Revision                       int64
	UpdatedAt                      time.Time
}

type policyResourceRow struct {
	PolicyGeneration        int64
	FenceGeneration         int64
	SettingsRevision        int64
	ConfiguredLogicalCutoff *time.Time
	PublishedRetentionFloor *time.Time
	RevocationEpoch         int64
	PurgeState              string
}

func loadRetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (retentionRow, error) {
	return scanRetentionRow(ctx, exec, false)
}

func loadRetentionRowForUpdate(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (retentionRow, error) {
	return scanRetentionRow(ctx, exec, true)
}

func scanRetentionRow(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, forUpdate bool) (retentionRow, error) {
	var row retentionRow
	var requestLogs, auditLogs, statistics, loadbalance *int32
	query := `SELECT request_logs_retention_days, audit_logs_retention_days,
		statistics_retention_days, loadbalance_events_retention_days, revision, updated_at
		FROM log_retention_settings WHERE singleton_key = 'global'`
	if forUpdate {
		query += " FOR UPDATE"
	}
	err := exec.QueryRow(ctx, query).
		Scan(&requestLogs, &auditLogs, &statistics, &loadbalance, &row.Revision, &row.UpdatedAt)
	if err != nil {
		return retentionRow{}, err
	}
	row.RequestLogsRetentionDays = nullableInt32ToInt(requestLogs)
	row.AuditLogsRetentionDays = nullableInt32ToInt(auditLogs)
	row.StatisticsRetentionDays = nullableInt32ToInt(statistics)
	row.LoadbalanceEventsRetentionDays = nullableInt32ToInt(loadbalance)
	return row, nil
}

func nullableInt32ToInt(value *int32) *int {
	if value == nil {
		return nil
	}
	converted := int(*value)
	return &converted
}

func scanPolicyResources(ctx context.Context, exec interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}) (map[string]policyResourceRow, error) {
	rows, err := exec.Query(ctx, `SELECT dataset, policy_generation, fence_generation, settings_revision,
		configured_logical_cutoff, published_retention_floor, retention_revocation_epoch, purge_state
		FROM log_retention_policy_resources`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]policyResourceRow{}
	for rows.Next() {
		var dataset string
		var row policyResourceRow
		if err := rows.Scan(&dataset, &row.PolicyGeneration, &row.FenceGeneration, &row.SettingsRevision, &row.ConfiguredLogicalCutoff,
			&row.PublishedRetentionFloor, &row.RevocationEpoch, &row.PurgeState); err != nil {
			return nil, err
		}
		result[dataset] = row
	}
	return result, rows.Err()
}
