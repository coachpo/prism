package modelrouting

import (
	"fmt"
	"sort"
	"strings"

	"github.com/coachpo/prism/backend/internal/providerauth"
)

const (
	TargetTypeModel                = "model"
	TargetTypeTerminal             = "connection"
	ConnectionIDFieldName          = "connection_id"
	TerminalTargetIDFieldName      = "terminal_target_id"
	ConnectionObjectFieldName      = "connection"
	TerminalTargetObjectFieldName  = "terminal_target"
	OwnerScopedConnectionRoutePath = "/api/models/{model_config_id}/connections"
)

type ModelNode struct {
	ConfigID  int
	ProfileID int
	ModelID   string
	APIFamily string
	IsEnabled bool
}

type TerminalTargetNode struct {
	ID        int
	Ref       string
	ProfileID int
	APIFamily string
}

type AuthoredAccessTarget struct {
	TargetType        string
	Position          int
	IsEnabled         *bool
	TargetModelID     *string
	TerminalTargetID  *int
	TerminalTargetRef *string
}

type ResolvedAccessTarget struct {
	TargetType          string
	Position            int
	IsEnabled           bool
	TargetModelConfigID *int
	TargetModelID       *string
	TerminalTargetID    *int
	TerminalTargetRef   *string
}

type ValidationIssue struct {
	Code    string
	Path    string
	Message string
}

type ValidationOptions struct {
	TerminalTargetField string
	IssuePath           func(code string, field string, index int, target AuthoredAccessTarget) string
	IssueDetail         func(code string, field string, index int, target AuthoredAccessTarget) string
}

type ResolveOptions struct {
	Source               ModelNode
	ModelsByID           map[string]ModelNode
	TerminalTargetsByID  map[int]TerminalTargetNode
	TerminalTargetsByRef map[string]TerminalTargetNode
	TerminalTargetField  string
	IssuePath            func(code string, field string, target AuthoredAccessTarget) string
	IssueDetail          func(code string, field string, target AuthoredAccessTarget) string
}

type OrderKey struct {
	Position int
	ID       int
}

type Cycle[T comparable] struct {
	Node T
	Path []T
}

func NormalizeTargetType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func IsModelTargetType(value string) bool {
	return NormalizeTargetType(value) == TargetTypeModel
}

func IsTerminalTargetType(value string) bool {
	return NormalizeTargetType(value) == TargetTypeTerminal
}

func IsSupportedTargetType(value string) bool {
	return IsModelTargetType(value) || IsTerminalTargetType(value)
}

func SameAPIFamily(left string, right string) bool {
	return providerauth.SameAPIFamily(left, right)
}

