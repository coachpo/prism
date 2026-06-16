package runtime

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/providercompat"
)

type runtimeRoutingPlanValidationIssue struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

func validateRuntimeRoutingPlan(plan *runtimeRoutingPlan) error {
	issues := validateRuntimeRoutingPlanIssues(plan)
	if len(issues) == 0 {
		return nil
	}
	return invalidRuntimeRoutingPlanValidationError(issues)
}

func validateRuntimeRoutingPlanIssues(plan *runtimeRoutingPlan) []runtimeRoutingPlanValidationIssue {
	issues := make([]runtimeRoutingPlanValidationIssue, 0)
	if plan == nil {
		return appendRuntimeRoutingPlanValidationIssue(issues, "plan_nil", "plan", "plan is nil")
	}
	if plan.ModelsByID == nil {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "models_by_id_nil", "plan.models_by_id", "model-id lookup is nil")
	}
	if plan.ModelsByConfigID == nil {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "models_by_config_id_nil", "plan.models_by_config_id", "model config-id lookup is nil")
	}
	if plan.TerminalTargetsByID == nil {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_targets_by_id_nil", "plan.terminal_targets_by_id", "terminal target lookup is nil")
	}

	modelIDs := sortedRuntimeRoutingPlanModelIDs(plan.ModelsByID)
	for _, modelID := range modelIDs {
		compiled := plan.ModelsByID[modelID]
		path := fmt.Sprintf("plan.models_by_id[%q]", modelID)
		issues = validateRuntimeRoutingPlanModelIssues(plan, issues, path, modelID, compiled)
	}

	configIDs := sortedRuntimeRoutingPlanConfigIDs(plan.ModelsByConfigID)
	for _, configID := range configIDs {
		compiled := plan.ModelsByConfigID[configID]
		path := fmt.Sprintf("plan.models_by_config_id[%d]", configID)
		if compiled.Model.ID != configID {
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_config_key_mismatch", path, fmt.Sprintf("model config lookup key %d does not match compiled model id %d", configID, compiled.Model.ID))
		}
		byModelID, ok := plan.ModelsByID[compiled.Model.ModelID]
		if !ok {
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_config_missing_model_lookup", path, fmt.Sprintf("model config lookup for %q has no model-id lookup", compiled.Model.ModelID))
		} else if byModelID.Model.ID != compiled.Model.ID {
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_config_lookup_disagrees", path, fmt.Sprintf("model config lookup for %q points to model id %d but model-id lookup points to %d", compiled.Model.ModelID, compiled.Model.ID, byModelID.Model.ID))
		}
	}

	connectionIDs := sortedRuntimeRoutingPlanConnectionIDs(plan.TerminalTargetsByID)
	for _, connectionID := range connectionIDs {
		connection := plan.TerminalTargetsByID[connectionID]
		path := fmt.Sprintf("plan.terminal_targets_by_id[%d]", connectionID)
		issues = validateRuntimeRoutingPlanTerminalTargetIssues(issues, path, connectionID, connection)
	}
	if len(plan.ModelsByID) != len(plan.ModelsByConfigID) {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_lookup_size_mismatch", "plan", fmt.Sprintf("model lookup sizes differ: by id=%d by config id=%d", len(plan.ModelsByID), len(plan.ModelsByConfigID)))
	}
	issues = validateRuntimeRoutingPlanTopologyIssues(plan, issues)
	return issues
}

