package modelrouting

import (
	"testing"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

const (
	testProfileID = 7
)

func testGraph() *DiagnosticsGraph {
	return &DiagnosticsGraph{
		ModelsByID:                   map[int]DiagnosticsModel{},
		AccessTargetsBySourceModelID: map[int][]DiagnosticsAccessTarget{},
		ConnectionsByID:              map[int]DiagnosticsConnection{},
		StrategiesByModelID:          map[int]DiagnosticsStrategy{},
	}
}

func (graph *DiagnosticsGraph) addModel(configID int, modelID string, apiFamily string, acceptedFormat string, enabled bool, strategyID *int) DiagnosticsModel {
	model := DiagnosticsModel{
		ConfigID:              configID,
		ProfileID:             testProfileID,
		ModelID:               modelID,
		APIFamily:             apiFamily,
		IsEnabled:             enabled,
		OpenAIAcceptedFormat:  stringRef(acceptedFormat),
		LoadbalanceStrategyID: strategyID,
	}
	graph.ModelsByID[configID] = model
	return model
}

func (graph *DiagnosticsGraph) addStrategy(strategyID int, subtype string) {
	graph.StrategiesByModelID[strategyID] = DiagnosticsStrategy{ID: strategyID, Subtype: subtype}
}

func (graph *DiagnosticsGraph) addConnection(connectionID int, apiFamily string, capability string, active bool) DiagnosticsConnection {
	connection := DiagnosticsConnection{
		ID:                   connectionID,
		ProfileID:            testProfileID,
		APIFamily:            apiFamily,
		IsActive:             active,
		OpenAITextCapability: stringRef(capability),
	}
	graph.ConnectionsByID[connectionID] = connection
	return connection
}

func (graph *DiagnosticsGraph) addTerminalRow(modelConfigID int, rowID int, connectionID int, position int, enabled bool) {
	graph.AccessTargetsBySourceModelID[modelConfigID] = append(graph.AccessTargetsBySourceModelID[modelConfigID], DiagnosticsAccessTarget{
		ID:                  rowID,
		ProfileID:           testProfileID,
		SourceModelConfigID: modelConfigID,
		TargetType:          TargetTypeTerminal,
		TargetConnectionID:  intRef(connectionID),
		Position:            position,
		IsEnabled:           enabled,
	})
}

func (graph *DiagnosticsGraph) addModelRow(modelConfigID int, rowID int, targetModelConfigID int, position int, enabled bool) {
	graph.AccessTargetsBySourceModelID[modelConfigID] = append(graph.AccessTargetsBySourceModelID[modelConfigID], DiagnosticsAccessTarget{
		ID:                  rowID,
		ProfileID:           testProfileID,
		SourceModelConfigID: modelConfigID,
		TargetType:          TargetTypeModel,
		TargetModelConfigID: intRef(targetModelConfigID),
		Position:            position,
		IsEnabled:           enabled,
	})
}

func stringRef(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intRef(value int) *int {
	return &value
}

func dualOperations() []string {
	return OpenAIAcceptedOperationSet(providerauth.OpenAITextCapabilityDualNative)
}

func chatOperations() []string {
	return OpenAIAcceptedOperationSet(providerauth.OpenAITextCapabilityChatCompletionsOnly)
}

func responsesOperations() []string {
	return OpenAIAcceptedOperationSet(providerauth.OpenAITextCapabilityResponsesOnly)
}

func resultForOperation(t *testing.T, result DiagnosticsResult, operation string) DiagnosticsOperationCoverage {
	t.Helper()
	for _, coverage := range result.OperationCoverage {
		if coverage.OperationName == operation {
			return coverage
		}
	}
	t.Fatalf("missing operation coverage for %q in %+v", operation, result.OperationCoverage)
	return DiagnosticsOperationCoverage{}
}

func TestOpenAICoverageDirectionalMatrix(t *testing.T) {
	cases := []struct {
		name             string
		modelFormat      string
		targetCapability string
		want             Coverage
	}{
		{name: "chat model vs chat target", modelFormat: "chat_completions_only", targetCapability: "chat_completions_only", want: CoverageFull},
		{name: "chat model vs responses target", modelFormat: "chat_completions_only", targetCapability: "responses_only", want: CoverageNone},
		{name: "chat model vs dual target", modelFormat: "chat_completions_only", targetCapability: "dual_native", want: CoverageFull},
		{name: "responses model vs chat target", modelFormat: "responses_only", targetCapability: "chat_completions_only", want: CoverageNone},
		{name: "responses model vs responses target", modelFormat: "responses_only", targetCapability: "responses_only", want: CoverageFull},
		{name: "responses model vs dual target", modelFormat: "responses_only", targetCapability: "dual_native", want: CoverageFull},
		{name: "dual model vs chat target", modelFormat: "dual_native", targetCapability: "chat_completions_only", want: CoveragePartial},
		{name: "dual model vs responses target", modelFormat: "dual_native", targetCapability: "responses_only", want: CoveragePartial},
		{name: "dual model vs dual target", modelFormat: "dual_native", targetCapability: "dual_native", want: CoverageFull},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			accepted := OpenAIAcceptedOperationSet(test.modelFormat)
			supported := OpenAITargetSupportedOperationSet(test.targetCapability)
			coverage, supportedAccepted, unsupported := ClassifyOpenAICoverage(accepted, supported)
			if coverage != test.want {
				t.Fatalf("expected coverage %s, got %s (supported=%v unsupported=%v)", test.want, coverage, supportedAccepted, unsupported)
			}
			if test.want == CoverageFull && len(unsupported) != 0 {
				t.Fatalf("expected full coverage to leave no unsupported operations, got %v", unsupported)
			}
			if test.want == CoverageNone && len(supportedAccepted) != 0 {
				t.Fatalf("expected none coverage to support no accepted operations, got %v", supportedAccepted)
			}
			if test.want == CoveragePartial && (len(supportedAccepted) == 0 || len(unsupported) == 0) {
				t.Fatalf("expected partial coverage to have both supported and unsupported accepted operations, got %v/%v", supportedAccepted, unsupported)
			}
		})
	}
}

