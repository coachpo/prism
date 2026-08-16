package routing

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

type testReservation struct {
	released bool
}

func (reservation *testReservation) Release() {
	reservation.released = true
}

type testReservationManager struct {
	results  map[string]ReservationResult
	calls    []Candidate
	requests []ReservationRequest
}

func (manager *testReservationManager) Reserve(_ context.Context, candidate Candidate, request ReservationRequest) (ReservationResult, error) {
	manager.calls = append(manager.calls, candidate)
	manager.requests = append(manager.requests, request)
	result, ok := manager.results[candidate.UpstreamID]
	if !ok {
		return ReservationResult{}, nil
	}
	return result, nil
}

func TestPlannerSelectsFallbackAfterReservationReject(t *testing.T) {
	accepted := &testReservation{}
	manager := &testReservationManager{results: map[string]ReservationResult{
		"primary": {Rejected: true, Reason: gatewaycore.RouteReasonQPSOverflow},
		"backup":  {Reservation: accepted},
	}}
	planner := Planner{Reservations: manager, Deterministic: true}

	plan, err := planner.Select(context.Background(), PlanRequest{
		OperationName:    "openai.chat_completions",
		RequestedModelID: "public-model",
		EffectiveModelID: "target-model",
		Candidates: []Candidate{
			{UpstreamID: "primary", ModelID: "target-model", Priority: 0, Weight: 1, Healthy: true},
			{UpstreamID: "backup", ModelID: "target-model", Priority: 1, Weight: 1, Healthy: true},
		},
	})
	if err != nil {
		t.Fatalf("select fallback route: %v", err)
	}
	if plan.Selected.UpstreamID != "backup" {
		t.Fatalf("expected backup selection after primary reservation reject, got %+v", plan.Selected)
	}
	if plan.Reservation != accepted {
		t.Fatalf("expected accepted reservation to be carried on plan")
	}
	if plan.RouteReason != gatewaycore.RouteReasonQPSOverflow {
		t.Fatalf("expected qps_overflow route reason, got %q", plan.RouteReason)
	}
	if len(plan.CandidateAttempts) != 2 {
		t.Fatalf("expected two candidate attempts, got %+v", plan.CandidateAttempts)
	}
	if plan.CandidateAttempts[0].Reason != gatewaycore.RouteReasonQPSOverflow || plan.CandidateAttempts[1].Reason != gatewaycore.RouteReasonDirectMatch {
		t.Fatalf("expected qps overflow then direct match attempts, got %+v", plan.CandidateAttempts)
	}
	if len(manager.calls) != 2 {
		t.Fatalf("expected reservation attempts for primary and backup, got %+v", manager.calls)
	}
	if got := manager.requests[0].RequestedModelID; got != "public-model" {
		t.Fatalf("expected reservation request model to be filled, got %q", got)
	}
}

func TestPlannerReturnsAdmissionErrorWhenReservationsExhausted(t *testing.T) {
	manager := &testReservationManager{results: map[string]ReservationResult{
		"primary": {Rejected: true, Reason: gatewaycore.RouteReasonQPSOverflow},
		"backup":  {Rejected: true, Reason: gatewaycore.RouteReasonConcurrencyOverflow},
	}}
	planner := Planner{Reservations: manager, Deterministic: true}

	_, err := planner.Select(context.Background(), PlanRequest{
		OperationName:    "openai.chat_completions",
		RequestedModelID: "public-model",
		EffectiveModelID: "target-model",
		Candidates: []Candidate{
			{UpstreamID: "primary", ModelID: "target-model", Priority: 0, Weight: 1, Healthy: true},
			{UpstreamID: "backup", ModelID: "target-model", Priority: 1, Weight: 1, Healthy: true},
		},
	})
	if err == nil {
		t.Fatal("expected admission exhaustion error")
	}
	var gatewayErr *gatewaycore.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("expected GatewayError, got %T %v", err, err)
	}
	if gatewayErr.Type != gatewaycore.ErrorTypeAdmission || gatewayErr.Code != "admission_exhausted" || gatewayErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected admission_exhausted 503, got %+v", gatewayErr)
	}
	if len(gatewayErr.Fields) != 1 || gatewayErr.Fields[0].Field != "route_reason" || gatewayErr.Fields[0].Code != string(gatewaycore.RouteReasonConcurrencyOverflow) {
		t.Fatalf("expected route_reason concurrency_overflow field, got %+v", gatewayErr.Fields)
	}
	if len(manager.calls) != 2 {
		t.Fatalf("expected reservations to exhaust both candidates, got %+v", manager.calls)
	}
}

