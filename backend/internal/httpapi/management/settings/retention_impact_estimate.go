package settings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type retentionBoundaryEstimate struct {
	Name        string
	MatchedRows int64
}

// estimateImpact produces bounded count/partition facts using partition
// catalog metadata and a bounded boundary count (SPEC §6.2: no unbounded
// COUNT for a prettier dialog).
func estimateImpact(ctx context.Context, tx pgx.Tx, dataset string, cutoff *time.Time, deleteAll bool) (retentionImpactCount, retentionImpactCount, map[string]any, []map[string]any, error) {
	unavailable := func(err error) (retentionImpactCount, retentionImpactCount, map[string]any, []map[string]any, error) {
		return retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
			retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
			map[string]any{"count": nil, "names_preview": []string{}, "names_total_count": nil, "truncated": false},
			[]map[string]any{}, err
	}
	// Partition catalog: name, start/end bounds and reltuples estimate.
	rows, err := tx.Query(ctx, `SELECT child.relname,
			CASE WHEN child.reltuples >= 0 THEN child.reltuples::bigint ELSE NULL END,
			pg_get_expr(child.relpartbound, child.oid)
		FROM pg_inherits AS inh
		JOIN pg_class AS parent ON parent.oid = inh.inhparent
		JOIN pg_class AS child ON child.oid = inh.inhrelid
		WHERE parent.relname = $1 AND parent.relkind = 'p'
		ORDER BY child.relname ASC`, dataset)
	if err != nil {
		return unavailable(err)
	}
	defer rows.Close()
	type partitionEstimate struct {
		Name   string
		Tuples *int64
		Bound  string
	}
	partitions := []partitionEstimate{}
	for rows.Next() {
		var item partitionEstimate
		if err := rows.Scan(&item.Name, &item.Tuples, &item.Bound); err != nil {
			return unavailable(err)
		}
		partitions = append(partitions, item)
	}
	if err := rows.Err(); err != nil {
		return unavailable(err)
	}
	if len(partitions) == 0 {
		return unavailable(nil)
	}

	droppedTuples := int64(0)
	retainedTuples := int64(0)
	droppedKnown := true
	retainedKnown := true
	droppedNames := []string{}
	var boundaryPartition *retentionBoundaryEstimate
	for _, partition := range partitions {
		startTime, startOK := parseBoundTime(parsePartitionBound(partition.Bound, false))
		endStr := parsePartitionBound(partition.Bound, true)
		endTime, endOK := parseBoundTime(endStr)
		if cutoff != nil && endOK && !endTime.After(*cutoff) {
			// Whole partition is at or before the cutoff: fully dropped.
			if partition.Tuples == nil {
				droppedKnown = false
			} else {
				droppedTuples += *partition.Tuples
			}
			droppedNames = append(droppedNames, partition.Name)
			continue
		}
		if partition.Tuples == nil {
			retainedKnown = false
		} else {
			retainedTuples += *partition.Tuples
		}
		if cutoff != nil && startOK && endOK && boundaryPartition == nil && !cutoff.Before(startTime) && endTime.After(*cutoff) {
			// First partition spanning the cutoff is the boundary partition.
			if partition.Tuples != nil {
				boundaryPartition = &retentionBoundaryEstimate{
					Name:        partition.Name,
					MatchedRows: *partition.Tuples,
				}
			}
		}
	}
	// Boundary rows: bounded exact count when feasible, else catalog estimate.
	matched := retentionImpactCount{Accuracy: "estimated", Method: "partition_metadata"}
	retained := retentionImpactCount{Accuracy: "estimated", Method: "partition_metadata"}
	if droppedKnown && boundaryPartition != nil {
		matched.Value = strPtr(fmt.Sprintf("%d", droppedTuples+boundaryPartition.MatchedRows))
	} else if droppedKnown {
		matched.Value = strPtr(fmt.Sprintf("%d", droppedTuples))
	} else {
		matched.Accuracy = "unavailable"
		matched.Method = "partition_metadata_unavailable"
	}
	if retainedKnown {
		retained.Value = strPtr(fmt.Sprintf("%d", retainedTuples))
	} else {
		retained.Accuracy = "unavailable"
		retained.Method = "partition_metadata_unavailable"
	}
	whole := map[string]any{
		"count":             fmt.Sprintf("%d", len(droppedNames)),
		"names_preview":     boundedNames(droppedNames),
		"names_total_count": fmt.Sprintf("%d", len(droppedNames)),
		"truncated":         len(droppedNames) > 8,
	}
	boundary := []map[string]any{}
	if boundaryPartition != nil {
		boundary = append(boundary, map[string]any{
			"name":         boundaryPartition.Name,
			"matched_rows": map[string]any{"value": strPtr(fmt.Sprintf("%d", boundaryPartition.MatchedRows)), "accuracy": "estimated", "method": "partition_metadata"},
		})
	}
	return matched, retained, whole, boundary, nil
}

func unavailableRetentionImpactEstimate() (retentionImpactCount, retentionImpactCount, map[string]any, []map[string]any) {
	return retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
		retentionImpactCount{Value: nil, Accuracy: "unavailable", Method: "unavailable"},
		map[string]any{"count": nil, "names_preview": []string{}, "names_total_count": nil, "truncated": false},
		[]map[string]any{}
}

func parsePartitionBound(expr string, end bool) string {
	// expr looks like: FOR VALUES FROM ('2026-08-01 00:00:00+00') TO ('2026-08-02 00:00:00+00')
	marker := " TO ("
	part := expr
	if !end {
		marker = " FROM ("
		part = expr
	}
	index := strings.Index(part, marker)
	if index < 0 {
		return ""
	}
	rest := part[index+len(marker):]
	endIndex := strings.Index(rest, ")")
	if endIndex < 0 {
		return ""
	}
	return strings.Trim(rest[:endIndex], " '")
}

func parseBoundTime(value string) (time.Time, bool) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		// pg partition bounds are often `2026-08-01 00:00:00+00`
		parsed, err = time.Parse("2006-01-02 15:04:05-07", value)
		if err != nil {
			parsed, err = time.Parse("2006-01-02 15:04:05Z07:00", value)
		}
	}
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}

func boundedNames(names []string) []string {
	if len(names) > 8 {
		return names[:8]
	}
	return names
}

func strPtr(value string) *string { return &value }

func unavailableImpactBytes(reason string) retentionImpactBytes {
	return retentionImpactBytes{Value: nil, Accuracy: "unavailable", Basis: reason}
}
