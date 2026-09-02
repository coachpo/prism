package modelrouting

import (
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
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
	DispositionCycle             = "cycle"
	DispositionDepthExceeded     = "depth_exceeded"
)

// diagnosticsMaxModelDepth bounds the Model Target chain this analyzer will
// walk. The runtime resolver carries the same bound; a graph that exceeds it
// cannot be routed either way, and without a bound a cycle reaches the analyzer
// as a Go stack overflow, which is fatal rather than recoverable.
const diagnosticsMaxModelDepth = 32

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
	ConfigID  int
	ProfileID int
	ModelID   string
	APIFamily string
	IsEnabled bool
	// DirectRequestEnabled is the client-facing entry qualification. Model
	// Target resolution still includes enabled nodes regardless of this bit.
	DirectRequestEnabled  bool
	OpenAIAcceptedFormat  *string
	OpenAIImageOperations *string
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
	ID                    int
	ProfileID             int
	APIFamily             string
	EndpointID            int
	IsActive              bool
	OpenAITextCapability  *string
	OpenAIImageCapability *string
	// Raw authored configuration, not a compiled schedule: compiling would
	// require a timezone database lookup, whose result varies per instance and
	// would make the analyzer's output depend on something other than the
	// configuration generation it claims to describe.
	RoutingScheduleTimezone *string
	RoutingWindows          []terminaltarget.Window
}

type DiagnosticsStrategy struct {
	ID      int
	Subtype string
}

// DiagnosticsResult is the authoritative static routing diagnostics for one
// model config, computed by Analyze.
type DiagnosticsResult struct {
	ModelConfigID         int                            `json:"model_config_id"`
	DirectRequestEnabled  bool                           `json:"direct_request_enabled"`
	OpenAIAcceptedFormat  *string                        `json:"openai_accepted_format"`
	Strategy              DiagnosticsStrategyResult      `json:"strategy"`
	AcceptedOperations    []string                       `json:"accepted_operations"`
	Stages                []DiagnosticsStage             `json:"stages"`
	Targets               []DiagnosticsTarget            `json:"targets"`
	OperationRoutes       []DiagnosticsOperationRoute    `json:"operation_routes"`
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
	Schedule                      *DiagnosticsRoutingSchedule  `json:"schedule,omitempty"`
}

type DiagnosticsOperationResult struct {
	OperationName         string `json:"operation_name"`
	Disposition           string `json:"disposition"`
	TerminalConnectionIDs []int  `json:"terminal_connection_ids,omitempty"`
}

type DiagnosticsOperationRoute struct {
	OperationName        string  `json:"operation_name"`
	Accepted             bool    `json:"accepted"`
	ConfiguredLeafExists bool    `json:"configured_leaf_exists"`
	StaticallyRoutable   bool    `json:"statically_routable"`
	ResolvedStage        *string `json:"resolved_stage"`
	AccessTargetIDs      []int   `json:"access_target_ids"`
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
	result.DirectRequestEnabled = modelIsDirectEntry(root)
	result.OpenAIAcceptedFormat = cloneStringPointer(root.OpenAIAcceptedFormat)
	rootStrategy := graph.strategyForModel(root)
	result.Strategy = DiagnosticsStrategyResult{ID: rootStrategy.ID, Type: rootStrategy.Subtype}
	modelStage := buildStage(graph, root, StageModelTargets, 1, "always", TargetTypeModel)
	terminalStage := buildStage(graph, root, StageTerminalTargets, 2, "model_targets_has_no_eligible_candidate", TargetTypeTerminal)
	coverageOperations := acceptedOperationsForTerminalCoverage(root, acceptedOperations)
	fillTerminalRowCoverage(graph, root, &modelStage, coverageOperations)
	fillTerminalRowCoverage(graph, root, &terminalStage, coverageOperations)

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
		modelCandidate, modelLeaves, modelCompatibleRows := stageCandidateForOperation(graph, root, &modelStage, rootStrategy, trimmed, newModelWalk(root.ConfigID))
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
		terminalCandidate, terminalRows, terminalCompatibleRows := stageCandidateForOperation(graph, root, &terminalStage, rootStrategy, trimmed, newModelWalk(root.ConfigID))
		coverage.CapabilityCovered = terminalCandidate || len(modelCompatibleRows) > 0 || len(terminalCompatibleRows) > 0 || graphHasCompatibleLeafForOperation(graph, root, trimmed)
		coverage.CompatibleAccessTargetIDs = sortedUniqueInts(append(append([]int(nil), modelCompatibleRows...), terminalCompatibleRows...))
		if terminalCandidate {
			coverage.StaticallyRoutable = true
			resolvedStage := StageTerminalTargets
			coverage.ResolvedStage = &resolvedStage
			coverage.AccessTargetIDs = sortedUniqueInts(append(append(append(append([]int(nil), modelCompatibleRows...), modelLeaves...), terminalCompatibleRows...), terminalRows...))
		} else {
			coverage.AccessTargetIDs = sortedUniqueInts(append(append([]int(nil), modelCompatibleRows...), terminalCompatibleRows...))
		}
		result.OperationCoverage = append(result.OperationCoverage, coverage)
	}
	result.Stages = append(result.Stages, modelStage, terminalStage)
	result.Targets = append(append([]DiagnosticsTarget(nil), modelStage.Targets...), terminalStage.Targets...)
	result.OperationRoutes = buildOperationRoutes(result.OperationCoverage)
	result.ConfigurationWarnings = GenerateConfigurationWarnings(graph, root, result, acceptedOperations)
	return result
}

