package realtime

import (
	"context"
	"strconv"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
)

func (s *Service) BuildAnalyticsSnapshot(ctx context.Context, profileID int, preset string, referenceNow time.Time) (AnalyticsSnapshotMessage, error) {
	preset = normalizeAnalyticsPreset(preset)
	referenceNow = referenceNow.UTC()
	snapshot, err := statsdomain.GetUsageSnapshot(ctx, s.pool, profileID, preset, referenceNow)
	if err != nil {
		return AnalyticsSnapshotMessage{}, err
	}
	endpointModels := map[string][]statsdomain.UsageModelStatistic{}
	for _, endpoint := range snapshot.EndpointStatistics {
		if endpoint.EndpointID == nil {
			continue
		}
		toTime := snapshot.TimeRange.EndAt.UTC()
		items, statsErr := statsdomain.GetEndpointModelStatistics(ctx, s.pool, statsdomain.EndpointModelStatisticsParams{
			ProfileID:  profileID,
			EndpointID: *endpoint.EndpointID,
			Preset:     preset,
			FromTime:   snapshot.TimeRange.StartAt,
			ToTime:     &toTime,
		}, referenceNow)
		if statsErr != nil {
			return AnalyticsSnapshotMessage{}, statsErr
		}
		endpointModels[strconv.Itoa(*endpoint.EndpointID)] = usageModelStatisticsFromEndpoint(items)
	}
	return AnalyticsSnapshotMessage{
		Type:                                analyticsSnapshotMessageType,
		Channel:                             analyticsChannel,
		ProfileID:                           profileID,
		Preset:                              preset,
		Sequence:                            s.nextAnalyticsSequence(profileID, preset),
		GeneratedAt:                         referenceNow,
		Snapshot:                            snapshot,
		EndpointModelStatisticsByEndpointID: endpointModels,
	}, nil
}
