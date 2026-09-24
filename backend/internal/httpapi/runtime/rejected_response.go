package runtime

import (
	"bytes"
	"io"
	"strings"
	"time"
)

// keepRejectedResponse reads a request-scoped rejection within the failed-
// response sampler bounds, records its safe diagnostic on the attempt, and
// keeps the complete response so the ingress can still return it when no later
// candidate answers. The upstream body is always closed here; a body that does
// not fit the bounds leaves the previously kept rejection in place.
func (state *requestExecutionState) keepRejectedResponse(plan requestPlan, outcome executionOutcome) {
	response := outcome.Response
	deadline := time.AfterFunc(FailedResponseSampleDeadline, func() { _ = response.Body.Close() })
	raw, err := io.ReadAll(io.LimitReader(response.Body, FailedResponseSampleBytes+1))
	deadline.Stop()
	_ = response.Body.Close()
	if err != nil {
		return
	}
	complete := int64(len(raw)) <= FailedResponseSampleBytes
	sample := raw
	if !complete {
		sample = raw[:FailedResponseSampleBytes]
	}
	contentType := strings.ToLower(strings.TrimSpace(response.Header.Get("Content-Type")))
	if diagnostic, ok := extractFailedResponseDiagnostic(sample, response.StatusCode, contentType, planBlocklistSensitiveRules(plan)); ok {
		outcome.Attempt.Diagnostics = &diagnostic
		if len(state.attempts) > 0 {
			state.attempts[len(state.attempts)-1].Diagnostics = &diagnostic
		}
	}
	if !complete {
		return
	}
	kept := *response
	kept.Header = response.Header.Clone()
	kept.Body = io.NopCloser(bytes.NewReader(raw))
	kept.ContentLength = int64(len(raw))
	outcome.Response = &kept
	state.rejected = &outcome
}

// exhaustedResult ends an ingress whose candidates produced no response to
// return. A kept request-scoped rejection is still the upstream's answer, so
// the client receives it instead of a synthesized gateway failure.
func (state *requestExecutionState) exhaustedResult(plan requestPlan) (executionResult, error) {
	if state.rejected != nil {
		return state.result(plan, *state.rejected), nil
	}
	return state.failureResult(plan)
}
