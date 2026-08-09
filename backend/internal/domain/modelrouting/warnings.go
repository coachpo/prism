package modelrouting

import "strings"

// ConfigurationWarning is a structured, non-persisted warning returned by
// routing-relevant mutation responses and by diagnostics. Frontends must key
// presentation off Code and the structured fields, never off Message.
type ConfigurationWarning struct {
	Code           string         `json:"code"`
	Severity       string         `json:"severity"`
	Message        string         `json:"message"`
	Path           string         `json:"path"`
	ModelConfigID  *int           `json:"model_config_id"`
	AccessTargetID *int           `json:"access_target_id"`
	ConnectionID   *int           `json:"connection_id"`
	OperationNames []string       `json:"operation_names"`
	Details        map[string]any `json:"details"`
}

const (
	WarningSeverityWarning = "warning"
	WarningSeverityDanger  = "danger"
)

// Stable configuration-warning codes. Frontend presentation must switch on
// these codes and their structured fields.
const (
	WarningCodeOpenAITargetIncompatible       = "openai_target_incompatible"
	WarningCodeOpenAITargetPartialCoverage    = "openai_target_partial_coverage"
	WarningCodeOpenAIOperationUncovered       = "openai_operation_uncovered"
	WarningCodeSingleStrategyTruncatesTargets = "single_strategy_truncates_targets"
)

// Uncovered-operation reasons carried in Details["reason"].
const (
	UncoveredReasonNoCompatibleTarget     = "no_compatible_target"
	UncoveredReasonNoStaticEligibleTarget = "no_static_eligible_target"
)

// WarningStage keys carried in Details["stage"].
const (
	WarningStageModelTargets    = "model_targets"
	WarningStageTerminalTargets = "terminal_targets"
)

// NewWarning builds a ConfigurationWarning with normalized required fields.
func NewWarning(code string, severity string, message string, path string, modelConfigID int, operationNames []string, details map[string]any) ConfigurationWarning {
	return ConfigurationWarning{
		Code:           code,
		Severity:       severity,
		Message:        message,
		Path:           path,
		ModelConfigID:  intPointerCopy(modelConfigID),
		OperationNames: append([]string(nil), operationNames...),
		Details:        cloneWarningDetails(details),
	}
}

func (warning ConfigurationWarning) WithAccessTarget(accessTargetID int) ConfigurationWarning {
	warning.AccessTargetID = intPointerCopy(accessTargetID)
	return warning
}

func (warning ConfigurationWarning) WithConnection(connectionID int) ConfigurationWarning {
	warning.ConnectionID = intPointerCopy(connectionID)
	return warning
}

