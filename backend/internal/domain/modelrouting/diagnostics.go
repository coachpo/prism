package modelrouting

import (
	"sort"
	"strings"
)

// Stage names used by diagnostics and warnings.
const (
	StageModelTargets    = "model_targets"
	StageTerminalTargets = "terminal_targets"
)

// Per-target per-operation dispositions.
const (
	DispositionCandidate         = "candidate"
	DispositionDisabled          = "disabled"
	DispositionInactive          = "inactive"
	DispositionIncompatible      = "incompatible"
	DispositionNoEligibleLeaf    = "no_eligible_leaf"
	DispositionTruncatedBySingle = "truncated_by_single"
	DispositionStructuralError   = "structural_error"
)

// Operation group statuses used by the compact model-list summary.
const (
	GroupStatusNotAccepted             = "not_accepted"
	GroupStatusRoutable                = "routable"
	GroupStatusCompatibleButIneligible = "compatible_but_ineligible"
	GroupStatusUncovered               = "uncovered"
)

// DiagnosticsGraph is an HTTP-neutral, DB-shaped snapshot of the authored
// model routing graph used by the static diagnostics analyzer. It never
// carries endpoint credentials, header/parameter values or runtime state.
type DiagnosticsGraph struct {
	ModelsByID                   map[int]DiagnosticsModel
	AccessTargetsBySourceModelID map[int][]DiagnosticsAccessTarget
	ConnectionsByID              map[int]DiagnosticsConnection
	StrategiesByModelID          map[int]DiagnosticsStrategy
}

type DiagnosticsModel struct {
	ConfigID              int
	ProfileID             int
	ModelID               string
	APIFamily             string
	IsEnabled             bool
	OpenAIAcceptedFormat  *string
	LoadbalanceStrategyID *int
}

type DiagnosticsAccessTarget struct {
	ID                  int
	ProfileID           int
	SourceModelConfigID int
	TargetType          string
	TargetModelConfigID *int
	TargetConnectionID  *int
	Position            int
	IsEnabled           bool
}

type DiagnosticsConnection struct {
	ID                   int
	ProfileID            int
	APIFamily            string
	IsActive             bool
	OpenAITextCapability *string
}

type DiagnosticsStrategy struct {
	ID      int
	Subtype string
}

// DiagnosticsResult is the authoritative static routing diagnostics for one
// model config, computed by Analyze.
type DiagnosticsResult struct {
	ModelConfigID         int                            `json:"model_config_id"`
	Strategy              DiagnosticsStrategyResult      `json:"strategy"`
	AcceptedOperations    []string                       `json:"accepted_operations"`
	Stages                []DiagnosticsStage             `json:"stages"`
	OperationCoverage     []DiagnosticsOperationCoverage `json:"operation_coverage"`
	ConfigurationWarnings []ConfigurationWarning         `json:"configuration_warnings"`
}

type DiagnosticsStrategyResult struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
}

type DiagnosticsStage struct {
	Stage       string              `json:"stage"`
	Order       int                 `json:"order"`
	EnteredWhen string              `json:"entered_when"`
	Targets     []DiagnosticsTarget `json:"targets"`
}

type DiagnosticsTarget struct {
	AccessTargetID                int                          `json:"access_target_id"`
	AuthoredStagePosition         int                          `json:"authored_stage_position"`
	EnabledStrategyIndex          *int                         `json:"enabled_strategy_index"`
	TargetModelConfigID           *int                         `json:"target_model_config_id,omitempty"`
	ConnectionID                  *int                         `json:"connection_id,omitempty"`
	Coverage                      string                       `json:"coverage"`
	SupportedOperations           []string                     `json:"supported_operations"`
	UnsupportedAcceptedOperations []string                     `json:"unsupported_accepted_operations"`
	OperationResults              []DiagnosticsOperationResult `json:"operation_results"`
}

type DiagnosticsOperationResult struct {
	OperationName         string `json:"operation_name"`
	Disposition           string `json:"disposition"`
	TerminalConnectionIDs []int  `json:"terminal_connection_ids,omitempty"`
}

