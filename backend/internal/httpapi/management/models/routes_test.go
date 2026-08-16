package models

import (
	"errors"
	"net/http"
	"testing"
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
