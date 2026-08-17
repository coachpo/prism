package models

// Ordered access-target rows are normalized at this boundary. The helpers
// retain authored positions, use IDs as the stable tie-breaker, and return
// copied slices when callers need a reordered view. The same rules serve
// graph persistence, response projection, and endpoint lookup without adding
// a second ordering policy.
//
// Model records use the same stable ID order for endpoint batch responses.
// Integer slice conversions stay here because they preserve PostgreSQL array
// values as ordered model-row data rather than as a generic conversion layer.
// No transaction, HTTP request, clock, or provider dependency belongs here.
// A caller can therefore inspect ordering behavior without constructing a
// database handle or a management request. This is the row-level boundary
// used by both authored mutation lists and persisted read projections.
// Stable ordering is observable in every model detail response.
// Position remains the primary key for authored order.
// IDs remain the deterministic tie-breaker for stored rows.
// No caller may infer a hidden target-type priority from these helpers.
//
import "sort"

func cloneAccessTargetRecords(values []accessTargetRecord) []accessTargetRecord {
	cloned := make([]accessTargetRecord, len(values))
	copy(cloned, values)
	return cloned
}

func sortAccessTargetRecords(values []accessTargetRecord) {
	sort.Slice(values, func(left int, right int) bool {
		if values[left].Position == values[right].Position {
			return values[left].ID < values[right].ID
		}
		return values[left].Position < values[right].Position
	})
}

func sortResolvedAccessTargetsByPosition(values []resolvedAccessTarget) []resolvedAccessTarget {
	ordered := make([]resolvedAccessTarget, len(values))
	copy(ordered, values)
	sort.Slice(ordered, func(left int, right int) bool {
		return ordered[left].Position < ordered[right].Position
	})
	return ordered
}

func sortPreservedConnectionAccessTargetsByPosition(values []preservedConnectionAccessTarget) []preservedConnectionAccessTarget {
	ordered := make([]preservedConnectionAccessTarget, len(values))
	copy(ordered, values)
	sort.Slice(ordered, func(left int, right int) bool {
		if ordered[left].Position == ordered[right].Position {
			return ordered[left].ID < ordered[right].ID
		}
		return ordered[left].Position < ordered[right].Position
	})
	return ordered
}

func cloneIntSlice(values []int) []int {
	if len(values) == 0 {
		return []int{}
	}
	cloned := make([]int, len(values))
	copy(cloned, values)
	return cloned
}

func intSliceFromInt32(values []int32) []int {
	items := make([]int, 0, len(values))
	for _, value := range values {
		items = append(items, int(value))
	}
	return items
}

func sortModelRecordsByID(records []modelRecord) {
	sort.Slice(records, func(left int, right int) bool {
		return records[left].ID < records[right].ID
	})
}
