package settings

import (
	"context"
	"fmt"
	"time"

	auditdomain "github.com/coachpo/prism/backend/internal/domain/audit"
	statsdomain "github.com/coachpo/prism/backend/internal/domain/stats"
	"github.com/coachpo/prism/backend/internal/platform/managementjobs"
	"github.com/jackc/pgx/v5"
)

// taggedPolicyValue renders a stored integer as the canonical tagged read
// union (valid value or repair_required with exact raw integer).
func taggedPolicyValue(value *int) retentionPolicyReadValue {
	if value == nil {
		return retentionPolicyReadValue{State: "valid", Value: nil}
	}
	if *value >= 1 && *value <= retentionMaxDays {
		return retentionPolicyReadValue{State: "valid", Value: value}
	}
	raw := fmt.Sprintf("%d", *value)
	issue := "retention_days_above_supported_max"
	return retentionPolicyReadValue{State: "repair_required", RawInteger: &raw, Issue: &issue}
}

func (s *retentionService) buildSettingsResponse(ctx context.Context, tx pgx.Tx, serverNow time.Time) (logRetentionSettingsResponse, error) {
	row, err := loadRetentionRow(ctx, tx)
	if err != nil {
		return logRetentionSettingsResponse{}, err
	}
	resources, err := scanPolicyResources(ctx, tx)
	if err != nil {
		return logRetentionSettingsResponse{}, err
	}

	response := logRetentionSettingsResponse{
		State:     "ready",
		Scope:     "instance",
		Revision:  fmt.Sprintf("%d", row.Revision),
		UpdatedAt: row.UpdatedAt.UTC().Format(time.RFC3339),
		ServerNow: serverNow.UTC().Format(time.RFC3339),
		Policies: retentionPolicies{
			RequestLogsRetentionDays:       row.RequestLogsRetentionDays,
			AuditLogsRetentionDays:         row.AuditLogsRetentionDays,
			StatisticsRetentionDays:        row.StatisticsRetentionDays,
			LoadbalanceEventsRetentionDays: row.LoadbalanceEventsRetentionDays,
		},
		Recommendations:          []retentionRecommendation{balancedV1Recommendation()},
		PolicyGeneration:         map[string]string{},
		ConfiguredLogicalCutoffs: map[string]*string{},
		PublishedRetentionFloors: map[string]*string{},
		RetentionSourceRevision:  map[string]string{},
		ActualCoverage:           map[string]retentionCoverageSummary{},
		Protection:               map[string]any{},
	}

	ownerSources := map[string]statsdomain.RetentionFloorEpochSource{}
	ownerSourceErrors := map[string]error{}
	for index, dataset := range retentionDatasets {
		var source statsdomain.RetentionFloorEpochSource
		sourceErr := withSettingsReadSavepoint(ctx, tx, fmt.Sprintf("settings_owner_source_%d", index), func() error {
			var err error
			source, err = statsdomain.LoadRetentionSourceProjection(ctx, tx, dataset, serverNow)
			return err
		})
		if sourceErr != nil {
			ownerSourceErrors[dataset] = sourceErr
			continue
		}
		ownerSources[dataset] = source
	}

	// A legacy value above 36500 enters the repair union for its field.
	repair := false
	for _, dataset := range retentionDatasets {
		value := policyFieldForRow(row, dataset)
		if value != nil && (*value < 1 || *value > retentionMaxDays) {
			repair = true
		}
		resource, ok := resources[dataset]
		if !ok {
			return logRetentionSettingsResponse{}, fmt.Errorf("retention resource %s is unavailable", dataset)
		}
		response.PolicyGeneration[dataset] = fmt.Sprintf("%d", resource.PolicyGeneration)
		response.ConfiguredLogicalCutoffs[dataset] = formatTimePtr(resource.ConfiguredLogicalCutoff)
		response.PublishedRetentionFloors[dataset] = formatTimePtr(resource.PublishedRetentionFloor)
	}
	if repair {
		response.State = "repair_required"
		response.RepairPreflightURL = "/api/maintenance/log-retention/preflights"
		response.Policies = retentionPolicyReadPolicies{
			RequestLogsRetentionDays:       taggedPolicyValue(row.RequestLogsRetentionDays),
			AuditLogsRetentionDays:         taggedPolicyValue(row.AuditLogsRetentionDays),
			StatisticsRetentionDays:        taggedPolicyValue(row.StatisticsRetentionDays),
			LoadbalanceEventsRetentionDays: taggedPolicyValue(row.LoadbalanceEventsRetentionDays),
		}
	}

	// Retention source projections (Observe-owned, single source of truth).
	for index, domain := range retentionDatasets {
		if sourceErr := ownerSourceErrors[domain]; sourceErr != nil {
			code := "retention_source_unavailable"
			response.RetentionSourceRevision[domain] = ""
			response.ActualCoverage[domain] = retentionCoverageSummary{
				ToTime:           nil,
				CoverageRevision: "",
				CoverageHash:     "",
				Source:           "owner_unavailable",
				Precision:        "unavailable",
				Gaps:             []any{},
				Freshness:        "error",
				SourceRevision:   "",
				ErrorCode:        &code,
			}
			continue
		}
		source := ownerSources[domain]
		response.RetentionSourceRevision[domain] = source.SourceRevision
		var coverage statsdomain.ActualCoverageProjection
		coverageErr := withSettingsReadSavepoint(ctx, tx, fmt.Sprintf("settings_owner_coverage_%d", index), func() error {
			var err error
			coverage, err = statsdomain.LoadActualCoverageProjection(ctx, tx, source)
			return err
		})
		if coverageErr != nil {
			code := "coverage_unavailable"
			response.ActualCoverage[domain] = retentionCoverageSummary{
				ToTime:              nil,
				CoverageRevision:    coverage.Revision,
				CoverageHash:        coverage.Hash,
				GeneratedAt:         formatTimePtr(coverage.GeneratedAt),
				Source:              "owner_unavailable",
				Precision:           "unavailable",
				Gaps:                []any{},
				Freshness:           "error",
				SourceRevision:      source.SourceRevision,
				RetentionEpoch:      source.RetentionEpoch,
				RetentionGeneration: source.RetentionGeneration,
				PurgeState:          source.PurgeState,
				ErrorCode:           &code,
			}
			continue
		}
		gaps := make([]any, 0, len(coverage.Gaps)+1)
		for _, gap := range coverage.Gaps {
			gaps = append(gaps, gap)
		}
		if coverage.GapReason != nil {
			gaps = append(gaps, map[string]any{
				"from_time": coverage.Earliest,
				"to_time":   serverNow.UTC().Format(time.RFC3339),
				"reason":    *coverage.GapReason,
			})
		}
		response.ActualCoverage[domain] = retentionCoverageSummary{
			FromTime:            formatTimePtr(coverage.Earliest),
			ToTime:              formatTimeValue(coverage.Latest),
			CoverageRevision:    coverage.Revision,
			CoverageHash:        coverage.Hash,
			GeneratedAt:         formatTimePtr(coverage.GeneratedAt),
			Source:              coverage.Source,
			Precision:           coveragePrecision(coverage.Precision, coverage.Complete, coverage.Freshness),
			Gaps:                gaps,
			Complete:            coverage.Complete,
			Freshness:           coverage.Freshness,
			SourceRevision:      source.SourceRevision,
			RetentionEpoch:      source.RetentionEpoch,
			RetentionGeneration: source.RetentionGeneration,
			PurgeState:          source.PurgeState,
		}
	}

	// Protection projections: the three Observe domains carry the token TTL +
	// grace window; audit embeds its own fence projection with no fixed TTL.
	for _, dataset := range []string{retentionDatasetRequestLogs, retentionDatasetUsageRequestEvents, retentionDatasetLoadbalanceEvents} {
		if ownerSourceErrors[dataset] != nil {
			response.Protection[dataset] = map[string]any{"kind": "observe_query_token", "state": "unavailable", "reason_code": "retention_source_unavailable"}
			continue
		}
		source := ownerSources[dataset]
		var reclaimNotBefore *string
		_ = withSettingsReadSavepoint(ctx, tx, fmt.Sprintf("settings_owner_protection_%d", datasetProtectionSavepointIndex(dataset)), func() error {
			reclaimNotBefore = s.physicalReclaimNotBefore(ctx, tx, dataset)
			return nil
		})
		response.Protection[dataset] = observeTokenProtection{
			Kind:                     "observe_query_token",
			TokenTTLSeconds:          managementjobs.ObserveTokenTTLSeconds(),
			ExtraGraceSeconds:        managementjobs.ObserveProtectionGraceSeconds(),
			PhysicalReclaimNotBefore: reclaimNotBefore,
			SourceRevision:           source.SourceRevision,
			RetentionEpoch:           source.RetentionEpoch,
			RetentionGeneration:      source.RetentionGeneration,
			PurgeState:               source.PurgeState,
		}
	}
	auditSource := ownerSources[retentionDatasetAuditLogs]
	if ownerSourceErrors[retentionDatasetAuditLogs] != nil {
		response.Protection[retentionDatasetAuditLogs] = map[string]any{"kind": "audit_retention_fence", "state": "unavailable", "reason_code": "retention_source_unavailable"}
	} else {
		var protection auditdomain.AuditFenceMaterializerProjection
		protectionErr := withSettingsReadSavepoint(ctx, tx, "settings_audit_protection", func() error {
			var err error
			protection, err = auditProtectionProjection(ctx, tx, serverNow)
			return err
		})
		if protectionErr != nil {
			response.Protection[retentionDatasetAuditLogs] = map[string]any{"kind": "audit_retention_fence", "state": "unavailable", "reason_code": "audit_protection_unavailable"}
		} else {
			response.Protection[retentionDatasetAuditLogs] = auditFenceProtection{
				Kind:            "audit_retention_fence",
				ContractVersion: 3,
				RetentionSource: retentionSourceProjectionMap(auditSource),
				AuditProtection: protection,
			}
		}
	}

	// Owner-drift inventory (current heads only).
	inventory, err := s.loadOwnerDriftInventory(ctx, tx)
	if err != nil {
		return logRetentionSettingsResponse{}, err
	}
	response.OwnerDriftInventory = inventory
	return response, nil
}