func TestDiagnosticsResponsesOperationsShareOneGroup(t *testing.T) {
	for _, operation := range responsesOperations() {
		if OpenAIOperationGroup(operation) != OpenAIOperationGroupResponses {
			t.Fatalf("expected %q to map to responses group, got %q", operation, OpenAIOperationGroup(operation))
		}
	}
	if OpenAIOperationGroup(providerauth.OpenAIUpstreamOperationChatCompletions) != OpenAIOperationGroupChatCompletions {
		t.Fatalf("expected chat completions to map to chat group")
	}
}

func TestDiagnosticsTwoStageDirectTerminalCoverage(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "fill-first")
	graph.addModel(10, "dual-root", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "openai", "responses_only", true)
	graph.addTerminalRow(10, 1000, 100, 0, true)

	result := Analyze(graph, 10, dualOperations())
	if len(result.Stages) != 2 || result.Stages[0].Stage != StageModelTargets || result.Stages[1].Stage != StageTerminalTargets {
		t.Fatalf("expected two stages in order, got %+v", result.Stages)
	}
	if len(result.Stages[0].Targets) != 0 {
		t.Fatalf("expected empty model stage, got %+v", result.Stages[0].Targets)
	}
	terminalRow := result.Stages[1].Targets[0]
	if terminalRow.AccessTargetID != 1000 || terminalRow.AuthoredStagePosition != 0 || terminalRow.EnabledStrategyIndex == nil || *terminalRow.EnabledStrategyIndex != 0 {
		t.Fatalf("unexpected terminal row contract: %+v", terminalRow)
	}
	if terminalRow.Coverage != string(CoveragePartial) {
		t.Fatalf("expected dual model vs responses-only target to be partial, got %s", terminalRow.Coverage)
	}

	chat := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationChatCompletions)
	if !chat.Accepted || chat.CapabilityCovered || chat.StaticallyRoutable || chat.ResolvedStage != nil {
		t.Fatalf("expected chat operation to be uncovered (no compatible leaf), got %+v", chat)
	}
	responses := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationResponses)
	if !responses.Accepted || !responses.CapabilityCovered || !responses.StaticallyRoutable || responses.ResolvedStage == nil || *responses.ResolvedStage != StageTerminalTargets {
		t.Fatalf("expected responses operation to resolve in terminal stage, got %+v", responses)
	}
	if len(responses.CompatibleAccessTargetIDs) != 1 || responses.CompatibleAccessTargetIDs[0] != 1000 {
		t.Fatalf("expected compatible access target 1000, got %v", responses.CompatibleAccessTargetIDs)
	}
	for _, operation := range responsesOperations() {
		rowResult := terminalRow.OperationResults[0]
		_ = operation
		if rowResult.Disposition != DispositionCandidate || len(rowResult.TerminalConnectionIDs) != 1 || rowResult.TerminalConnectionIDs[0] != 100 {
			t.Fatalf("expected terminal row candidate for responses family, got %+v", rowResult)
		}
	}

	warnings := result.ConfigurationWarnings
	wantCodes := []string{WarningCodeOpenAIOperationUncovered, WarningCodeOpenAITargetPartialCoverage}
	if len(warnings) != 2 {
		t.Fatalf("expected 2 warnings (uncovered + partial), got %+v", warnings)
	}
	for index, want := range wantCodes {
		if warnings[index].Code != want {
			t.Fatalf("expected warning %d to be %s, got %+v", index, want, warnings[index])
		}
	}
	if warnings[1].AccessTargetID == nil || *warnings[1].AccessTargetID != 1000 || warnings[1].ConnectionID == nil || *warnings[1].ConnectionID != 100 {
		t.Fatalf("expected partial warning to carry access target and connection ids, got %+v", warnings[1])
	}
	if len(warnings[1].OperationNames) != 1 || warnings[1].OperationNames[0] != providerauth.OpenAIUpstreamOperationChatCompletions {
		t.Fatalf("expected partial warning to list chat completions as unsupported, got %+v", warnings[1])
	}
}