func TestPlannerSkipsUnhealthyCandidatesBeforeReservation(t *testing.T) {
	manager := &testReservationManager{results: map[string]ReservationResult{
		"healthy": {},
	}}
	planner := Planner{Reservations: manager, Deterministic: true}

	plan, err := planner.Select(context.Background(), PlanRequest{
		OperationName:    "openai.chat_completions",
		RequestedModelID: "public-model",
		EffectiveModelID: "target-model",
		Candidates: []Candidate{
			{UpstreamID: "circuit-open", ModelID: "target-model", Priority: 0, Weight: 1, Healthy: true, CircuitOpen: true},
			{UpstreamID: "unhealthy", ModelID: "target-model", Priority: 1, Weight: 1, Healthy: false},
			{UpstreamID: "healthy", ModelID: "target-model", Priority: 2, Weight: 1, Healthy: true},
		},
	})
	if err != nil {
		t.Fatalf("select healthy route: %v", err)
	}
	if plan.Selected.UpstreamID != "healthy" {
		t.Fatalf("expected healthy candidate, got %+v", plan.Selected)
	}
	if len(manager.calls) != 1 || manager.calls[0].UpstreamID != "healthy" {
		t.Fatalf("expected reservation only for healthy candidate, got %+v", manager.calls)
	}
	if len(plan.CandidateAttempts) != 3 {
		t.Fatalf("expected three attempts including skips, got %+v", plan.CandidateAttempts)
	}
	if plan.CandidateAttempts[0].Reason != gatewaycore.RouteReasonCircuitOpenSkip || plan.CandidateAttempts[1].Reason != gatewaycore.RouteReasonCircuitOpenSkip || plan.CandidateAttempts[2].Reason != gatewaycore.RouteReasonDirectMatch {
		t.Fatalf("expected skipped candidates before direct match, got %+v", plan.CandidateAttempts)
	}
}

func TestPlannerReturnsRoutingErrorWhenNoHealthyCandidates(t *testing.T) {
	manager := &testReservationManager{results: map[string]ReservationResult{}}
	planner := Planner{Reservations: manager, Deterministic: true}

	_, err := planner.Select(context.Background(), PlanRequest{
		OperationName:    "openai.chat_completions",
		RequestedModelID: "public-model",
		EffectiveModelID: "target-model",
		Candidates: []Candidate{
			{UpstreamID: "circuit-open", ModelID: "target-model", Priority: 0, Weight: 1, Healthy: true, CircuitOpen: true},
			{UpstreamID: "unhealthy", ModelID: "target-model", Priority: 1, Weight: 1, Healthy: false},
		},
	})
	if err == nil {
		t.Fatal("expected no healthy upstream error")
	}
	var gatewayErr *gatewaycore.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("expected GatewayError, got %T %v", err, err)
	}
	if gatewayErr.Type != gatewaycore.ErrorTypeRouting || gatewayErr.Code != string(gatewaycore.RouteReasonNoHealthyUpstream) || gatewayErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected no_healthy_upstream 503, got %+v", gatewayErr)
	}
	if len(manager.calls) != 0 {
		t.Fatalf("expected no reservation calls for unhealthy candidates, got %+v", manager.calls)
	}
}

