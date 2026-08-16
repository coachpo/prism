package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

const (
	runtimeAccessTargetTypeConnection = modelrouting.TargetTypeTerminal
	runtimeAccessTargetTypeModel      = modelrouting.TargetTypeModel
)

type runtimeRoundRobinTargetCursor interface {
	ClaimRoundRobinTargetCursor(profileID int, sourceModelConfigID int, strategyID int, targetSetHash string, targetCount int) int
}

func orderRuntimeAccessTargets(profileID int, sourceModelConfigID int, strategy loadbalance.RuntimeStrategy, targets []runtimeAccessTargetRecord, cursor runtimeRoundRobinTargetCursor) []runtimeAccessTargetRecord {
	ordered := sortedEnabledRuntimeAccessTargets(targets)
	if len(ordered) == 0 {
		return nil
	}
	switch normalizedRuntimeLegacyStrategyType(strategy) {
	case "single":
		return ordered[:1]
	case "round-robin":
		if len(ordered) >= 2 && cursor != nil {
			setHash := runtimeAccessTargetSetHash(ordered)
			offset := cursor.ClaimRoundRobinTargetCursor(profileID, sourceModelConfigID, strategy.ID, setHash, len(ordered))
			if offset != 0 {
				ordered = append(ordered[offset:], ordered[:offset]...)
			}
		}
	}
	return ordered
}

func sortedEnabledRuntimeAccessTargets(targets []runtimeAccessTargetRecord) []runtimeAccessTargetRecord {
	ordered := enabledRuntimeAccessTargets(targets)
	if len(ordered) == 0 {
		return nil
	}
	sortRuntimeAccessTargets(ordered)
	return ordered
}

func enabledRuntimeAccessTargets(targets []runtimeAccessTargetRecord) []runtimeAccessTargetRecord {
	if len(targets) == 0 {
		return nil
	}
	filtered := make([]runtimeAccessTargetRecord, 0, len(targets))
	for _, target := range targets {
		if target.IsEnabled {
			filtered = append(filtered, target)
		}
	}
	return filtered
}

func sortRuntimeAccessTargets(targets []runtimeAccessTargetRecord) {
	sort.Slice(targets, func(left int, right int) bool {
		return compareRuntimeAccessTargets(targets[left], targets[right]) < 0
	})
}

func compareRuntimeAccessTargets(left runtimeAccessTargetRecord, right runtimeAccessTargetRecord) int {
	return modelrouting.CompareAccessTargetOrder(
		modelrouting.OrderKey{Position: left.Position, ID: left.ID},
		modelrouting.OrderKey{Position: right.Position, ID: right.ID},
	)
}

func runtimeAccessTargetSetHash(targets []runtimeAccessTargetRecord) string {
	hasher := sha256.New()
	for _, target := range targets {
		modelID := 0
		if target.TargetModelConfigID != nil {
			modelID = *target.TargetModelConfigID
		}
		connectionID := 0
		if target.TargetConnectionID != nil {
			connectionID = *target.TargetConnectionID
		}
		_, _ = fmt.Fprintf(hasher, "%d:%d:%s:%d:%d|", target.ID, target.Position, target.TargetType, modelID, connectionID)
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func normalizedRuntimeLegacyStrategyType(strategy loadbalance.RuntimeStrategy) string {
	if strategy.LegacyStrategyType == nil {
		return "fill-first"
	}
	return strings.ToLower(strings.TrimSpace(*strategy.LegacyStrategyType))
}