func validateRuntimeRoutingPlanModelIssues(plan *runtimeRoutingPlan, issues []runtimeRoutingPlanValidationIssue, path string, modelID string, compiled runtimeRoutingPlanModel) []runtimeRoutingPlanValidationIssue {
	if strings.TrimSpace(modelID) == "" {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_id_empty", path, "model map contains an empty model id")
	}
	if strings.TrimSpace(compiled.Model.ModelID) == "" {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_record_model_id_empty", path+".model.model_id", "compiled model has an empty model_id")
	}
	if compiled.Model.ModelID != modelID {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_id_key_mismatch", path, fmt.Sprintf("model map key %q does not match model_id %q", modelID, compiled.Model.ModelID))
	}
	if compiled.Model.ID <= 0 {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_config_id_invalid", path+".model.id", fmt.Sprintf("compiled model %q has invalid config id %d", compiled.Model.ModelID, compiled.Model.ID))
	}
	if strings.TrimSpace(compiled.Model.APIFamily) == "" {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_api_family_empty", path+".model.api_family", fmt.Sprintf("compiled model %q has an empty api family", compiled.Model.ModelID))
	}
	byConfigID, ok := plan.ModelsByConfigID[compiled.Model.ID]
	if !ok {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_missing_config_lookup", path, fmt.Sprintf("model %q missing config-id lookup", compiled.Model.ModelID))
	} else if byConfigID.Model.ModelID != compiled.Model.ModelID {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_config_lookup_mismatch", path, fmt.Sprintf("model %q config-id lookup points to %q", compiled.Model.ModelID, byConfigID.Model.ModelID))
	}
	if compiled.HasStrategy {
		issues = validateRuntimeRoutingPlanStrategyIssues(issues, path+".strategy", compiled)
	}
	return validateRuntimeRoutingPlanTargets(plan, issues, path, compiled)
}

func validateRuntimeRoutingPlanStrategyIssues(issues []runtimeRoutingPlanValidationIssue, path string, compiled runtimeRoutingPlanModel) []runtimeRoutingPlanValidationIssue {
	if compiled.Strategy.ID <= 0 {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "strategy_id_invalid", path+".id", fmt.Sprintf("model %q strategy has invalid id %d", compiled.Model.ModelID, compiled.Strategy.ID))
	}
	strategyType := normalizedRuntimeLegacyStrategyType(compiled.Strategy)
	switch strategyType {
	case "single", "fill-first", "round-robin", "cheapest_eligible_context", runtimeFacadeSelectionPolicyOrderedEligibleContext:
	default:
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "strategy_type_invalid", path+".legacy_strategy_type", fmt.Sprintf("model %q strategy has unsupported legacy_strategy_type %q", compiled.Model.ModelID, strategyType))
	}
	return issues
}

func validateRuntimeRoutingPlanTargets(plan *runtimeRoutingPlan, issues []runtimeRoutingPlanValidationIssue, path string, compiled runtimeRoutingPlanModel) []runtimeRoutingPlanValidationIssue {
	seenTargetIDs := map[int]struct{}{}
	for index, target := range compiled.OrderedEnabledTargets {
		targetPath := fmt.Sprintf("%s.ordered_enabled_targets[%d]", path, index)
		issues = validateRuntimeRoutingPlanTargetIssues(plan, issues, targetPath, compiled, target)
		if _, exists := seenTargetIDs[target.ID]; exists {
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "target_duplicate", targetPath, fmt.Sprintf("model %q target %d appears more than once", compiled.Model.ModelID, target.ID))
		}
		seenTargetIDs[target.ID] = struct{}{}
		if index > 0 && compareRuntimeAccessTargets(compiled.OrderedEnabledTargets[index-1], target) > 0 {
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "targets_not_sorted", path+".ordered_enabled_targets", fmt.Sprintf("model %q ordered targets are not sorted", compiled.Model.ModelID))
		}
	}
	issues = validateRuntimeRoutingPlanFallbackTargets(issues, path, compiled)
	issues = validateRuntimeRoutingPlanTerminalTargets(issues, path, compiled)
	return issues
}

