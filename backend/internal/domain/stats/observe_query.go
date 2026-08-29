package stats

import (
	"fmt"
	"strings"
	"time"
)

// Observe query-context contract.
//
// A query context freezes one consistent dataset snapshot: requested bounds,
// effective usage bounds, retention coverage and the signing material for
// fragment cursors. Fragments only accept the opaque signed token; they never
// re-parse presets or wall clocks.

// Coverage describes one dataset's effective coverage over the requested
// window.
type Coverage struct {
	RequestedPreset     string             `json:"requested_preset"`
	FromTime            time.Time          `json:"from_time"`
	ToTime              time.Time          `json:"to_time"`
	RetentionFromTime   *time.Time         `json:"retention_from_time"`
	Source              string             `json:"source"` // raw | rollup | hybrid
	Complete            bool               `json:"complete"`
	Gaps                []CoverageGap      `json:"gaps"`
	Precision           *CoveragePrecision `json:"precision,omitempty"`
	RetentionEpoch      string             `json:"retention_epoch,omitempty"`
	RetentionGeneration string             `json:"retention_generation,omitempty"`
	PurgeState          string             `json:"purge_state,omitempty"`
	SourceRevision      string             `json:"source_revision,omitempty"`
}

type CoverageGap struct {
	FromTime time.Time `json:"from_time"`
	ToTime   time.Time `json:"to_time"`
	Reason   string    `json:"reason"`
}

type CoveragePrecision struct {
	TTFT       string `json:"ttft"`        // exact | approximate
	OutputRate string `json:"output_rate"` // exact | approximate
}

// QueryBounds resolves one requested window into effective dataset bounds.
type QueryBounds struct {
	RequestedPreset    string
	RequestedFrom      *time.Time
	RequestedTo        *time.Time
	UsageFrom          time.Time
	UsageTo            time.Time
	UsageRetentionFrom *time.Time
	Source             string
	Complete           bool
	Gaps               []CoverageGap
}

// ResolveQueryBoundsFromActualCoverage resolves a domain window against the
// owning actual-coverage projection. It is the only query-context path that
// gives `all` a lower bound: a policy duration or product fallback is not
// evidence that the domain contains rows. A dirty projection may still
// provide a diagnostic range, but that range must not be used to shrink the
// SQL interval because the owner has explicitly said it is not current.
func ResolveQueryBoundsFromActualCoverage(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time, source RetentionFloorEpochSource, actual ActualCoverageProjection) (QueryBounds, error) {
	return resolveQueryBoundsFromActualCoverageWithCustomLimit(preset, fromTime, toTime, referenceNow, source, actual, 30*24*time.Hour)
}

func resolveQueryBoundsFromActualCoverageWithCustomLimit(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time, source RetentionFloorEpochSource, actual ActualCoverageProjection, maxCustomRange time.Duration) (QueryBounds, error) {
	normalizedPreset := strings.TrimSpace(preset)
	if normalizedPreset == "" {
		normalizedPreset = "24h"
	}
	requestedFrom, requestedTo, err := resolveRequestedWindowWithCustomLimit(normalizedPreset, fromTime, toTime, referenceNow, maxCustomRange)
	if err != nil {
		return QueryBounds{}, err
	}
	if normalizedPreset == "all" {
		if actual.Earliest != nil {
			requestedFrom = actual.Earliest.UTC()
			if floor := effectiveCoverageFloor(source); floor != nil && requestedFrom.Before(*floor) {
				requestedFrom = *floor
			}
		} else {
			// An owner-complete empty domain is represented as an empty half-open
			// interval, not as a synthetic 30-day history.
			requestedFrom = requestedTo
		}
	}

	retentionFrom := effectiveCoverageFloor(source)
	effectiveFrom := requestedFrom
	effectiveTo := requestedTo
	gaps := make([]CoverageGap, 0, len(actual.Gaps)+2)
	complete := actual.Complete && actual.Freshness == "fresh"
	if retentionFrom != nil && effectiveFrom.Before(*retentionFrom) {
		gaps = append(gaps, CoverageGap{FromTime: effectiveFrom, ToTime: *retentionFrom, Reason: "retention_deleted"})
		effectiveFrom = *retentionFrom
		complete = false
	}
	if actual.Freshness == "fresh" && actual.Earliest != nil && effectiveFrom.Before(actual.Earliest.UTC()) {
		gapEnd := actual.Earliest.UTC()
		if gapEnd.Before(effectiveTo) {
			gaps = append(gaps, CoverageGap{FromTime: effectiveFrom, ToTime: gapEnd, Reason: "actual_coverage_unavailable"})
			effectiveFrom = gapEnd
			complete = false
		}
	}
	// latest_retained_at is an observed row timestamp, not an exclusive query
	// boundary. Keep the frozen `to_time` at the shared reference instant so a
	// row exactly at the owner watermark is not silently excluded by the
	// half-open SQL predicate. The owner freshness/completeness state still
	// communicates whether the tail is fully materialized.
	for _, gap := range actual.Gaps {
		from, fromOK := coverageGapTime(gap["from_time"])
		to, toOK := coverageGapTime(gap["to_time"])
		reason, reasonOK := gap["reason"].(string)
		if fromOK && toOK && from.Before(to) && reasonOK && strings.TrimSpace(reason) != "" {
			gaps = append(gaps, CoverageGap{FromTime: from, ToTime: to, Reason: reason})
			complete = false
		}
	}
	if !effectiveFrom.Before(effectiveTo) && normalizedPreset != "all" {
		// Keep a valid requested interval when actual coverage has no overlap;
		// the explicit gap/complete=false state prevents a zero/empty claim.
		effectiveFrom = requestedFrom
		effectiveTo = requestedTo
	}
	return QueryBounds{
		RequestedPreset:    normalizedPreset,
		RequestedFrom:      queryRequestedTimePointer(normalizedPreset, requestedFrom),
		RequestedTo:        queryRequestedTimePointer(normalizedPreset, requestedTo),
		UsageFrom:          effectiveFrom,
		UsageTo:            effectiveTo,
		UsageRetentionFrom: retentionFrom,
		Source:             "raw",
		Complete:           complete && len(gaps) == 0,
		Gaps:               gaps,
	}, nil
}

