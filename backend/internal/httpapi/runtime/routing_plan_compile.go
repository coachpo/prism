package runtime

import "sort"

func compileRuntimeRoutingPlan(snapshot *planningSnapshot) (*runtimeRoutingPlan, error) {
	if snapshot == nil {
		return nil, invalidRuntimeRoutingPlanError("planning snapshot is nil")
	}
	plan := &runtimeRoutingPlan{
		ModelsByID:          make(map[string]runtimeRoutingPlanModel, len(snapshot.ModelsByID)),
		ModelsByConfigID:    make(map[int]runtimeRoutingPlanModel, len(snapshot.ModelsByID)),
		TerminalTargetsByID: make(map[int]runtimeConnection, len(snapshot.ConnectionsByID)),
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
		compiled.PeerTiers = compileRuntimeRoutingPlanPeerTiers(compiled.OrderedEnabledTargets)
		compiled.OrderedTerminalTargets = compileRuntimeRoutingPlanTerminalTargets(compiled.OrderedEnabledTargets)
		plan.ModelsByID[modelID] = compiled
		plan.ModelsByConfigID[model.ID] = compiled
	}

	connectionIDs := make([]int, 0, len(snapshot.ConnectionsByID))
	for connectionID := range snapshot.ConnectionsByID {
		connectionIDs = append(connectionIDs, connectionID)
	}
	sort.Ints(connectionIDs)
	for _, connectionID := range connectionIDs {
		plan.TerminalTargetsByID[connectionID] = snapshot.ConnectionsByID[connectionID]
	}
	return plan, nil
}

func compileRuntimeRoutingPlanPeerTiers(targets []runtimeAccessTargetRecord) []runtimeRoutingPlanPeerTier {
	if len(targets) == 0 {
		return nil
	}
	peerTargets := make([]runtimeAccessTargetRecord, 0, len(targets))
	for _, target := range targets {
		if target.TargetType == runtimeAccessTargetTypeModel {
			peerTargets = append(peerTargets, target)
		}
	}
	if len(peerTargets) == 0 {
		return nil
	}
	sort.Slice(peerTargets, func(left int, right int) bool {
		return compareRuntimePeerTierTargets(peerTargets[left], peerTargets[right]) < 0
	})

	tiers := make([]runtimeRoutingPlanPeerTier, 0)
	for index := 0; index < len(peerTargets); {
		targetPriority := peerTargets[index].TargetPriority
		next := index + 1
		for next < len(peerTargets) && peerTargets[next].TargetPriority == targetPriority {
			next++
		}
		tiers = append(tiers, runtimeRoutingPlanPeerTier{
			TargetPriority:  targetPriority,
			WeightedPeerSet: compileRuntimeRoutingPlanWeightedPeerSet(peerTargets[index:next]),
		})
		index = next
	}
	return tiers
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

func compileRuntimeRoutingPlanWeightedPeerSet(targets []runtimeAccessTargetRecord) runtimeRoutingPlanWeightedPeerSet {
	if len(targets) == 0 {
		return runtimeRoutingPlanWeightedPeerSet{}
	}
	weightedTargets := make([]runtimeAccessTargetRecord, 0, len(targets))
	totalWeight := 0
	for _, target := range targets {
		if target.TargetType != runtimeAccessTargetTypeModel {
			continue
		}
		weightedTargets = append(weightedTargets, target)
		totalWeight += effectiveRuntimeAccessTargetWeight(target)
	}
	return runtimeRoutingPlanWeightedPeerSet{
		Targets:     weightedTargets,
		TotalWeight: totalWeight,
	}
}

func compareRuntimePeerTierTargets(left runtimeAccessTargetRecord, right runtimeAccessTargetRecord) int {
	if left.TargetPriority != right.TargetPriority {
		if left.TargetPriority < right.TargetPriority {
			return -1
		}
		return 1
	}
	return compareRuntimeAccessTargets(left, right)
}
