package modelrouting

import (
	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
	"sort"
	"strings"
)

// Route witness types shared by the setup readiness projection (Model SPEC
// §4.4.1): the server-side analyzer is the single route-witness truth; the
// coordinator only ever holds the stable representative ref, never the full
// witness graph.

// ReadinessAxis is the three-state readiness axis with typed reason codes.
type ReadinessAxis struct {
	State       string   `json:"state"` // ready | not_ready | unknown
	ReasonCodes []string `json:"reason_codes"`
}

// RouteWitnessRef is the stable representative route witness. All ids are
// positive decimal strings on the wire and share the canonical EntityRef
// identity.
type RouteWitnessRef struct {
	WitnessID        string `json:"witness_id"`
	Generation       string `json:"generation"`
	ModelConfigID    string `json:"model_config_id"`
	ModelID          string `json:"model_id"`
	OperationName    string `json:"operation_name"`
	TerminalTargetID string `json:"terminal_target_id"`
	EndpointID       string `json:"endpoint_id"`
	Coverage         string `json:"coverage"` // full | partial | none
}

// RouteWitnessOperation is the small, HTTP-neutral registry projection the
// analyzer needs. The runtime package remains the catalog owner and passes an
// ordered slice into AnalyzeRouteWitnessSnapshotWithOperations.
type RouteWitnessOperation struct {
	Name      string
	APIFamily string
}

// ModelRouteReadinessSummary is the per-model compact readiness projection
// embedded in the models list.
type ModelRouteReadinessSummary struct {
	Configuration         ReadinessAxis          `json:"configuration"`
	Application           ReadinessAxis          `json:"application"`
	RouteWitnessCount     int                    `json:"route_witness_count"`
	RepresentativeWitness *RouteWitnessRef       `json:"representative_witness"`
	RouteSchedule         RouteScheduleQualifier `json:"route_schedule"`
}

// ProfileRouteReadiness is the top-level aggregate of one immutable analyzer
// snapshot.
type ProfileRouteReadiness struct {
	RouteWitnessGeneration       string                 `json:"route_witness_generation"`
	Configuration                ReadinessAxis          `json:"configuration"`
	Application                  ReadinessAxis          `json:"application"`
	ConfigurationReadyModelCount *int                   `json:"configuration_ready_model_count"`
	RouteReadyModelCount         *int                   `json:"route_ready_model_count"`
	RouteWitnessCount            *int                   `json:"route_witness_count"`
	RepresentativeWitness        *RouteWitnessRef       `json:"representative_witness"`
	RouteSchedule                RouteScheduleQualifier `json:"route_schedule"`
}

// routeWitnessSnapshot is the immutable analyzer output for one generation.
type RouteWitnessSnapshot struct {
	Generation                   int
	ConfigurationReadyModelCount int
	RouteReadyModelCount         int
	RouteWitnessCount            int
	ConfigurationReadyByModel    map[int]bool
	Witnesses                    []RouteWitnessRef
	ByModelConfigID              map[int][]RouteWitnessRef
	ByModelID                    map[string][]RouteWitnessRef
	// Schedule-limited witness counts. A witness is schedule-limited when its
	// terminal target has windows that do not cover the whole week, i.e. the
	// route it proves exists only during part of the week. Purely a
	// configuration property, so it stays stable within one generation.
	ScheduleLimitedWitnessCount    int
	ScheduleLimitedByModelConfigID map[int]int
	operationRanks                 map[string]int
}

// RouteScheduleQualifier qualifies a "ready" readiness verdict: the route
// exists, but only inside a routing window.
//
// It is a separate structured field rather than a reason code because
// ReasonCodes is empty by construction whenever the state is "ready", and the
// setup card renders reason codes verbatim next to a green check. Putting this
// there would print a raw enum key beside a success mark.
type RouteScheduleQualifier struct {
	ScheduleLimited     bool `json:"schedule_limited"`
	LimitedWitnessCount int  `json:"limited_witness_count"`
	TotalWitnessCount   int  `json:"total_witness_count"`
}

