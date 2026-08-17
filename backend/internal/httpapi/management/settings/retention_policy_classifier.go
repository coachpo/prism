package settings

import "time"

// isDestructiveTransition classifies a before/after policy change (SPEC §5.2).
func isDestructiveTransition(before *int, after *int) bool {
	if before == nil && after != nil {
		return true // NULL -> N enables scheduled logical cleanup
	}
	if before != nil && after != nil && *after < *before {
		return true // shortening
	}
	return false
}

func policyFieldValue(policies retentionPolicies, dataset string) *int {
	switch dataset {
	case retentionDatasetRequestLogs:
		return policies.RequestLogsRetentionDays
	case retentionDatasetAuditLogs:
		return policies.AuditLogsRetentionDays
	case retentionDatasetUsageRequestEvents:
		return policies.StatisticsRetentionDays
	default:
		return policies.LoadbalanceEventsRetentionDays
	}
}

func policyFieldForRow(row retentionRow, dataset string) *int {
	switch dataset {
	case retentionDatasetRequestLogs:
		return row.RequestLogsRetentionDays
	case retentionDatasetAuditLogs:
		return row.AuditLogsRetentionDays
	case retentionDatasetUsageRequestEvents:
		return row.StatisticsRetentionDays
	default:
		return row.LoadbalanceEventsRetentionDays
	}
}

func validateRetentionPolicies(policies retentionPolicies) []FieldViolation {
	violations := []FieldViolation{}
	check := func(path string, value *int) {
		if value == nil {
			return
		}
		if *value < 1 || *value > retentionMaxDays {
			violations = append(violations, FieldViolation{Path: path, Reason: "must be an integer between 1 and 36500 or null", Limit: retentionMaxDays})
		}
	}
	check("policies.request_logs_retention_days", policies.RequestLogsRetentionDays)
	check("policies.audit_logs_retention_days", policies.AuditLogsRetentionDays)
	check("policies.statistics_retention_days", policies.StatisticsRetentionDays)
	check("policies.loadbalance_events_retention_days", policies.LoadbalanceEventsRetentionDays)
	return violations
}

func intPtrsEqual(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func utcDayCutoff(now time.Time, retentionDays int) time.Time {
	utc := now.UTC()
	dayStart := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	return dayStart.AddDate(0, 0, -retentionDays)
}

func isManagedDataset(dataset string) bool {
	for _, item := range retentionDatasets {
		if item == dataset {
			return true
		}
	}
	return false
}
