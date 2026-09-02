package models

import (
	"context"
	"fmt"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

// listModelTargetIncomingCounts returns topology counts for the current
// profile. A non-entry model remains a valid Model Target, but without an
// incoming edge it cannot receive traffic through the direct client surface.
func listModelTargetIncomingCounts(ctx context.Context, exec queryExecutor, profileID int) (map[int]int, error) {
	rows, err := exec.Query(ctx, `SELECT target_model_config_id, COUNT(*) FROM model_access_targets WHERE profile_id = $1 AND target_model_config_id IS NOT NULL GROUP BY target_model_config_id`, profileID)
	if err != nil {
		return nil, fmt.Errorf("query incoming model-target counts for profile %d: %w", profileID, err)
	}
	defer rows.Close()
	counts := map[int]int{}
	for rows.Next() {
		var modelConfigID, count int
		if err := rows.Scan(&modelConfigID, &count); err != nil {
			return nil, fmt.Errorf("scan incoming model-target count: %w", err)
		}
		counts[modelConfigID] = count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incoming model-target counts: %w", err)
	}
	return counts, nil
}

func directRequestWarnings(record modelRecord, incomingModelTargetCount int) []modelrouting.ConfigurationWarning {
	if record.DirectRequestEnabled || incomingModelTargetCount > 0 {
		return []modelrouting.ConfigurationWarning{}
	}
	return []modelrouting.ConfigurationWarning{modelrouting.NewWarning(
		modelrouting.WarningCodeModelTargetUnreferenced,
		modelrouting.WarningSeverityWarning,
		"该模型未开放客户端直接入口，且当前没有被任何模型目标引用。",
		"direct_request_enabled",
		record.ID,
		nil,
		map[string]any{"incoming_model_target_count": incomingModelTargetCount},
	)}
}

func mergeConfigurationWarnings(primary, extra []modelrouting.ConfigurationWarning) []modelrouting.ConfigurationWarning {
	if len(primary) == 0 && len(extra) == 0 {
		return []modelrouting.ConfigurationWarning{}
	}
	if len(primary) == 0 {
		return append([]modelrouting.ConfigurationWarning(nil), extra...)
	}
	if len(extra) == 0 {
		return append([]modelrouting.ConfigurationWarning(nil), primary...)
	}
	merged := make([]modelrouting.ConfigurationWarning, 0, len(primary)+len(extra))
	merged = append(merged, primary...)
	merged = append(merged, extra...)
	return merged
}
