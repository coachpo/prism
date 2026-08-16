package modelrouting

import (
	"testing"
)

func strategyPtr(id int) *int { return &id }

func ptr[T any](value T) *T { return &value }

// A model with two directly owned Terminal Targets: every accepted operation
// witnesses through whichever connections support it, and a disabled model
// produces no witness at all. Nothing here falls back — the earlier name came
// from a routing tier that does not exist.
func TestAnalyzeRouteWitnessSnapshotDirectTerminalTargets(t *testing.T) {
	graph := &DiagnosticsGraph{
		ModelsByID: map[int]DiagnosticsModel{
			1: {ConfigID: 1, ProfileID: 1, ModelID: "dual-model", APIFamily: "openai", IsEnabled: true, OpenAIAcceptedFormat: ptr("dual_native"), LoadbalanceStrategyID: strategyPtr(10)},
			2: {ConfigID: 2, ProfileID: 1, ModelID: "disabled-model", APIFamily: "openai", IsEnabled: false, OpenAIAcceptedFormat: ptr("dual_native"), LoadbalanceStrategyID: strategyPtr(10)},
		},
		AccessTargetsBySourceModelID: map[int][]DiagnosticsAccessTarget{
			1: {
				{ID: 101, SourceModelConfigID: 1, TargetType: "connection", TargetConnectionID: ptr(501), Position: 0, IsEnabled: true},
				{ID: 102, SourceModelConfigID: 1, TargetType: "connection", TargetConnectionID: ptr(502), Position: 1, IsEnabled: true},
			},
		},
		ConnectionsByID: map[int]DiagnosticsConnection{
			501: {ID: 501, ProfileID: 1, APIFamily: "openai", IsActive: true, OpenAITextCapability: ptr("dual_native")},
			502: {ID: 502, ProfileID: 1, APIFamily: "openai", IsActive: true, OpenAITextCapability: ptr("chat_completions_only")},
		},
		StrategiesByModelID: map[int]DiagnosticsStrategy{10: {ID: 10, Subtype: "fill-first"}},
	}

	snapshot := AnalyzeRouteWitnessSnapshot(graph, 7)
	if snapshot.GenerationLabel() != "7" {
		t.Fatalf("expected generation label 7, got %q", snapshot.GenerationLabel())
	}
	if snapshot.ConfigurationReadyModelCount != 1 {
		t.Fatalf("expected 1 configuration-ready model, got %d", snapshot.ConfigurationReadyModelCount)
	}
	// dual_native accepted operations: chat, responses, responses.input_tokens,
	// responses.compact — all witness through connection 501 (dual). Connection
	// 502 (chat-only) covers chat too.
	if snapshot.RouteWitnessCount != 5 {
		t.Fatalf("expected 5 route witnesses, got %d: %+v", snapshot.RouteWitnessCount, snapshot.Witnesses)
	}
	if snapshot.RouteReadyModelCount != 1 {
		t.Fatalf("expected 1 route-ready model, got %d", snapshot.RouteReadyModelCount)
	}
	representative := snapshot.RepresentativeWitnessRef()
	if representative == nil {
		t.Fatal("expected representative witness")
	}
	if representative.ModelConfigID != "1" || representative.OperationName != "openai.chat_completions" {
		t.Fatalf("unexpected representative witness %+v", representative)
	}
	if representative.Generation != "7" {
		t.Fatalf("expected representative generation 7, got %q", representative.Generation)
	}
	summary := snapshot.ModelSummary(1)
	if summary.Configuration.State != "ready" || summary.Application.State != "ready" {
		t.Fatalf("expected model 1 ready/ready, got %+v", summary)
	}
	if summary.RouteWitnessCount != 5 {
		t.Fatalf("expected model 1 witness count 5, got %d", summary.RouteWitnessCount)
	}
	disabledSummary := snapshot.ModelSummary(2)
	if disabledSummary.Configuration.State != "not_ready" {
		t.Fatalf("expected disabled model not_ready, got %+v", disabledSummary)
	}
}

// A parent whose only access target is a Model Target takes its witness from the
// child's own terminal target, narrowed to the operations the child accepts.
func TestAnalyzeRouteWitnessSnapshotResolvesThroughModelTarget(t *testing.T) {
	// Parent has no terminal targets; child model has a terminal target.
	graph := &DiagnosticsGraph{
		ModelsByID: map[int]DiagnosticsModel{
			1: {ConfigID: 1, ProfileID: 1, ModelID: "parent", APIFamily: "openai", IsEnabled: true, OpenAIAcceptedFormat: ptr("dual_native"), LoadbalanceStrategyID: strategyPtr(10)},
			2: {ConfigID: 2, ProfileID: 1, ModelID: "child", APIFamily: "openai", IsEnabled: true, OpenAIAcceptedFormat: ptr("chat_completions_only"), LoadbalanceStrategyID: strategyPtr(10)},
		},
		AccessTargetsBySourceModelID: map[int][]DiagnosticsAccessTarget{
			1: {
				{ID: 201, SourceModelConfigID: 1, TargetType: "model", TargetModelConfigID: ptr(2), Position: 0, IsEnabled: true},
			},
			2: {
				{ID: 202, SourceModelConfigID: 2, TargetType: "connection", TargetConnectionID: ptr(601), Position: 0, IsEnabled: true},
			},
		},
		ConnectionsByID: map[int]DiagnosticsConnection{
			601: {ID: 601, ProfileID: 1, APIFamily: "openai", IsActive: true, OpenAITextCapability: ptr("chat_completions_only")},
		},
		StrategiesByModelID: map[int]DiagnosticsStrategy{10: {ID: 10, Subtype: "fill-first"}},
	}

	snapshot := AnalyzeRouteWitnessSnapshot(graph, 1)
	// Parent accepts 4 operations; only chat_completions resolves through the
	// child (chat-only). The other three parent operations have no path.
	parentWitnesses := snapshot.ByModelConfigID[1]
	if len(parentWitnesses) != 1 || parentWitnesses[0].OperationName != "openai.chat_completions" {
		t.Fatalf("expected parent to resolve chat via child, got %+v", parentWitnesses)
	}
	if parentWitnesses[0].TerminalTargetID != "601" {
		t.Fatalf("expected parent witness to carry the child's terminal target, got %+v", parentWitnesses[0])
	}
}

