package models

import (
	"encoding/json"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/domain/modelsdev"
)

// unselectableReason codes carried on source rows. The UI keys off these
// codes; message text lives in the frontend dictionary.
const (
	unselectableModelDisabled    = "model_disabled"
	unselectableNoTerminalTarget = "no_reachable_terminal_target"
	unselectableNoTextOperations = "no_accepted_text_operations"
)

// exportFactsInput bundles one consistent snapshot's raw rows.
type exportFactsInput struct {
	ModelRows  []exportModelRow
	TargetRows map[int][]exportTargetRow
	Bindings   map[int]catalogBindingRecord
	Catalog    *modelsdev.Catalog
	Graph      *modelrouting.DiagnosticsGraph
}

// buildSourceFacts projects the snapshot into the platform-scoped domain fact
// set. Enrichment candidates derive strictly from the in-memory catalog; a
// failed fetch or vanished offering marks enrichment unavailable without
// failing and without re-guessing coordinates.
func buildSourceFacts(platform modelexport.Platform, input exportFactsInput) (modelexport.SourceFacts, map[int]modelexport.PlatformCandidate) {
	facts := modelexport.SourceFacts{
		Platform:      platform,
		TargetVersion: modelexport.TargetVersion(platform),
		Enrichment:    map[int]modelexport.PlatformCandidate{},
	}
	if input.Catalog != nil {
		facts.CatalogRevision = input.Catalog.ETag
	}
	candidates := map[int]modelexport.PlatformCandidate{}
	for _, model := range input.ModelRows {
		routableTargetIDs, primaryRoutable := exportStaticRouteEvidence(model, input.Graph)
		targets := filterExportTargets(input.TargetRows[model.ID], routableTargetIDs)
		selectable, reason := exportSelectable(model, primaryRoutable)
		binding := input.Bindings[model.ID]
		prismMetadata := canonicalMetadataFromBinding(binding)
		if model.DisplayName != nil {
			// model_configs.display_name is first-party Prism truth. It must win
			// over both the persisted catalog source name and live models.dev
			// enrichment, including when the explicit value is an empty string.
			prismMetadata[modelexport.MetaName] = marshalRawJSON(*model.DisplayName)
		}
		fact := modelexport.ModelFact{
			ModelConfigID:         model.ID,
			ModelID:               model.ModelID,
			APIFamily:             model.APIFamily,
			DisplayName:           model.DisplayName,
			IsEnabled:             model.IsEnabled,
			Selectable:            selectable,
			OpenAIAcceptedFormat:  model.OpenAIAcceptedFormat,
			OpenAIImageOperations: model.OpenAIImageOperations,
			CatalogBinding:        exportCatalogEvidence(binding),
			Enrichment:            modelexport.EnrichmentEvidence{},
			PrismMetadata:         prismMetadata,
			Targets:               exportTargetFacts(targets),
		}
		if !selectable && reason != nil {
			fact.UnselectableReason = reason
		}
		if binding.ProviderID != "" && binding.CatalogModelID != "" {
			fact.Enrichment.OfferingProviderID = binding.ProviderID
			fact.Enrichment.OfferingModelID = binding.CatalogModelID
			if input.Catalog != nil {
				if offering, ok := input.Catalog.Find(binding.ProviderID, binding.CatalogModelID); ok {
					fact.Enrichment.Available = true
					candidate := modelexport.DeriveCandidate(platform, model.APIFamily, model.OpenAIAcceptedFormat, offering)
					candidates[model.ID] = candidate
					facts.Enrichment[model.ID] = candidate
				}
			}
		}
		facts.Models = append(facts.Models, fact)
	}
	return facts, candidates
}

// exportSelectable applies the selection truth rules. OpenAI models that
// accept no text operation cannot drive a coding client and are excluded.
func exportSelectable(model exportModelRow, primaryRoutable bool) (bool, *string) {
	reason := func(code string) *string { return &code }
	switch {
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

// exportCatalogEvidence strips clock fields from the binding record so the
// evidence stays digest-stable.
func exportCatalogEvidence(record catalogBindingRecord) modelexport.CatalogEvidence {
	if record.ModelConfigID == 0 && record.ProviderID == "" {
		return modelexport.CatalogEvidence{}
	}
	evidence := modelexport.CatalogEvidence{
		Bound:           true,
		ProviderID:      record.ProviderID,
		CatalogModelID:  record.CatalogModelID,
		CatalogRevision: record.CatalogRevision,
		MatchSource:     record.MatchSource,
		HasOverrides:    !record.Override.empty(),
	}
	return evidence
}

// canonicalMetadataFromBinding maps the stored effective metadata onto the
// canonical leaf names. Absent leaves stay absent; explicit values pass
// through verbatim.
func canonicalMetadataFromBinding(record catalogBindingRecord) map[string]json.RawMessage {
	values := map[string]json.RawMessage{}
	addString := func(leaf string, value *string) {
		if value != nil {
			values[leaf] = marshalRawJSON(*value)
		}
	}
	addBool := func(leaf string, value *bool) {
		if value != nil {
			values[leaf] = marshalRawJSON(*value)
		}
	}
	addInt := func(leaf string, value *int64) {
		if value != nil {
			values[leaf] = marshalRawJSON(*value)
		}
	}
	addList := func(leaf string, value []string) {
		if value != nil {
			values[leaf] = marshalRawJSON(value)
		}
	}
	effective := record.Source.effective(record.Override)
	addString(modelexport.MetaName, effective.Name)
	addString(modelexport.MetaDescription, effective.Description)
	addString(modelexport.MetaFamily, effective.Family)
	addBool(modelexport.MetaReasoning, effective.Reasoning)
	addBool(modelexport.MetaAttachment, effective.Attachment)
	addBool(modelexport.MetaToolCall, effective.ToolCall)
	addBool(modelexport.MetaTemperature, effective.Temperature)
	addInt(modelexport.MetaContextWindow, effective.LimitContext)
	addInt(modelexport.MetaMaxOutputTokens, effective.LimitOutput)
	addInt(modelexport.MetaMaxInputTokens, effective.LimitInput)
	addList(modelexport.MetaModalitiesInput, effective.ModalitiesInput)
	addList(modelexport.MetaModalitiesOutput, effective.ModalitiesOutput)
	addString(modelexport.MetaStatus, effective.Status)
	addString(modelexport.MetaReleaseDate, effective.ReleaseDate)
	addString(modelexport.MetaKnowledge, effective.Knowledge)
	return values
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