type DiagnosticsOperationCoverage struct {
	OperationName             string  `json:"operation_name"`
	Accepted                  bool    `json:"accepted"`
	CapabilityCovered         bool    `json:"capability_covered"`
	StaticallyRoutable        bool    `json:"statically_routable"`
	ResolvedStage             *string `json:"resolved_stage"`
	CompatibleAccessTargetIDs []int   `json:"compatible_access_target_ids"`
	AccessTargetIDs           []int   `json:"access_target_ids"`
}

// Analyze computes the static two-stage routing diagnostics for a root model
// over an accepted operation set. For OpenAI models the caller passes the
// canonical operation set derived from the root accepted format; for other
// families the caller passes the family's registered model-bound operations
// (terminal coverage stays not_applicable for non-OpenAI).
//
// The analyzer is pure and static: it never reads or changes Ban/retry state,
// QPS/in-flight counters, current-state, round-robin cursors or endpoint
// secrets, and it never sends provider requests.
func Analyze(graph *DiagnosticsGraph, rootModelConfigID int, acceptedOperations []string) DiagnosticsResult {
	result := DiagnosticsResult{
		ModelConfigID:      rootModelConfigID,
		AcceptedOperations: append([]string(nil), acceptedOperations...),
	}
	root, ok := graph.ModelsByID[rootModelConfigID]
	if !ok {
		return result
	}
	rootStrategy := graph.strategyForModel(root)
	result.Strategy = DiagnosticsStrategyResult{ID: rootStrategy.ID, Type: rootStrategy.Subtype}
	modelStage := buildStage(graph, root, StageModelTargets, 1, "always", TargetTypeModel)
	terminalStage := buildStage(graph, root, StageTerminalTargets, 2, "model_targets_has_no_eligible_candidate", TargetTypeTerminal)
	fillTerminalRowCoverage(graph, root, &modelStage, acceptedOperations)
	fillTerminalRowCoverage(graph, root, &terminalStage, acceptedOperations)

	for _, operation := range acceptedOperations {
		trimmed := strings.TrimSpace(operation)
		if trimmed == "" {
			continue
		}
		coverage := DiagnosticsOperationCoverage{OperationName: trimmed, Accepted: root.acceptsOperation(trimmed)}
		if !coverage.Accepted {
			result.OperationCoverage = append(result.OperationCoverage, coverage)
			continue
		}
		modelCandidate, modelLeaves, modelCompatibleRows := stageCandidateForOperation(graph, root, &modelStage, rootStrategy, trimmed)
		if modelCandidate {
			coverage.CapabilityCovered = true
			coverage.StaticallyRoutable = true
			resolvedStage := StageModelTargets
			coverage.ResolvedStage = &resolvedStage
			coverage.CompatibleAccessTargetIDs = sortedUniqueInts(modelCompatibleRows)
			coverage.AccessTargetIDs = sortedUniqueInts(append(append([]int(nil), modelCompatibleRows...), modelLeaves...))
			result.OperationCoverage = append(result.OperationCoverage, coverage)
			continue
		}
		terminalCandidate, terminalRows, terminalCompatibleRows := stageCandidateForOperation(graph, root, &terminalStage, rootStrategy, trimmed)
		coverage.CapabilityCovered = terminalCandidate || graphHasCompatibleLeafForOperation(graph, root, trimmed)
		coverage.CompatibleAccessTargetIDs = sortedUniqueInts(terminalCompatibleRows)
		if terminalCandidate {
			coverage.StaticallyRoutable = true
			resolvedStage := StageTerminalTargets
			coverage.ResolvedStage = &resolvedStage
			coverage.AccessTargetIDs = sortedUniqueInts(append(append([]int(nil), terminalCompatibleRows...), terminalRows...))
		} else {
			coverage.AccessTargetIDs = sortedUniqueInts(append([]int(nil), terminalCompatibleRows...))
		}
		result.OperationCoverage = append(result.OperationCoverage, coverage)
	}
	result.Stages = append(result.Stages, modelStage, terminalStage)
	result.ConfigurationWarnings = GenerateConfigurationWarnings(graph, root, result, acceptedOperations)
	return result
}

