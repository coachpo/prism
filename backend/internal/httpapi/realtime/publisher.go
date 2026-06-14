package realtime

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/pgxutil"
)

var errDashboardActivityNotFound = errors.New("dashboard activity not found")

func (s *Service) PublishDashboardSnapshot(ctx context.Context, profileID int) (bool, error) {
	s.InvalidateDashboardSnapshot(profileID)
	return s.PublishLatestDashboardSnapshot(ctx, profileID)
}

func (s *Service) PublishLatestDashboardSnapshot(ctx context.Context, profileID int) (bool, error) {
	message, err := s.BuildDashboardSnapshot(ctx, profileID)
	if err != nil {
		return false, err
	}
	if !s.HasDashboardSubscribers(profileID) {
		return false, nil
	}
	return s.manager.BroadcastToProfile(profileID, dashboardChannel, message) > 0, nil
}

func (s *Service) PublishPendingDashboardSnapshot(ctx context.Context, profileID int) (bool, error) {
	return s.PublishLatestDashboardSnapshot(ctx, profileID)
}

func (s *Service) PublishDashboardActivity(ctx context.Context, requestLogID int, profileID int) (bool, error) {
	if !s.HasDashboardSubscribers(profileID) {
		return false, nil
	}
	message, err := s.BuildDashboardActivity(ctx, requestLogID, profileID)
	if err != nil {
		if errors.Is(err, errDashboardActivityNotFound) {
			return false, nil
		}
		return false, err
	}
	return s.manager.BroadcastToProfile(profileID, dashboardChannel, message) > 0, nil
}

func (s *Service) InvalidateDashboardSnapshot(profileID int) {
	if s == nil || s.dashboardSnapshots == nil {
		return
	}
	s.dashboardSnapshots.InvalidateProfileSilently(profileID)
}

func (s *Service) HasDashboardSubscribers(profileID int) bool {
	return s.manager.HasSubscribers(profileID, dashboardChannel)
}

func (s *Service) BuildDashboardSnapshot(ctx context.Context, profileID int) (DashboardSnapshotMessage, error) {
	referenceNow := s.now().UTC()
	return pgxutil.InReadOnlyTxValue(ctx, s.pool, "realtime dashboard snapshot", func(tx pgx.Tx) (DashboardSnapshotMessage, error) {
		aggregate, err := s.loadOrBuildDashboardAggregateSnapshot(ctx, tx, profileID, referenceNow)
		if err != nil {
			return DashboardSnapshotMessage{}, err
		}
		return DashboardSnapshotMessage{
			Type:      dashboardSnapshotMessageType,
			ProfileID: profileID,
			Snapshot:  statsdomain.NewDashboardSnapshot(aggregate, referenceNow),
		}, nil
	})
}

func (s *Service) BuildDashboardActivity(ctx context.Context, requestLogID int, profileID int) (DashboardActivityMessage, error) {
	generatedAt := s.now().UTC()
	response, err := pgxutil.InReadOnlyTxValue(ctx, s.pool, "realtime dashboard activity", func(tx pgx.Tx) (statsdomain.DashboardRecentActivityResponse, error) {
		return statsdomain.GetDashboardRecentActivityForRequestLog(ctx, tx, profileID, requestLogID, generatedAt)
	})
	if err != nil {
		return DashboardActivityMessage{}, err
	}
	if len(response.Items) == 0 {
		return DashboardActivityMessage{}, fmt.Errorf("%w: request log %d not found for profile %d", errDashboardActivityNotFound, requestLogID, profileID)
	}
	return DashboardActivityMessage{
		Type:              dashboardActivityMessageType,
		ProfileID:         profileID,
		ActivityWatermark: response.ActivityWatermark,
		Activity:          response.Items[0],
	}, nil
}

func (s *Service) handleDashboardAggregateInvalidation(invalidation statsdomain.DashboardAggregateInvalidation) {
	if s == nil {
		return
	}
	if invalidation.All {
		for _, profileID := range s.manager.ActiveProfileIDs(dashboardChannel) {
			s.schedulePendingDashboardReplay(profileID)
		}
		return
	}
	s.schedulePendingDashboardReplay(invalidation.ProfileID)
}

func (s *Service) schedulePendingDashboardReplay(profileID int) {
	if s == nil || profileID <= 0 || !s.HasDashboardSubscribers(profileID) {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), defaultAsyncDashboardTimeout)
		defer cancel()
		_, _ = s.publishPendingDashboardSnapshot(ctx, profileID)
	}()
}

func (s *Service) loadOrBuildDashboardAggregateSnapshot(ctx context.Context, tx pgx.Tx, profileID int, referenceNow time.Time) (statsdomain.DashboardAggregateSnapshot, error) {
	referenceNow = referenceNow.UTC()
	if snapshot, ok := s.dashboardSnapshots.LoadProfile(profileID); ok {
		return snapshot, nil
	}
	snapshot, err := statsdomain.BuildDashboardAggregateSnapshot(ctx, tx, profileID, referenceNow)
	if err != nil {
		return statsdomain.DashboardAggregateSnapshot{}, err
	}
	s.dashboardSnapshots.StoreProfile(snapshot)
	return snapshot, nil
}