func TestPlannerCanonicalizesUnknownReservationReason(t *testing.T) {
	manager := &testReservationManager{results: map[string]ReservationResult{
		"primary": {Rejected: true, Reason: gatewaycore.RouteReason("unknown_reason")},
	}}
	planner := Planner{Reservations: manager, Deterministic: true}

	_, err := planner.Select(context.Background(), PlanRequest{
		OperationName:    "openai.chat_completions",
		RequestedModelID: "public-model",
		EffectiveModelID: "target-model",
		Candidates:       []Candidate{{UpstreamID: "primary", ModelID: "target-model", Priority: 0, Weight: 1, Healthy: true}},
	})
	if err == nil {
		t.Fatal("expected admission exhaustion error")
	}
	var gatewayErr *gatewaycore.GatewayError
	if !errors.As(err, &gatewayErr) {
		t.Fatalf("expected GatewayError, got %T %v", err, err)
	}
	if gatewayErr.Type != gatewaycore.ErrorTypeAdmission || gatewayErr.Code != "admission_exhausted" {
		t.Fatalf("expected admission exhaustion, got %+v", gatewayErr)
	}
	if len(gatewayErr.Fields) != 1 || gatewayErr.Fields[0].Code != string(gatewaycore.RouteReasonPolicyReject) {
		t.Fatalf("expected unknown reservation reason to canonicalize to policy_reject, got %+v", gatewayErr.Fields)
	}
}

func TestOrderCandidatesRotatesWeightedPriorityTiers(t *testing.T) {
	candidates := []Candidate{
		{UpstreamID: "tier0-a", Priority: 0, Weight: 1},
		{UpstreamID: "tier0-b", Priority: 0, Weight: 3},
		{UpstreamID: "tier1-a", Priority: 1, Weight: 1},
	}
	ordered := OrderCandidates(candidates, 1, false)
	assertCandidateOrder(t, ordered, "tier0-b", "tier0-a", "tier1-a")

	ordered = OrderCandidates(candidates, 1, true)
	assertCandidateOrder(t, ordered, "tier0-a", "tier0-b", "tier1-a")
}

func TestNormalizeReservationRequestDerivesEndpointRequirements(t *testing.T) {
	textRequest, err := NormalizeReservationRequest(ReservationRequest{OperationName: "openai.chat_completions"})
	if err != nil {
		t.Fatalf("normalize text reservation: %v", err)
	}
	if !textRequest.RequireQPS || !textRequest.RequireConcurrency || !textRequest.RequireRPM || !textRequest.RequireTPM || textRequest.RequireIPM || !textRequest.TextEndpoint || textRequest.ImageEndpoint {
		t.Fatalf("expected text endpoint to require qps/concurrency/rpm/tpm only, got %+v", textRequest)
	}

	imageRequest, err := NormalizeReservationRequest(ReservationRequest{OperationName: "openai.images_generations"})
	if err != nil {
		t.Fatalf("normalize image reservation: %v", err)
	}
	if !imageRequest.RequireQPS || !imageRequest.RequireConcurrency || imageRequest.RequireRPM || imageRequest.RequireTPM || !imageRequest.RequireIPM || imageRequest.TextEndpoint || !imageRequest.ImageEndpoint {
		t.Fatalf("expected image endpoint to require qps/concurrency/ipm only, got %+v", imageRequest)
	}
}

func TestInMemoryReservationManagerEnforcesTextRequirements(t *testing.T) {
	nowAt := time.Date(2026, 6, 8, 18, 30, 0, 0, time.UTC)
	manager := &InMemoryReservationManager{Now: func() time.Time { return nowAt }}
	candidate := Candidate{UpstreamID: "text", Healthy: true, QPSLimit: 2, RPMLimit: 1, TPMLimit: 9, MaxConcurrency: 2}
	request := ReservationRequest{OperationName: "openai.chat_completions", InputTokens: 4, OutputTokens: 4}
	first, err := manager.Reserve(context.Background(), candidate, request)
	if err != nil || first.Rejected || first.Reservation == nil {
		t.Fatalf("expected first text reservation accepted, result=%+v err=%v", first, err)
	}

	second, err := manager.Reserve(context.Background(), candidate, request)
	if err != nil {
		t.Fatalf("reserve second text request: %v", err)
	}
	if !second.Rejected || second.Reason != gatewaycore.RouteReasonRPMOverflow {
		t.Fatalf("expected rpm_overflow on second text reservation, got %+v", second)
	}
	first.Reservation.Release()

	manager = &InMemoryReservationManager{Now: func() time.Time { return nowAt }}
	candidate.RPMLimit = 10
	candidate.TPMLimit = 7
	reservation, err := manager.Reserve(context.Background(), candidate, request)
	if err != nil {
		t.Fatalf("reserve tpm-limited text request: %v", err)
	}
	if !reservation.Rejected || reservation.Reason != gatewaycore.RouteReasonTPMOverflow {
		t.Fatalf("expected tpm_overflow for tokenized text reservation, got %+v", reservation)
	}
}

