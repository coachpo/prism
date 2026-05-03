package runtime

import (
	"sort"
	"strings"
)

const (
	proxySelectionStrategyOrderedFallback = "ordered_fallback"
	proxySelectionStrategyWeightedStatic  = "weighted_static"
	proxySelectionStrategyPriorityStatic  = "priority_static"
)

func normalizedRuntimeProxySelectionStrategy(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isSupportedRuntimeProxySelectionStrategy(value string) bool {
	switch normalizedRuntimeProxySelectionStrategy(value) {
	case proxySelectionStrategyOrderedFallback, proxySelectionStrategyWeightedStatic, proxySelectionStrategyPriorityStatic:
		return true
	default:
		return false
	}
}

func orderRuntimeProxyTargetCandidates(strategy string, candidates []runtimeProxyTargetRecord) []runtimeProxyTargetRecord {
	ordered := append([]runtimeProxyTargetRecord(nil), candidates...)
	strategy = normalizedRuntimeProxySelectionStrategy(strategy)
	if !isSupportedRuntimeProxySelectionStrategy(strategy) {
		strategy = proxySelectionStrategyOrderedFallback
	}
	if strategy == proxySelectionStrategyPriorityStatic {
		sort.Slice(ordered, func(left int, right int) bool {
			return compareRuntimeProxyTargetPriority(ordered[left], ordered[right]) < 0
		})
		return ordered
	}
	sort.Slice(ordered, func(left int, right int) bool {
		return compareRuntimeProxyTargetPosition(ordered[left], ordered[right]) < 0
	})
	return ordered
}

func compareRuntimeProxyTargetPriority(left runtimeProxyTargetRecord, right runtimeProxyTargetRecord) int {
	if left.TargetPriority != right.TargetPriority {
		if left.TargetPriority < right.TargetPriority {
			return -1
		}
		return 1
	}
	return compareRuntimeProxyTargetPosition(left, right)
}

func compareRuntimeProxyTargetPosition(left runtimeProxyTargetRecord, right runtimeProxyTargetRecord) int {
	if left.Position != right.Position {
		if left.Position < right.Position {
			return -1
		}
		return 1
	}
	if left.ID < right.ID {
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	return 0
}