// normalizeActualCoveragePreset keeps legacy callers that only supplied
// from_time/to_time on the same owner-bound path as the explicit time_range
// contract. A single bound retains the old one-sided-window meaning, while a
// pair of explicit bounds is the legacy spelling of custom.
func normalizeActualCoveragePreset(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time) (string, *time.Time, *time.Time, error) {
	normalized := strings.TrimSpace(preset)
	if normalized == "" {
		switch {
		case fromTime == nil && toTime == nil:
			normalized = "24h"
		case fromTime == nil && toTime != nil:
			resolvedFrom := toTime.UTC().Add(-24 * time.Hour)
			fromTime = &resolvedFrom
			normalized = "custom"
		case fromTime != nil && toTime == nil:
			resolvedTo := referenceNow.UTC()
			toTime = &resolvedTo
			normalized = "custom"
		default:
			normalized = "custom"
		}
	}
	if normalized == "all" && (fromTime != nil || toTime != nil) {
		return "", nil, nil, &HTTPError{StatusCode: 422, Detail: "time_range=all cannot include explicit bounds"}
	}
	if normalized == "custom" && (fromTime == nil || toTime == nil) {
		return "", nil, nil, &HTTPError{StatusCode: 422, Detail: "time_range=custom requires from_time and to_time"}
	}
	return normalized, fromTime, toTime, nil
}

func queryRequestedTimePointer(preset string, value time.Time) *time.Time {
	if preset == "all" {
		return nil
	}
	return &value
}

func resolveRequestedWindow(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time) (time.Time, time.Time, error) {
	return resolveRequestedWindowWithCustomLimit(preset, fromTime, toTime, referenceNow, 30*24*time.Hour)
}

func resolveRequestedWindowWithCustomLimit(preset string, fromTime *time.Time, toTime *time.Time, referenceNow time.Time, maxCustomRange time.Duration) (time.Time, time.Time, error) {
	referenceNow = referenceNow.UTC()
	switch preset {
	case "1h", "6h", "24h", "7d", "30d":
		return referenceNow.Add(-presetDuration(preset)), referenceNow, nil
	case "all":
		return referenceNow, referenceNow, nil
	case "custom":
		if fromTime == nil || toTime == nil {
			return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: "preset=custom requires from_time and to_time"}
		}
		start := fromTime.UTC()
		end := toTime.UTC()
		if !end.After(start) {
			return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: "invalid_time_range"}
		}
		if maxCustomRange > 0 && end.Sub(start) > maxCustomRange {
			days := int(maxCustomRange / (24 * time.Hour))
			return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: fmt.Sprintf("custom range cannot exceed %d days", days)}
		}
		return start, end, nil
	default:
		return time.Time{}, time.Time{}, &HTTPError{StatusCode: 422, Detail: fmt.Sprintf("unknown preset %q", preset)}
	}
}

func effectiveCoverageFloor(source RetentionFloorEpochSource) *time.Time {
	floor := source.ConfiguredCutoff
	if source.PublishedFloor != nil && (floor == nil || source.PublishedFloor.After(*floor)) {
		floor = source.PublishedFloor
	}
	if floor == nil {
		return nil
	}
	resolved := floor.UTC()
	return &resolved
}

func coverageGapTime(value any) (time.Time, bool) {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func presetDuration(preset string) time.Duration {
	switch preset {
	case "1h":
		return time.Hour
	case "6h":
		return 6 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}