// buildOperationRoutes projects the per-operation route summary used by the
// diagnostics DTO: one row per operation with the accepted flag, whether a
// compatible configured leaf exists, static routability, the resolved stage
// and the ordered access-target ids.
func buildOperationRoutes(coverage []DiagnosticsOperationCoverage) []DiagnosticsOperationRoute {
	routes := make([]DiagnosticsOperationRoute, 0, len(coverage))
	for _, item := range coverage {
		routes = append(routes, DiagnosticsOperationRoute{
			OperationName:        item.OperationName,
			Accepted:             item.Accepted,
			ConfiguredLeafExists: item.CapabilityCovered,
			StaticallyRoutable:   item.StaticallyRoutable,
			ResolvedStage:        cloneStringPointer(item.ResolvedStage),
			AccessTargetIDs:      append([]int(nil), item.AccessTargetIDs...),
		})
	}
	return routes
}

// acceptedOperationsForTerminalCoverage returns the root model's actual
// accepted operation set for the directional target coverage badge. The
// diagnostics operation list intentionally includes all registered OpenAI
// model-bound operations so callers can see root-unaccepted rows; those rows
// must not make a target look Partial/None when they are outside the model's
// accepted format.
func acceptedOperationsForTerminalCoverage(root DiagnosticsModel, operationList []string) []string {
	if !IsOpenAIFamily(root.APIFamily) {
		return operationList
	}
	if root.OpenAIAcceptedFormat == nil && root.OpenAIImageOperations == nil {
		return operationList
	}
	return OpenAIAcceptedOperationSetForDimensions(root.OpenAIAcceptedFormat, root.OpenAIImageOperations)
}

// modelEnabledStrategyIndexes numbers the model's enabled access targets in one
// sequence over the authored mixed list. Model Target and Terminal Target rows
// are type-neutral peers to the strategy, so the index a row carries has to come
// from that single ordering — numbering each stage from zero would make index 0
// mean "first of its type", which is not what any strategy acts on.
func modelEnabledStrategyIndexes(graph *DiagnosticsGraph, root DiagnosticsModel) map[int]int {
	indexes := map[int]int{}
	next := 0
	for _, target := range sortedDiagnosticsAccessTargets(graph.AccessTargetsBySourceModelID[root.ConfigID]) {
		if !target.IsEnabled {
			continue
		}
		indexes[target.ID] = next
		next++
	}
	return indexes
}

