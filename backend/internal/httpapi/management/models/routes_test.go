package models

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/modelrouting"
)

func requireModelDomainError(t *testing.T, err error, status int, detail string) *domainError {
	t.Helper()

	var domainErr *domainError
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected domainError, got %T", err)
	}
	if domainErr.StatusCode != status || domainErr.Detail != detail {
		t.Fatalf("expected domainError (%d, %q), got (%d, %q)", status, detail, domainErr.StatusCode, domainErr.Detail)
	}
	return domainErr
}

func TestValidateAccessTargetsAllowsSparsePositions(t *testing.T) {
	targets := []modelAccessTargetRequest{
		{TargetType: "model", TargetModelID: stringPtr("alpha"), Position: 2},
		{TargetType: "model", TargetModelID: stringPtr("beta"), Position: 5},
	}

	if err := validateAccessTargets(targets); err != nil {
		t.Fatalf("expected sparse positions to pass, got %v", err)
	}
}

func TestValidateAccessTargetsRejectsDuplicatePositionWithStableIssue(t *testing.T) {
	targets := []modelAccessTargetRequest{
		{TargetType: "model", TargetModelID: stringPtr("alpha"), Position: 1},
		{TargetType: "model", TargetModelID: stringPtr("beta"), Position: 1},
	}

	err := validateAccessTargets(targets)
	domainErr := requireModelDomainError(t, err, http.StatusBadRequest, "access_targets must contain unique position values")
	issues, ok := domainErr.Fields["routing_plan_issues"].([]routingPlanValidationIssue)
	if !ok {
		t.Fatalf("expected routing_plan_issues payload, got %+v", domainErr.Fields)
	}
	if len(issues) != 1 {
		t.Fatalf("expected one routing_plan_issue, got %+v", issues)
	}
	if issues[0].Code != "target_position_duplicate" || issues[0].Path != "access_targets[1].position" || issues[0].Message != "access_targets must contain unique position values" {
		t.Fatalf("unexpected routing_plan_issue: %+v", issues[0])
	}
}

func TestModelResponseProjectionsPreserveOpenAIImageOperations(t *testing.T) {
	imageOperations := "generations_and_edits"
	record := modelRecord{
		ID:                    7,
		ProfileID:             1,
		APIFamily:             "openai",
		ModelID:               "gpt-image-2",
		OpenAIImageOperations: &imageOperations,
		IsEnabled:             true,
	}

	list := buildModelListResponse(record, nil, nil, nil, nil, time.Unix(0, 0).UTC())
	if list.OpenAIImageOperations == nil || *list.OpenAIImageOperations != imageOperations {
		t.Fatalf("list response lost image operations: %+v", list.OpenAIImageOperations)
	}

	detail := buildModelDetailResponse(record, nil, nil, time.Unix(0, 0).UTC())
	if detail.OpenAIImageOperations == nil || *detail.OpenAIImageOperations != imageOperations {
		t.Fatalf("detail response lost image operations: %+v", detail.OpenAIImageOperations)
	}

	target := modelTargetSummaryFromRecord(record)
	if target.OpenAIImageOperations == nil || *target.OpenAIImageOperations != imageOperations {
		t.Fatalf("target-model response lost image operations: %+v", target.OpenAIImageOperations)
	}
}

func TestAttachRoutingSummariesSupportsPureImageModel(t *testing.T) {
	imageOperations := "generations_and_edits"
	connectionID := 11
	record := modelRecord{
		ID:                    7,
		ProfileID:             1,
		APIFamily:             "openai",
		ModelID:               "gpt-image-2",
		OpenAIImageOperations: &imageOperations,
		IsEnabled:             true,
	}
	accessTargets := map[int][]accessTargetRecord{
		record.ID: {
			{
				ID:                  13,
				ProfileID:           1,
				SourceModelConfigID: record.ID,
				TargetType:          modelrouting.TargetTypeTerminal,
				TargetConnectionID:  &connectionID,
				IsEnabled:           true,
				Connection: &connectionTargetSummary{
					ID:                    connectionID,
					ProfileID:             1,
					APIFamily:             "openai",
					IsActive:              true,
					OpenAIImageCapability: &imageOperations,
				},
			},
		},
	}
	summaries := map[int]modelrouting.RoutingSummary{}

	if err := attachRoutingSummaries([]modelRecord{record}, accessTargets, nil, summaries); err != nil {
		t.Fatalf("attach pure-image summary: %v", err)
	}
	summary := summaries[record.ID]
	if summary.Coverage != string(modelrouting.CoverageFull) {
		t.Fatalf("expected full image coverage, got %+v", summary)
	}
	if len(summary.WarningCodes) != 0 {
		t.Fatalf("expected no image coverage warnings, got %+v", summary.WarningCodes)
	}
	groups := map[string]string{}
	for _, group := range summary.OperationGroups {
		groups[group.Group] = group.Status
	}
	for _, group := range []string{
		modelrouting.OpenAIOperationGroupImagesGenerations,
		modelrouting.OpenAIOperationGroupImagesEdits,
	} {
		if groups[group] != modelrouting.GroupStatusRoutable {
			t.Fatalf("expected %s to be routable, got %+v", group, summary.OperationGroups)
		}
	}
	if len(groups) != 2 {
		t.Fatalf("expected only image operation groups, got %+v", summary.OperationGroups)
	}
}