// routeWitnessAnalyzer computes static route witnesses for one profile
// graph. It is pure and cycle-safe: child-model resolution is bounded and
// never revisits a model on the current path.
type routeWitnessAnalyzer struct {
	graph         *DiagnosticsGraph
	snapshot      RouteWitnessSnapshot
	operations    map[string][]string
	operationRank map[string]int
	resolving     map[int]bool
	configuration map[int]bool
	application   map[int]bool
}

// operationRegistryRank returns the canonical operation ordering used for the
// stable representative selection (numeric order of the runtime operation
// registry).
func operationRegistryRank(operationName string) int {
	switch operationName {
	case "openai.chat_completions":
		return 1
	case "openai.responses":
		return 2
	case "openai.responses.input_tokens":
		return 3
	case "openai.responses.compact":
		return 4
	default:
		return 100
	}
}

// AnalyzeRouteWitnessSnapshot computes the immutable route-witness snapshot
// for the profile graph. Route eligibility: enabled model with resolvable
// explicit strategy; owner-accepted operation; an access target the model's
// strategy actually reaches; enabled terminal target with active Connection
// and existing Endpoint (the graph loader only includes connections joined to
// live endpoints).
func AnalyzeRouteWitnessSnapshot(graph *DiagnosticsGraph, generation int) RouteWitnessSnapshot {
	operations := make([]RouteWitnessOperation, 0, len(OpenAIRegisteredOperationList()))
	for _, operation := range OpenAIRegisteredOperationList() {
		operations = append(operations, RouteWitnessOperation{Name: operation, APIFamily: "openai"})
	}
	return AnalyzeRouteWitnessSnapshotWithOperations(graph, generation, operations)
}

// AnalyzeRouteWitnessSnapshotWithOperations computes witnesses against the
// caller-supplied ordered runtime operation projection. This keeps the domain
// pure while allowing every registered API family to participate.
func AnalyzeRouteWitnessSnapshotWithOperations(graph *DiagnosticsGraph, generation int, catalog []RouteWitnessOperation) RouteWitnessSnapshot {
	operationsByFamily := map[string][]string{}
	operationRanks := map[string]int{}
	for _, operation := range catalog {
		family := strings.ToLower(strings.TrimSpace(operation.APIFamily))
		name := strings.TrimSpace(operation.Name)
		if family == "" || name == "" || containsString(operationsByFamily[family], name) {
			continue
		}
		operationRanks[name] = len(operationRanks) + 1
		operationsByFamily[family] = append(operationsByFamily[family], name)
	}
	snapshot := RouteWitnessSnapshot{
		Generation:                generation,
		ConfigurationReadyByModel: map[int]bool{},
		ByModelConfigID:           map[int][]RouteWitnessRef{},
		ByModelID:                 map[string][]RouteWitnessRef{},
		operationRanks:            operationRanks,
	}
	if graph == nil {
		return snapshot
	}
	analyzer := &routeWitnessAnalyzer{
		graph:         graph,
		snapshot:      snapshot,
		operations:    operationsByFamily,
		operationRank: operationRanks,
		resolving:     map[int]bool{},
		configuration: map[int]bool{},
		application:   map[int]bool{},
	}
	// Two bottom-up passes, which is an evaluation order and not a routing
	// tier: every model's own reachable terminal targets first, so that a
	// model target row can then consume the child's complete witness set.
	// Model Target and Terminal Target rows are type-neutral peers to the
	// strategy — see docs/architecture.md — and both passes therefore skip
	// rows the strategy does not reach.
	for _, model := range graph.ModelsByID {
		if !model.IsEnabled {
			continue
		}
		if !analyzer.modelConfigurationReady(model) {
			continue
		}
		analyzer.snapshot.ConfigurationReadyModelCount++
		for _, operation := range analyzer.acceptedOperations(model) {
			analyzer.resolveTerminalFallback(model, operation)
		}
	}
	for _, model := range graph.ModelsByID {
		if !model.IsEnabled || !analyzer.configuration[model.ConfigID] {
			continue
		}
		for _, operation := range analyzer.acceptedOperations(model) {
			analyzer.resolveModelFirst(model.ConfigID, operation)
		}
	}
	analyzer.finalizeSnapshot()
	return analyzer.snapshot
}

