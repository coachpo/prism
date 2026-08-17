package settings

import (
	"context"
	"strings"
	"time"

	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/jackc/pgx/v5"
)

// ownerSnapshotFor builds the discriminated owner snapshot for a listed
// affected domain (Observe §6.1.1 realization + audit fence projection).
func (s *retentionService) ownerSnapshotFor(ctx context.Context, tx pgx.Tx, dataset string, now time.Time) (any, error) {
	coverageSnapshot := func(source statsdomain.RetentionFloorEpochSource) (map[string]any, statsdomain.ActualCoverageProjection, error) {
		coverage, err := statsdomain.LoadActualCoverageProjection(ctx, tx, source)
		if err != nil {
			return nil, statsdomain.ActualCoverageProjection{}, err
		}
		if coverage.Revision == "" || coverage.Hash == "" || coverage.GeneratedAt == nil {
			return nil, statsdomain.ActualCoverageProjection{}, &settingsConflictError{code: "retention_owner_unavailable"}
		}
		return map[string]any{
			"domain":            coverage.Domain,
			"earliest":          formatTimePtr(coverage.Earliest),
			"latest":            formatTimePtr(coverage.Latest),
			"coverage_revision": coverage.Revision,
			"coverage_hash":     coverage.Hash,
			"generated_at":      formatTimePtr(coverage.GeneratedAt),
			"source":            coverage.Source,
			"precision":         coverage.Precision,
			"complete":          coverage.Complete,
			"freshness":         coverage.Freshness,
			"gaps":              coverage.Gaps,
			"gap_reason":        coverage.GapReason,
		}, coverage, nil
	}
	if dataset == retentionDatasetAuditLogs {
		source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
		if err != nil {
			return nil, err
		}
		if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
			return nil, &settingsConflictError{code: "retention_job_conflict"}
		}
		coverage, _, err := coverageSnapshot(source)
		if err != nil {
			return nil, err
		}
		protection, err := auditProtectionProjection(ctx, tx, now)
		if err != nil {
			return nil, err
		}
		protectionMap := map[string]any{
			"contract_version":        protection.ContractVersion,
			"fence_generation":        protection.FenceGeneration,
			"reader_fence_state":      protection.ReaderFenceState,
			"materializer_generation": protection.MaterializerGeneration,
			"materializer_state":      protection.MaterializerState,
			"generated_at":            protection.GeneratedAt,
		}
		return map[string]any{
			"kind":                  "audit",
			"dataset":               dataset,
			"contract_version":      3,
			"retention_source":      retentionOwnerSourceProjectionMap(source),
			"audit_protection":      protectionMap,
			"actual_coverage":       coverage,
			"storage_fact_evidence": auditStorageFactEvidence(ctx, tx, source.SourceRevision),
		}, nil
	}
	source, err := statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, now)
	if err != nil {
		return nil, err
	}
	if source.PurgeState == "running" || source.PurgeState == "recovery_required" {
		return nil, &settingsConflictError{code: "retention_job_conflict"}
	}
	coverage, projection, err := coverageSnapshot(source)
	if err != nil {
		return nil, err
	}
	materializationCut, err := exactObserveMaterializationCut(dataset, projection.MaterializationCut)
	if err != nil {
		return nil, err
	}
	snapshot := map[string]any{
		"kind":                       "observe",
		"contract_version":           1,
		"dataset":                    dataset,
		"policy_generation":          source.RetentionGeneration,
		"retention_revocation_epoch": source.RetentionEpoch,
		"configured_cutoff":          formatTimePtr(source.ConfiguredCutoff),
		"published_floor":            formatTimePtr(source.PublishedFloor),
		"fence_generation":           source.FenceGeneration,
		"purge_state":                normalizedRetentionOwnerPurgeState(source.PurgeState),
		"coverage_revision":          projection.Revision,
		"coverage_hash":              projection.Hash,
		"generated_at":               formatTimePtr(projection.GeneratedAt),
		"actual_coverage":            coverage,
		"materialization_cut":        materializationCut,
	}
	return snapshot, nil
}

func normalizedRetentionOwnerPurgeState(state string) string {
	if state == "published" || state == "rolled_back" {
		return "idle"
	}
	return state
}

// exactObserveMaterializationCut preserves the owner-defined discriminated
// union. Settings validates its shape but never synthesizes a cut from its
// own read timestamp or compresses the variants into a nullable scalar.
func exactObserveMaterializationCut(dataset string, value map[string]any) (map[string]any, error) {
	kind, ok := value["kind"].(string)
	if !ok {
		return nil, &settingsConflictError{code: "retention_owner_unavailable"}
	}
	requireString := func(field string) bool {
		item, present := value[field]
		text, valid := item.(string)
		return present && valid && strings.TrimSpace(text) != ""
	}
	validOptional := func(field string) bool {
		item, present := value[field]
		if !present || item == nil {
			return true
		}
		text, valid := item.(string)
		return valid && strings.TrimSpace(text) != ""
	}
	requireUTCInstant := func(field string) bool {
		text, ok := value[field].(string)
		if !ok || strings.TrimSpace(text) == "" || !strings.HasSuffix(text, "Z") {
			return false
		}
		_, err := time.Parse(time.RFC3339Nano, text)
		return err == nil
	}
	optionalPair := func(left, right string) bool {
		leftValue, leftPresent := value[left]
		rightValue, rightPresent := value[right]
		if !leftPresent || !rightPresent {
			return false
		}
		if leftValue == nil || rightValue == nil {
			return leftValue == nil && rightValue == nil
		}
		return validOptional(left) && validOptional(right)
	}
	switch dataset {
	case retentionDatasetRequestLogs:
		if kind != "request_visibility_cut" || !requireString("request_committed_cut") || !requireUTCInstant("request_committed_cut") || len(value) != 2 {
			return nil, &settingsConflictError{code: "retention_owner_unavailable"}
		}
	case retentionDatasetUsageRequestEvents:
		if kind != "usage_hybrid_cut" || !requireString("raw_committed_cut") || !requireUTCInstant("raw_committed_cut") || !optionalPair("rollup_manifest_cut", "build_revision") || len(value) != 4 {
			return nil, &settingsConflictError{code: "retention_owner_unavailable"}
		}
	case retentionDatasetLoadbalanceEvents:
		if kind != "event_hybrid_cut" || !requireString("raw_committed_cut") || !requireUTCInstant("raw_committed_cut") || !optionalPair("rollup_manifest_cut", "build_revision") || len(value) != 4 {
			return nil, &settingsConflictError{code: "retention_owner_unavailable"}
		}
	default:
		return nil, &settingsConflictError{code: "retention_owner_unavailable"}
	}
	return value, nil
}
