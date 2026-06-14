package stats

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDashboardSnapshotStatsOnlyBuilder(t *testing.T) {
	generatedAt := time.Date(2026, 6, 14, 12, 0, 0, 0, time.UTC)
	latestUsageEventID := 42
	latestUsageEventCreatedAt := generatedAt.Add(-time.Minute)
	snapshot := NewDashboardSnapshot(DashboardAggregateSnapshot{
		ProfileID:        7,
		GeneratedAt:      generatedAt,
		SnapshotRevision: "01K00000000000000000000000",
		SourceWatermark: DashboardSnapshotSourceWatermark{
			LatestUsageEventCreatedAt: &latestUsageEventCreatedAt,
			LatestUsageEventID:        &latestUsageEventID,
		},
	}, generatedAt)

	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal dashboard snapshot: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"recent_requests", "request_log_id", "ingress_request_id", "request_cursor"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("dashboard snapshot must stay stats-only; found %q in %s", forbidden, body)
		}
	}
	if snapshot.SourceWatermark.LatestUsageEventID == nil || *snapshot.SourceWatermark.LatestUsageEventID != latestUsageEventID {
		t.Fatalf("expected stats-native source watermark in dashboard snapshot, got %+v", snapshot.SourceWatermark)
	}
}

func TestDashboardStreamShareFromUsageEvents(t *testing.T) {
	metricSnapshot := newDashboardMetricSnapshot(DashboardAggregateSnapshot{
		StatsSummary24H:           StatsSummaryResponse{TotalRequests: 99},
		StreamRequestCount24H:     1,
		UsageEventRequestCount24H: 2,
	})
	if metricSnapshot.StreamShare != 50 {
		t.Fatalf("expected stream share from usage-event counts, got %.2f", metricSnapshot.StreamShare)
	}

	zeroUsageMetricSnapshot := newDashboardMetricSnapshot(DashboardAggregateSnapshot{StreamRequestCount24H: 1})
	if zeroUsageMetricSnapshot.StreamShare != 0 {
		t.Fatalf("expected zero stream share without usage events, got %.2f", zeroUsageMetricSnapshot.StreamShare)
	}
}