func TestInMemoryReservationManagerEnforcesImageAndConcurrencyRequirements(t *testing.T) {
	nowAt := time.Date(2026, 6, 8, 18, 35, 0, 0, time.UTC)
	manager := &InMemoryReservationManager{Now: func() time.Time { return nowAt }}
	imageCandidate := Candidate{UpstreamID: "image", Healthy: true, QPSLimit: 3, IPMLimit: 1, MaxConcurrency: 2}
	first, err := manager.Reserve(context.Background(), imageCandidate, ReservationRequest{OperationName: "openai.images_edits", ImageCount: 1})
	if err != nil || first.Rejected || first.Reservation == nil {
		t.Fatalf("expected first image reservation accepted, result=%+v err=%v", first, err)
	}
	second, err := manager.Reserve(context.Background(), imageCandidate, ReservationRequest{OperationName: "openai.images_edits", ImageCount: 1})
	if err != nil {
		t.Fatalf("reserve second image request: %v", err)
	}
	if !second.Rejected || second.Reason != gatewaycore.RouteReasonIPMOverflow {
		t.Fatalf("expected ipm_overflow on second image reservation, got %+v", second)
	}

	manager = &InMemoryReservationManager{Now: func() time.Time { return nowAt }}
	concurrencyCandidate := Candidate{UpstreamID: "concurrency", Healthy: true, QPSLimit: 3, RPMLimit: 3, TPMLimit: 30, MaxConcurrency: 1}
	held, err := manager.Reserve(context.Background(), concurrencyCandidate, ReservationRequest{OperationName: "openai.responses", InputTokens: 1, OutputTokens: 1})
	if err != nil || held.Rejected || held.Reservation == nil {
		t.Fatalf("expected held text reservation accepted, result=%+v err=%v", held, err)
	}
	blocked, err := manager.Reserve(context.Background(), concurrencyCandidate, ReservationRequest{OperationName: "openai.responses", InputTokens: 1, OutputTokens: 1})
	if err != nil {
		t.Fatalf("reserve blocked concurrency request: %v", err)
	}
	if !blocked.Rejected || blocked.Reason != gatewaycore.RouteReasonConcurrencyOverflow {
		t.Fatalf("expected concurrency_overflow while reservation held, got %+v", blocked)
	}
	held.Reservation.Release()

	afterRelease, err := manager.Reserve(context.Background(), concurrencyCandidate, ReservationRequest{OperationName: "openai.responses", InputTokens: 1, OutputTokens: 1})
	if err != nil || afterRelease.Rejected || afterRelease.Reservation == nil {
		t.Fatalf("expected reservation accepted after release, result=%+v err=%v", afterRelease, err)
	}
}

func assertCandidateOrder(t *testing.T, candidates []Candidate, want ...string) {
	t.Helper()
	if len(candidates) != len(want) {
		t.Fatalf("expected candidate order %v, got %+v", want, candidates)
	}
	for index, upstreamID := range want {
		if candidates[index].UpstreamID != upstreamID {
			t.Fatalf("expected candidate %d to be %q, got %+v", index, upstreamID, candidates)
		}
	}
}

