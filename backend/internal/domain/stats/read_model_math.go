package stats

import (
	"math"
	"sort"
	"strings"
	"time"
)

func normalizeTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	resolved := value.UTC()
	return &resolved
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func roundFloat(value float64, places int) float64 {
	factor := math.Pow10(places)
	return math.Round(value*factor) / factor
}

func successRate(successCount int, totalCount int) float64 {
	if totalCount <= 0 {
		return 0
	}
	return roundFloat((float64(successCount)/float64(totalCount))*100, 2)
}

func percentileContInt(values []int, percentile float64) *int {
	if len(values) == 0 {
		return nil
	}
	ordered := append([]int(nil), values...)
	sort.Ints(ordered)
	rank := float64(len(ordered)-1) * percentile
	lowerIndex := int(math.Floor(rank))
	upperIndex := int(math.Ceil(rank))
	lowerValue := float64(ordered[lowerIndex])
	upperValue := float64(ordered[upperIndex])
	interpolated := lowerValue + (upperValue-lowerValue)*(rank-float64(lowerIndex))
	resolved := int(math.Round(interpolated))
	return &resolved
}

func effectiveWindowStart(startAt *time.Time, endAt time.Time, events []snapshotEvent) time.Time {
	if startAt != nil {
		return startAt.UTC()
	}
	if len(events) == 0 {
		return endAt.UTC()
	}
	minValue := events[0].CreatedAt.UTC()
	for _, event := range events[1:] {
		if event.CreatedAt.Before(minValue) {
			minValue = event.CreatedAt.UTC()
		}
	}
	return minValue
}

func resolveTimePreset(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time) (*time.Time, *time.Time) {
	normalizedPreset := strings.TrimSpace(strings.ToLower(preset))
	if normalizedPreset == "" || normalizedPreset == "custom" {
		return normalizeTimePointer(fromTime), normalizeTimePointer(toTime)
	}
	referenceTime := referenceNow.UTC()
	if toTime != nil {
		referenceTime = toTime.UTC()
	}
	switch normalizedPreset {
	case "1h":
		from := referenceTime.Add(-time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "6h":
		from := referenceTime.Add(-6 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "7h":
		from := referenceTime.Add(-7 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "24h":
		from := referenceTime.Add(-24 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "last_7_days", "7d":
		from := referenceTime.Add(-7 * 24 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "last_30_days", "30d":
		from := referenceTime.Add(-30 * 24 * time.Hour)
		return &from, normalizeTimePointer(toTime)
	case "all":
		return nil, normalizeTimePointer(toTime)
	default:
		return normalizeTimePointer(fromTime), normalizeTimePointer(toTime)
	}
}

func requestOutputRateTPS(outputTokens int, hasOutputTokens bool, ttftMS *int, completionDurationMS *int) *float64 {
	if !hasOutputTokens || ttftMS == nil || completionDurationMS == nil {
		return nil
	}
	postTTFT := *completionDurationMS - *ttftMS
	if postTTFT <= 0 {
		return nil
	}
	resolved := roundFloat((float64(outputTokens)*1000)/float64(postTTFT), 2)
	return &resolved
}

func bucketFloor(value time.Time, granularity string) time.Time {
	normalized := value.UTC()
	switch granularity {
	case "hour":
		return time.Date(normalized.Year(), normalized.Month(), normalized.Day(), normalized.Hour(), 0, 0, 0, time.UTC)
	default:
		return time.Date(normalized.Year(), normalized.Month(), normalized.Day(), 0, 0, 0, 0, time.UTC)
	}
}

func bucketStep(granularity string) time.Duration {
	if granularity == "hour" {
		return time.Hour
	}
	return 24 * time.Hour
}

func bucketMinutes(granularity string) float64 {
	if granularity == "hour" {
		return 60
	}
	return 1440
}

func bucketRange(startAt *time.Time, endAt time.Time, eventTimes []time.Time, granularity string) []time.Time {
	var current time.Time
	if startAt == nil {
		if len(eventTimes) > 0 {
			minValue := eventTimes[0].UTC()
			for _, candidate := range eventTimes[1:] {
				if candidate.Before(minValue) {
					minValue = candidate.UTC()
				}
			}
			current = bucketFloor(minValue, granularity)
		} else {
			current = bucketFloor(endAt, granularity)
		}
	} else {
		current = bucketFloor(startAt.UTC(), granularity)
	}
	endBucket := bucketFloor(endAt, granularity)
	step := bucketStep(granularity)
	buckets := make([]time.Time, 0)
	for !current.After(endBucket) {
		buckets = append(buckets, current)
		current = current.Add(step)
	}
	return buckets
}

func timeSliceFromEvents(events []snapshotEvent) []time.Time {
	items := make([]time.Time, 0, len(events))
	for _, event := range events {
		items = append(items, event.CreatedAt)
	}
	return items
}

func pricedStatusPointer(status string) *bool {
	priced := status == "priced"
	return &priced
}

func intPtr(value int) *int {
	resolved := value
	return &resolved
}