func TestDiagnosticsModelStageWinsOverTerminalFallback(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "fill-first")
	graph.addModel(10, "dual-root", "openai", "dual_native", true, intRef(1))
	graph.addModel(20, "dual-child", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "openai", "dual_native", true)
	graph.addConnection(200, "openai", "dual_native", true)
	graph.addModelRow(10, 1001, 20, 0, true)
	graph.addTerminalRow(20, 1002, 100, 0, true)
	graph.addTerminalRow(10, 1003, 200, 1, true)

	result := Analyze(graph, 10, dualOperations())
	chat := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationChatCompletions)
	if !chat.StaticallyRoutable || chat.ResolvedStage == nil || *chat.ResolvedStage != StageModelTargets {
		t.Fatalf("expected model stage to resolve before terminal fallback, got %+v", chat)
	}
	modelRow := result.Stages[0].Targets[0]
	if len(modelRow.OperationResults) != 1 || modelRow.OperationResults[0].Disposition != DispositionCandidate {
		t.Fatalf("expected model row candidate with leaf, got %+v", modelRow.OperationResults)
	}
	if len(modelRow.OperationResults[0].TerminalConnectionIDs) != 1 || modelRow.OperationResults[0].TerminalConnectionIDs[0] != 100 {
		t.Fatalf("expected model row to resolve child terminal 100, got %+v", modelRow.OperationResults[0])
	}
	terminalRow := result.Stages[1].Targets[0]
	if len(terminalRow.OperationResults) != 0 {
		t.Fatalf("expected terminal row to carry no results for an operation resolved in the model stage, got %+v", terminalRow.OperationResults)
	}
}