func validateRuntimeRoutingPlanTargetIssues(plan *runtimeRoutingPlan, issues []runtimeRoutingPlanValidationIssue, path string, compiled runtimeRoutingPlanModel, target runtimeAccessTargetRecord) []runtimeRoutingPlanValidationIssue {
	if target.ID <= 0 {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "target_id_invalid", path+".id", fmt.Sprintf("model %q target has invalid id %d", compiled.Model.ModelID, target.ID))
	}
	if !target.IsEnabled {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "target_disabled", path+".is_enabled", fmt.Sprintf("model %q target %d is disabled in ordered target set", compiled.Model.ModelID, target.ID))
	}
	if target.ProfileID != compiled.Model.ProfileID {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "target_profile_mismatch", path+".profile_id", fmt.Sprintf("model %q target %d has profile %d", compiled.Model.ModelID, target.ID, target.ProfileID))
	}
	if target.SourceModelConfigID != compiled.Model.ID {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "target_source_model_mismatch", path+".source_model_config_id", fmt.Sprintf("model %q target %d has source model %d", compiled.Model.ModelID, target.ID, target.SourceModelConfigID))
	}
	switch target.TargetType {
	case runtimeAccessTargetTypeConnection:
		return validateRuntimeRoutingPlanConnectionTargetIssues(plan, issues, path, compiled, target)
	case runtimeAccessTargetTypeModel:
		return validateRuntimeRoutingPlanModelTargetIssues(plan, issues, path, compiled, target)
	default:
		return appendRuntimeRoutingPlanValidationIssue(issues, "target_type_invalid", path+".target_type", fmt.Sprintf("model %q target %d has unsupported target_type %q", compiled.Model.ModelID, target.ID, target.TargetType))
	}
}

func validateRuntimeRoutingPlanConnectionTargetIssues(_ *runtimeRoutingPlan, issues []runtimeRoutingPlanValidationIssue, path string, compiled runtimeRoutingPlanModel, target runtimeAccessTargetRecord) []runtimeRoutingPlanValidationIssue {
	if target.TargetConnectionID == nil || *target.TargetConnectionID <= 0 {
		return appendRuntimeRoutingPlanValidationIssue(issues, "connection_target_missing_connection", path+".target_connection_id", fmt.Sprintf("model %q target %d has no terminal connection id", compiled.Model.ModelID, target.ID))
	}
	return issues
}

func validateRuntimeRoutingPlanModelTargetIssues(_ *runtimeRoutingPlan, issues []runtimeRoutingPlanValidationIssue, path string, compiled runtimeRoutingPlanModel, target runtimeAccessTargetRecord) []runtimeRoutingPlanValidationIssue {
	if target.TargetModelConfigID == nil || *target.TargetModelConfigID <= 0 {
		return appendRuntimeRoutingPlanValidationIssue(issues, "model_target_missing_model", path+".target_model_config_id", fmt.Sprintf("model %q target %d has no target model config id", compiled.Model.ModelID, target.ID))
	}
	if strings.TrimSpace(target.TargetModelID) == "" {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "model_target_id_empty", path+".target_model_id", fmt.Sprintf("model %q target %d has an empty target_model_id", compiled.Model.ModelID, target.ID))
	}
	return issues
}

func validateRuntimeRoutingPlanFallbackTargets(issues []runtimeRoutingPlanValidationIssue, path string, compiled runtimeRoutingPlanModel) []runtimeRoutingPlanValidationIssue {
	if len(compiled.OrderedFallbackTargets) != len(compiled.OrderedEnabledTargets) {
		return appendRuntimeRoutingPlanValidationIssue(issues, "fallback_target_count_mismatch", path+".ordered_fallback_targets", fmt.Sprintf("model %q fallback target count differs from ordered target count", compiled.Model.ModelID))
	}
	for index := range compiled.OrderedFallbackTargets {
		if compiled.OrderedFallbackTargets[index] != compiled.OrderedEnabledTargets[index] {
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "fallback_target_mismatch", fmt.Sprintf("%s.ordered_fallback_targets[%d]", path, index), fmt.Sprintf("model %q fallback target %d does not match ordered enabled target", compiled.Model.ModelID, index))
		}
	}
	return issues
}

