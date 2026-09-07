package models

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelexport"
	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/coachpo/prism/backend/internal/httpapi/runtime"
)

func buildOpenCodeSourceFacts(models []exportModelRow, targets map[int][]exportTargetRow, graph *modelrouting.DiagnosticsGraph, metadata map[int]openCodeStoredMetadata) modelexport.OpenCodeSourceFacts {
	facts := modelexport.OpenCodeSourceFacts{TargetVersion: modelexport.OpenCodeTargetVersion, Models: []modelexport.OpenCodeModelFact{}}
	for _, model := range models {
		allowed, primaryRoutable := exportStaticRouteEvidence(model, graph)
		selectable, reason := exportSelectable(model, primaryRoutable)
		if selectable && !openCodeModelAddressable(model) {
			selectable, reason = false, stringPointer("unaddressable_model_id")
		}
		stored := metadata[model.ID]
		if stored.Source == nil {
			stored.Source = map[string]json.RawMessage{}
		}
		if stored.Override == nil {
			stored.Override = map[string]json.RawMessage{}
		}
		fact := modelexport.OpenCodeModelFact{
			ModelConfigID: model.ID, ModelID: model.ModelID, APIFamily: model.APIFamily,
			DisplayName: model.DisplayName, IsEnabled: model.IsEnabled, Selectable: selectable,
			UnselectableReason: reason, OpenAIAcceptedFormat: model.OpenAIAcceptedFormat,
			OpenAIImageOperations: model.OpenAIImageOperations,
			Targets:               exportTargetFacts(filterExportTargets(targets[model.ID], allowed)),
			SourceMetadata:        stored.Source, OverrideMetadata: stored.Override,
		}
		if selectable && !modelexport.ProjectOpenCodeMetadata(fact).LimitsValid {
			fact.Selectable, fact.UnselectableReason = false, stringPointer("invalid_metadata_limits")
		}
		facts.Models = append(facts.Models, fact)
	}
	return facts
}

// Google SDK 3.0.73 interpolates IDs directly into its URL. Compare the
// decoded HTTP path to the unchanged ID, then use Prism's runtime matcher.
// JSON-body identities do not participate in URL parsing.
func openCodeModelAddressable(model exportModelRow) bool {
	ops := exportClientOperations(model)
	if len(ops) == 0 {
		return false
	}
	for _, operation := range runtime.RuntimeOperationCatalog() {
		if operation.Name != ops[0] {
			continue
		}
		if operation.ModelBindingSource != runtime.RuntimeOperationModelBindingPath {
			return true
		}
		// WHATWG URL parsing changes backslashes to path separators and strips
		// some control characters; net/url does not perform those normalizations.
		if strings.ContainsFunc(model.ModelID, func(r rune) bool { return r == '\\' || r < 0x20 || r == 0x7f }) {
			return false
		}
		path := strings.ReplaceAll(operation.PathTemplate, "{model}", model.ModelID)
		parsed, err := url.Parse("https://prism.invalid" + path)
		if err != nil {
			return false
		}
		params, matched := operation.PathMatcher.Match(parsed.Path)
		return matched && params["model"] == model.ModelID
	}
	return false
}