func TestDiagnosticsModelStageDeadFallsBackToTerminal(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "fill-first")
	graph.addModel(10, "dual-root", "openai", "dual_native", true, intRef(1))
	graph.addModel(20, "dead-child", "openai", "chat_completions_only", true, intRef(1))
	graph.addConnection(100, "openai", "chat_completions_only", true)
	graph.addConnection(200, "openai", "dual_native", true)
	graph.addModelRow(10, 1001, 20, 0, true)
	graph.addTerminalRow(20, 1002, 100, 0, true)
	graph.addTerminalRow(10, 1003, 200, 1, true)

	result := Analyze(graph, 10, dualOperations())
	responses := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationResponses)
	if !responses.StaticallyRoutable || responses.ResolvedStage == nil || *responses.ResolvedStage != StageTerminalTargets {
		t.Fatalf("expected dead model stage to fall back to terminal stage, got %+v", responses)
	}
	modelRow := result.Stages[0].Targets[0]
	if modelRow.OperationResults[0].Disposition != DispositionNoEligibleLeaf {
		t.Fatalf("expected dead model row to report no eligible leaf, got %+v", modelRow.OperationResults)
	}
	terminalRow := result.Stages[1].Targets[0]
	if terminalRow.OperationResults[0].Disposition != DispositionCandidate {
		t.Fatalf("expected fallback terminal row to be candidate, got %+v", terminalRow.OperationResults)
	}
}

func TestDiagnosticsSingleTruncatesPerStageAndUncoveredWithCompatibleTruncatedRow(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "single")
	graph.addModel(10, "single-root", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "openai", "chat_completions_only", true)
	graph.addConnection(200, "openai", "dual_native", true)
	graph.addTerminalRow(10, 1000, 100, 0, true)
	graph.addTerminalRow(10, 1001, 200, 1, true)

	result := Analyze(graph, 10, dualOperations())
	responses := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationResponses)
	if responses.StaticallyRoutable || !responses.CapabilityCovered {
		t.Fatalf("expected truncated compatible row to keep operation non-routable but capability covered, got %+v", responses)
	}
	stage := result.Stages[1]
	first := stage.Targets[0]
	second := stage.Targets[1]
	if first.OperationResults[0].Disposition != DispositionIncompatible {
		t.Fatalf("expected first single row to be incompatible for responses, got %+v", first.OperationResults)
	}
	if second.OperationResults[0].Disposition != DispositionTruncatedBySingle {
		t.Fatalf("expected second single row to be truncated, got %+v", second.OperationResults)
	}
	hasSingleWarning := false
	hasUncovered := false
	for _, warning := range result.ConfigurationWarnings {
		if warning.Code == WarningCodeSingleStrategyTruncatesTargets {
			hasSingleWarning = true
			if warning.Details["stage"] != StageTerminalTargets {
				t.Fatalf("expected single warning on terminal stage, got %+v", warning.Details)
			}
		}
		if warning.Code == WarningCodeOpenAIOperationUncovered && warning.Details["reason"] == UncoveredReasonNoStaticEligibleTarget {
			hasUncovered = true
		}
	}
	if !hasSingleWarning || !hasUncovered {
		t.Fatalf("expected single truncation and uncovered-with-compatible warnings, got %+v", result.ConfigurationWarnings)
	}
}

func TestDiagnosticsDisabledFirstRowThenEnabledSelectsNext(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "single")
	graph.addModel(10, "single-disabled-first", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "openai", "chat_completions_only", true)
	graph.addConnection(200, "openai", "dual_native", true)
	graph.addTerminalRow(10, 1000, 100, 0, false)
	graph.addTerminalRow(10, 1001, 200, 1, true)

	result := Analyze(graph, 10, dualOperations())
	stage := result.Stages[1]
	disabled := stage.Targets[0]
	enabled := stage.Targets[1]
	if disabled.EnabledStrategyIndex != nil || disabled.OperationResults[0].Disposition != DispositionDisabled {
		t.Fatalf("expected disabled row without strategy index, got %+v", disabled)
	}
	if enabled.EnabledStrategyIndex == nil || *enabled.EnabledStrategyIndex != 0 {
		t.Fatalf("expected enabled row to take strategy index 0, got %+v", enabled)
	}
	responses := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationResponses)
	if !responses.StaticallyRoutable {
		t.Fatalf("expected single to consider the first enabled row, got %+v", responses)
	}
}