func TargetEnabled(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

func CompareAccessTargetOrder(left OrderKey, right OrderKey) int {
	if left.Position < right.Position {
		return -1
	}
	if left.Position > right.Position {
		return 1
	}
	if left.ID < right.ID {
		return -1
	}
	if left.ID > right.ID {
		return 1
	}
	return 0
}

func CompareAuthoredAccessTargets(left AuthoredAccessTarget, right AuthoredAccessTarget) int {
	order := CompareAccessTargetOrder(OrderKey{Position: left.Position}, OrderKey{Position: right.Position})
	if order != 0 {
		return order
	}
	leftKey := AuthoredAccessTargetKey(left)
	rightKey := AuthoredAccessTargetKey(right)
	if leftKey < rightKey {
		return -1
	}
	if leftKey > rightKey {
		return 1
	}
	return 0
}

func SortAuthoredAccessTargets(values []AuthoredAccessTarget) []AuthoredAccessTarget {
	ordered := make([]AuthoredAccessTarget, len(values))
	copy(ordered, values)
	sort.Slice(ordered, func(left int, right int) bool {
		return CompareAuthoredAccessTargets(ordered[left], ordered[right]) < 0
	})
	return ordered
}

func AuthoredAccessTargetKey(target AuthoredAccessTarget) string {
	if IsModelTargetType(target.TargetType) && target.TargetModelID != nil {
		return "model:" + strings.TrimSpace(*target.TargetModelID)
	}
	if IsTerminalTargetType(target.TargetType) {
		if target.TerminalTargetRef != nil {
			return "terminal_ref:" + strings.TrimSpace(*target.TerminalTargetRef)
		}
		if target.TerminalTargetID != nil {
			return fmt.Sprintf("terminal:%d", *target.TerminalTargetID)
		}
	}
	return NormalizeTargetType(target.TargetType)
}

func ValidateAuthoredAccessTargets(targets []AuthoredAccessTarget, options ValidationOptions) []ValidationIssue {
	issues := make([]ValidationIssue, 0)
	seenTargets := map[string]struct{}{}
	seenPositions := map[int]struct{}{}

	for index, target := range targets {
		if !IsSupportedTargetType(target.TargetType) {
			return appendValidationIssue(issues, options, "target_type_invalid", "target_type", index, target)
		}
		if target.Position < 0 {
			return appendValidationIssue(issues, options, "target_position_invalid", "position", index, target)
		}
		if _, exists := seenPositions[target.Position]; exists {
			return appendValidationIssue(issues, options, "target_position_duplicate", "position", index, target)
		}
		seenPositions[target.Position] = struct{}{}
		targetKey, pointerIssues := validatePointerContract(target, options, index)
		if len(pointerIssues) > 0 {
			return append(issues, pointerIssues...)
		}
		if _, exists := seenTargets[targetKey]; exists {
			field := ""
			if IsModelTargetType(target.TargetType) {
				field = "target_model_id"
			} else if IsTerminalTargetType(target.TargetType) {
				field = terminalTargetField(options)
			}
			return appendValidationIssue(issues, options, "target_duplicate", field, index, target)
		}
		seenTargets[targetKey] = struct{}{}
	}
	return issues
}

func ValidateSourceModelTargets(source ModelNode, targets []AuthoredAccessTarget, options ValidationOptions) []ValidationIssue {
	if strings.TrimSpace(source.ModelID) == "" && source.ConfigID <= 0 {
		return nil
	}

	issues := make([]ValidationIssue, 0)
	for index, target := range targets {
		if !IsModelTargetType(target.TargetType) || target.TargetModelID == nil {
			continue
		}
		targetModelID := strings.TrimSpace(*target.TargetModelID)
		if targetModelID != "" && targetModelID == strings.TrimSpace(source.ModelID) {
			return appendValidationIssue(issues, options, "model_graph_cycle", "target_model_id", index, target)
		}
	}
	return nil
}

func ResolveAuthoredAccessTargets(targets []AuthoredAccessTarget, options ResolveOptions) ([]ResolvedAccessTarget, []ValidationIssue) {
	orderedTargets := SortAuthoredAccessTargets(targets)
	resolved := make([]ResolvedAccessTarget, 0, len(orderedTargets))
	issues := make([]ValidationIssue, 0)

	for _, target := range orderedTargets {
		switch {
		case IsModelTargetType(target.TargetType):
			targetModelID := ""
			if target.TargetModelID != nil {
				targetModelID = strings.TrimSpace(*target.TargetModelID)
			}
			model, ok := options.ModelsByID[targetModelID]
			if !ok {
				return nil, appendResolveIssue(issues, options, "model_target_missing_model", "target_model_id", target)
			}
			if modelTargetsSelf(options.Source, model) {
				return nil, appendResolveIssue(issues, options, "model_graph_cycle", "target_model_id", target)
			}

			if !ModelTargetCompatible(options.Source, model) {
				return nil, appendResolveIssue(issues, options, "target_api_family_mismatch", "target_model_id", target)
			}
			modelID := model.ModelID
			configID := model.ConfigID
			resolved = append(resolved, ResolvedAccessTarget{
				TargetType:          TargetTypeModel,
				Position:            target.Position,
				IsEnabled:           TargetEnabled(target.IsEnabled),
				TargetModelConfigID: &configID,
				TargetModelID:       &modelID,
			})

		case IsTerminalTargetType(target.TargetType):
			terminal, ok := resolveTerminalTargetNode(target, options)
			if !ok {
				return nil, appendResolveIssue(issues, options, "connection_target_missing_connection", terminalTargetFieldFromResolve(options), target)
			}
			if !TerminalTargetCompatible(options.Source, terminal, terminal.ProfileID, terminal.APIFamily) {
				return nil, appendResolveIssue(issues, options, "target_api_family_mismatch", terminalTargetFieldFromResolve(options), target)
			}
			terminalID := terminal.ID
			terminalRef := terminal.Ref
			resolved = append(resolved, ResolvedAccessTarget{
				TargetType:        TargetTypeTerminal,
				Position:          target.Position,
				IsEnabled:         TargetEnabled(target.IsEnabled),
				TerminalTargetID:  &terminalID,
				TerminalTargetRef: &terminalRef,
			})
		}
	}
	return resolved, nil
}

func ModelTargetCompatible(source ModelNode, target ModelNode) bool {
	return target.ProfileID == source.ProfileID && SameAPIFamily(target.APIFamily, source.APIFamily)
}

func TerminalTargetCompatible(source ModelNode, terminal TerminalTargetNode, targetProfileID int, targetAPIFamily string) bool {
	if terminal.ProfileID != source.ProfileID || !SameAPIFamily(terminal.APIFamily, source.APIFamily) {
		return false
	}

	if targetProfileID != 0 && targetProfileID != source.ProfileID {
		return false
	}
	if strings.TrimSpace(targetAPIFamily) != "" && !SameAPIFamily(targetAPIFamily, source.APIFamily) {
		return false
	}
	return true
}

func FindCycle[T comparable](graph map[T][]T, roots []T, less func(T, T) bool) *Cycle[T] {
	state := map[T]int{}
	orderedRoots := sortedValues(roots, less)
	var visit func(T, []T) *Cycle[T]
	visit = func(node T, stack []T) *Cycle[T] {
		switch state[node] {
		case 1:
			cyclePath := append(append([]T{}, stack...), node)
			return &Cycle[T]{Node: node, Path: cyclePath}
		case 2:
			return nil
		}
		state[node] = 1
		children := sortedValues(graph[node], less)
		for _, child := range children {
			if cycle := visit(child, append(stack, node)); cycle != nil {
				return cycle
			}
		}
		state[node] = 2
		return nil
	}
	for _, root := range orderedRoots {
		if cycle := visit(root, nil); cycle != nil {
			return cycle
		}
	}
	return nil
}

func GraphRoots[T comparable](graph map[T][]T, less func(T, T) bool) []T {
	seen := map[T]struct{}{}
	roots := make([]T, 0, len(graph))
	for source, targets := range graph {
		if _, ok := seen[source]; !ok {
			seen[source] = struct{}{}
			roots = append(roots, source)
		}
		for _, target := range targets {
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				roots = append(roots, target)
			}
		}
	}
	return sortedValues(roots, less)
}