func buildStage(graph *DiagnosticsGraph, root DiagnosticsModel, stage string, order int, enteredWhen string, targetType string) DiagnosticsStage {
	built := DiagnosticsStage{Stage: stage, Order: order, EnteredWhen: enteredWhen, Targets: []DiagnosticsTarget{}}
	authored := sortedDiagnosticsAccessTargets(graph.AccessTargetsBySourceModelID[root.ConfigID])
	enabledIndex := 0
	stageRows := make([]DiagnosticsAccessTarget, 0, len(authored))
	for _, target := range authored {
		if IsTargetTypeForStage(target.TargetType, targetType) {
			stageRows = append(stageRows, target)
		}
	}
	for index, target := range stageRows {
		row := DiagnosticsTarget{
			AccessTargetID:        target.ID,
			AuthoredStagePosition: index,
		}
		if target.IsEnabled {
			row.EnabledStrategyIndex = intPointerCopy(enabledIndex)
			enabledIndex++
		}
		if IsModelTargetType(target.TargetType) {
			row.TargetModelConfigID = cloneIntPointer(target.TargetModelConfigID)
		} else {
			row.ConnectionID = cloneIntPointer(target.TargetConnectionID)
		}
		built.Targets = append(built.Targets, row)
	}
	return built
}

func fillTerminalRowCoverage(graph *DiagnosticsGraph, root DiagnosticsModel, stage *DiagnosticsStage, acceptedOperations []string) {
	for index := range stage.Targets {
		row := &stage.Targets[index]
		if row.ConnectionID == nil {
			continue
		}
		connection, ok := graph.ConnectionsByID[*row.ConnectionID]
		if !ok {
			row.Coverage = string(CoverageNotApplicable)
			continue
		}
		if !IsOpenAIFamily(connection.APIFamily) || connection.OpenAITextCapability == nil {
			row.Coverage = string(CoverageNotApplicable)
			continue
		}
		supported := OpenAITargetSupportedOperationSet(*connection.OpenAITextCapability)
		coverage, supportedAccepted, unsupportedAccepted := ClassifyOpenAICoverage(acceptedOperations, supported)
		row.Coverage = string(coverage)
		row.SupportedOperations = supportedAccepted
		row.UnsupportedAcceptedOperations = unsupportedAccepted
	}
}

func IsTargetTypeForStage(targetType string, stageType string) bool {
	if stageType == TargetTypeModel {
		return IsModelTargetType(targetType)
	}
	return IsTerminalTargetType(targetType)
}

// stageCandidateForOperation resolves one stage for one operation, filling
// per-target operation results in place. It returns whether the stage produced
// a candidate, the resolved terminal leaf connection ids, and the ids of rows
// whose subtree/leaf is capability-compatible (ignoring enable/active).
func stageCandidateForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, stage *DiagnosticsStage, strategy DiagnosticsStrategy, operation string) (bool, []int, []int) {
	candidate := false
	leaves := []int{}
	compatibleRows := []int{}
	strategyConsiders := stageStrategyConsideredRows(stage.Targets, strategy)
	for index := range stage.Targets {
		row := &stage.Targets[index]
		disposition, rowLeaves := resolveRowForOperation(graph, root, stage.Stage, *row, strategyConsiders[row.AccessTargetID], operation)
		if len(rowLeaves) > 0 {
			leaves = append(leaves, rowLeaves...)
		}
		if disposition == DispositionCandidate {
			candidate = true
			compatibleRows = append(compatibleRows, row.AccessTargetID)
		}
		row.OperationResults = []DiagnosticsOperationResult{{OperationName: operation, Disposition: disposition, TerminalConnectionIDs: rowLeaves}}
	}
	// Compatible rows include capability-compatible leaves even when statically
	// ineligible (disabled/inactive/truncated).
	for index := range stage.Targets {
		row := &stage.Targets[index]
		if len(row.OperationResults) == 0 || row.OperationResults[0].Disposition != DispositionCandidate {
			if rowIsCapabilityCompatibleForOperation(graph, root, stage.Stage, *row, operation) {
				compatibleRows = append(compatibleRows, row.AccessTargetID)
			}
		}
	}
	return candidate, leaves, sortedUniqueInts(compatibleRows)
}