func validateRuntimeRoutingPlanTerminalTargets(issues []runtimeRoutingPlanValidationIssue, path string, compiled runtimeRoutingPlanModel) []runtimeRoutingPlanValidationIssue {
	expectedTerminalTargets := orderedRuntimeRoutingPlanTerminalTargets(compiled.OrderedEnabledTargets)
	if len(compiled.OrderedTerminalTargets) != len(expectedTerminalTargets) {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_count_mismatch", path+".ordered_terminal_targets", fmt.Sprintf("model %q terminal target count differs from ordered enabled terminal target count", compiled.Model.ModelID))
	}
	for index := range compiled.OrderedTerminalTargets {
		if index >= len(expectedTerminalTargets) || compiled.OrderedTerminalTargets[index] != expectedTerminalTargets[index] {
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_mismatch", fmt.Sprintf("%s.ordered_terminal_targets[%d]", path, index), fmt.Sprintf("model %q terminal target %d does not match ordered enabled terminal targets", compiled.Model.ModelID, index))
		}
	}
	return issues
}

func validateRuntimeRoutingPlanTerminalTargetIssues(issues []runtimeRoutingPlanValidationIssue, path string, connectionID int, connection runtimeConnection) []runtimeRoutingPlanValidationIssue {
	if connection.ID != connectionID {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_key_mismatch", path, fmt.Sprintf("terminal target key %d does not match connection id %d", connectionID, connection.ID))
	}
	if connection.ID <= 0 {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_id_invalid", path+".id", fmt.Sprintf("terminal target has invalid id %d", connection.ID))
	}
	if connection.ProfileID <= 0 {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_profile_invalid", path+".profile_id", fmt.Sprintf("terminal target %d has invalid profile id %d", connection.ID, connection.ProfileID))
	}
	if strings.TrimSpace(connection.APIFamily) == "" {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_api_family_empty", path+".api_family", fmt.Sprintf("terminal target %d has an empty api family", connection.ID))
	}
	if connection.Endpoint.ID <= 0 {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_endpoint_invalid", path+".endpoint.id", fmt.Sprintf("terminal target %d has invalid endpoint id %d", connection.ID, connection.Endpoint.ID))
	}
	if strings.TrimSpace(connection.Endpoint.BaseURL) == "" {
		issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_base_url_empty", path+".endpoint.base_url", fmt.Sprintf("terminal target %d has an empty endpoint base url", connection.ID))
	}
	if providercompat.IsOpenAI(connection.APIFamily) {
		if connection.OpenAITextCapability == nil || !providercompat.IsSupportedOpenAITextCapability(*connection.OpenAITextCapability) {
			capability := ""
			if connection.OpenAITextCapability != nil {
				capability = *connection.OpenAITextCapability
			}
			issues = appendRuntimeRoutingPlanValidationIssue(issues, "terminal_target_openai_text_capability_invalid", path+".openai_text_capability", fmt.Sprintf("terminal target %d has unsupported OpenAI text capability %q", connection.ID, capability))
		}
	}
	return issues
}