func buildStage(graph *DiagnosticsGraph, root DiagnosticsModel, stage string, order int, enteredWhen string, targetType string) DiagnosticsStage {
	built := DiagnosticsStage{Stage: stage, Order: order, EnteredWhen: enteredWhen, Targets: []DiagnosticsTarget{}}
	authored := sortedDiagnosticsAccessTargets(graph.AccessTargetsBySourceModelID[root.ConfigID])
	enabledIndexes := modelEnabledStrategyIndexes(graph, root)
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
		if enabledIndex, ok := enabledIndexes[target.ID]; ok {
			row.EnabledStrategyIndex = intPointerCopy(enabledIndex)
		}
		if IsModelTargetType(target.TargetType) {
			row.TargetModelConfigID = cloneIntPointer(target.TargetModelConfigID)
		} else {
			row.ConnectionID = cloneIntPointer(target.TargetConnectionID)
			if row.ConnectionID != nil {
				if connection, ok := graph.ConnectionsByID[*row.ConnectionID]; ok {
					row.Schedule = routingScheduleProjection(connection)
				}
			}
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
		if !IsOpenAIFamily(connection.APIFamily) || (connection.OpenAITextCapability == nil && connection.OpenAIImageCapability == nil) {
			row.Coverage = string(CoverageNotApplicable)
			continue
		}
		supported := OpenAITargetSupportedOperationSetForDimensions(connection.OpenAITextCapability, connection.OpenAIImageCapability)
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
func stageCandidateForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, stage *DiagnosticsStage, strategy DiagnosticsStrategy, operation string, walk modelWalk) (bool, []int, []int) {
	candidate := false
	leaves := []int{}
	compatibleRows := []int{}
	strategyConsiders := strategyConsideredRows(graph, root, strategy)
	for index := range stage.Targets {
		row := &stage.Targets[index]
		disposition, rowLeaves := resolveRowForOperation(graph, root, stage.Stage, *row, strategyConsiders[row.AccessTargetID], operation, walk)
		if len(rowLeaves) > 0 {
			leaves = append(leaves, rowLeaves...)
		}
		if disposition == DispositionCandidate {
			candidate = true
			compatibleRows = append(compatibleRows, row.AccessTargetID)
		}
		row.OperationResults = append(row.OperationResults, DiagnosticsOperationResult{OperationName: operation, Disposition: disposition, TerminalConnectionIDs: rowLeaves})
	}
	// Compatible rows include capability-compatible leaves even when statically
	// ineligible (disabled/inactive/truncated).
	for index := range stage.Targets {
		row := &stage.Targets[index]
		candidateForOperation := false
		for _, result := range row.OperationResults {
			if result.OperationName == operation {
				candidateForOperation = result.Disposition == DispositionCandidate
				break
			}
		}
		if !candidateForOperation {
			if rowIsCapabilityCompatibleForOperation(graph, root, stage.Stage, *row, operation) {
				compatibleRows = append(compatibleRows, row.AccessTargetID)
			}
		}
	}
	return candidate, leaves, sortedUniqueInts(compatibleRows)
}

// strategyConsideredRows decides which of the model's enabled access targets the
// strategy actually acts on. `single` keeps the first row of the authored mixed
// list and nothing else — the runtime truncates that one sequence, so counting
// per stage here would keep one Model Target and one Terminal Target and report
// a route the runtime will never take.
func strategyConsideredRows(graph *DiagnosticsGraph, root DiagnosticsModel, strategy DiagnosticsStrategy) map[int]bool {
	considered := map[int]bool{}
	enabledIDs := enabledAccessTargetIDsInAuthoredOrder(graph, root)
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

func enabledAccessTargetIDsInAuthoredOrder(graph *DiagnosticsGraph, root DiagnosticsModel) []int {
	enabledIDs := []int{}
	for _, target := range sortedDiagnosticsAccessTargets(graph.AccessTargetsBySourceModelID[root.ConfigID]) {
		if target.IsEnabled {
			enabledIDs = append(enabledIDs, target.ID)
		}
	}
	return enabledIDs
}

// modelWalk carries the Model Target chain already entered on this branch. It
// exists so a cycle and an over-deep chain stay distinguishable: both terminate
// the walk, but only one of them is a graph the operator can fix by shortening.
type modelWalk struct {
	depth   int
	visited map[int]bool
}

// newModelWalk seeds the walk with the model the analysis started from, so an
// edge pointing back at the root is a cycle like any other.
func newModelWalk(rootModelConfigID int) modelWalk {
	return modelWalk{depth: 0, visited: map[int]bool{rootModelConfigID: true}}
}

func (w modelWalk) enter(modelConfigID int) modelWalk {
	next := modelWalk{depth: w.depth + 1, visited: make(map[int]bool, len(w.visited)+1)}
	for id := range w.visited {
		next.visited[id] = true
	}
	next.visited[modelConfigID] = true
	return next
}

func resolveRowForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, stage string, row DiagnosticsTarget, considered bool, operation string, walk modelWalk) (string, []int) {
	if row.EnabledStrategyIndex == nil {
		return DispositionDisabled, nil
	}
	if !considered {
		return DispositionTruncatedBySingle, nil
	}
	if stage == StageModelTargets {
		return resolveModelRowForOperation(graph, root, row, operation, walk)
	}
	return resolveTerminalRowForOperation(graph, root, row, operation)
}

func resolveModelRowForOperation(graph *DiagnosticsGraph, root DiagnosticsModel, row DiagnosticsTarget, operation string, walk modelWalk) (string, []int) {
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
	// Write-time validation keeps the stored graph acyclic, but this analyzer
	// runs on whatever the database holds — including a graph that arrived
	// around that validation. An unguarded walk would meet a cycle as a Go
	// stack overflow, which no recover can catch.
	if walk.visited[child.ConfigID] {
		return DispositionCycle, nil
	}
	if walk.depth >= diagnosticsMaxModelDepth {
		return DispositionDepthExceeded, nil
	}
	childWalk := walk.enter(child.ConfigID)
	childStrategy := graph.strategyForModel(child)
	childModelStage := buildStage(graph, child, StageModelTargets, 1, "always", TargetTypeModel)
	childTerminalStage := buildStage(graph, child, StageTerminalTargets, 2, "model_targets_has_no_eligible_candidate", TargetTypeTerminal)
	childModelCandidate, childModelLeaves, _ := stageCandidateForOperation(graph, child, &childModelStage, childStrategy, operation, childWalk)
	if childModelCandidate {
		return DispositionCandidate, childModelLeaves
	}
	childTerminalCandidate, childTerminalLeaves, _ := stageCandidateForOperation(graph, child, &childTerminalStage, childStrategy, operation, childWalk)
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
	if IsOpenAIImageOperation(operation) {
		if connection.OpenAIImageCapability == nil {
			return false
		}
		return OpenAIImageSupportsOperation(*connection.OpenAIImageCapability, operation)
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
	if IsOpenAIImageOperation(operation) {
		if model.OpenAIImageOperations == nil {
			return false
		}
		return OpenAIImageSupportsOperation(*model.OpenAIImageOperations, operation)
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

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}
