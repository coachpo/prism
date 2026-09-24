package runtime

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

var rejectedResponseTestAttempts = []runtimeTerminalAttempt{{Connection: runtimeConnection{ID: 1}}, {Connection: runtimeConnection{ID: 2}}}

func TestLaunchAfterRequestScopedRejectionIsReroute(t *testing.T) {
	cases := []struct {
		name    string
		outcome executionOutcome
		want    string
	}{
		{name: "request-scoped rejection", outcome: executionOutcome{Launched: true, RerouteEligible: true}, want: attemptTriggerReroute},
		{name: "target failure", outcome: executionOutcome{Launched: true, FailoverEligible: true}, want: attemptTriggerFailover},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := requestPlan{TerminalAttempts: rejectedResponseTestAttempts}
			state := newRequestExecutionState(plan)
			test.outcome.Attempt = executionAttempt{Connection: rejectedResponseTestAttempts[0].Connection, LaunchOrdinal: 1, AttemptTrigger: attemptTriggerInitial}
			state.recordLaunchedAttempt(test.outcome)
			if got := state.nextLaunchTrigger(plan, 1, rejectedResponseTestAttempts[1]); got != test.want {
				t.Fatalf("next launch trigger = %q, want %q", got, test.want)
			}
		})
	}
}

func TestExhaustedIngressReturnsKeptRejection(t *testing.T) {
	envelope := `{"error":{"message":"image input is not supported","type":"invalid_request_error"}}`
	cases := []struct {
		name     string
		body     string
		wantKept bool
	}{
		{name: "complete rejection is returned", body: envelope, wantKept: true},
		{name: "oversized rejection falls back to the gateway failure", body: strings.Repeat("x", int(FailedResponseSampleBytes)+1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			plan := requestPlan{RequestedModelID: "reroute-model", TerminalAttempts: rejectedResponseTestAttempts}
			state := newRequestExecutionState(plan)
			outcome := executionOutcome{
				TerminalAttempt: rejectedResponseTestAttempts[0], Connection: rejectedResponseTestAttempts[0].Connection, Launched: true, RerouteEligible: true,
				Response: &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(test.body))},
				Attempt:  executionAttempt{Connection: rejectedResponseTestAttempts[0].Connection, LaunchOrdinal: 1, AttemptTrigger: attemptTriggerInitial},
			}
			state.recordLaunchedAttempt(outcome)
			state.keepRejectedResponse(plan, outcome)

			result, err := state.exhaustedResult(plan)
			if !test.wantKept {
				if runtimeErr, ok := err.(*domainError); !ok || runtimeErr.StatusCode != http.StatusBadGateway {
					t.Fatalf("expected the synthesized gateway failure, got result=%+v err=%v", result, err)
				}
				return
			}
			if err != nil || result.Response.StatusCode != http.StatusBadRequest || result.WinnerOrdinal != 1 {
				t.Fatalf("expected the kept 400 as the ingress result, got result=%+v err=%v", result, err)
			}
			if body, _ := io.ReadAll(result.Response.Body); string(body) != test.body || state.attempts[0].Diagnostics == nil {
				t.Fatalf("expected the upstream body and a safe diagnostic, got body=%q diagnostics=%+v", body, state.attempts[0].Diagnostics)
			}
		})
	}
}
