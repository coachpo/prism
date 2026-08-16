package stats

import (
	"testing"
	"time"
)

func TestResolveQueryBoundsFromActualCoverageUsesOwnerEarliestForAll(t *testing.T) {
	referenceNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	earliest := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	latest := time.Date(2026, 8, 12, 11, 59, 0, 0, time.UTC)
	source := RetentionFloorEpochSource{Domain: "request_logs", SourceRevision: "source-7"}
	actual := ActualCoverageProjection{
		Earliest:  &earliest,
		Latest:    &latest,
		Revision:  "coverage-7",
		Hash:      "hash-7",
		Complete:  true,
		Freshness: "fresh",
	}

	bounds, err := ResolveQueryBoundsFromActualCoverage("all", nil, nil, referenceNow, source, actual)
	if err != nil {
		t.Fatalf("resolve all bounds: %v", err)
	}
	if !bounds.UsageFrom.Equal(earliest) || !bounds.UsageTo.Equal(referenceNow) {
		t.Fatalf("all bounds = %s..%s, want owner earliest %s..%s", bounds.UsageFrom, bounds.UsageTo, earliest, referenceNow)
	}
	if !bounds.Complete || len(bounds.Gaps) != 0 {
		t.Fatalf("all bounds completeness = %v, gaps = %#v; want complete with no gaps", bounds.Complete, bounds.Gaps)
	}

	coverage := QueryCoverageFromActualBounds(bounds, source, actual)
	if coverage.Precision != nil || coverage.State != "known" || !coverage.Complete {
		t.Fatalf("coverage = %#v; page precision or unknown state leaked", coverage)
	}
}

func TestResolveQueryBoundsFromActualCoverageMarksUncoveredHistory(t *testing.T) {
	referenceNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	earliest := time.Date(2026, 8, 1, 9, 30, 0, 0, time.UTC)
	latest := time.Date(2026, 8, 12, 11, 59, 0, 0, time.UTC)
	source := RetentionFloorEpochSource{Domain: "audit_logs", SourceRevision: "source-8"}
	actual := ActualCoverageProjection{
		Earliest:  &earliest,
		Latest:    &latest,
		Revision:  "coverage-8",
		Hash:      "hash-8",
		Complete:  true,
		Freshness: "fresh",
	}

	bounds, err := ResolveQueryBoundsFromActualCoverage("30d", nil, nil, referenceNow, source, actual)
	if err != nil {
		t.Fatalf("resolve 30d bounds: %v", err)
	}
	if bounds.Complete || len(bounds.Gaps) != 1 || bounds.Gaps[0].Reason != "actual_coverage_unavailable" {
		t.Fatalf("30d bounds = %#v; want one actual-coverage gap", bounds)
	}
	coverage := QueryCoverageFromActualBounds(bounds, source, actual)
	if coverage.State != "legacy_unknown" || coverage.Precision != nil {
		t.Fatalf("coverage = %#v; want legacy_unknown without fabricated precision", coverage)
	}
}

func TestResolveQueryBoundsFromActualCoverageDoesNotShrinkDirtyOwnerRange(t *testing.T) {
	referenceNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	earliest := referenceNow.Add(-5 * 24 * time.Hour)
	latest := referenceNow.Add(-time.Minute)
	source := RetentionFloorEpochSource{Domain: "request_logs", SourceRevision: "source-dirty"}
	actual := ActualCoverageProjection{
		Earliest:  &earliest,
		Latest:    &latest,
		Revision:  "coverage-dirty",
		Hash:      "hash-dirty",
		Complete:  false,
		Freshness: "stale",
	}

	bounds, err := ResolveQueryBoundsFromActualCoverage("30d", nil, nil, referenceNow, source, actual)
	if err != nil {
		t.Fatalf("resolve dirty 30d bounds: %v", err)
	}
	wantFrom := referenceNow.Add(-30 * 24 * time.Hour)
	if !bounds.UsageFrom.Equal(wantFrom) || !bounds.UsageTo.Equal(referenceNow) {
		t.Fatalf("dirty bounds = %s..%s, want requested interval %s..%s", bounds.UsageFrom, bounds.UsageTo, wantFrom, referenceNow)
	}
	if bounds.Complete || len(bounds.Gaps) != 0 {
		t.Fatalf("dirty bounds completeness = %v, gaps = %#v; stale diagnostics must not become a synthetic range gap", bounds.Complete, bounds.Gaps)
	}
}

func TestNormalizeActualCoveragePresetPreservesSingleFromCompatibility(t *testing.T) {
	referenceNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	from := referenceNow.Add(-2 * time.Hour)
	preset, resolvedFrom, resolvedTo, err := normalizeActualCoveragePreset("", &from, nil, referenceNow)
	if err != nil {
		t.Fatalf("normalize single from: %v", err)
	}
	if preset != "custom" || !resolvedFrom.Equal(from) || !resolvedTo.Equal(referenceNow) {
		t.Fatalf("normalized = %q, %v..%v; want custom %v..%v", preset, resolvedFrom, resolvedTo, from, referenceNow)
	}
}

func TestNormalizeActualCoveragePresetAcceptsLegacyExplicitBounds(t *testing.T) {
	referenceNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	to := referenceNow.Add(-3 * time.Hour)
	from := to.Add(-90 * time.Minute)

	preset, resolvedFrom, resolvedTo, err := normalizeActualCoveragePreset("", &from, &to, referenceNow)
	if err != nil {
		t.Fatalf("normalize explicit bounds: %v", err)
	}
	if preset != "custom" || !resolvedFrom.Equal(from) || !resolvedTo.Equal(to) {
		t.Fatalf("normalized = %q, %v..%v; want custom %v..%v", preset, resolvedFrom, resolvedTo, from, to)
	}

	preset, resolvedFrom, resolvedTo, err = normalizeActualCoveragePreset("", nil, &to, referenceNow)
	if err != nil {
		t.Fatalf("normalize legacy to-only bound: %v", err)
	}
	wantFrom := to.Add(-24 * time.Hour)
	if preset != "custom" || !resolvedFrom.Equal(wantFrom) || !resolvedTo.Equal(to) {
		t.Fatalf("to-only normalized = %q, %v..%v; want custom %v..%v", preset, resolvedFrom, resolvedTo, wantFrom, to)
	}
}
