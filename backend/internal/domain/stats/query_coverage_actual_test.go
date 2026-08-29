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

func TestResolveQueryBoundsCustomRangeLimitIsCallerSpecific(t *testing.T) {
	referenceNow := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	from := referenceNow.Add(-31 * 24 * time.Hour)
	source := RetentionFloorEpochSource{Domain: "request_logs", SourceRevision: "source-export"}
	actual := ActualCoverageProjection{Complete: true, Freshness: "fresh"}

	if _, err := ResolveQueryBoundsFromActualCoverage("custom", &from, &referenceNow, referenceNow, source, actual); err == nil {
		t.Fatal("ordinary 30-day custom range unexpectedly accepted 31 days")
	}
	if _, err := resolveQueryBoundsFromActualCoverageWithCustomLimit("custom", &from, &referenceNow, referenceNow, source, actual, 31*24*time.Hour); err != nil {
		t.Fatalf("export-specific 31-day custom range rejected: %v", err)
	}
	tooEarly := from.Add(-time.Nanosecond)
	if _, err := resolveQueryBoundsFromActualCoverageWithCustomLimit("custom", &tooEarly, &referenceNow, referenceNow, source, actual, 31*24*time.Hour); err == nil {
		t.Fatal("31 days plus one nanosecond unexpectedly accepted")
	}
}

func TestCoverageFromQueryBoundsRequiresFreshCompleteOwner(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	bounds := QueryBounds{RequestedPreset: "24h", UsageFrom: now.Add(-24 * time.Hour), UsageTo: now, Source: "raw", Complete: true, Gaps: []CoverageGap{}}
	snapshot := QueryContextDomainSnapshot{Domain: "usage_request_events", RetentionEpoch: "2", RetentionGeneration: "7", FenceGeneration: "11", SourceRevision: "source-7", CoverageRevision: "coverage-7", CoverageHash: "hash-7", CoverageGeneratedAt: &now, Complete: true, Freshness: "fresh", PurgeState: "idle"}

	coverage := CoverageFromQueryBounds(bounds, snapshot)
	if coverage.Precision == nil || coverage.Gaps == nil || coverage.RetentionEpoch != "2" || coverage.RetentionGeneration != "7" || coverage.PurgeState != "idle" || coverage.SourceRevision != "source-7" {
		t.Fatalf("coverage = %#v; want exact precision with frozen owner metadata", coverage)
	}
	for name, mutate := range map[string]func(*QueryBounds, *QueryContextDomainSnapshot){
		"incomplete": func(_ *QueryBounds, value *QueryContextDomainSnapshot) { value.Complete = false },
		"stale":      func(_ *QueryBounds, value *QueryContextDomainSnapshot) { value.Freshness = "stale" },
		"gapped": func(value *QueryBounds, _ *QueryContextDomainSnapshot) {
			value.Gaps = []CoverageGap{{FromTime: value.UsageFrom, ToTime: value.UsageFrom.Add(time.Hour), Reason: "retention_deleted"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidateBounds, candidateSnapshot := bounds, snapshot
			mutate(&candidateBounds, &candidateSnapshot)
			if got := CoverageFromQueryBounds(candidateBounds, candidateSnapshot); got.Precision != nil || got.Complete || (name == "gapped" && len(got.Gaps) != 1) {
				t.Fatalf("coverage = %#v; non-authoritative coverage must fail closed", got)
			}
		})
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