func validateRuntimeRoutingPlanTopologyIssues(plan *runtimeRoutingPlan, issues []runtimeRoutingPlanValidationIssue) []runtimeRoutingPlanValidationIssue {
	state := map[int]int{}
	var visit func(runtimeRoutingPlanModel, []string) []runtimeRoutingPlanValidationIssue
	visit = func(compiled runtimeRoutingPlanModel, stack []string) []runtimeRoutingPlanValidationIssue {
		if len(stack) > runtimeAccessResolverMaxDepth {
			return appendRuntimeRoutingPlanValidationIssue(issues, "model_graph_depth_exceeded", fmt.Sprintf("plan.models_by_config_id[%d]", compiled.Model.ID), fmt.Sprintf("model access graph exceeded maximum depth of %d", runtimeAccessResolverMaxDepth))
		}
		switch state[compiled.Model.ID] {
		case 1:
			cycle := append(append([]string{}, stack...), compiled.Model.ModelID)
			return appendRuntimeRoutingPlanValidationIssue(issues, "model_graph_cycle", fmt.Sprintf("plan.models_by_config_id[%d]", compiled.Model.ID), fmt.Sprintf("model access cycle detected: %s", strings.Join(cycle, " -> ")))
		case 2:
			return issues
		}
		state[compiled.Model.ID] = 1
		for _, target := range compiled.OrderedEnabledTargets {
			if target.TargetType != runtimeAccessTargetTypeModel || target.TargetModelConfigID == nil {
				continue
			}
			child, ok := plan.ModelsByConfigID[*target.TargetModelConfigID]
			if !ok {
				continue
			}
			issues = visit(child, append(stack, compiled.Model.ModelID))
		}
		state[compiled.Model.ID] = 2
		return issues
	}
	for _, configID := range sortedRuntimeRoutingPlanConfigIDs(plan.ModelsByConfigID) {
		issues = visit(plan.ModelsByConfigID[configID], nil)
	}
	return issues
}

func appendRuntimeRoutingPlanValidationIssue(issues []runtimeRoutingPlanValidationIssue, code string, path string, message string) []runtimeRoutingPlanValidationIssue {
	issues = append(issues, runtimeRoutingPlanValidationIssue{
		Code:    strings.TrimSpace(code),
		Path:    strings.TrimSpace(path),
		Message: strings.TrimSpace(message),
	})
	return issues
}

func orderedRuntimeRoutingPlanTerminalTargets(source []runtimeAccessTargetRecord) []runtimeAccessTargetRecord {
	if len(source) == 0 {
		return nil
	}
	items := make([]runtimeAccessTargetRecord, 0, len(source))
	for _, target := range source {
		if target.TargetType == runtimeAccessTargetTypeConnection {
			items = append(items, target)
		}
	}
	return items
}

func sortedRuntimeRoutingPlanModelIDs(source map[string]runtimeRoutingPlanModel) []string {
	items := make([]string, 0, len(source))
	for key := range source {
		items = append(items, key)
	}
	sort.Strings(items)
	return items
}

func sortedRuntimeRoutingPlanConfigIDs(source map[int]runtimeRoutingPlanModel) []int {
	items := make([]int, 0, len(source))
	for key := range source {
		items = append(items, key)
	}
	sort.Ints(items)
	return items
}

func sortedRuntimeRoutingPlanConnectionIDs(source map[int]runtimeConnection) []int {
	items := make([]int, 0, len(source))
	for key := range source {
		items = append(items, key)
	}
	sort.Ints(items)
	return items
}

func invalidRuntimeRoutingPlanValidationError(issues []runtimeRoutingPlanValidationIssue) error {
	if len(issues) == 0 {
		return invalidRuntimeRoutingPlanError("unknown validation failure")
	}
	parts := make([]string, 0, len(issues))
	for _, issue := range issues {
		parts = append(parts, fmt.Sprintf("%s at %s: %s", issue.Code, issue.Path, issue.Message))
	}
	return &domainError{
		StatusCode: http.StatusServiceUnavailable,
		Detail:     fmt.Sprintf("Invalid runtime routing plan: %s", strings.Join(parts, "; ")),
		Fields: map[string]any{
			"routing_plan_issues": issues,
		},
	}
}

func invalidRuntimeRoutingPlanError(detail string) error {
	trimmedDetail := strings.TrimSpace(detail)
	if trimmedDetail == "" {
		trimmedDetail = "unknown validation failure"
	}
	return &domainError{
		StatusCode: http.StatusServiceUnavailable,
		Detail:     fmt.Sprintf("Invalid runtime routing plan: %s", trimmedDetail),
	}
}
