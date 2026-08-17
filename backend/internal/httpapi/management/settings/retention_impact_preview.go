package settings

import (
	"context"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	"github.com/jackc/pgx/v5"
)

// policyImpact builds a bounded policy-change impact for one dataset.
func (s *retentionService) policyImpact(ctx context.Context, tx pgx.Tx, dataset string, before, after *int, now time.Time) (retentionImpactDetails, error) {
	cutoff := utcDayCutoff(now, *after)
	details := retentionImpactDetails{
		Change: map[string]any{
			"kind":       "policy_change",
			"before":     taggedPolicyValue(before),
			"after_days": after,
		},
		ResolvedCutoff:           formatTimePtr(&cutoff),
		MatchedLogicalBytes:      unavailableImpactBytes("logical_bytes_unavailable"),
		ReclaimablePhysicalBytes: unavailableImpactBytes("physical_bytes_unavailable"),
		SemanticFactsComplete:    true,
		Consumers:                consumerLabels(dataset),
	}
	coverageAfter, coverageErr := s.logicalCoverageAfter(ctx, tx, dataset, now, &cutoff)
	if coverageErr != nil {
		return retentionImpactDetails{}, coverageErr
	}
	details.LogicalCoverageAfter = coverageAfter
	details.SemanticFactsComplete = coverageAfter.CoverageReady
	var matched retentionImpactCount
	var retained retentionImpactCount
	var whole map[string]any
	var boundary []map[string]any
	matched, retained, whole, boundary = unavailableRetentionImpactEstimate()
	_ = withSettingsReadSavepoint(ctx, tx, "settings_impact_policy", func() error {
		var err error
		matched, retained, whole, boundary, err = estimateImpact(ctx, tx, dataset, &cutoff, false)
		return err
	})
	details.MatchedRows = matched
	details.RetainedRows = retained
	details.WholePartitions = whole
	details.BoundaryPartitions = toAnySlice(boundary)
	details.StorageLayers = []any{}
	details.NonCascades = nonCascades(dataset)
	details.Warnings = []string{"延长保留期不会恢复已经清理的记录"}
	if dataset != retentionDatasetAuditLogs {
		deadline := managementjobs.ObserveProtectionDeadline(now)
		details.PhysicalReclaimNotBefore = formatTimePtr(&deadline)
	}
	return details, nil
}

// manualImpact builds a bounded manual-cleanup impact.
func (s *retentionService) manualImpact(ctx context.Context, tx pgx.Tx, dataset string, selection manualCleanupSelection, now time.Time) (retentionImpactDetails, error) {
	var cutoff *time.Time
	deleteAll := false
	switch selection.Mode {
	case "delete_all":
		if selection.Days != nil || selection.Cutoff != nil {
			return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
		}
		deleteAll = true
	case "keep_days":
		if selection.Days == nil || selection.Cutoff != nil || !isRetentionPreset(*selection.Days) {
			return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
		}
		value := utcDayCutoff(now, *selection.Days)
		cutoff = &value
	case "cutoff":
		if selection.Cutoff == nil || selection.Days != nil {
			return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
		}
		parsed, err := parseRetentionCutoff(*selection.Cutoff, now)
		if err != nil {
			return retentionImpactDetails{}, &settingsConflictError{code: "invalid_retention_cutoff"}
		}
		cutoff = &parsed
	default:
		return retentionImpactDetails{}, &settingsConflictError{code: "validation_failed"}
	}

	impactCutoff := cutoff
	if deleteAll {
		// Preview delete-all against the same present-time fence semantics as
		// the worker: historical partitions and rows before the preview fence
		// are affected, while the current/future horizon remains protected.
		fence := now.UTC()
		impactCutoff = &fence
	}
	details := retentionImpactDetails{
		Change: map[string]any{
			"kind": "manual_cleanup",
			"mode": selection.Mode,
		},
		ResolvedCutoff:           formatRetentionCutoff(cutoff),
		MatchedLogicalBytes:      unavailableImpactBytes("logical_bytes_unavailable"),
		ReclaimablePhysicalBytes: unavailableImpactBytes("physical_bytes_unavailable"),
		SemanticFactsComplete:    true,
		Consumers:                consumerLabels(dataset),
		Warnings: []string{
			"预览是提交前估算；delete-all 的最终 purge_to_time 由 worker 在 execution fence 锁定",
			"手动清理不享受 scheduled query-token grace",
		},
	}
	coverageAfter, coverageErr := s.logicalCoverageAfter(ctx, tx, dataset, now, impactCutoff)
	if coverageErr != nil {
		return retentionImpactDetails{}, coverageErr
	}
	coverageReady := coverageAfter.CoverageReady
	if deleteAll {
		// The execution fence is taken by the worker after the job is accepted.
		// A preview must not turn that future fence into an exact empty logical
		// range, even when the current owner model is fresh.
		coverageAfter.FromTime = nil
		coverageAfter.Accuracy = "unavailable"
		coverageAfter.Basis = "unavailable"
		coverageAfter.Gaps = append(coverageAfter.Gaps, map[string]any{
			"reason": "execution_fence_pending",
		})
	}
	details.LogicalCoverageAfter = coverageAfter
	details.SemanticFactsComplete = coverageReady
	var matched retentionImpactCount
	var retained retentionImpactCount
	var whole map[string]any
	var boundary []map[string]any
	matched, retained, whole, boundary = unavailableRetentionImpactEstimate()
	_ = withSettingsReadSavepoint(ctx, tx, "settings_impact_manual", func() error {
		var err error
		matched, retained, whole, boundary, err = estimateImpact(ctx, tx, dataset, impactCutoff, deleteAll)
		return err
	})
	details.MatchedRows = matched
	details.RetainedRows = retained
	details.WholePartitions = whole
	details.BoundaryPartitions = toAnySlice(boundary)
	details.StorageLayers = []any{}
	details.NonCascades = nonCascades(dataset)
	// Manual purge has no scheduled Observe-token grace. Its protection is the
	// owner purge fence and final publish, so do not expose a fabricated
	// 48-hour deadline (audit likewise has its own fence contract).
	return details, nil
}