func cloneWarningDetails(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	cloned := make(map[string]any, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func intPointerCopy(value int) *int {
	resolved := value
	return &resolved
}

// GenerateConfigurationWarnings derives the structured configuration warnings
// for a root model from its diagnostics result: uncovered accepted operations,
// per-terminal-target Full/Partial/None coverage, and per-stage single
// truncation. Warnings are computed on the proposed final state, are not
// persisted, and never carry secrets or cross-profile IDs.
func GenerateConfigurationWarnings(graph *DiagnosticsGraph, root DiagnosticsModel, result DiagnosticsResult, acceptedOperations []string) []ConfigurationWarning {
	warnings := make([]ConfigurationWarning, 0, 8)
	strategy := graph.strategyForModel(root)

	for _, coverage := range result.OperationCoverage {
		if !coverage.Accepted || coverage.StaticallyRoutable || !IsOpenAIFamily(root.APIFamily) {
			continue
		}
		reason := UncoveredReasonNoCompatibleTarget
		if coverage.CapabilityCovered {
			reason = UncoveredReasonNoStaticEligibleTarget
		}
		message := "该入口操作没有可路由的目标。"
		if reason == UncoveredReasonNoStaticEligibleTarget {
			message = "存在兼容目标，但当前没有可参与路由的目标。"
		}
		warnings = append(warnings, NewWarning(
			WarningCodeOpenAIOperationUncovered,
			WarningSeverityDanger,
			message,
			"openai_accepted_format",
			root.ConfigID,
			[]string{coverage.OperationName},
			map[string]any{"reason": reason},
		))
	}

	for _, stage := range result.Stages {
		for _, target := range stage.Targets {
			if target.ConnectionID == nil {
				continue
			}
			if len(target.UnsupportedAcceptedOperations) == 0 {
				continue
			}
			if target.Coverage == string(CoverageNone) {
				warning := NewWarning(
					WarningCodeOpenAITargetIncompatible,
					WarningSeverityDanger,
					"该目标与模型的入口能力不兼容。",
					"openai_text_capability",
					root.ConfigID,
					target.UnsupportedAcceptedOperations,
					map[string]any{"stage": stage.Stage},
				).WithAccessTarget(target.AccessTargetID)
				if target.ConnectionID != nil {
					warning = warning.WithConnection(*target.ConnectionID)
				}
				warnings = append(warnings, warning)
				continue
			}
			if target.Coverage == string(CoveragePartial) {
				warning := NewWarning(
					WarningCodeOpenAITargetPartialCoverage,
					WarningSeverityWarning,
					"该目标只承接部分入口能力。",
					"openai_text_capability",
					root.ConfigID,
					target.UnsupportedAcceptedOperations,
					map[string]any{"stage": stage.Stage},
				).WithAccessTarget(target.AccessTargetID)
				if target.ConnectionID != nil {
					warning = warning.WithConnection(*target.ConnectionID)
				}
				warnings = append(warnings, warning)
			}
		}
	}

	if strings.EqualFold(strings.TrimSpace(strategy.Subtype), "single") {
		for _, stage := range []string{StageModelTargets, StageTerminalTargets} {
			enabledCount := 0
			for _, stageResult := range result.Stages {
				if stageResult.Stage != stage {
					continue
				}
				for _, target := range stageResult.Targets {
					if target.EnabledStrategyIndex != nil {
						enabledCount++
					}
				}
			}
			if enabledCount > 1 {
				warnings = append(warnings, NewWarning(
					WarningCodeSingleStrategyTruncatesTargets,
					WarningSeverityWarning,
					"该阶段只有第一个启用目标会参与路由。",
					"loadbalance_strategy_id",
					root.ConfigID,
					nil,
					map[string]any{"stage": stage},
				))
			}
		}
	}
	return warnings
}

// GenerateOpenAIWarningsForTarget computes direct owner-scoped warnings for one
// terminal target against its owner model's accepted operation set. It is used
// by owner-scoped Connection mutations; root-model diagnostics re-analyze the
// full graph with requested root accepted set.
func GenerateOpenAIWarningsForTarget(ownerAcceptedFormat string, targetCapability string, path string, modelConfigID int, accessTargetID int, connectionID int) []ConfigurationWarning {
	accepted := OpenAIAcceptedOperationSet(ownerAcceptedFormat)
	if len(accepted) == 0 {
		return nil
	}
	supported := OpenAITargetSupportedOperationSet(targetCapability)
	coverage, _, unsupported := ClassifyOpenAICoverage(accepted, supported)
	if len(unsupported) == 0 {
		return nil
	}
	code := WarningCodeOpenAITargetPartialCoverage
	severity := WarningSeverityWarning
	message := "该目标只承接部分入口能力。"
	if coverage == CoverageNone {
		code = WarningCodeOpenAITargetIncompatible
		severity = WarningSeverityDanger
		message = "该目标与模型的入口能力不兼容。"
	}
	warning := NewWarning(code, severity, message, path, modelConfigID, unsupported, nil).WithAccessTarget(accessTargetID)
	if connectionID > 0 {
		warning = warning.WithConnection(connectionID)
	}
	return []ConfigurationWarning{warning}
}

// RoutingSummary is the compact model-list projection of the diagnostics
// analyzer. Models list/detail must reuse this projection; the frontend must
// not re-derive coverage or eligibility from card text.
type RoutingSummary struct {
	EnabledAccessTargetCount int                     `json:"enabled_access_target_count"`
	TotalAccessTargetCount   int                     `json:"total_access_target_count"`
	Coverage                 string                  `json:"coverage"`
	OperationGroups          []RoutingOperationGroup `json:"operation_groups"`
	SingleTruncatedStages    []string                `json:"single_truncated_stages"`
	WarningCodes             []string                `json:"warning_codes"`
}

type RoutingOperationGroup struct {
	Group  string `json:"group"`
	Status string `json:"status"`
}

// BuildRoutingSummary projects a diagnostics result into the compact
// model-list summary shape.
func BuildRoutingSummary(graph *DiagnosticsGraph, root DiagnosticsModel, result DiagnosticsResult) RoutingSummary {
	summary := RoutingSummary{
		OperationGroups:       []RoutingOperationGroup{},
		SingleTruncatedStages: []string{},
		WarningCodes:          []string{},
	}
	enabledCount := 0
	totalCount := 0
	for _, stage := range result.Stages {
		for _, target := range stage.Targets {
			totalCount++
			if target.EnabledStrategyIndex != nil {
				enabledCount++
			}
		}
	}
	summary.EnabledAccessTargetCount = enabledCount
	summary.TotalAccessTargetCount = totalCount

	groupStatus := map[string]string{
		OpenAIOperationGroupChatCompletions: GroupStatusNotAccepted,
		OpenAIOperationGroupResponses:       GroupStatusNotAccepted,
	}
	routableCount := 0
	acceptedCount := 0
	for _, coverage := range result.OperationCoverage {
		group := OpenAIOperationGroup(coverage.OperationName)
		if group == "" || !coverage.Accepted {
			continue
		}
		acceptedCount++
		current := groupStatus[group]
		status := GroupStatusUncovered
		if coverage.CapabilityCovered {
			status = GroupStatusCompatibleButIneligible
		}
		if coverage.StaticallyRoutable {
			status = GroupStatusRoutable
			routableCount++
		}
		if statusRank(status) < statusRank(current) {
			groupStatus[group] = status
		}
	}
	for _, group := range []string{OpenAIOperationGroupChatCompletions, OpenAIOperationGroupResponses} {
		status := groupStatus[group]
		if status != GroupStatusNotAccepted {
			summary.OperationGroups = append(summary.OperationGroups, RoutingOperationGroup{Group: group, Status: status})
		}
	}
	if acceptedCount == 0 {
		summary.Coverage = string(CoverageNotApplicable)
	} else if routableCount == acceptedCount {
		summary.Coverage = string(CoverageFull)
	} else if routableCount > 0 {
		summary.Coverage = string(CoveragePartial)
	} else {
		summary.Coverage = string(CoverageNone)
	}

	strategy := graph.strategyForModel(root)
	if strings.EqualFold(strings.TrimSpace(strategy.Subtype), "single") {
		for _, stage := range []string{StageModelTargets, StageTerminalTargets} {
			enabledRows := 0
			for _, stageResult := range result.Stages {
				if stageResult.Stage != stage {
					continue
				}
				for _, target := range stageResult.Targets {
					if target.EnabledStrategyIndex != nil {
						enabledRows++
					}
				}
			}
			if enabledRows > 1 {
				summary.SingleTruncatedStages = append(summary.SingleTruncatedStages, stage)
			}
		}
	}
	seenCodes := map[string]struct{}{}
	for _, warning := range result.ConfigurationWarnings {
		if _, ok := seenCodes[warning.Code]; ok {
			continue
		}
		seenCodes[warning.Code] = struct{}{}
		summary.WarningCodes = append(summary.WarningCodes, warning.Code)
	}
	return summary
}

func statusRank(status string) int {
	switch status {
	case GroupStatusRoutable:
		return 0
	case GroupStatusCompatibleButIneligible:
		return 1
	case GroupStatusUncovered:
		return 2
	default:
		return 3
	}
}