func TestDiagnosticsInactiveConnectionIsNotCapabilityIncompatible(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "fill-first")
	graph.addModel(10, "inactive-root", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "openai", "dual_native", false)
	graph.addTerminalRow(10, 1000, 100, 0, true)

	result := Analyze(graph, 10, dualOperations())
	row := result.Stages[1].Targets[0]
	if row.OperationResults[0].Disposition != DispositionInactive {
		t.Fatalf("expected inactive connection disposition, got %+v", row.OperationResults)
	}
	chat := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationChatCompletions)
	if chat.StaticallyRoutable {
		t.Fatalf("expected inactive connection to keep operation non-routable, got %+v", chat)
	}
	if !chat.CapabilityCovered {
		t.Fatalf("expected inactive connection to still count as capability covered, got %+v", chat)
	}
}

func TestDiagnosticsStructuralErrorsAndCrossFamily(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "fill-first")
	graph.addModel(10, "structural-root", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "anthropic", "dual_native", true)
	graph.addConnection(200, "openai", "dual_native", true)
	graph.addTerminalRow(10, 1000, 100, 0, true)
	// Missing connection id on an enabled row.
	graph.AccessTargetsBySourceModelID[10] = append(graph.AccessTargetsBySourceModelID[10], DiagnosticsAccessTarget{
		ID: 1001, ProfileID: testProfileID, SourceModelConfigID: 10, TargetType: TargetTypeTerminal, Position: 1, IsEnabled: true,
	})

	result := Analyze(graph, 10, dualOperations())
	stage := result.Stages[1]
	if len(stage.Targets) != 2 {
		t.Fatalf("expected two terminal rows, got %+v", stage.Targets)
	}
	if stage.Targets[0].OperationResults[0].Disposition != DispositionStructuralError {
		t.Fatalf("expected cross-family connection to be structural error, got %+v", stage.Targets[0].OperationResults)
	}
	if stage.Targets[1].OperationResults[0].Disposition != DispositionStructuralError {
		t.Fatalf("expected missing connection to be structural error, got %+v", stage.Targets[1].OperationResults)
	}
	chat := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationChatCompletions)
	if chat.StaticallyRoutable || chat.CapabilityCovered {
		t.Fatalf("expected structural errors to keep operation uncovered, got %+v", chat)
	}
}

func TestDiagnosticsRootAcceptedSetPervadesRecursion(t *testing.T) {
	// Root accepts dual; intermediate accepts chat-only; final terminal is
	// responses-only. The recursion must stay on the ROOT accepted set, so the
	// responses leaf is compatible even though the intermediate model itself
	// does not accept responses as an ingress format.
	graph := testGraph()
	graph.addStrategy(1, "fill-first")
	graph.addModel(10, "dual-root", "openai", "dual_native", true, intRef(1))
	graph.addModel(20, "chat-intermediate", "openai", "chat_completions_only", true, intRef(1))
	graph.addConnection(100, "openai", "responses_only", true)
	graph.addModelRow(10, 1001, 20, 0, true)
	graph.addTerminalRow(20, 1002, 100, 0, true)

	result := Analyze(graph, 10, dualOperations())
	responses := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationResponses)
	if !responses.StaticallyRoutable || responses.ResolvedStage == nil || *responses.ResolvedStage != StageModelTargets {
		t.Fatalf("expected root accepted set to resolve responses through chat-only intermediate, got %+v", responses)
	}
	modelRow := result.Stages[0].Targets[0]
	if modelRow.OperationResults[0].Disposition != DispositionCandidate {
		t.Fatalf("expected model row candidate under root accepted set, got %+v", modelRow.OperationResults)
	}
}

