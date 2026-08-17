package runtime

import (
	"context"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/loadbalance"
	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

func (s *Service) executeRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, bodySource *runtimeRequestBodySource) (executionResult, error) {
	state := newRequestExecutionState(plan)
	limits := requestExecutionLimitsForPlan(plan)
	terminalAttempts := plan.orderedTerminalAttempts()

	for index := 0; index < len(terminalAttempts); index++ {
		// Launch safety bound (Requests SPEC §4.6): at most 64 launched
		// upstream attempts per ingress. The executor checks before the next
		// launch and terminates with gateway 503 + typed
		// attempt_budget_exhausted; it never constructs a 65th upstream row.
		if state.launchedAttempts >= MaxLaunchedUpstreamAttempts {
			result, err := state.budgetExhaustedResult(plan)
			return result, err
		}
		if limits.remainingLaunchCapacity(state) <= 0 {
			break
		}
		// Fixed safety cap: never launch the 65th upstream attempt.
		if state.launchedAttempts >= MaxLaunchedUpstreamAttempts {
			return state.attemptBudgetExhaustedResult(plan)
		}
		if limits.shouldHedge(plan, state, index) {
			hedged, err := s.executeHedgedRequest(ctx, method, plan, requestQuery, index, limits.HedgePolicy, bodySource, &state)
			if err != nil {
				return executionResult{}, err
			}
			state.recordHedgedResult(hedged)
			if hedged.Winner != nil {
				return s.executionResultForHedgedWinner(ctx, plan, state, hedged.Winner), nil
			}
			index += hedged.ConsumedConnections - 1
			continue
		}

		lifecycle := runtimeAttemptLifecycle{LaunchOrdinal: state.nextLaunchOrdinal, AttemptTrigger: state.nextLaunchTrigger(plan, index, terminalAttempts[index])}
		outcome := s.executeSingleAttempt(ctx, method, plan, requestQuery, terminalAttempts[index], bodySource, lifecycle)
		result, done, err := s.handleSingleExecutionOutcome(ctx, plan, &state, outcome, index, limits.MaxAttempts)
		if err != nil {
			return executionResult{}, err
		}
		if done {
			return result, nil
		}
	}
	result, err := state.failureResult(plan)
	return result, err
}

func (s *Service) executionResultForHedgedWinner(ctx context.Context, plan requestPlan, state requestExecutionState, winner *executionOutcome) executionResult {
	if winner.Response.StatusCode >= 200 && winner.Response.StatusCode <= 299 && winner.Launched {
		s.recordRuntimeSuccess(ctx, plan, winner.Connection, winner.TerminalAttempt.Strategy, winner.Attempt.ResponseHeadersLatencyMS, winner.Attempt.CompletedAt)
	}
	return state.result(plan, *winner)
}

