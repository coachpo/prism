package stats

import (
	"time"
)

// QueryCoverage is the non-null query-scoped coverage projection that every
// successful Requests/Audit list response carries (SPEC: `known|legacy_unknown`
// union, never null; legacy data that cannot be rebuilt is explicitly
// `legacy_unknown` and must not enter true-empty).
type QueryCoverage struct {
	RequestedFromTime   time.Time               `json:"requested_from_time"`
	RequestedToTime     time.Time               `json:"requested_to_time"`
	EffectiveFromTime   time.Time               `json:"effective_from_time"`
	EffectiveToTime     time.Time               `json:"effective_to_time"`
	RetentionFromTime   *time.Time              `json:"retention_from_time,omitempty"`
	Complete            bool                    `json:"complete"`
	Gaps                []QueryCoverageGap      `json:"gaps"`
	Precision           *QueryCoveragePrecision `json:"precision,omitempty"`
	State               string                  `json:"state"` // known | legacy_unknown
	SourceRevision      string                  `json:"source_revision"`
	RetentionEpoch      string                  `json:"retention_epoch,omitempty"`
	RetentionGeneration string                  `json:"retention_generation,omitempty"`
	PurgeState          string                  `json:"purge_state,omitempty"`
}

type QueryCoverageGap struct {
	FromTime time.Time `json:"from_time"`
	ToTime   time.Time `json:"to_time"`
	Reason   string    `json:"reason"`
}

type QueryCoveragePrecision struct {
	RowCount int `json:"row_count"`
}

// QueryCoverageFromActualBounds converts the owner-resolved query bounds into
// the Requests list union. The row-list endpoint must not attach a page-size
// count to this projection: a page count is not evidence of domain
// completeness. A dirty, stale, or gapped owner projection is therefore
// explicit legacy_unknown even when the SQL query itself returns no rows.
func QueryCoverageFromActualBounds(bounds QueryBounds, source RetentionFloorEpochSource, actual ActualCoverageProjection) QueryCoverage {
	requestedFrom := bounds.RequestedFrom
	if requestedFrom == nil {
		resolved := bounds.UsageFrom.UTC()
		requestedFrom = &resolved
	}
	requestedTo := bounds.RequestedTo
	if requestedTo == nil {
		resolved := bounds.UsageTo.UTC()
		requestedTo = &resolved
	}
	state := "known"
	if !bounds.Complete || actual.Freshness != "fresh" {
		state = "legacy_unknown"
	}
	gaps := make([]QueryCoverageGap, 0, len(bounds.Gaps))
	for _, gap := range bounds.Gaps {
		gaps = append(gaps, QueryCoverageGap{FromTime: gap.FromTime.UTC(), ToTime: gap.ToTime.UTC(), Reason: gap.Reason})
	}
	return QueryCoverage{
		RequestedFromTime:   requestedFrom.UTC(),
		RequestedToTime:     requestedTo.UTC(),
		EffectiveFromTime:   bounds.UsageFrom.UTC(),
		EffectiveToTime:     bounds.UsageTo.UTC(),
		RetentionFromTime:   retentionTime(bounds.UsageRetentionFrom),
		Complete:            bounds.Complete,
		Gaps:                gaps,
		Precision:           nil,
		State:               state,
		SourceRevision:      source.SourceRevision,
		RetentionEpoch:      source.RetentionEpoch,
		RetentionGeneration: source.RetentionGeneration,
		PurgeState:          source.PurgeState,
	}
}

// KnownCoverage builds a complete known-state coverage for the requested
// window when the retained floor covers it.
func KnownCoverage(requestedFrom, requestedTo, effectiveFrom, effectiveTo time.Time, retentionFrom *time.Time, rowCount int, sourceRevision string) QueryCoverage {
	return QueryCoverage{
		RequestedFromTime: requestedFrom.UTC(),
		RequestedToTime:   requestedTo.UTC(),
		EffectiveFromTime: effectiveFrom.UTC(),
		EffectiveToTime:   effectiveTo.UTC(),
		RetentionFromTime: retentionTime(retentionFrom),
		Complete:          true,
		Gaps:              []QueryCoverageGap{},
		Precision:         &QueryCoveragePrecision{RowCount: rowCount},
		State:             "known",
		SourceRevision:    sourceRevision,
	}
}

// LegacyUnknownCoverage builds the explicit historical-unknown union: the
// requested window reaches before the retained floor (or the floor is
// unavailable), so coverage cannot be promised and must display partial /
// unknown rather than true-empty.
func LegacyUnknownCoverage(requestedFrom, requestedTo time.Time, retentionFrom *time.Time, sourceRevision string) QueryCoverage {
	complete := false
	if retentionFrom != nil && !requestedFrom.Before(*retentionFrom) {
		complete = true
	}
	return QueryCoverage{
		RequestedFromTime: requestedFrom.UTC(),
		RequestedToTime:   requestedTo.UTC(),
		EffectiveFromTime: requestedFrom.UTC(),
		EffectiveToTime:   requestedTo.UTC(),
		RetentionFromTime: retentionTime(retentionFrom),
		Complete:          complete,
		Gaps: []QueryCoverageGap{
			{
				FromTime: requestedFrom.UTC(),
				ToTime:   gapEndTime(retentionFrom, requestedFrom),
				Reason:   "outside_retention_floor",
			},
		},
		Precision:      nil,
		State:          "legacy_unknown",
		SourceRevision: sourceRevision,
	}
}

func retentionTime(retentionFrom *time.Time) *time.Time {
	if retentionFrom == nil {
		return nil
	}
	resolved := retentionFrom.UTC()
	return &resolved
}

func gapEndTime(retentionFrom *time.Time, requestedFrom time.Time) time.Time {
	if retentionFrom == nil {
		return requestedFrom.UTC()
	}
	return retentionFrom.UTC()
}
