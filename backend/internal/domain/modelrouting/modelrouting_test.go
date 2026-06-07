package modelrouting

import "testing"

func TestValidateAuthoredAccessTargetsContracts(t *testing.T) {
	terminalRef := "primary"
	modelID := "router-child"
	weight := 2
	priority := 3
	isEnabled := false

	targets := []AuthoredAccessTarget{
		{TargetType: TargetTypeTerminal, Position: 0, TerminalTargetRef: &terminalRef},
		{TargetType: TargetTypeModel, Position: 1, TargetModelID: &modelID, Weight: &weight, TargetPriority: &priority, IsEnabled: &isEnabled},
	}
	if issues := ValidateAuthoredAccessTargets(targets, ValidationOptions{TerminalTargetField: "connection_ref"}); len(issues) != 0 {
		t.Fatalf("expected valid targets, got %#v", issues)
	}

	duplicateRef := "primary"
	issues := ValidateAuthoredAccessTargets([]AuthoredAccessTarget{
		{TargetType: TargetTypeTerminal, Position: 0, TerminalTargetRef: &terminalRef},
		{TargetType: TargetTypeTerminal, Position: 1, TerminalTargetRef: &duplicateRef},
	}, ValidationOptions{TerminalTargetField: "connection_ref"})
	assertSingleIssue(t, issues, "target_duplicate", "access_targets[1].connection_ref")
}

func TestValidateAuthoredAccessTargetsReportsMetadataContract(t *testing.T) {
	terminalRef := "primary"
	weight := 1
	issues := ValidateAuthoredAccessTargets([]AuthoredAccessTarget{
		{TargetType: TargetTypeTerminal, Position: 0, TerminalTargetRef: &terminalRef, Weight: &weight},
	}, ValidationOptions{TerminalTargetField: "connection_ref"})
	assertSingleIssue(t, issues, "terminal_target_metadata_invalid", "access_targets[0].weight")
}

func TestValidateSourceModelTargetsReportsSelfTarget(t *testing.T) {
	modelID := "router"
	issues := ValidateSourceModelTargets(
		ModelNode{ConfigID: 10, ModelID: modelID, ProfileID: 1, APIFamily: "openai"},
		[]AuthoredAccessTarget{{TargetType: TargetTypeModel, Position: 0, TargetModelID: &modelID}},
		ValidationOptions{},
	)
	assertSingleIssue(t, issues, "model_graph_cycle", "access_targets[0].target_model_id")
}

func TestResolveAuthoredAccessTargetsOrdersAndDefaultsTargets(t *testing.T) {
	childID := "child"
	terminalRef := "primary"
	weight := 5
	priority := 7
	resolved, issues := ResolveAuthoredAccessTargets(
		[]AuthoredAccessTarget{
			{TargetType: TargetTypeModel, Position: 2, TargetModelID: &childID, Weight: &weight, TargetPriority: &priority},
			{TargetType: TargetTypeTerminal, Position: 1, TerminalTargetRef: &terminalRef},
		},
		ResolveOptions{
			Source: ModelNode{ConfigID: 1, ModelID: "router", ProfileID: 3, APIFamily: "openai"},
			ModelsByID: map[string]ModelNode{
				childID: {ConfigID: 2, ModelID: childID, ProfileID: 3, APIFamily: "OpenAI"},
			},
			TerminalTargetsByRef: map[string]TerminalTargetNode{
				terminalRef: {ID: 9, Ref: terminalRef, ProfileID: 3, APIFamily: "openai"},
			},
			TerminalTargetField: "connection_ref",
		},
	)
	if len(issues) != 0 {
		t.Fatalf("expected no issues, got %#v", issues)
	}
	if len(resolved) != 2 {
		t.Fatalf("expected 2 resolved targets, got %d", len(resolved))
	}
	if resolved[0].TargetType != TargetTypeTerminal || resolved[0].TerminalTargetRef == nil || *resolved[0].TerminalTargetRef != terminalRef {
		t.Fatalf("expected terminal target first, got %#v", resolved[0])
	}
	if resolved[1].TargetType != TargetTypeModel || resolved[1].Weight != weight || resolved[1].TargetPriority != priority {
		t.Fatalf("expected weighted model target second, got %#v", resolved[1])
	}
}

func TestResolveAuthoredAccessTargetsReportsCompatibilityIssues(t *testing.T) {
	childID := "child"
	resolved, issues := ResolveAuthoredAccessTargets(
		[]AuthoredAccessTarget{{TargetType: TargetTypeModel, Position: 0, TargetModelID: &childID}},
		ResolveOptions{
			Source:     ModelNode{ConfigID: 1, ModelID: "router", ProfileID: 3, APIFamily: "openai"},
			ModelsByID: map[string]ModelNode{childID: {ConfigID: 2, ModelID: childID, ProfileID: 3, APIFamily: "anthropic"}},
		},
	)
	if resolved != nil {
		t.Fatalf("expected no resolved targets, got %#v", resolved)
	}
	assertSingleIssue(t, issues, "target_api_family_mismatch", "access_targets[0].target_model_id")
}

func TestFindCycleReturnsDeterministicCycle(t *testing.T) {
	graph := map[string][]string{
		"router-b": {"router-c"},
		"router-c": {"router-b"},
		"router-a": {"router-z"},
	}
	cycle := FindCycle(graph, []string{"router-c", "router-a", "router-b"}, LessString)
	if cycle == nil {
		t.Fatal("expected cycle")
	}
	if cycle.Node != "router-b" {
		t.Fatalf("expected cycle node router-b, got %q", cycle.Node)
	}
	expectedPath := []string{"router-b", "router-c", "router-b"}
	if len(cycle.Path) != len(expectedPath) {
		t.Fatalf("expected path %v, got %v", expectedPath, cycle.Path)
	}
	for index, expected := range expectedPath {
		if cycle.Path[index] != expected {
			t.Fatalf("expected path %v, got %v", expectedPath, cycle.Path)
		}
	}
}

func assertSingleIssue(t *testing.T, issues []ValidationIssue, code string, path string) {
	t.Helper()
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %#v", issues)
	}
	if issues[0].Code != code {
		t.Fatalf("expected issue code %q, got %q", code, issues[0].Code)
	}
	if issues[0].Path != path {
		t.Fatalf("expected issue path %q, got %q", path, issues[0].Path)
	}
}
