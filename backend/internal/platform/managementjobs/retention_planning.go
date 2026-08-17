package managementjobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

const (
	observeTokenTTLSeconds        = int64(24 * 60 * 60)
	observeProtectionGraceSeconds = int64(24 * 60 * 60)
)

// utcDayAlignedCutoff returns the UTC day-aligned logical cutoff for a policy
// (SPEC §3.3): date_trunc('day', server_now at UTC) - N days.
func utcDayAlignedCutoff(now time.Time, retentionDays int) time.Time {
	dayStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	return dayStart.AddDate(0, 0, -retentionDays)
}

// observeProtectionDeadline is the earliest allowed physical reclaim instant:
// logical publication + token TTL + grace (at least 48h).
func observeProtectionDeadline(publishedAt time.Time) time.Time {
	return publishedAt.Add(time.Duration(observeTokenTTLSeconds+observeProtectionGraceSeconds) * time.Second)
}

// ObserveTokenTTLSeconds exposes the fixed 24h token TTL (SPEC §3.3).
func ObserveTokenTTLSeconds() int64 { return observeTokenTTLSeconds }

// ObserveProtectionGraceSeconds exposes the fixed 24h extra protection grace.
func ObserveProtectionGraceSeconds() int64 { return observeProtectionGraceSeconds }

// ObserveProtectionDeadline returns the earliest allowed physical reclaim
// instant: logical publication + token TTL + grace (>= 48h).
func ObserveProtectionDeadline(publishedAt time.Time) time.Time {
	return observeProtectionDeadline(publishedAt)
}

// PlanScheduledRetention is the exported UTC-day planner entry point.
func (s *Store) PlanScheduledRetention(ctx context.Context) error {
	return s.planScheduledRetention(ctx)
}

// planScheduledRetention advances per-dataset policy resources and creates
// one durable automatic job per destructive change (SPEC §5.4/§7.4).
func (s *Store) planScheduledRetention(ctx context.Context) error {
	return pgxutil.InTx(ctx, s.pool, "retention_plan", func(tx pgx.Tx) error {
		var settingsRow struct {
			RequestLogs *int32
			AuditLogs   *int32
			Statistics  *int32
			Loadbalance *int32
			Revision    int64
		}
		err := tx.QueryRow(ctx, `SELECT request_logs_retention_days, audit_logs_retention_days,
			statistics_retention_days, loadbalance_events_retention_days, revision
			FROM log_retention_settings WHERE singleton_key = 'global'`).
			Scan(&settingsRow.RequestLogs, &settingsRow.AuditLogs, &settingsRow.Statistics, &settingsRow.Loadbalance, &settingsRow.Revision)
		if err != nil {
			return fmt.Errorf("load global log retention settings: %w", err)
		}

		now := s.now().UTC()
		datasets := []struct {
			dataset string
			days    *int32
		}{
			{"request_logs", settingsRow.RequestLogs},
			{"audit_logs", settingsRow.AuditLogs},
			{"usage_request_events", settingsRow.Statistics},
			{"loadbalance_events", settingsRow.Loadbalance},
		}

		for _, item := range datasets {
			var desiredCutoff *time.Time
			if item.days != nil {
				cutoff := utcDayAlignedCutoff(now, int(*item.days))
				desiredCutoff = &cutoff
			}
			var resourceCutoff *time.Time
			var resourceGeneration int64
			var resourceFenceGeneration int64
			var resourcePurgeState string
			err := tx.QueryRow(ctx, `SELECT configured_logical_cutoff, policy_generation, fence_generation, purge_state
				FROM log_retention_policy_resources WHERE dataset = $1 FOR UPDATE`, item.dataset).
				Scan(&resourceCutoff, &resourceGeneration, &resourceFenceGeneration, &resourcePurgeState)
			if err != nil {
				return fmt.Errorf("load policy resource %s: %w", item.dataset, err)
			}
			if (desiredCutoff == nil && resourceCutoff == nil) ||
				(desiredCutoff != nil && resourceCutoff != nil && desiredCutoff.Equal(*resourceCutoff)) {
				continue
			}
			// A manual purge owns the resource while running or recovering. The
			// scheduler may observe the newer settings on its next tick, but it
			// must not rewrite the fenced source underneath a partial purge.
			if resourcePurgeState == "running" || resourcePurgeState == "recovery_required" {
				continue
			}
			destructive := (resourceCutoff == nil && desiredCutoff != nil) ||
				(resourceCutoff != nil && desiredCutoff != nil && desiredCutoff.Before(*resourceCutoff))

			newGeneration := resourceGeneration + 1
			if _, err := tx.Exec(ctx, `UPDATE log_retention_policy_resources
				SET policy_generation = $2, fence_generation = fence_generation + 1,
					settings_revision = $3, configured_logical_cutoff = $4, updated_at = now()
				WHERE dataset = $1`, item.dataset, newGeneration, settingsRow.Revision, desiredCutoff); err != nil {
				return fmt.Errorf("advance policy resource %s: %w", item.dataset, err)
			}
			ownerSource, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, item.dataset, now)
			if err != nil {
				return fmt.Errorf("load refreshed policy source %s: %w", item.dataset, err)
			}
			if err := statsdomain.RefreshActualCoverageProjection(ctx, tx, ownerSource, now); err != nil {
				return fmt.Errorf("refresh policy coverage %s: %w", item.dataset, err)
			}

			// Extension and disable are non-destructive changes. They advance the
			// owner generation but must not manufacture work for an older cutoff;
			// queued old-generation work is cancelled while a running worker must
			// revalidate the owner fence before every physical step.
			if !destructive {
				if _, err := tx.Exec(ctx, `UPDATE management_jobs SET
					state = CASE WHEN state = 'queued' THEN 'cancelled' ELSE state END,
					terminal_disposition = CASE WHEN state = 'queued' THEN 'cancelled' ELSE NULL END,
					cancel_requested = TRUE,
					finished_at = CASE WHEN state = 'queued' THEN now() ELSE finished_at END,
					updated_at = now()
					WHERE type = 'log_retention' AND contract_version = 2 AND origin = 'automatic'
					  AND resource_key = $1 AND state IN ('queued','running','cancel_requested')
					  AND COALESCE(policy_generation, 0) < $2`, item.dataset, newGeneration); err != nil {
					return fmt.Errorf("cancel stale automatic retention work for %s: %w", item.dataset, err)
				}
				continue
			}

			if _, err := s.createAutomaticRetentionJobTx(ctx, tx, item.dataset, *desiredCutoff, settingsRow.Revision, newGeneration, resourceFenceGeneration+1, resourcePurgeState, now); err != nil {
				return err
			}
		}
		return nil
	})
}