func LessInt(left int, right int) bool {
	return left < right
}

func LessString(left string, right string) bool {
	return left < right
}

func appendValidationIssue(issues []ValidationIssue, options ValidationOptions, code string, field string, index int, target AuthoredAccessTarget) []ValidationIssue {
	path := defaultIssuePath(index, field)
	if options.IssuePath != nil {
		path = options.IssuePath(code, field, index, target)
	}
	detail := defaultIssueDetail(code, field, target)
	if options.IssueDetail != nil {
		if customDetail := strings.TrimSpace(options.IssueDetail(code, field, index, target)); customDetail != "" {
			detail = customDetail
		}
	}
	return append(issues, cleanIssue(code, path, detail))
}

func appendResolveIssue(issues []ValidationIssue, options ResolveOptions, code string, field string, target AuthoredAccessTarget) []ValidationIssue {
	path := defaultIssuePath(target.Position, field)
	if options.IssuePath != nil {
		path = options.IssuePath(code, field, target)
	}
	detail := defaultIssueDetail(code, field, target)
	if options.IssueDetail != nil {
		if customDetail := strings.TrimSpace(options.IssueDetail(code, field, target)); customDetail != "" {
			detail = customDetail
		}
	}
	return append(issues, cleanIssue(code, path, detail))
}

func cleanIssue(code string, path string, detail string) ValidationIssue {
	return ValidationIssue{Code: strings.TrimSpace(code), Path: strings.TrimSpace(path), Message: strings.TrimSpace(detail)}
}

