package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
	"github.com/coachpo/prism/backend/internal/platform/managementsideeffects"
	"github.com/jackc/pgx/v5"
)

const dashboardSnapshotWindowTolerance = 2 * time.Minute

func (s *Service) loadOrBuildDashboardAggregateSnapshot(ctx context.Context, profileID int, referenceNow time.Time) (statsdomain.DashboardAggregateSnapshot, error) {
	if snapshot, ok := s.dashboardSnapshots.LoadFreshProfile(profileID, func(snapshot statsdomain.DashboardAggregateSnapshot) bool {
		return dashboardAggregateSnapshotFresh(snapshot, referenceNow)
	}); ok {
		return snapshot, nil
	}
	return pgxutil.InReadOnlyTxValue(ctx, s.pool, "stats dashboard snapshot", func(tx pgx.Tx) (statsdomain.DashboardAggregateSnapshot, error) {
		return s.loadOrBuildDashboardAggregateSnapshotInTx(ctx, tx, profileID, referenceNow)
	})
}

func (s *Service) loadOrBuildDashboardAggregateSnapshotInTx(ctx context.Context, tx pgx.Tx, profileID int, referenceNow time.Time) (statsdomain.DashboardAggregateSnapshot, error) {
	if snapshot, ok := s.dashboardSnapshots.LoadFreshProfile(profileID, func(snapshot statsdomain.DashboardAggregateSnapshot) bool {
		return dashboardAggregateSnapshotFresh(snapshot, referenceNow)
	}); ok {
		return snapshot, nil
	}
	snapshot, err := statsdomain.BuildDashboardAggregateSnapshot(ctx, tx, profileID, referenceNow)
	if err != nil {
		return statsdomain.DashboardAggregateSnapshot{}, err
	}
	s.dashboardSnapshots.StoreProfile(snapshot)
	return snapshot, nil
}

func dashboardAggregateSnapshotFresh(snapshot statsdomain.DashboardAggregateSnapshot, referenceNow time.Time) bool {
	return !statsdomain.NewDashboardSnapshotHealth(snapshot.GeneratedAt, referenceNow).Stale
}

func (s *Service) InvalidateDashboardSnapshot(profileID int) {
	s.evictDashboardAggregateSnapshot(profileID)
}

func (s *Service) InvalidateAllDashboardSnapshots() {
	if s == nil || s.dashboardSnapshots == nil {
		return
	}
	s.dashboardSnapshots.InvalidateAll()
}

func (s *Service) evictDashboardAggregateSnapshot(profileID int) {
	if s == nil || s.dashboardSnapshots == nil {
		return
	}
	s.dashboardSnapshots.InvalidateProfile(profileID)
}

func (s *Service) handleDashboardSnapshotInvalidation(_ context.Context, event managementsideeffects.Event) error {
	var payload managementsideeffects.DashboardSnapshotInvalidatePayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return managementsideeffects.PermanentError{Err: fmt.Errorf("decode dashboard snapshot invalidation payload: %w", err)}
	}
	if payload.ProfileID <= 0 {
		return managementsideeffects.PermanentError{Err: fmt.Errorf("dashboard snapshot invalidation profile_id required")}
	}
	s.evictDashboardAggregateSnapshot(payload.ProfileID)
	return nil
}

func withinDashboardTolerance(actual time.Duration, expected time.Duration) bool {
	return absDuration(actual-expected) <= dashboardSnapshotWindowTolerance
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