func TestPlannerUsesAcceptedReservationBeforeSelection(t *testing.T) {
	nowAt := time.Date(2026, 6, 8, 18, 45, 0, 0, time.UTC)
	manager := &InMemoryReservationManager{Now: func() time.Time { return nowAt }}
	primary := Candidate{UpstreamID: "primary", ModelID: "target-model", Priority: 0, Weight: 1, Healthy: true, QPSLimit: 1, RPMLimit: 10, TPMLimit: 100, MaxConcurrency: 1}
	seed, err := manager.Reserve(context.Background(), primary, ReservationRequest{OperationName: "openai.chat_completions", InputTokens: 1, OutputTokens: 1})
	if err != nil || seed.Rejected || seed.Reservation == nil {
		t.Fatalf("seed primary qps reservation: result=%+v err=%v", seed, err)
	}
	seed.Reservation.Release()

	planner := Planner{Reservations: manager, Deterministic: true}
	plan, err := planner.Select(context.Background(), PlanRequest{
		OperationName:    "openai.chat_completions",
		RequestedModelID: "public-model",
		EffectiveModelID: "target-model",
		Reservation:      ReservationRequest{InputTokens: 1, OutputTokens: 1},
		Candidates: []Candidate{
			primary,
			{UpstreamID: "backup", ModelID: "target-model", Priority: 1, Weight: 1, Healthy: true, QPSLimit: 1, RPMLimit: 10, TPMLimit: 100, MaxConcurrency: 1},
		},
	})
	if err != nil {
		t.Fatalf("select qps fallback route: %v", err)
	}
	if plan.Selected.UpstreamID != "backup" || plan.Reservation == nil {
		t.Fatalf("expected backup selected only with accepted reservation, got %+v", plan)
	}
	if plan.RouteReason != gatewaycore.RouteReasonQPSOverflow {
		t.Fatalf("expected qps_overflow route reason, got %+v", plan.CandidateAttempts)
	}
	if plan.CandidateAttempts[0].Reason != gatewaycore.RouteReasonQPSOverflow || plan.CandidateAttempts[1].Reason != gatewaycore.RouteReasonDirectMatch {
		t.Fatalf("expected qps overflow then direct match attempts, got %+v", plan.CandidateAttempts)
	}
}

func TestModelRedirectChangesEffectiveModelAndReentersPlanner(t *testing.T) {
	manager := &testReservationManager{results: map[string]ReservationResult{
		"redirected-primary": {},
	}}
	request, decision, err := ApplyRedirect(PlanRequest{
		OperationName:    "openai.chat_completions",
		RequestedModelID: "public-alpha",
	}, RedirectRule{Type: RedirectTypeModel, TargetModelID: "public-beta"}, map[string][]Candidate{
		"public-beta": {{UpstreamID: "redirected-primary", ModelID: "public-beta", Priority: 0, Weight: 1, Healthy: true}},
	})
	if err != nil {
		t.Fatalf("apply model redirect: %v", err)
	}
	planner := Planner{Reservations: manager, Deterministic: true}
	plan, err := planner.Select(context.Background(), request)
	if err != nil {
		t.Fatalf("select redirected model: %v", err)
	}
	if decision.RouteReason != gatewaycore.RouteReasonModelRedirect || plan.RouteReason != gatewaycore.RouteReasonModelRedirect {
		t.Fatalf("expected model_redirect decision and plan reason, decision=%+v plan=%+v", decision, plan)
	}
	if plan.RequestedModelID != "public-alpha" || plan.EffectiveModelID != "public-beta" {
		t.Fatalf("expected requested/effective models to remain distinct, got %+v", plan)
	}
	if len(manager.calls) != 1 || manager.calls[0].UpstreamID != "redirected-primary" {
		t.Fatalf("expected redirected model candidates to be planned normally, got %+v", manager.calls)
	}
}

