package runtime

import "sort"

func compileRuntimeRoutingPlan(snapshot *planningSnapshot) (*runtimeRoutingPlan, error) {
	if snapshot == nil {
		return nil, invalidRuntimeRoutingPlanError("planning snapshot is nil")
	}
	plan := &runtimeRoutingPlan{
		ModelsByID:                     make(map[string]runtimeRoutingPlanModel, len(snapshot.ModelsByID)),
		ModelsByConfigID:               make(map[int]runtimeRoutingPlanModel, len(snapshot.ModelsByID)),
		TerminalTargetsByID:            make(map[int]runtimeConnection, len(snapshot.TerminalTargetsByID)),
		AuthoredTargetsBySourceModelID: make(map[int][]runtimeAccessTargetRecord, len(snapshot.AccessTargetsBySourceModelID)),
	}

	for sourceModelID, targets := range snapshot.AccessTargetsBySourceModelID {
		plan.AuthoredTargetsBySourceModelID[sourceModelID] = sortedAuthoredRuntimeAccessTargets(targets)
	}

	modelIDs := make([]string, 0, len(snapshot.ModelsByID))
	for modelID := range snapshot.ModelsByID {
		modelIDs = append(modelIDs, modelID)
	}
	sort.Strings(modelIDs)

	for _, modelID := range modelIDs {
		model := snapshot.ModelsByID[modelID]
		compiled := runtimeRoutingPlanModel{Model: model}
		if strategy, ok := snapshot.StrategiesByModelID[model.ID]; ok {
			compiled.HasStrategy = true
			compiled.Strategy = strategy
		}
		compiled.OrderedEnabledTargets = sortedEnabledRuntimeAccessTargets(snapshot.AccessTargetsBySourceModelID[model.ID])
		compiled.OrderedFallbackTargets = cloneRuntimeAccessTargetRecords(compiled.OrderedEnabledTargets)
		compiled.OrderedTerminalTargets = compileRuntimeRoutingPlanTerminalTargets(compiled.OrderedEnabledTargets)
		plan.ModelsByID[modelID] = compiled
		plan.ModelsByConfigID[model.ID] = compiled
	}

	connectionIDs := make([]int, 0, len(snapshot.TerminalTargetsByID))
	for connectionID := range snapshot.TerminalTargetsByID {
		connectionIDs = append(connectionIDs, connectionID)
	}
	sort.Ints(connectionIDs)
	for _, connectionID := range connectionIDs {
		plan.TerminalTargetsByID[connectionID] = snapshot.TerminalTargetsByID[connectionID]
	}
	return plan, nil
}

func compileRuntimeRoutingPlanTerminalTargets(targets []runtimeAccessTargetRecord) []runtimeAccessTargetRecord {
	if len(targets) == 0 {
		return nil
	}
	terminalTargets := make([]runtimeAccessTargetRecord, 0, len(targets))
	for _, target := range targets {
		if target.TargetType == runtimeAccessTargetTypeConnection {
			terminalTargets = append(terminalTargets, target)
		}
	}
	return terminalTargets
}

func sortedAuthoredRuntimeAccessTargets(targets []runtimeAccessTargetRecord) []runtimeAccessTargetRecord {
	if len(targets) == 0 {
		return nil
	}
	ordered := make([]runtimeAccessTargetRecord, len(targets))
	copy(ordered, targets)
	sortRuntimeAccessTargets(ordered)
	return ordered
}
