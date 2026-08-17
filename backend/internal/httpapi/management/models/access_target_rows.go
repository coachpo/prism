package models

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