func (analyzer *routeWitnessAnalyzer) acceptedOperations(model DiagnosticsModel) []string {
	if IsOpenAIFamily(model.APIFamily) {
		return OpenAIAcceptedOperationSetForDimensions(model.OpenAIAcceptedFormat, model.OpenAIImageOperations)
	}
	return append([]string(nil), analyzer.operations[strings.ToLower(strings.TrimSpace(model.APIFamily))]...)
}

// GenerationLabel renders the canonical positive decimal generation string.
func (snapshot RouteWitnessSnapshot) GenerationLabel() string {
	if snapshot.Generation < 1 {
		snapshot.Generation = 1
	}
	return intToDecimalString(snapshot.Generation)
}

// RepresentativeWitnessRef returns the stable representative witness for the
// snapshot (the first entry of the canonically ordered witness list), or nil
// when the snapshot has no witnesses.
func (snapshot RouteWitnessSnapshot) RepresentativeWitnessRef() *RouteWitnessRef {
	if len(snapshot.Witnesses) == 0 {
		return nil
	}
	representative := snapshot.Witnesses[0]
	representative.Generation = snapshot.GenerationLabel()
	return &representative
}

// ModelSummary builds the compact per-model readiness summary for one model
// config.
func (snapshot RouteWitnessSnapshot) ModelSummary(modelConfigID int) ModelRouteReadinessSummary {
	modelWitnesses := snapshot.ByModelConfigID[modelConfigID]
	configuration := ReadinessAxis{State: "not_ready", ReasonCodes: []string{"model_not_ready"}}
	if snapshot.ConfigurationReadyByModel[modelConfigID] {
		configuration = ReadinessAxis{State: "ready", ReasonCodes: []string{}}
	}
	application := ReadinessAxis{State: "not_ready", ReasonCodes: []string{"no_route_witness"}}
	if len(modelWitnesses) > 0 {
		application = ReadinessAxis{State: "ready", ReasonCodes: []string{}}
	}
	limited := snapshot.ScheduleLimitedByModelConfigID[modelConfigID]
	summary := ModelRouteReadinessSummary{
		Configuration:     configuration,
		Application:       application,
		RouteWitnessCount: len(modelWitnesses),
		RouteSchedule: RouteScheduleQualifier{
			ScheduleLimited:     limited > 0,
			LimitedWitnessCount: limited,
			TotalWitnessCount:   len(modelWitnesses),
		},
	}
	if len(modelWitnesses) > 0 {
		sorted := sortWitnessesByRegistryRank(modelWitnesses, snapshot.operationRanks)
		representative := sorted[0]
		representative.Generation = snapshot.GenerationLabel()
		summary.RepresentativeWitness = &representative
	}
	return summary
}

