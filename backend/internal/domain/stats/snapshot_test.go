package stats

import (
	"encoding/json"
	"reflect"
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

func TestUsageSnapshotUncategorizedTokens(t *testing.T) {
	if usageSnapshotTokenComponentBasis != "disjoint" {
		t.Fatalf("expected token component basis %q, got %q", "disjoint", usageSnapshotTokenComponentBasis)
	}
	cases := []struct {
		name                                    string
		total, input, output, cached, reasoning int
		wantUncategorized                       int
	}{
		{"complete components reconstruct the total", 1200, 600, 150, 400, 50, 0},
		{"bare provider total surfaces as the whole total", 1200, 0, 0, 0, 0, 1200},
		{"evaluation-measured residual", 40160, 18262, 4474, 13200, 1824, 2400},
		{"components exceeding the total clamp to zero", 150, 100, 100, 0, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := usageSnapshotUncategorizedTokens(tc.total, tc.input, tc.output, tc.cached, tc.reasoning)
			if got != tc.wantUncategorized {
				t.Fatalf("expected uncategorized=%d, got %d", tc.wantUncategorized, got)
			}
		})
	}
}

func TestCloneDashboardRoutingHealthMapPreservesEveryField(t *testing.T) {
	source := DashboardRoutingHealthMap{
		Nodes:                     []DashboardRoutingNode{{ID: "node-1", Name: "model-a"}},
		Links:                     []DashboardRoutingLink{{ID: "link-1", ModelID: "model-a", EndpointID: 3}},
		EndpointCount:             11,
		ModelCount:                12,
		ActiveConnectionTotal:     13,
		ActiveTerminalTargetTotal: 14,
		TrafficRequestTotal24H:    15,
	}
	cloned := cloneDashboardRoutingHealthMap(source)
	if !reflect.DeepEqual(cloned, source) {
		t.Fatalf("expected clone to preserve every field, got %+v want %+v", cloned, source)
	}
	// The slice backing arrays must be detached: mutating the clone must not
	// reach the source.
	cloned.Nodes[0].Name = "mutated"
	cloned.Links[0].EndpointID = 999
	if source.Nodes[0].Name == "mutated" || source.Links[0].EndpointID == 999 {
		t.Fatalf("expected clone slice backing arrays to be detached, got %+v", source)
	}
}