func TestAnalyzeRouteWitnessSnapshotCycleSafe(t *testing.T) {
	// A <-> B model-target cycle must not hang or duplicate witnesses.
	graph := &DiagnosticsGraph{
		ModelsByID: map[int]DiagnosticsModel{
			1: {ConfigID: 1, ProfileID: 1, ModelID: "a", APIFamily: "openai", IsEnabled: true, OpenAIAcceptedFormat: ptr("dual_native"), LoadbalanceStrategyID: strategyPtr(10)},
			2: {ConfigID: 2, ProfileID: 1, ModelID: "b", APIFamily: "openai", IsEnabled: true, OpenAIAcceptedFormat: ptr("dual_native"), LoadbalanceStrategyID: strategyPtr(10)},
		},
		AccessTargetsBySourceModelID: map[int][]DiagnosticsAccessTarget{
			1: {{ID: 301, SourceModelConfigID: 1, TargetType: "model", TargetModelConfigID: ptr(2), Position: 0, IsEnabled: true}},
			2: {{ID: 302, SourceModelConfigID: 2, TargetType: "model", TargetModelConfigID: ptr(1), Position: 0, IsEnabled: true}},
		},
		ConnectionsByID:     map[int]DiagnosticsConnection{},
		StrategiesByModelID: map[int]DiagnosticsStrategy{10: {ID: 10, Subtype: "fill-first"}},
	}
	snapshot := AnalyzeRouteWitnessSnapshot(graph, 1)
	if snapshot.RouteWitnessCount != 0 {
		t.Fatalf("expected zero witnesses for a cycle with no terminal targets, got %d", snapshot.RouteWitnessCount)
	}
}

func TestAnalyzeRouteWitnessSnapshotUnresolvableStrategy(t *testing.T) {
	graph := &DiagnosticsGraph{
		ModelsByID: map[int]DiagnosticsModel{
			1: {ConfigID: 1, ProfileID: 1, ModelID: "broken", APIFamily: "openai", IsEnabled: true, OpenAIAcceptedFormat: ptr("dual_native"), LoadbalanceStrategyID: strategyPtr(999)},
		},
		AccessTargetsBySourceModelID: map[int][]DiagnosticsAccessTarget{},
		ConnectionsByID:              map[int]DiagnosticsConnection{},
		StrategiesByModelID:          map[int]DiagnosticsStrategy{10: {ID: 10, Subtype: "fill-first"}},
	}
	snapshot := AnalyzeRouteWitnessSnapshot(graph, 1)
	if snapshot.ConfigurationReadyModelCount != 0 {
		t.Fatalf("expected zero configuration-ready models when strategy is unresolved, got %d", snapshot.ConfigurationReadyModelCount)
	}
	summary := snapshot.ModelSummary(1)
	if summary.Configuration.State != "not_ready" {
		t.Fatalf("expected not_ready configuration, got %+v", summary)
	}
}

func TestAnalyzeRouteWitnessSnapshotDeterministicOrder(t *testing.T) {
	// Two terminal targets on the same model: the representative must be the
	// numerically smallest terminal target regardless of authored order.
	graph := &DiagnosticsGraph{
		ModelsByID: map[int]DiagnosticsModel{
			1: {ConfigID: 1, ProfileID: 1, ModelID: "m", APIFamily: "openai", IsEnabled: true, OpenAIAcceptedFormat: ptr("dual_native"), LoadbalanceStrategyID: strategyPtr(10)},
		},
		AccessTargetsBySourceModelID: map[int][]DiagnosticsAccessTarget{
			1: {
				{ID: 403, SourceModelConfigID: 1, TargetType: "connection", TargetConnectionID: ptr(703), Position: 1, IsEnabled: true},
				{ID: 401, SourceModelConfigID: 1, TargetType: "connection", TargetConnectionID: ptr(701), Position: 0, IsEnabled: true},
			},
		},
		ConnectionsByID: map[int]DiagnosticsConnection{
			701: {ID: 701, ProfileID: 1, APIFamily: "openai", IsActive: true, OpenAITextCapability: ptr("dual_native")},
			703: {ID: 703, ProfileID: 1, APIFamily: "openai", IsActive: true, OpenAITextCapability: ptr("dual_native")},
		},
		StrategiesByModelID: map[int]DiagnosticsStrategy{10: {ID: 10, Subtype: "fill-first"}},
	}
	snapshot := AnalyzeRouteWitnessSnapshot(graph, 1)
	representative := snapshot.RepresentativeWitnessRef()
	if representative == nil || representative.TerminalTargetID != "701" {
		t.Fatalf("expected representative terminal target 701 (numeric order), got %+v", representative)
	}
}
