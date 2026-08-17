package models

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
	"github.com/jackc/pgx/v5"
)

func loadModelRecordByModelID(ctx context.Context, exec queryExecutor, profileID int, modelID string) (modelRecord, bool, error) {
	record, err := scanModelRecord(exec.QueryRow(ctx, `SELECT `+modelRecordSelectColumns+` FROM model_configs WHERE profile_id = $1 AND model_id = $2 LIMIT 1`, profileID, modelID))
	if err == pgx.ErrNoRows {
		return modelRecord{}, false, nil
	}
	if err != nil {
		return modelRecord{}, false, fmt.Errorf("load model %q in profile %d: %w", modelID, profileID, err)
	}
	return record, true, nil
}

func routingPlanValidationIssueError(code string, path string, detail string) error {
	return routingPlanValidationError(http.StatusBadRequest, detail, []routingPlanValidationIssue{{
		Code:    strings.TrimSpace(code),
		Path:    strings.TrimSpace(path),
		Message: strings.TrimSpace(detail),
	}})
}

func routingPlanValidationIssueErrorWithStatus(statusCode int, code string, path string, detail string) error {
	return routingPlanValidationError(statusCode, detail, []routingPlanValidationIssue{{
		Code:    strings.TrimSpace(code),
		Path:    strings.TrimSpace(path),
		Message: strings.TrimSpace(detail),
	}})
}

func routingPlanValidationError(statusCode int, detail string, issues []routingPlanValidationIssue) error {
	if len(issues) == 0 {
		return &domainError{StatusCode: statusCode, Detail: detail}
	}
	return &domainError{
		StatusCode: statusCode,
		Detail:     detail,
		Fields: map[string]any{
			"routing_plan_issues": issues,
		},
	}
}

func accessTargetIssuePath(index int, field string) string {
	path := fmt.Sprintf("access_targets[%d]", index)
	if strings.TrimSpace(field) == "" {
		return path
	}
	return path + "." + strings.TrimSpace(field)
}

func validatePublicAccessTargets(accessTargets []modelAccessTargetRequest) error {
	if err := validateAccessTargets(accessTargets); err != nil {
		return err
	}
	for _, accessTarget := range accessTargets {
		if err := validatePublicAccessTarget(accessTarget); err != nil {
			return err
		}
	}
	return nil
}

func validateAccessTargets(accessTargets []modelAccessTargetRequest) error {
	issues := modelrouting.ValidateAuthoredAccessTargets(modelRoutingTargetsFromRequests(accessTargets), modelRoutingValidationOptions())
	return modelRoutingIssuesError(issues)
}

func validateAccessTargetsForSourceModel(sourceModelID string, accessTargets []modelAccessTargetRequest) error {
	issues := modelrouting.ValidateSourceModelTargets(
		modelrouting.ModelNode{ModelID: strings.TrimSpace(sourceModelID)},
		modelRoutingTargetsFromRequests(accessTargets),
		modelRoutingValidationOptions(),
	)
	return modelRoutingIssuesError(issues)
}

func modelRoutingTargetsFromRequests(accessTargets []modelAccessTargetRequest) []modelrouting.AuthoredAccessTarget {
	items := make([]modelrouting.AuthoredAccessTarget, 0, len(accessTargets))
	for _, target := range accessTargets {
		items = append(items, modelRoutingTargetFromRequest(target))
	}
	return items
}

func modelRoutingTargetFromRequest(target modelAccessTargetRequest) modelrouting.AuthoredAccessTarget {
	return modelrouting.AuthoredAccessTarget{TargetType: target.TargetType, Position: target.Position, IsEnabled: target.IsEnabled, TargetModelID: target.TargetModelID, TerminalTargetID: target.ConnectionID}
}

func modelRoutingValidationOptions() modelrouting.ValidationOptions {
	return modelrouting.ValidationOptions{IssuePath: func(code string, field string, index int, target modelrouting.AuthoredAccessTarget) string {
		if code == "target_duplicate" {
			return accessTargetIssuePath(index, "")
		}
		return accessTargetIssuePath(index, field)
	}}
}

func modelRoutingIssuesError(issues []modelrouting.ValidationIssue) error {
	if len(issues) == 0 {
		return nil
	}
	statusCode := http.StatusBadRequest
	for _, issue := range issues {
		if issue.Code == modelrouting.OpenAITextModeMismatchIssueCode {
			statusCode = http.StatusUnprocessableEntity
			break
		}
	}
	return routingPlanValidationError(statusCode, issues[0].Message, modelRoutingValidationIssues(issues))
}

func modelRoutingValidationIssues(issues []modelrouting.ValidationIssue) []routingPlanValidationIssue {
	items := make([]routingPlanValidationIssue, 0, len(issues))
	for _, issue := range issues {
		items = append(items, routingPlanValidationIssue{Code: issue.Code, Path: issue.Path, Message: issue.Message})
	}
	return items
}

func resolvePersistedDisplayName(modelID string, displayName *string) *string {
	if displayName == nil {
		return stringPtr(modelID)
	}
	return displayName
}

func isValidAPIFamily(value string) bool {
	return value == "openai" || value == "anthropic" || value == "gemini"
}

func joinModelIDs(records []modelRecord) string {
	modelIDs := make([]string, 0, len(records))
	for _, record := range records {
		modelIDs = append(modelIDs, record.ModelID)
	}
	return strings.Join(modelIDs, ", ")
}
