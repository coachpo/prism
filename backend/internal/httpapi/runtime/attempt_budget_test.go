package runtime

import (
	"net/http"
	"testing"
)

func TestAttemptBudgetExhaustedResultTypedCode(t *testing.T) {
	state := requestExecutionState{launchedAttempts: maxUpstreamAttemptsPerIngress}
	plan := requestPlan{RequestedModelID: "budget-model"}
	_, err := state.attemptBudgetExhaustedResult(plan)
	domainErr, ok := err.(*domainError)
	if !ok {
		t.Fatalf("expected domainError, got %#v", err)
	}
	if domainErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", domainErr.StatusCode)
	}
	if domainErr.ErrorCode != runtimeAttemptBudgetExhaustedErrorCode {
		t.Fatalf("expected typed attempt_budget_exhausted, got %q", domainErr.ErrorCode)
	}
	fields, ok := domainErr.Fields["attempt_limit"].(int)
	if !ok || fields != 64 {
		t.Fatalf("expected attempt_limit 64 in fields, got %#v", domainErr.Fields)
	}
	if state.launchedAttempts != 64 {
		t.Fatalf("budget result must not construct a 65th attempt, launched=%d", state.launchedAttempts)
	}
}

func TestMaxUpstreamAttemptsPerIngressValue(t *testing.T) {
	if maxUpstreamAttemptsPerIngress != 64 {
		t.Fatalf("expected fixed 64-attempt safety cap, got %d", maxUpstreamAttemptsPerIngress)
	}
}
