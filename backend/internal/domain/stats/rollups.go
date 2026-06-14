package stats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const DashboardStatsStaleAfter = 2 * time.Minute

type DashboardSnapshotHealth struct {
	LagSeconds        int64 `json:"lag_seconds"`
	Stale             bool  `json:"stale"`
	StaleAfterSeconds int64 `json:"stale_after_seconds"`
}

type DashboardSnapshotCoverage struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
}

func NewDashboardSnapshotHealth(generatedAt time.Time, referenceNow time.Time) DashboardSnapshotHealth {
	generatedAt = generatedAt.UTC()
	if generatedAt.IsZero() {
		generatedAt = referenceNow.UTC()
	}
	lag := referenceNow.UTC().Sub(generatedAt)
	if lag < 0 {
		lag = 0
	}
	return DashboardSnapshotHealth{LagSeconds: int64(lag.Seconds()), Stale: lag > DashboardStatsStaleAfter, StaleAfterSeconds: int64(DashboardStatsStaleAfter.Seconds())}
}

type DashboardStatsRollupMetrics struct {
	RequestCount    int64
	ErrorCount      int64
	AuditEventCount int64
	ActiveProfiles  int64
}

type DashboardStatsRollup struct {
	GeneratedAt time.Time
	Coverage    DashboardSnapshotCoverage
	Health      DashboardSnapshotHealth
	Metrics     DashboardStatsRollupMetrics
}