func TestDiagnosticsRoundRobinReturnsPotentialUnionWithoutCursor(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "round-robin")
	graph.addModel(10, "rr-root", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "openai", "dual_native", true)
	graph.addConnection(200, "openai", "dual_native", true)
	graph.addTerminalRow(10, 1000, 100, 0, true)
	graph.addTerminalRow(10, 1001, 200, 1, true)

	result := Analyze(graph, 10, dualOperations())
	chat := resultForOperation(t, result, providerauth.OpenAIUpstreamOperationChatCompletions)
	if !chat.StaticallyRoutable {
		t.Fatalf("expected round-robin to expose candidate union, got %+v", chat)
	}
	if len(chat.CompatibleAccessTargetIDs) != 2 {
		t.Fatalf("expected both rows compatible under round-robin union, got %v", chat.CompatibleAccessTargetIDs)
	}
	for _, row := range result.Stages[1].Targets {
		if row.OperationResults[0].Disposition != DispositionCandidate {
			t.Fatalf("expected round-robin row to be candidate, got %+v", row.OperationResults)
		}
	}
}

func TestDiagnosticsNonOpenAIFamilyCoverageNotApplicable(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "fill-first")
	graph.addModel(10, "anthropic-root", "anthropic", "", true, intRef(1))
	graph.addConnection(100, "anthropic", "", true)
	graph.addTerminalRow(10, 1000, 100, 0, true)

	result := Analyze(graph, 10, []string{"anthropic.messages", "anthropic.count_tokens"})
	row := result.Stages[1].Targets[0]
	if row.Coverage != string(CoverageNotApplicable) {
		t.Fatalf("expected non-OpenAI terminal coverage to be not_applicable, got %s", row.Coverage)
	}
	coverage := resultForOperation(t, result, "anthropic.messages")
	if !coverage.Accepted || !coverage.StaticallyRoutable {
		t.Fatalf("expected non-OpenAI operation to resolve with structural eligibility, got %+v", coverage)
	}
	if row.OperationResults[0].Disposition != DispositionCandidate {
		t.Fatalf("expected non-OpenAI terminal row to be structurally eligible, got %+v", row.OperationResults)
	}
	if len(result.ConfigurationWarnings) != 0 {
		t.Fatalf("expected no capability warnings for non-OpenAI family, got %+v", result.ConfigurationWarnings)
	}
}

func TestRoutingSummaryProjection(t *testing.T) {
	graph := testGraph()
	graph.addStrategy(1, "single")
	graph.addModel(10, "summary-root", "openai", "dual_native", true, intRef(1))
	graph.addConnection(100, "openai", "chat_completions_only", true)
	graph.addConnection(200, "openai", "dual_native", true)
	graph.addTerminalRow(10, 1000, 100, 0, true)
	graph.addTerminalRow(10, 1001, 200, 1, true)

	root := graph.ModelsByID[10]
	result := Analyze(graph, 10, dualOperations())
	summary := BuildRoutingSummary(graph, root, result)
	if summary.EnabledAccessTargetCount != 2 || summary.TotalAccessTargetCount != 2 {
		t.Fatalf("expected 2 enabled / 2 total access targets, got %+v", summary)
	}
	if summary.Coverage != string(CoveragePartial) {
		t.Fatalf("expected partial overall coverage (chat routable, responses compatible but truncated), got %s", summary.Coverage)
	}
	if len(summary.SingleTruncatedStages) != 1 || summary.SingleTruncatedStages[0] != StageTerminalTargets {
		t.Fatalf("expected single truncation on terminal stage, got %v", summary.SingleTruncatedStages)
	}
	groups := map[string]string{}
	for _, group := range summary.OperationGroups {
		groups[group.Group] = group.Status
	}
	if groups[OpenAIOperationGroupChatCompletions] != GroupStatusRoutable {
		t.Fatalf("expected chat group routable, got %+v", groups)
	}
	if groups[OpenAIOperationGroupResponses] != GroupStatusCompatibleButIneligible {
		t.Fatalf("expected responses group compatible_but_ineligible, got %+v", groups)
	}
	wantCodes := []string{WarningCodeOpenAIOperationUncovered, WarningCodeOpenAITargetPartialCoverage, WarningCodeSingleStrategyTruncatesTargets}
	for _, code := range wantCodes {
		found := false
		for _, warningCode := range summary.WarningCodes {
			if warningCode == code {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected summary warning code %s, got %v", code, summary.WarningCodes)
		}
	}
}
