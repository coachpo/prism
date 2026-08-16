package runtime

import (
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

type runtimeRoutingPlan struct {
	ModelsByID                     map[string]runtimeRoutingPlanModel
	ModelsByConfigID               map[int]runtimeRoutingPlanModel
	TerminalTargetsByID            map[int]runtimeConnection
	AuthoredTargetsBySourceModelID map[int][]runtimeAccessTargetRecord
}

type runtimeRoutingPlanModel struct {
	Model                  runtimeModelRecord
	HasStrategy            bool
	Strategy               loadbalance.RuntimeStrategy
	OrderedEnabledTargets  []runtimeAccessTargetRecord
	OrderedTerminalTargets []runtimeAccessTargetRecord
}

func (plan *runtimeRoutingPlan) authoredTargetsForModel(modelConfigID int) []runtimeAccessTargetRecord {
	if plan == nil {
		return nil
	}
	return plan.AuthoredTargetsBySourceModelID[modelConfigID]
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