func LoadDashboardRollupStats(ctx context.Context, exec queryExecutor, profileID int, window string, now time.Time) (DashboardStatsRollup, error) {
	from, to, err := dashboardWindowBounds(window, now.UTC())
	if err != nil {
		return DashboardStatsRollup{}, err
	}
	response := DashboardStatsRollup{GeneratedAt: now.UTC(), Coverage: DashboardSnapshotCoverage{From: from, To: to}}
	rows, err := exec.Query(ctx, `SELECT metric, value::bigint, source_high_water_mark, generated_at FROM management_stat_buckets WHERE dimension_key = 'profile_id' AND dimension_value = $1 AND bucket_size = $2 AND bucket_start = $3`, fmt.Sprintf("%d", profileID), window, from)
	if err != nil {
		return DashboardStatsRollup{}, fmt.Errorf("query dashboard stats rollup for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	found := false
	highWaterMark := from
	generatedAt := time.Time{}
	for rows.Next() {
		found = true
		var metric string
		var value int64
		var sourceHighWaterMark time.Time
		var rowGeneratedAt time.Time
		if err := rows.Scan(&metric, &value, &sourceHighWaterMark, &rowGeneratedAt); err != nil {
			return DashboardStatsRollup{}, fmt.Errorf("scan dashboard stats rollup: %w", err)
		}
		switch metric {
		case "request_count":
			response.Metrics.RequestCount = value
		case "error_count":
			response.Metrics.ErrorCount = value
		case "audit_event_count":
			response.Metrics.AuditEventCount = value
		case "active_profiles":
			response.Metrics.ActiveProfiles = value
		}
		if sourceHighWaterMark.After(highWaterMark) {
			highWaterMark = sourceHighWaterMark.UTC()
		}
		if rowGeneratedAt.After(generatedAt) {
			generatedAt = rowGeneratedAt.UTC()
		}
	}
	if err := rows.Err(); err != nil {
		return DashboardStatsRollup{}, fmt.Errorf("iterate dashboard stats rollup: %w", err)
	}
	if found {
		response.GeneratedAt = generatedAt
	}
	response.Health = NewDashboardSnapshotHealth(highWaterMark, now)
	response.Health.Stale = !found || response.Health.Stale
	return response, nil
}

func RefreshDashboardStatsRollup(ctx context.Context, exec queryExecutor, profileID int, window string, now time.Time) error {
	from, to, err := dashboardWindowBounds(window, now.UTC())
	if err != nil {
		return err
	}
	var requestCount int64
	var errorCount int64
	var usageHighWaterMark sql.NullTime
	if err := exec.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(CASE WHEN success_flag = FALSE THEN 1 ELSE 0 END), 0), MAX(created_at) FROM usage_request_events WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3`, profileID, from, to).Scan(&requestCount, &errorCount, &usageHighWaterMark); err != nil {
		return fmt.Errorf("refresh usage-event dashboard stats rollup: %w", err)
	}
	sourceHighWaterMark := from
	if usageHighWaterMark.Valid {
		sourceHighWaterMark = usageHighWaterMark.Time.UTC()
	}
	var auditEventCount int64
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM audit_logs WHERE profile_id = $1 AND created_at >= $2 AND created_at < $3 AND audit_enabled_at_request = TRUE`, profileID, from, to).Scan(&auditEventCount); err != nil {
		return fmt.Errorf("refresh audit dashboard stats rollup: %w", err)
	}
	var activeProfiles int64
	if err := exec.QueryRow(ctx, `SELECT COUNT(*) FROM profiles WHERE deleted_at IS NULL`).Scan(&activeProfiles); err != nil {
		return fmt.Errorf("refresh profile dashboard stats rollup: %w", err)
	}
	metrics := map[string]int64{"request_count": requestCount, "error_count": errorCount, "audit_event_count": auditEventCount, "active_profiles": activeProfiles}
	for metric, value := range metrics {
		if _, err := exec.Exec(ctx, `INSERT INTO management_stat_buckets (bucket_start, bucket_size, metric, dimension_key, dimension_value, value, source_high_water_mark, generated_at) VALUES ($1, $2, $3, 'profile_id', $4, $5, $6, $7) ON CONFLICT (bucket_start, bucket_size, metric, dimension_key, dimension_value) DO UPDATE SET value = EXCLUDED.value, source_high_water_mark = EXCLUDED.source_high_water_mark, generated_at = EXCLUDED.generated_at`, from, window, metric, fmt.Sprintf("%d", profileID), value, sourceHighWaterMark, now.UTC()); err != nil {
			return fmt.Errorf("upsert dashboard stats rollup metric %s: %w", metric, err)
		}
	}
	_, err = exec.Exec(ctx, `INSERT INTO management_stat_refresh_state (job_name, last_source_high_water_mark, last_success_at, last_error, updated_at) VALUES ('dashboard_stats', $1, $2, NULL, $2) ON CONFLICT (job_name) DO UPDATE SET last_source_high_water_mark = EXCLUDED.last_source_high_water_mark, last_success_at = EXCLUDED.last_success_at, last_error = NULL, updated_at = EXCLUDED.updated_at`, sourceHighWaterMark, now.UTC())
	return err
}

func dashboardWindowBounds(window string, now time.Time) (time.Time, time.Time, error) {
	switch window {
	case "1h":
		return now.Add(-time.Hour).Truncate(time.Hour), now.Truncate(time.Hour).Add(time.Hour), nil
	case "24h":
		return now.Add(-24 * time.Hour).Truncate(time.Hour), now.Truncate(time.Hour).Add(time.Hour), nil
	case "7d":
		return now.Add(-7 * 24 * time.Hour).Truncate(24 * time.Hour), now.Truncate(24 * time.Hour).Add(24 * time.Hour), nil
	case "30d":
		return now.Add(-30 * 24 * time.Hour).Truncate(24 * time.Hour), now.Truncate(24 * time.Hour).Add(24 * time.Hour), nil
	default:
		return time.Time{}, time.Time{}, &HTTPError{StatusCode: 400, Detail: "Unsupported dashboard stats window", Code: "stats_window_unsupported"}
	}
}

func IsStatsWindowUnsupported(err error) bool {
	var httpErr *HTTPError
	return errors.As(err, &httpErr) && httpErr.Code == "stats_window_unsupported"
}