func formatTimePtr(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func formatTimeValue(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339)
	return &formatted
}

func retentionSourceProjectionMap(source statsdomain.RetentionFloorEpochSource) map[string]any {
	return map[string]any{
		"contract_version":       source.ContractVersion,
		"domain":                 source.Domain,
		"source_revision":        source.SourceRevision,
		"retention_epoch":        source.RetentionEpoch,
		"retention_generation":   source.RetentionGeneration,
		"fence_generation":       source.FenceGeneration,
		"configured_cutoff":      formatTimePtr(source.ConfiguredCutoff),
		"published_floor":        formatTimePtr(source.PublishedFloor),
		"purge_state":            source.PurgeState,
		"physical_reclaim_state": source.PhysicalReclaimState,
		"desired_work_identity":  source.DesiredWorkIdentity,
		"updated_at":             source.UpdatedAt.UTC().Format(time.RFC3339),
		"generated_at":           source.GeneratedAt.UTC().Format(time.RFC3339),
	}
}

func retentionOwnerSourceProjectionMap(source statsdomain.RetentionFloorEpochSource) map[string]any {
	projection := retentionSourceProjectionMap(source)
	// `published` is the durable job result, not an in-flight reader state. The
	// Observe preflight union exposes the safe steady-state as idle while the
	// source revision/epoch/fence still carry the publication identity.
	if source.PurgeState == "published" || source.PurgeState == "rolled_back" {
		projection["purge_state"] = "idle"
	}
	return projection
}

