package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ResolveChainQueryBounds is the shared Requests/export entry point for the
// owner-resolved window. The export path must consume the same actual bounds
// as the interactive chain list rather than reintroducing a floor-only clip.
func ResolveChainQueryBounds(ctx context.Context, exec queryExecutor, params ChainQueryParams, referenceNow time.Time) (ChainQueryParams, error) {
	source, err := LoadRetentionSourceProjection(ctx, exec, "request_logs", referenceNow.UTC())
	if err != nil {
		return ChainQueryParams{}, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return ChainQueryParams{}, &HTTPError{StatusCode: 503, Code: "request_log_purge_in_progress", Detail: "request logs are temporarily unavailable while retention cleanup is publishing"}
	}
	return resolveChainQueryBounds(ctx, exec, params, referenceNow.UTC(), source)
}

func resolveChainQueryBounds(ctx context.Context, exec queryExecutor, params ChainQueryParams, referenceNow time.Time, source RetentionFloorEpochSource) (ChainQueryParams, error) {
	actual, err := LoadActualCoverageProjection(ctx, exec, source)
	if err != nil {
		return ChainQueryParams{}, err
	}
	preset, fromTime, toTime, err := normalizeActualCoveragePreset(params.CoveragePreset, params.FromTime, params.ToTime, referenceNow)
	if err != nil {
		return ChainQueryParams{}, err
	}
	bounds, err := ResolveQueryBoundsFromActualCoverage(preset, fromTime, toTime, referenceNow, source, actual)
	if err != nil {
		return ChainQueryParams{}, err
	}
	params.CoveragePreset = bounds.RequestedPreset
	params.CoverageRequestedFrom = bounds.RequestedFrom
	params.CoverageRequestedTo = bounds.RequestedTo
	from := bounds.UsageFrom.UTC()
	to := bounds.UsageTo.UTC()
	params.FromTime = &from
	params.ToTime = &to
	return params, nil
}

// populateChainCoverage keeps the chain envelope on the same owner projections
// as ordinary Requests and Observe. The JSON fields are intentionally raw so
// the domain-specific coverage contracts can evolve without a second chain
// policy or a browser-computed range.
func populateChainCoverage(ctx context.Context, exec queryExecutor, params ChainQueryParams, now time.Time, requestSource RetentionFloorEpochSource, response *ChainResponse) error {
	usageSource, err := LoadRetentionSourceProjection(ctx, exec, "usage_request_events", now)
	if err != nil {
		return err
	}
	requestCoverage, err := chainCoverageProjection(ctx, exec, params, now, requestSource, "request_logs")
	if err != nil {
		return err
	}
	usageCoverage, err := chainCoverageProjection(ctx, exec, params, now, usageSource, "usage_request_events")
	if err != nil {
		return err
	}
	response.SourceCoverage = &requestCoverage
	response.AttemptCoverage = &requestCoverage
	response.DrilldownCoverage = &requestCoverage
	response.RawFinalizedCoverage = &usageCoverage
	return nil
}

func chainCoverageProjection(ctx context.Context, exec queryExecutor, params ChainQueryParams, now time.Time, source RetentionFloorEpochSource, domain string) (json.RawMessage, error) {
	actual, err := LoadActualCoverageProjection(ctx, exec, source)
	if err != nil {
		return nil, err
	}
	preset, fromTime, toTime, err := normalizeActualCoveragePreset(params.CoveragePreset, params.CoverageRequestedFrom, params.CoverageRequestedTo, now)
	if err != nil {
		return nil, err
	}
	bounds, err := ResolveQueryBoundsFromActualCoverage(preset, fromTime, toTime, now, source, actual)
	if err != nil {
		return nil, err
	}
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
	gaps := make([]map[string]any, 0, len(bounds.Gaps))
	for _, gap := range bounds.Gaps {
		gaps = append(gaps, map[string]any{
			"from_time": gap.FromTime.UTC().Format(time.RFC3339),
			"to_time":   gap.ToTime.UTC().Format(time.RFC3339),
			"reason":    gap.Reason,
		})
	}
	state := "known"
	if !bounds.Complete || actual.Freshness != "fresh" {
		state = "legacy_unknown"
	}
	payload := map[string]any{
		"domain":               domain,
		"requested_from_time":  requestedFrom.UTC().Format(time.RFC3339),
		"requested_to_time":    requestedTo.UTC().Format(time.RFC3339),
		"effective_from_time":  bounds.UsageFrom.UTC().Format(time.RFC3339),
		"effective_to_time":    bounds.UsageTo.UTC().Format(time.RFC3339),
		"retention_from_time":  formatChainTime(bounds.UsageRetentionFrom),
		"complete":             bounds.Complete,
		"gaps":                 gaps,
		"state":                state,
		"source_revision":      source.SourceRevision,
		"retention_epoch":      source.RetentionEpoch,
		"retention_generation": source.RetentionGeneration,
		"purge_state":          source.PurgeState,
		"actual_coverage": map[string]any{
			"from_time":           formatChainTime(actual.Earliest),
			"to_time":             formatChainTime(actual.Latest),
			"source":              actual.Source,
			"precision":           actual.Precision,
			"complete":            actual.Complete,
			"freshness":           actual.Freshness,
			"gap_reason":          actual.GapReason,
			"coverage_revision":   actual.Revision,
			"coverage_hash":       actual.Hash,
			"generated_at":        formatChainTime(actual.GeneratedAt),
			"materialization_cut": actual.MaterializationCut,
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal %s chain coverage: %w", domain, err)
	}
	return json.RawMessage(raw), nil
}

func formatChainTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC().Format(time.RFC3339)
}