// ProfileReadiness builds the top-level profile aggregate from the snapshot.
// Unknown (analyzer failure / generation unavailable) is expressed by the
// caller; here only authoritative results are produced.
func (snapshot RouteWitnessSnapshot) ProfileReadiness() ProfileRouteReadiness {
	configurationReadyCount := snapshot.ConfigurationReadyModelCount
	routeReadyCount := snapshot.RouteReadyModelCount
	witnessCount := snapshot.RouteWitnessCount
	configuration := ReadinessAxis{State: "not_ready", ReasonCodes: []string{"no_ready_model"}}
	if configurationReadyCount > 0 {
		configuration = ReadinessAxis{State: "ready", ReasonCodes: []string{}}
	}
	application := ReadinessAxis{State: "not_ready", ReasonCodes: []string{"no_route_witness"}}
	if witnessCount > 0 {
		application = ReadinessAxis{State: "ready", ReasonCodes: []string{}}
	}
	return ProfileRouteReadiness{
		RouteWitnessGeneration: snapshot.GenerationLabel(),
		RouteSchedule: RouteScheduleQualifier{
			ScheduleLimited:     snapshot.ScheduleLimitedWitnessCount > 0,
			LimitedWitnessCount: snapshot.ScheduleLimitedWitnessCount,
			TotalWitnessCount:   witnessCount,
		},
		Configuration:                configuration,
		Application:                  application,
		ConfigurationReadyModelCount: &configurationReadyCount,
		RouteReadyModelCount:         &routeReadyCount,
		RouteWitnessCount:            &witnessCount,
		RepresentativeWitness:        snapshot.RepresentativeWitnessRef(),
	}
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// modelConfigurationReady reports whether the enabled model's own structure
// is valid: the explicit loadbalance_strategy_id resolves to an existing
// strategy (the default is never used as a fallback).
func (analyzer *routeWitnessAnalyzer) modelConfigurationReady(model DiagnosticsModel) bool {
	if resolved, ok := analyzer.configuration[model.ConfigID]; ok {
		return resolved
	}
	ready := false
	if model.LoadbalanceStrategyID != nil {
		if _, ok := analyzer.graph.StrategiesByModelID[*model.LoadbalanceStrategyID]; ok {
			ready = true
		}
	}
	analyzer.configuration[model.ConfigID] = ready
	if ready {
		analyzer.snapshot.ConfigurationReadyByModel[model.ConfigID] = true
	}
	return ready
}

// resolveModelFirst resolves one owner-accepted operation through the
// Model-first stage: an enabled child model that itself has a route witness
// for the operation. Cycle-safe via the resolving set.
func (analyzer *routeWitnessAnalyzer) resolveModelFirst(modelConfigID int, operation string) {
	if analyzer.resolving[modelConfigID] {
		return
	}
	owner, ok := analyzer.graph.ModelsByID[modelConfigID]
	if !ok {
		return
	}
	analyzer.resolving[modelConfigID] = true
	defer delete(analyzer.resolving, modelConfigID)
	considered := analyzer.strategyConsideredRows(modelConfigID)
	for _, target := range analyzer.graph.AccessTargetsBySourceModelID[modelConfigID] {
		if !IsModelTargetType(target.TargetType) || target.TargetModelConfigID == nil || !target.IsEnabled {
			continue
		}
		if !considered[target.ID] {
			continue
		}
		child, ok := analyzer.graph.ModelsByID[*target.TargetModelConfigID]
		if !ok || !child.IsEnabled || !SameAPIFamily(owner.APIFamily, child.APIFamily) || !analyzer.modelConfigurationReady(child) {
			continue
		}
		analyzer.resolveModelFirst(child.ConfigID, operation)
		if len(analyzer.snapshot.ByModelConfigID[child.ConfigID]) == 0 {
			continue
		}
		for _, witness := range analyzer.snapshot.ByModelConfigID[child.ConfigID] {
			if witness.OperationName == operation {
				terminalID := mustParseDecimalInt(witness.TerminalTargetID)
				endpointID := mustParseDecimalInt(witness.EndpointID)
				analyzer.addWitness(modelConfigID, operation, terminalID, endpointID, witness.Coverage)
				analyzer.application[modelConfigID] = true
				return
			}
		}
	}
}

// strategyConsideredRows reports which of the model's enabled access targets
// its strategy actually acts on. A witness is a claim that traffic can reach a
// terminal target, so a row the strategy never reaches cannot witness anything:
// under `single` only the first row of the authored mixed list counts.
func (analyzer *routeWitnessAnalyzer) strategyConsideredRows(modelConfigID int) map[int]bool {
	model, ok := analyzer.graph.ModelsByID[modelConfigID]
	if !ok {
		return map[int]bool{}
	}
	return strategyConsideredRows(analyzer.graph, model, analyzer.graph.strategyForModel(model))
}

// resolveTerminalFallback resolves the operation through the enabled
// terminal targets of the model itself.
func (analyzer *routeWitnessAnalyzer) resolveTerminalFallback(model DiagnosticsModel, operation string) {
	considered := analyzer.strategyConsideredRows(model.ConfigID)
	for _, target := range analyzer.graph.AccessTargetsBySourceModelID[model.ConfigID] {
		if !IsTerminalTargetType(target.TargetType) || target.TargetConnectionID == nil || !target.IsEnabled {
			continue
		}
		if !considered[target.ID] {
			continue
		}
		connection, ok := analyzer.graph.ConnectionsByID[*target.TargetConnectionID]
		if !ok || !connection.IsActive || connection.ID < 1 || connection.EndpointID < 1 {
			continue
		}
		if !SameAPIFamily(connection.APIFamily, model.APIFamily) {
			continue
		}
		if IsOpenAIFamily(connection.APIFamily) {
			supported := OpenAITargetSupportedOperationSetForDimensions(connection.OpenAITextCapability, connection.OpenAIImageCapability)
			if !containsString(supported, operation) {
				continue
			}
		} else if !containsString(analyzer.operations[strings.ToLower(strings.TrimSpace(connection.APIFamily))], operation) {
			continue
		}
		coverage := analyzeCoverageLabel(model, connection)
		analyzer.addWitness(model.ConfigID, operation, connection.ID, connection.EndpointID, coverage)
		analyzer.application[model.ConfigID] = true
	}
}

func analyzeCoverageLabel(model DiagnosticsModel, connection DiagnosticsConnection) string {
	if !IsOpenAIFamily(model.APIFamily) {
		return string(CoverageFull)
	}
	accepted := OpenAIAcceptedOperationSetForDimensions(model.OpenAIAcceptedFormat, model.OpenAIImageOperations)
	supported := OpenAITargetSupportedOperationSetForDimensions(connection.OpenAITextCapability, connection.OpenAIImageCapability)
	coverage, _, _ := ClassifyOpenAICoverage(accepted, supported)
	return string(coverage) // full | partial | none (none cannot happen here)
}

func (analyzer *routeWitnessAnalyzer) addWitness(modelConfigID int, operation string, terminalTargetID int, endpointID int, coverage string) {
	model := analyzer.graph.ModelsByID[modelConfigID]
	witness := RouteWitnessRef{
		WitnessID:        formatRouteWitnessID(modelConfigID, operation, terminalTargetID, endpointID),
		ModelConfigID:    intToDecimalString(modelConfigID),
		ModelID:          model.ModelID,
		OperationName:    operation,
		TerminalTargetID: intToDecimalString(terminalTargetID),
		EndpointID:       intToDecimalString(endpointID),
		Coverage:         coverage,
	}
	analyzer.snapshot.Witnesses = append(analyzer.snapshot.Witnesses, witness)
	analyzer.snapshot.ByModelConfigID[modelConfigID] = append(analyzer.snapshot.ByModelConfigID[modelConfigID], witness)
	analyzer.snapshot.ByModelID[model.ModelID] = append(analyzer.snapshot.ByModelID[model.ModelID], witness)
	// Pure configuration test: never IsOpenAt. Whether the window happens to be
	// open at this instant is not a property of the generation.
	if connection, ok := analyzer.graph.ConnectionsByID[terminalTargetID]; ok &&
		len(connection.RoutingWindows) > 0 && !terminaltarget.WindowsCoverFullWeek(connection.RoutingWindows) {
		analyzer.snapshot.ScheduleLimitedWitnessCount++
		if analyzer.snapshot.ScheduleLimitedByModelConfigID == nil {
			analyzer.snapshot.ScheduleLimitedByModelConfigID = map[int]int{}
		}
		analyzer.snapshot.ScheduleLimitedByModelConfigID[modelConfigID]++
	}
}

// finalizeSnapshot sorts witnesses and selects the stable representative by
// the canonical numeric order (model_config_id -> operation registry rank ->
// terminal_target_id -> endpoint_id -> witness id), independent of any API
// array order.
func (analyzer *routeWitnessAnalyzer) finalizeSnapshot() {
	witnesses := analyzer.snapshot.Witnesses
	sort.SliceStable(witnesses, func(left, right int) bool {
		a, b := witnesses[left], witnesses[right]
		if a.ModelConfigID != b.ModelConfigID {
			return compareDecimalStrings(a.ModelConfigID, b.ModelConfigID) < 0
		}
		leftRank := analyzer.rank(a.OperationName)
		rightRank := analyzer.rank(b.OperationName)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if a.TerminalTargetID != b.TerminalTargetID {
			return compareDecimalStrings(a.TerminalTargetID, b.TerminalTargetID) < 0
		}
		if a.EndpointID != b.EndpointID {
			return compareDecimalStrings(a.EndpointID, b.EndpointID) < 0
		}
		return a.WitnessID < b.WitnessID
	})
	analyzer.snapshot.Witnesses = witnesses
	routeReadyModels := map[int]bool{}
	for modelConfigID, modelWitnesses := range analyzer.snapshot.ByModelConfigID {
		if len(modelWitnesses) > 0 {
			routeReadyModels[modelConfigID] = true
		}
	}
	analyzer.snapshot.ConfigurationReadyModelCount = len(analyzer.snapshot.ConfigurationReadyByModel)
	analyzer.snapshot.RouteReadyModelCount = len(routeReadyModels)
	analyzer.snapshot.RouteWitnessCount = len(witnesses)
	for modelConfigID := range analyzer.snapshot.ByModelConfigID {
		analyzer.snapshot.ByModelConfigID[modelConfigID] = sortWitnessesByRegistryRank(analyzer.snapshot.ByModelConfigID[modelConfigID], analyzer.operationRank)
	}
}

func (analyzer *routeWitnessAnalyzer) rank(operation string) int {
	if rank, ok := analyzer.operationRank[operation]; ok {
		return rank
	}
	return operationRegistryRank(operation)
}

func sortWitnessesByRegistryRank(witnesses []RouteWitnessRef, ranks map[string]int) []RouteWitnessRef {
	sorted := append([]RouteWitnessRef(nil), witnesses...)
	sort.SliceStable(sorted, func(left, right int) bool {
		a, b := sorted[left], sorted[right]
		leftRank, leftKnown := ranks[a.OperationName]
		if !leftKnown {
			leftRank = operationRegistryRank(a.OperationName)
		}
		rightRank, rightKnown := ranks[b.OperationName]
		if !rightKnown {
			rightRank = operationRegistryRank(b.OperationName)
		}
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if a.TerminalTargetID != b.TerminalTargetID {
			return compareDecimalStrings(a.TerminalTargetID, b.TerminalTargetID) < 0
		}
		return compareDecimalStrings(a.EndpointID, b.EndpointID) < 0
	})
	return sorted
}

func containsString(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func intToDecimalString(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

func compareDecimalStrings(left string, right string) int {
	trimmedLeft := strings.TrimLeft(left, "0")
	trimmedRight := strings.TrimLeft(right, "0")
	if trimmedLeft == "" {
		trimmedLeft = "0"
	}
	if trimmedRight == "" {
		trimmedRight = "0"
	}
	if len(trimmedLeft) != len(trimmedRight) {
		if len(trimmedLeft) < len(trimmedRight) {
			return -1
		}
		return 1
	}
	return strings.Compare(trimmedLeft, trimmedRight)
}

func formatRouteWitnessID(modelConfigID int, operation string, terminalTargetID int, endpointID int) string {
	return strings.Join([]string{
		intToDecimalString(modelConfigID),
		operation,
		intToDecimalString(terminalTargetID),
		intToDecimalString(endpointID),
	}, ":")
}

func mustParseDecimalInt(value string) int {
	result := 0
	for _, c := range value {
		if c < '0' || c > '9' {
			break
		}
		result = result*10 + int(c-'0')
	}
	return result
}

// ModelEntityRef is the canonical Model EntityRef wire projection
// (umbrella SPEC §8.3): discriminated identity + name provenance + deleted
// state, all ids as positive decimal strings.
type ModelEntityRef struct {
	Kind          string `json:"kind"`
	ModelConfigID string `json:"model_config_id"`
	ModelID       string `json:"model_id"`
	Name          string `json:"name"`
	NameSource    string `json:"name_source"`
	Deleted       *bool  `json:"deleted"`
}