func stageStrategyConsideredRows(targets []DiagnosticsTarget, strategy DiagnosticsStrategy) map[int]bool {
	considered := map[int]bool{}
	enabledIDs := make([]int, 0, len(targets))
	for _, row := range targets {
		if row.EnabledStrategyIndex != nil {
			enabledIDs = append(enabledIDs, row.AccessTargetID)
		}
	}
	if strings.EqualFold(strings.TrimSpace(strategy.Subtype), "single") && len(enabledIDs) > 1 {
		for index, id := range enabledIDs {
			considered[id] = index == 0
		}
		return considered
	}
	for _, id := range enabledIDs {
		considered[id] = true
	}
	return considered
}

func resolveRowForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, stage string, row DiagnosticsTarget, considered bool, operation string) (string, []int) {
	if row.EnabledStrategyIndex == nil {
		return DispositionDisabled, nil
	}
	if !considered {
		return DispositionTruncatedBySingle, nil
	}
	if stage == StageModelTargets {
		return resolveModelRowForOperation(graph, root, row, operation)
	}
	return resolveTerminalRowForOperation(graph, root, row, operation)
}

func resolveModelRowForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, row DiagnosticsTarget, operation string) (string, []int) {
	if row.TargetModelConfigID == nil {
		return DispositionStructuralError, nil
	}
	child, ok := graph.ModelsByID[*row.TargetModelConfigID]
	if !ok {
		return DispositionStructuralError, nil
	}
	if child.ProfileID != root.ProfileID || !SameAPIFamily(child.APIFamily, root.APIFamily) {
		return DispositionStructuralError, nil
	}
	if !child.IsEnabled {
		return DispositionNoEligibleLeaf, nil
	}
	childStrategy := graph.strategyForModel(child)
	childModelStage := buildStage(graph, child, StageModelTargets, 1, "always", TargetTypeModel)
	childTerminalStage := buildStage(graph, child, StageTerminalTargets, 2, "model_targets_has_no_eligible_candidate", TargetTypeTerminal)
	childModelCandidate, childModelLeaves, _ := stageCandidateForOperation(graph, child, &childModelStage, childStrategy, operation)
	if childModelCandidate {
		return DispositionCandidate, childModelLeaves
	}
	childTerminalCandidate, childTerminalLeaves, _ := stageCandidateForOperation(graph, child, &childTerminalStage, childStrategy, operation)
	if childTerminalCandidate {
		return DispositionCandidate, childTerminalLeaves
	}
	return DispositionNoEligibleLeaf, nil
}

func resolveTerminalRowForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, row DiagnosticsTarget, operation string) (string, []int) {
	if row.ConnectionID == nil {
		return DispositionStructuralError, nil
	}
	connection, ok := graph.ConnectionsByID[*row.ConnectionID]
	if !ok {
		return DispositionStructuralError, nil
	}
	if connection.ProfileID != root.ProfileID || !SameAPIFamily(connection.APIFamily, root.APIFamily) {
		return DispositionStructuralError, nil
	}
	if !connection.IsActive {
		return DispositionInactive, nil
	}
	if !terminalSupportsOperation(connection, operation) {
		return DispositionIncompatible, nil
	}
	return DispositionCandidate, []int{connection.ID}
}

func rowIsCapabilityCompatibleForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, stage string, row DiagnosticsTarget, operation string) bool {
	if stage == StageModelTargets {
		if row.TargetModelConfigID == nil {
			return false
		}
		child, ok := graph.ModelsByID[*row.TargetModelConfigID]
		if !ok || child.ProfileID != root.ProfileID || !SameAPIFamily(child.APIFamily, root.APIFamily) || !child.IsEnabled {
			return false
		}
		return graphHasCompatibleLeafForOperation(graph, child, operation)
	}
	if row.ConnectionID == nil {
		return false
	}
	connection, ok := graph.ConnectionsByID[*row.ConnectionID]
	if !ok || connection.ProfileID != root.ProfileID || !SameAPIFamily(connection.APIFamily, root.APIFamily) {
		return false
	}
	return terminalSupportsOperation(connection, operation)
}