func (s *retentionService) logicalCoverageAfter(ctx context.Context, tx pgx.Tx, dataset string, now time.Time, cutoff *time.Time) (retentionCoverageProjection, error) {
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
	if err != nil {
		return retentionCoverageProjection{}, err
	}
	coverage, err := statsdomain.LoadActualCoverageProjection(ctx, tx, source)
	if err != nil {
		return retentionCoverageProjection{}, err
	}
	coverageReady := coverage.Complete && coverage.Freshness == "fresh" && coverage.Revision != "" && coverage.Hash != "" && coverage.GeneratedAt != nil
	earliest := coverage.Earliest
	latest := coverage.Latest
	gaps := make([]any, 0, len(coverage.Gaps)+1)
	for _, gap := range coverage.Gaps {
		gaps = append(gaps, gap)
	}
	if cutoff != nil {
		if earliest == nil || latest == nil {
			// An empty/unknown owner projection must remain empty/unknown. Do
			// not turn a policy cutoff into a fabricated retained interval.
			earliest = nil
			latest = nil
			gaps = append(gaps, map[string]any{
				"from_time": coverage.Earliest,
				"to_time":   coverage.Latest,
				"reason":    "no_retained_intersection",
			})
		} else if latest.Before(*cutoff) {
			earliest = nil
			latest = nil
			gaps = append(gaps, map[string]any{
				"from_time": coverage.Earliest,
				"to_time":   coverage.Latest,
				"reason":    "no_retained_intersection",
			})
		} else if earliest.Before(*cutoff) {
			clamped := cutoff.UTC()
			earliest = &clamped
		}
	}
	if coverage.GapReason != nil {
		gaps = append(gaps, map[string]any{"from_time": earliest, "to_time": latest, "reason": *coverage.GapReason})
	}
	accuracy := coveragePrecision(coverage.Precision, coverage.Complete, coverage.Freshness)
	if !coverage.Complete || coverage.Freshness != "fresh" {
		// A dirty, unavailable or purge-fenced owner projection cannot prove
		// semantic coverage facts even when its last precision label was
		// owner_bounds. The UI must keep confirmation disabled until the owner
		// publishes a fresh bounded read model.
		accuracy = "unavailable"
	}
	basis := "unavailable"
	if coverage.Complete && coverage.Freshness == "fresh" {
		basis = "owner_read_model"
	}
	toTime := now.UTC().Format(time.RFC3339)
	if latest != nil {
		toTime = latest.UTC().Format(time.RFC3339)
	}
	return retentionCoverageProjection{
		FromTime:      formatTimePtr(earliest),
		ToTime:        toTime,
		Gaps:          gaps,
		Accuracy:      accuracy,
		Basis:         basis,
		CoverageReady: coverageReady,
	}, nil
}

func isRetentionPreset(days int) bool {
	return days == 1 || days == 7 || days == 30 || days == 90
}

func toAnySlice(values []map[string]any) []any {
	result := make([]any, 0, len(values))
	for _, value := range values {
		result = append(result, value)
	}
	return result
}

func consumerLabels(dataset string) []string {
	switch dataset {
	case retentionDatasetRequestLogs:
		return []string{"Requests", "recent activity", "ingress/attempt chain", "精确请求 deep link"}
	case retentionDatasetUsageRequestEvents:
		return []string{"Overview/Analytics", "模型/Endpoint/Proxy Key/Terminal Target 统计", "token 与 spend"}
	case retentionDatasetAuditLogs:
		return []string{"Audit metadata/header/body 证据"}
	default:
		return []string{"Observe Events", "已记录 retry/ban/unban/recovery/admission evidence"}
	}
}

func nonCascades(dataset string) []any {
	if dataset == retentionDatasetRequestLogs {
		return []any{map[string]any{
			"dataset":       "audit_logs",
			"effect":        "preserved",
			"retained_rows": retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "owner_preservation_relation"},
		}}
	}
	if dataset == retentionDatasetAuditLogs {
		return []any{map[string]any{
			"dataset":       "request_logs",
			"effect":        "preserved",
			"retained_rows": retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "owner_preservation_relation"},
		}}
	}
	return []any{}
}
