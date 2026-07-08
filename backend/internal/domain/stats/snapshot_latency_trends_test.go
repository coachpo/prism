package stats

import (
	"testing"
	"time"
)

func TestBuildLatencyTrendSeriesAlignsBucketsAndPercentiles(t *testing.T) {
	startAt := time.Date(2026, time.July, 8, 10, 0, 0, 0, time.UTC)
	endAt := startAt.Add(2 * time.Hour)
	latencies := []int{100, 200, 800}
	events := []snapshotEvent{
		{CreatedAt: startAt.Add(5 * time.Minute), ModelID: "model-a", ModelLabel: "Model A", ResponseTimeMS: &latencies[0], SuccessFlag: true},
		{CreatedAt: startAt.Add(15 * time.Minute), ModelID: "model-a", ModelLabel: "Model A", ResponseTimeMS: &latencies[1], SuccessFlag: true},
		{CreatedAt: startAt.Add(45 * time.Minute), ModelID: "model-a", ModelLabel: "Model A", ResponseTimeMS: &latencies[2], SuccessFlag: false},
	}

	requestTrends := buildRequestTrendSeries(events, &startAt, endAt, "hour")
	latencyTrends := buildLatencyTrendSeries(events, &startAt, endAt, "hour")

	if len(latencyTrends) == 0 {
		t.Fatal("expected latency trend series")
	}
	allLatency := latencyTrends[0]
	allRequests := requestTrends[0]
	if allLatency.Key != "all" {
		t.Fatalf("expected first latency series to be all, got %q", allLatency.Key)
	}
	if len(allLatency.Points) != len(allRequests.Points) {
		t.Fatalf("expected latency buckets to align with request buckets: latency=%d request=%d", len(allLatency.Points), len(allRequests.Points))
	}
	for index, point := range allLatency.Points {
		if !point.BucketStart.Equal(allRequests.Points[index].BucketStart) {
			t.Fatalf("bucket %d mismatch: latency=%s request=%s", index, point.BucketStart, allRequests.Points[index].BucketStart)
		}
	}
	firstPoint := allLatency.Points[0]
	if firstPoint.P50MS == nil || *firstPoint.P50MS != 200 {
		t.Fatalf("expected first bucket p50=200ms, got %#v", firstPoint.P50MS)
	}
	if firstPoint.P95MS == nil || *firstPoint.P95MS != 740 {
		t.Fatalf("expected first bucket p95=740ms, got %#v", firstPoint.P95MS)
	}
	if allLatency.Points[1].P50MS != nil || allLatency.Points[1].P95MS != nil {
		t.Fatalf("expected empty bucket latency percentiles to be nil, got %+v", allLatency.Points[1])
	}
}