func validatePointerContract(target AuthoredAccessTarget, options ValidationOptions, index int) (string, []ValidationIssue) {

	if IsModelTargetType(target.TargetType) {
		if target.TargetModelID == nil || strings.TrimSpace(*target.TargetModelID) == "" {
			return "", appendValidationIssue(nil, options, "model_target_id_empty", "target_model_id", index, target)
		}
		if hasTerminalIdentifier(target) {
			return "", appendValidationIssue(nil, options, "model_target_has_connection", terminalTargetField(options), index, target)
		}
		return "model:" + strings.TrimSpace(*target.TargetModelID), nil
	}
	if !hasTerminalIdentifier(target) {
		return "", appendValidationIssue(nil, options, "connection_target_missing_connection", terminalTargetField(options), index, target)
	}
	if target.TargetModelID != nil && strings.TrimSpace(*target.TargetModelID) != "" {
		return "", appendValidationIssue(nil, options, "connection_target_has_model", "target_model_id", index, target)
	}
	return AuthoredAccessTargetKey(target), nil
}

func resolveTerminalTargetNode(target AuthoredAccessTarget, options ResolveOptions) (TerminalTargetNode, bool) {
	if target.TerminalTargetRef != nil {
		terminal, ok := options.TerminalTargetsByRef[strings.TrimSpace(*target.TerminalTargetRef)]
		return terminal, ok
	}
	if target.TerminalTargetID != nil {
		terminal, ok := options.TerminalTargetsByID[*target.TerminalTargetID]
		return terminal, ok
	}
	return TerminalTargetNode{}, false
}

func modelTargetsSelf(source ModelNode, target ModelNode) bool {
	if source.ConfigID > 0 && target.ConfigID == source.ConfigID {
		return true
	}
	return strings.TrimSpace(source.ModelID) != "" && strings.TrimSpace(source.ModelID) == strings.TrimSpace(target.ModelID)
}

func hasTerminalIdentifier(target AuthoredAccessTarget) bool {
	if target.TerminalTargetID != nil && *target.TerminalTargetID > 0 {
		return true
	}
	return target.TerminalTargetRef != nil && strings.TrimSpace(*target.TerminalTargetRef) != ""
}

func terminalTargetField(options ValidationOptions) string {
	field := strings.TrimSpace(options.TerminalTargetField)
	if field == "" {
		return "connection_id"
	}
	return field
}

func terminalTargetFieldFromResolve(options ResolveOptions) string {
	field := strings.TrimSpace(options.TerminalTargetField)
	if field == "" {
		return "connection_id"
	}
	return field
}

func defaultIssuePath(index int, field string) string {
	path := fmt.Sprintf("access_targets[%d]", index)
	if strings.TrimSpace(field) == "" {
		return path
	}
	return path + "." + strings.TrimSpace(field)
}

func defaultIssueDetail(code string, field string, target AuthoredAccessTarget) string {
	switch code {
	case "target_type_invalid":
		return "target_type must be 'model' or 'connection'"
	case "target_position_invalid":
		return "position must be greater than or equal to 0"
	case "target_position_duplicate":
		return "access_targets must contain unique position values"
	case "model_target_id_empty":
		return "target_model_id is required for model access targets"
	case "model_target_has_connection":
		return fmt.Sprintf("%s must be omitted for model access targets", field)
	case "connection_target_missing_connection":
		return fmt.Sprintf("%s is required for terminal targets", field)
	case "connection_target_has_model":
		return "target_model_id must be omitted for terminal targets"
	case "target_duplicate":
		return "access_targets must contain unique target references"
	case "model_graph_cycle":
		return "Model access target cannot target itself"
	case "model_target_missing_model":
		if target.TargetModelID != nil {
			return fmt.Sprintf("Target model '%s' not found", strings.TrimSpace(*target.TargetModelID))
		}
		return "Target model not found"
	case "target_api_family_mismatch":
		if field == "target_model_id" {
			return "Model access targets must use the same api_family as the source model"
		}
		return "Connection access targets must use the same api_family as the source model"
	default:
		return strings.TrimSpace(code)
	}
}

func sortedValues[T comparable](values []T, less func(T, T) bool) []T {
	if len(values) == 0 {
		return nil
	}
	ordered := make([]T, len(values))
	copy(ordered, values)
	if less != nil {
		sort.Slice(ordered, func(left int, right int) bool {
			return less(ordered[left], ordered[right])
		})
	}
	return ordered
}