func TestUpstreamRedirectNarrowsCandidatesWithoutChangingModel(t *testing.T) {
	manager := &testReservationManager{results: map[string]ReservationResult{
		"east": {},
	}}
	request, decision, err := ApplyRedirect(PlanRequest{
		OperationName:    "openai.responses",
		RequestedModelID: "public-alpha",
		EffectiveModelID: "public-alpha",
		Candidates: []Candidate{
			{UpstreamID: "west", ModelID: "public-alpha", Priority: 0, Weight: 1, Healthy: true},
			{UpstreamID: "east", ModelID: "public-alpha", Priority: 1, Weight: 1, Healthy: true},
		},
	}, RedirectRule{Type: RedirectTypeUpstream, UpstreamIDs: []string{"east"}}, nil)
	if err != nil {
		t.Fatalf("apply upstream redirect: %v", err)
	}
	planner := Planner{Reservations: manager, Deterministic: true}
	plan, err := planner.Select(context.Background(), request)
	if err != nil {
		t.Fatalf("select upstream redirect: %v", err)
	}
	if decision.RouteReason != gatewaycore.RouteReasonUpstreamRedirect || plan.RouteReason != gatewaycore.RouteReasonUpstreamRedirect {
		t.Fatalf("expected upstream_redirect decision and plan reason, decision=%+v plan=%+v", decision, plan)
	}
	if plan.RequestedModelID != "public-alpha" || plan.EffectiveModelID != "public-alpha" {
		t.Fatalf("expected upstream redirect to preserve requested/effective model, got %+v", plan)
	}
	if len(manager.calls) != 1 || manager.calls[0].UpstreamID != "east" || plan.Selected.UpstreamID != "east" {
		t.Fatalf("expected upstream redirect to pin east only, calls=%+v plan=%+v", manager.calls, plan)
	}
}

func TestRetryPolicyClassifiesOnlyPreCommitRetryCategories(t *testing.T) {
	policy := RetryPolicy{FailoverStatusCodes: []int{408, 500, 503}}

	cases := []struct {
		name       string
		statusCode int
		retryable  bool
		reason     gatewaycore.RouteReason
	}{
		{name: "unconfigured 429", statusCode: http.StatusTooManyRequests, retryable: false},
		{name: "configured 5xx", statusCode: http.StatusServiceUnavailable, retryable: true, reason: gatewaycore.RouteReasonRetry5xx},
		{name: "configured non-5xx", statusCode: http.StatusRequestTimeout, retryable: true, reason: gatewaycore.RouteReasonRetryHTTP},
		{name: "unconfigured 5xx", statusCode: http.StatusNotImplemented, retryable: false},
		{name: "auth provider error", statusCode: http.StatusForbidden, retryable: false},
		{name: "validation provider error", statusCode: http.StatusUnprocessableEntity, retryable: false},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			decision := policy.ClassifyHTTPStatus(test.statusCode)
			if decision.Retryable != test.retryable {
				t.Fatalf("expected retryable=%t for status %d, got %+v", test.retryable, test.statusCode, decision)
			}
			if test.reason != "" && decision.Reason != test.reason {
				t.Fatalf("expected route reason %q, got %+v", test.reason, decision)
			}
		})
	}
	if decision := (RetryPolicy{FailoverStatusCodes: []int{http.StatusTooManyRequests}}).ClassifyHTTPStatus(http.StatusTooManyRequests); !decision.Retryable || decision.Reason != gatewaycore.RouteReasonRetry429 {
		t.Fatalf("expected configured 429 to remain retryable, got %+v", decision)
	}
}

type testRetryTimeoutError struct{}

func (testRetryTimeoutError) Error() string { return "connect timeout" }
func (testRetryTimeoutError) Timeout() bool { return true }
func (testRetryTimeoutError) Is(err error) bool {
	return err == context.DeadlineExceeded
}

func TestRetryPolicyClassifiesConnectTimeoutTransportError(t *testing.T) {
	decision := RetryPolicy{}.ClassifyTransportError(nil, testRetryTimeoutError{})
	if !decision.Retryable || decision.Reason != gatewaycore.RouteReasonRetryConnectTimeout {
		t.Fatalf("expected retry_connect_timeout decision, got %+v", decision)
	}
}

func TestRetryPolicyClassifiesPreCommitTransportErrors(t *testing.T) {
	decision := RetryPolicy{}.ClassifyTransportError(nil, errors.New("connection reset before response headers"))
	if !decision.Retryable || decision.Reason != gatewaycore.RouteReasonRetryTransport {
		t.Fatalf("expected retry_transport decision, got %+v", decision)
	}
	for _, contextErr := range []error{context.Canceled, context.DeadlineExceeded} {
		if decision := (RetryPolicy{}).ClassifyTransportError(contextErr, testRetryTimeoutError{}); decision.Retryable {
			t.Fatalf("expected request cancellation %v to remain definitive, got %+v", contextErr, decision)
		}
	}
}
