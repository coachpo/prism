package models

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

// unselectableReason codes carried on source rows. The UI keys off these
// codes; message text lives in the frontend dictionary.
const (
	unselectableModelDisabled    = "model_disabled"
	unselectableModelNotDirect   = "model_not_direct_entry"
	unselectableNoTerminalTarget = "no_reachable_terminal_target"
	unselectableNoTextOperations = "no_accepted_text_operations"
)

// exportSelectable applies the selection truth rules. OpenAI models that
// accept no text operation cannot drive a coding client and are excluded.
func exportSelectable(model exportModelRow, primaryRoutable bool) (bool, *string) {
	reason := func(code string) *string { return &code }
	switch {
	case !model.DirectRequestEnabled:
		return false, reason(unselectableModelNotDirect)
	case !model.IsEnabled:
		return false, reason(unselectableModelDisabled)
	case model.APIFamily == "openai" && isBlankPointer(model.OpenAIAcceptedFormat):
		return false, reason(unselectableNoTextOperations)
	case len(exportClientOperations(model)) == 0:
		return false, reason(unselectableNoTextOperations)
	case !primaryRoutable:
		return false, reason(unselectableNoTerminalTarget)
	default:
		return true, nil
	}
}

// exportClientOperations is the exact operation set the generated client can
// use. dual_native is intentionally narrowed to Responses.
func exportClientOperations(model exportModelRow) []string {
	switch model.APIFamily {
	case "openai":
		if isBlankPointer(model.OpenAIAcceptedFormat) {
			return nil
		}
		operations := modelrouting.OpenAIAcceptedOperationSet(*model.OpenAIAcceptedFormat)
		if *model.OpenAIAcceptedFormat == "chat_completions_only" {
			return operations
		}
		filtered := make([]string, 0, len(operations))
		for _, operation := range operations {
			if operation != "openai.chat_completions" {
				filtered = append(filtered, operation)
			}
		}
		return filtered
	case "anthropic":
		return []string{"anthropic.messages", "anthropic.count_tokens"}
	case "gemini":
		// The coding-client streaming route is the primary conversation
		// operation. Non-stream generation and token counting remain in the
		// accepted set used to collect every actually reachable pricing leaf.
		return []string{"gemini.stream_generate_content", "gemini.generate_content", "gemini.count_tokens"}
	default:
		return nil
	}
}

// exportStaticRouteEvidence uses the same analyzer as the management routing
// diagnostics. The first client operation is the primary conversation route;
// only terminal leaves actually reachable for the exported operation set feed
// pricing and source evidence.
func exportStaticRouteEvidence(model exportModelRow, graph *modelrouting.DiagnosticsGraph) (map[int]struct{}, bool) {
	terminalIDs := map[int]struct{}{}
	operations := exportClientOperations(model)
	if graph == nil || len(operations) == 0 {
		return terminalIDs, false
	}
	analysis := modelrouting.Analyze(graph, model.ID, operations)
	routable := map[string]bool{}
	for _, route := range analysis.OperationRoutes {
		routable[route.OperationName] = route.Accepted && route.StaticallyRoutable
	}
	for _, target := range analysis.Targets {
		for _, result := range target.OperationResults {
			if !routable[result.OperationName] || result.Disposition != modelrouting.DispositionCandidate {
				continue
			}
			for _, id := range result.TerminalConnectionIDs {
				terminalIDs[id] = struct{}{}
			}
		}
	}
	return terminalIDs, routable[operations[0]]
}

func filterExportTargets(rows []exportTargetRow, allowed map[int]struct{}) []exportTargetRow {
	filtered := make([]exportTargetRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := allowed[row.TerminalTargetID]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func isBlankPointer(value *string) bool {
	return value == nil || *value == ""
}

// exportTargetFacts converts store rows into ordered domain target facts.
// Reachability filtering already happened in SQL (enabled chains, active
// connections); OpenAI text-mode reachability narrows pricing further.
func exportTargetFacts(rows []exportTargetRow) []modelexport.TargetFact {
	facts := make([]modelexport.TargetFact, 0, len(rows))
	for _, row := range rows {
		fact := modelexport.TargetFact{
			TerminalTargetID:     row.TerminalTargetID,
			Position:             row.HopPosition,
			EndpointID:           row.EndpointID,
			EndpointName:         row.EndpointName,
			OpenAITextCapability: row.OpenAITextCapability,
			Pricing:              row.Pricing,
		}
		facts = append(facts, fact)
	}
	return facts
}

// reachablePricingTargets narrows the reachable targets to those actually
// serving the model's accepted operations, mirroring runtime attempt
// eligibility for price-truth purposes.
func reachablePricingTargets(fact modelexport.ModelFact) []modelexport.TargetPriceSnapshot {
	snapshots := make([]modelexport.TargetPriceSnapshot, 0, len(fact.Targets))
	for _, target := range fact.Targets {
		if target.Pricing == nil {
			snapshots = append(snapshots, modelexport.TargetPriceSnapshot{TerminalTargetID: target.TerminalTargetID})
			continue
		}
		snapshot := *target.Pricing
		snapshot.TerminalTargetID = target.TerminalTargetID
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func marshalRawJSON(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return raw
}
