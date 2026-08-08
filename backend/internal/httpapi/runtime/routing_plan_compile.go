package runtime

import "sort"

func compileRuntimeRoutingPlan(snapshot *planningSnapshot) (*runtimeRoutingPlan, error) {
	if snapshot == nil {
		return nil, invalidRuntimeRoutingPlanError("planning snapshot is nil")
	}
	plan := &runtimeRoutingPlan{
		ModelsByID:          make(map[string]runtimeRoutingPlanModel, len(snapshot.ModelsByID)),
		ModelsByConfigID:    make(map[int]runtimeRoutingPlanModel, len(snapshot.ModelsByID)),
		TerminalTargetsByID: make(map[int]runtimeConnection, len(snapshot.TerminalTargetsByID)),
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
