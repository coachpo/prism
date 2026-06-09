package runtime

import (
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

type runtimeRoutingPlan struct {
	ModelsByID          map[string]runtimeRoutingPlanModel
	ModelsByConfigID    map[int]runtimeRoutingPlanModel
	TerminalTargetsByID map[int]runtimeConnection
}

type runtimeRoutingPlanModel struct {
	Model                  runtimeModelRecord
	HasStrategy            bool
	Strategy               loadbalance.RuntimeStrategy
	OrderedEnabledTargets  []runtimeAccessTargetRecord
	OrderedFallbackTargets []runtimeAccessTargetRecord
	PeerTiers              []runtimeRoutingPlanPeerTier
	OrderedTerminalTargets []runtimeAccessTargetRecord
}

type runtimeRoutingPlanPeerTier struct {
	TargetPriority  int
	WeightedPeerSet runtimeRoutingPlanWeightedPeerSet
}

type runtimeRoutingPlanWeightedPeerSet struct {
	Targets     []runtimeAccessTargetRecord
	TotalWeight int
}

func (plan *runtimeRoutingPlan) requestedModelByID(modelID string) (runtimeModelRecord, bool) {
	if plan == nil {
		return runtimeModelRecord{}, false
	}
	compiled, ok := plan.ModelsByID[modelID]
	if !ok {
		return runtimeModelRecord{}, false
	}
	return compiled.Model, true
}

func (plan *runtimeRoutingPlan) strategyForModel(model runtimeModelRecord) (loadbalance.RuntimeStrategy, bool) {
	if plan == nil {
		return loadbalance.RuntimeStrategy{}, false
	}
	compiled, ok := plan.ModelsByConfigID[model.ID]
	if !ok || !compiled.HasStrategy {
		return loadbalance.RuntimeStrategy{}, false
	}
	return compiled.Strategy, true
}

func (plan *runtimeRoutingPlan) orderedPeerTiersForModel(model runtimeModelRecord) []runtimeRoutingPlanPeerTier {
	if plan == nil {
		return nil
	}
	compiled, ok := plan.ModelsByConfigID[model.ID]
	if !ok {
		return nil
	}
	return cloneRuntimeRoutingPlanPeerTiers(compiled.PeerTiers)
}

func (plan *runtimeRoutingPlan) orderedModelTargetsForStrategy(profileID int, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, cursor runtimeRoundRobinTargetCursor) []runtimeAccessTargetRecord {
	if plan == nil {
		return nil
	}
	compiled, ok := plan.ModelsByConfigID[model.ID]
	if !ok {
		return nil
	}
	ordered := make([]runtimeAccessTargetRecord, 0, len(compiled.OrderedEnabledTargets))
	for _, target := range compiled.OrderedEnabledTargets {
		if target.TargetType == runtimeAccessTargetTypeModel {
			ordered = append(ordered, target)
		}
	}
	return orderRuntimeRoutingPlanTargetsForStrategy(profileID, model.ID, strategy, ordered, cursor)
}

func (plan *runtimeRoutingPlan) orderedTerminalTargetsForModel(model runtimeModelRecord) []runtimeAccessTargetRecord {
	if plan == nil {
		return nil
	}
	compiled, ok := plan.ModelsByConfigID[model.ID]
	if !ok {
		return nil
	}
	return cloneRuntimeAccessTargetRecords(compiled.OrderedTerminalTargets)
}

func (plan *runtimeRoutingPlan) orderedTerminalTargetsForStrategy(profileID int, model runtimeModelRecord, strategy loadbalance.RuntimeStrategy, cursor runtimeRoundRobinTargetCursor) []runtimeAccessTargetRecord {
	ordered := plan.orderedTerminalTargetsForModel(model)
	return orderRuntimeRoutingPlanTargetsForStrategy(profileID, model.ID, strategy, ordered, cursor)
}

func orderRuntimeRoutingPlanTargetsForStrategy(profileID int, sourceModelConfigID int, strategy loadbalance.RuntimeStrategy, ordered []runtimeAccessTargetRecord, cursor runtimeRoundRobinTargetCursor) []runtimeAccessTargetRecord {
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

func (plan *runtimeRoutingPlan) terminalConnectionForAccessTarget(sourceModel runtimeModelRecord, target runtimeAccessTargetRecord) (runtimeConnection, bool) {
	if plan == nil {
		return runtimeConnection{}, false
	}
	terminalTargetConnectionID := target.terminalTargetConnectionID()
	if terminalTargetConnectionID == nil {
		return runtimeConnection{}, false
	}
	connection, ok := plan.TerminalTargetsByID[*terminalTargetConnectionID]
	if !ok {
		return runtimeConnection{}, false
	}
	if connection.ProfileID != sourceModel.ProfileID || !modelrouting.SameAPIFamily(connection.APIFamily, sourceModel.APIFamily) {
		return runtimeConnection{}, false
	}
	if target.TargetConnectionProfileID != 0 && target.TargetConnectionProfileID != sourceModel.ProfileID {
		return runtimeConnection{}, false
	}
	if strings.TrimSpace(target.TargetConnectionAPIFamily) != "" && !modelrouting.SameAPIFamily(target.TargetConnectionAPIFamily, sourceModel.APIFamily) {
		return runtimeConnection{}, false
	}
	return connection, true
}

func (plan *runtimeRoutingPlan) modelForAccessTarget(sourceModel runtimeModelRecord, target runtimeAccessTargetRecord) (runtimeModelRecord, bool, error) {
	if plan == nil || target.TargetModelConfigID == nil || !target.TargetModelEnabled {
		return runtimeModelRecord{}, false, nil
	}
	if target.TargetModelProfileID != sourceModel.ProfileID || !modelrouting.SameAPIFamily(target.TargetModelAPIFamily, sourceModel.APIFamily) {
		return runtimeModelRecord{}, false, nil
	}
	childModel, ok := plan.requestedModelByID(target.TargetModelID)
	if !ok || childModel.ID != *target.TargetModelConfigID {
		return runtimeModelRecord{}, false, nil
	}
	if childModel.ProfileID != sourceModel.ProfileID || !modelrouting.SameAPIFamily(childModel.APIFamily, sourceModel.APIFamily) {
		return runtimeModelRecord{}, false, nil
	}
	if childModel.FacadeEnabled {
		return runtimeModelRecord{}, false, nestedRuntimeFacadeTargetError()
	}
	return childModel, true, nil
}

func cloneRuntimeAccessTargetRecords(source []runtimeAccessTargetRecord) []runtimeAccessTargetRecord {
	if len(source) == 0 {
		return nil
	}
	cloned := make([]runtimeAccessTargetRecord, len(source))
	copy(cloned, source)
	return cloned
}

func cloneRuntimeRoutingPlanPeerTiers(source []runtimeRoutingPlanPeerTier) []runtimeRoutingPlanPeerTier {
	if len(source) == 0 {
		return nil
	}
	cloned := make([]runtimeRoutingPlanPeerTier, 0, len(source))
	for _, tier := range source {
		cloned = append(cloned, runtimeRoutingPlanPeerTier{
			TargetPriority: tier.TargetPriority,
			WeightedPeerSet: runtimeRoutingPlanWeightedPeerSet{
				Targets:     cloneRuntimeAccessTargetRecords(tier.WeightedPeerSet.Targets),
				TotalWeight: tier.WeightedPeerSet.TotalWeight,
			},
		})
	}
	return cloned
}