// graphHasCompatibleLeafForOperation walks the authored graph reachable from
// model and reports whether any terminal leaf is capability-compatible with
// the operation, ignoring row enablement, connection activation and strategy
// truncation. Model targets only follow enabled same-profile/same-family
// models, matching runtime reachability.
func graphHasCompatibleLeafForOperation(graph *DiagnosticsGraph, model DiagnosticsModel, operation string) bool {
	return graphHasCompatibleLeafForOperationVisited(graph, model, operation, map[int]struct{}{})
}

func graphHasCompatibleLeafForOperationVisited(graph *DiagnosticsGraph, model DiagnosticsModel, operation string, visited map[int]struct{}) bool {
	if _, seen := visited[model.ConfigID]; seen {
		return false
	}
	nextVisited := make(map[int]struct{}, len(visited)+1)
	for id := range visited {
		nextVisited[id] = struct{}{}
	}
	nextVisited[model.ConfigID] = struct{}{}
	for _, target := range sortedDiagnosticsAccessTargets(graph.AccessTargetsBySourceModelID[model.ConfigID]) {
		if IsModelTargetType(target.TargetType) {
			if target.TargetModelConfigID == nil {
				continue
			}
			child, ok := graph.ModelsByID[*target.TargetModelConfigID]
			if !ok || child.ProfileID != model.ProfileID || !SameAPIFamily(child.APIFamily, model.APIFamily) || !child.IsEnabled {
				continue
			}
			if graphHasCompatibleLeafForOperationVisited(graph, child, operation, nextVisited) {
				return true
			}
			continue
		}
		if target.TargetConnectionID == nil {
			continue
		}
		connection, ok := graph.ConnectionsByID[*target.TargetConnectionID]
		if !ok || connection.ProfileID != model.ProfileID || !SameAPIFamily(connection.APIFamily, model.APIFamily) {
			continue
		}
		if terminalSupportsOperation(connection, operation) {
			return true
		}
	}
	return false
}

func terminalSupportsOperation(connection DiagnosticsConnection, operation string) bool {
	if !IsOpenAIFamily(connection.APIFamily) {
		// Non-OpenAI families have no capability matrix; structural eligibility
		// (enabled row, active connection, strategy consideration) is the only
		// compatibility rule.
		return true
	}
	if connection.OpenAITextCapability == nil {
		return false
	}
	return OpenAIFormatSupportsOperation(*connection.OpenAITextCapability, operation)
}

func (model DiagnosticsModel) acceptsOperation(operation string) bool {
	if !IsOpenAIFamily(model.APIFamily) {
		// Non-OpenAI families use the runtime registry's model-bound operations
		// with structural eligibility only; there is no capability matrix.
		return true
	}
	if model.OpenAIAcceptedFormat == nil {
		return false
	}
	return OpenAIFormatSupportsOperation(*model.OpenAIAcceptedFormat, operation)
}

func IsOpenAIFamily(apiFamily string) bool {
	return strings.EqualFold(strings.TrimSpace(apiFamily), "openai")
}

func (graph *DiagnosticsGraph) strategyForModel(model DiagnosticsModel) DiagnosticsStrategy {
	if model.LoadbalanceStrategyID == nil || graph == nil {
		return DiagnosticsStrategy{}
	}
	if strategy, ok := graph.StrategiesByModelID[*model.LoadbalanceStrategyID]; ok {
		return strategy
	}
	return DiagnosticsStrategy{}
}

func sortedDiagnosticsAccessTargets(targets []DiagnosticsAccessTarget) []DiagnosticsAccessTarget {
	if len(targets) == 0 {
		return nil
	}
	ordered := make([]DiagnosticsAccessTarget, len(targets))
	copy(ordered, targets)
	sort.Slice(ordered, func(left int, right int) bool {
		return CompareAccessTargetOrder(
			OrderKey{Position: ordered[left].Position, ID: ordered[left].ID},
			OrderKey{Position: ordered[right].Position, ID: ordered[right].ID},
		) < 0
	})
	return ordered
}

func sortedUniqueInts(values []int) []int {
	if len(values) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	unique := make([]int, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Ints(unique)
	return unique
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}