func (s *Service) handleSingleExecutionOutcome(ctx context.Context, plan requestPlan, state *requestExecutionState, outcome executionOutcome, index int, maxAttempts int) (executionResult, bool, error) {
	if outcome.FatalError != nil {
		return executionResult{}, false, outcome.FatalError
	}
	if outcome.UnbannedRecord != nil {
		s.recordRuntimeUnbanned(ctx, plan, outcome.Connection, *outcome.UnbannedRecord, s.nowUTC())
	}
	if outcome.Skipped {
		return executionResult{}, false, nil
	}
	if outcome.AdmissionReason != "" {
		state.recordAdmissionRejection(outcome.AdmissionReason)
		if outcome.AdmissionState != nil {
			s.recordRuntimeAdmissionRejected(ctx, plan, outcome.Connection, *outcome.AdmissionState, s.nowUTC())
		}
		return executionResult{}, false, nil
	}
	if outcome.Launched {
		state.recordLaunchedAttempt(outcome)
	}
	if outcome.Err != nil {
		state.lastError = upstreamFailureClass(outcome.Err)
		if outcome.Launched && !outcome.SuppressTransportFeedback {
			s.recordRuntimeTransportFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
		}
		if outcome.FailoverEligible && index < len(plan.orderedTerminalAttempts())-1 && state.launchedAttempts < maxAttempts {
			state.recordRetry(outcome.RetryDecision.Reason)
			return executionResult{}, false, nil
		}
		result, err := state.failureResult(plan)
		return result, true, err
	}
	if outcome.FailoverEligible && outcome.Launched {
		s.recordRuntimeFailoverHTTPFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
	}
	if outcome.FailoverEligible && index < len(plan.orderedTerminalAttempts())-1 && state.launchedAttempts < maxAttempts {
		state.lastError = safediag.HTTPFallbackCode(outcome.Response.StatusCode)
		state.recordRetry(outcome.RetryDecision.Reason)
		// Intermediate retry/failover: the bounded sampler owns the failed
		// response body; the next launch never waits for it.
		s.startFailedResponseSampler(ctx, plan, &outcome)
		if len(state.attempts) > 0 {
			state.attempts[len(state.attempts)-1].Sampler = outcome.Attempt.Sampler
		}
		if outcome.Attempt.Sampler == nil && outcome.Response != nil {
			// No sampler owns this body (sampler-cap hit or non-sampled path);
			// close it here so the next launch is not blocked by an open body.
			_ = outcome.Response.Body.Close()
		}
		return executionResult{}, false, nil
	}
	if outcome.Response.StatusCode >= 200 && outcome.Response.StatusCode <= 299 && outcome.Launched {
		s.recordRuntimeSuccess(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.ResponseHeadersLatencyMS, outcome.Attempt.CompletedAt)
	}
	return state.result(plan, outcome), true, nil
}

