package startup

import (
	"context"
	"fmt"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/jackc/pgx/v5"
)

// seedRetentionCoverage refreshes the owner-maintained bounded coverage
// models once after startup. It only reads aggregate bounds and updates the
// projection; it never creates a cleanup job or deletes data. Subsequent
// writes mark the projection dirty and retention publication refreshes it in
// the same transaction as the owner floor/epoch.
func (s Service) seedRetentionCoverage(ctx context.Context, conn *pgx.Conn) error {
	var exists bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.retention_coverage_read_models') IS NOT NULL`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return nil
	}
	for _, dataset := range []string{"request_logs", "audit_logs", "usage_request_events", "loadbalance_events"} {
		now := s.now().UTC()
		source, err := statsdomain.LoadRetentionSourceProjection(ctx, conn, dataset, now)
		if err != nil {
			return fmt.Errorf("load %s retention coverage source: %w", dataset, err)
		}
		if err := statsdomain.RefreshActualCoverageProjection(ctx, conn, source, now); err != nil {
			return fmt.Errorf("seed %s actual coverage: %w", dataset, err)
		}
	}
	return nil
}