func auditProtectionProjection(ctx context.Context, exec interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, now time.Time) (auditdomain.AuditFenceMaterializerProjection, error) {
	return auditdomain.LoadAuditFenceMaterializerProjection(ctx, exec, now)
}

func auditStorageFactEvidence(ctx context.Context, tx pgx.Tx, sourceRevision string) map[string]any {
	result := map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}
	_ = withSettingsReadSavepoint(ctx, tx, "settings_audit_storage_facts", func() error {
		var err error
		result, err = auditStorageFactEvidenceRead(ctx, tx, sourceRevision)
		return err
	})
	return result
}

func auditStorageFactEvidenceRead(ctx context.Context, tx pgx.Tx, sourceRevision string) (map[string]any, error) {
	var generation *string
	var complete bool
	if err := tx.QueryRow(ctx, `SELECT current_generation, facts_complete
		FROM audit_storage_fact_state WHERE id = 1`).Scan(&generation, &complete); err != nil {
		return map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}, err
	}
	if generation == nil || !complete {
		return map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}, nil
	}
	var factCount int64
	var mismatched bool
	if err := tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(bool_or(observe_source_revision <> $2), FALSE)
		FROM audit_storage_daily_facts WHERE storage_fact_generation = $1`, *generation, sourceRevision).
		Scan(&factCount, &mismatched); err != nil {
		return map[string]any{"state": "unavailable", "reason_code": "bounded_read_unavailable"}, err
	}
	if factCount == 0 {
		return map[string]any{"state": "unavailable", "reason_code": "facts_not_ready"}, nil
	}
	if mismatched {
		return map[string]any{"state": "unavailable", "reason_code": "source_revision_mismatch"}, nil
	}
	return map[string]any{"state": "bound", "generation": *generation}, nil
}

func coveragePrecision(precision string, complete bool, freshness string) string {
	if !complete || freshness != "fresh" {
		return "unavailable"
	}
	switch precision {
	case "owner_bounds":
		return "exact"
	case "estimated":
		return "estimated"
	default:
		return "unavailable"
	}
}