func (s *Service) executeHedgedRequest(ctx context.Context, method string, plan requestPlan, requestQuery string, startIndex int, hedgePolicy loadbalance.RuntimeHedgePolicy, bodySource *runtimeRequestBodySource, state *requestExecutionState) (hedgedExecutionResult, error) {
	terminalAttempts := plan.orderedTerminalAttempts()
	totalCandidates := hedgePolicy.MaxAdditionalAttempts + 1
	remainingConnections := len(terminalAttempts) - startIndex
	if totalCandidates > remainingConnections {
		totalCandidates = remainingConnections
	}
	if totalCandidates > MaxLaunchedUpstreamAttempts {
		totalCandidates = MaxLaunchedUpstreamAttempts
	}
	if totalCandidates <= 0 {
		return hedgedExecutionResult{}, nil
	}

	results := make(chan hedgedAttemptResult, totalCandidates)
	cancelFuncs := make([]context.CancelCauseFunc, 0, totalCandidates)
	inFlight := 0
	launchedCandidates := 0
	nextOrder := 0
	// Immutable launch ordinals and hedge triggers are frozen at the launch
	// site (Observe SPEC §3.5); the first hedged launch carries the trigger
	// classified by the caller, later launches are hedge-triggered.
	firstTrigger := attemptTriggerHedge
	if state != nil && state.launchedAttempts == 0 {
		firstTrigger = attemptTriggerInitial
	}
	nextOrdinal := 0
	if state != nil {
		nextOrdinal = state.nextLaunchOrdinal
	}
	launchAttempt := func(order int) {
		attemptCtx, cancel := context.WithCancelCause(ctx)
		cancelFuncs = append(cancelFuncs, cancel)
		terminalAttempt := terminalAttempts[startIndex+order]
		trigger := attemptTriggerHedge
		if order == 0 {
			trigger = firstTrigger
		}
		lifecycle := runtimeAttemptLifecycle{LaunchOrdinal: nextOrdinal + order, AttemptTrigger: trigger}
		inFlight++
		launchedCandidates++
		go func() {
			results <- hedgedAttemptResult{Order: order, Outcome: s.executeSingleAttempt(attemptCtx, method, plan, requestQuery, terminalAttempt, bodySource, lifecycle)}
		}()
	}
	launchAttempt(0)
	nextOrder = 1

	timer := time.NewTimer(hedgePolicy.Delay)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	nonWinningAttempts := make([]executionAttempt, 0, totalCandidates)
	result := hedgedExecutionResult{ConsumedConnections: launchedCandidates}
	var winner *executionOutcome

	for inFlight > 0 {
		var timerCh <-chan time.Time
		if winner == nil && nextOrder < totalCandidates {
			timerCh = timer.C
		}
		select {
		case <-timerCh:
			launchAttempt(nextOrder)
			nextOrder++
			result.ConsumedConnections = launchedCandidates
			if winner == nil && nextOrder < totalCandidates {
				timer.Reset(hedgePolicy.Delay)
			}
		case attemptResult := <-results:
			inFlight--
			outcome := attemptResult.Outcome
			if outcome.FatalError != nil {
				for _, cancel := range cancelFuncs {
					cancel(nil)
				}
				return hedgedExecutionResult{}, outcome.FatalError
			}
			if outcome.UnbannedRecord != nil {
				s.recordRuntimeUnbanned(ctx, plan, outcome.Connection, *outcome.UnbannedRecord, s.nowUTC())
			}
			if outcome.Skipped {
				continue
			}
			if outcome.AdmissionReason != "" {
				result.AdmissionRejections++
				result.LastAdmissionReason = outcome.AdmissionReason
				result.RouteReason = runtimeAdmissionRouteReason(outcome.AdmissionReason)
				if outcome.AdmissionState != nil {
					s.recordRuntimeAdmissionRejected(ctx, plan, outcome.Connection, *outcome.AdmissionState, s.nowUTC())
				}
				continue
			}
			if outcome.Launched {
				result.LaunchedAttempts++
			}
			if winner != nil {
				if outcome.Response != nil && outcome.Attempt.Sampler == nil {
					// Non-winner responses that are failover-eligible get the
					// bounded sampler (which exclusively owns the failed-response
					// body); other bodies are closed here.
					if outcome.FailoverEligible && outcome.Launched {
						s.startFailedResponseSampler(ctx, plan, &outcome)
					}
					if outcome.Attempt.Sampler == nil {
						_ = outcome.Response.Body.Close()
					}
				}
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				continue
			}
			if outcome.Err != nil {
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				if !outcome.SuppressTransportFeedback {
					result.LastError = upstreamFailureClass(outcome.Err)
					s.recordRuntimeTransportFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
				}
				continue
			}
			if outcome.FailoverEligible {
				if outcome.Launched {
					nonWinningAttempts = append(nonWinningAttempts, outcome.Attempt)
				}
				result.LastError = safediag.HTTPFallbackCode(outcome.Response.StatusCode)
				s.recordRuntimeFailoverHTTPFailure(ctx, plan, outcome.Connection, outcome.TerminalAttempt.Strategy, outcome.Attempt.CompletedAt)
				// Failover-eligible loser responses are sampled (sampler owns
				// close); non-sampled bodies are closed here.
				s.startFailedResponseSampler(ctx, plan, &outcome)
				if len(nonWinningAttempts) > 0 && outcome.Attempt.Sampler != nil {
					nonWinningAttempts[len(nonWinningAttempts)-1].Sampler = outcome.Attempt.Sampler
				}
				if outcome.Attempt.Sampler == nil && outcome.Response != nil {
					_ = outcome.Response.Body.Close()
				}
				continue
			}
			winner = &outcome
			for order, cancel := range cancelFuncs {
				if order == attemptResult.Order {
					continue
				}
				cancel(errHedgeLoserCanceled)
			}
		}
	}

	result.ConsumedConnections = launchedCandidates
	result.Attempts = nonWinningAttempts
	if winner != nil {
		result.Winner = winner
		result.Attempts = append(result.Attempts, winner.Attempt)
	}
	return result, nil
}
