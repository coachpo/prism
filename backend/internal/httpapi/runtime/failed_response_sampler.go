package runtime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

// MaxFailedResponseSamplers bounds concurrent intermediate failed-response
// sampler goroutines per ingress (Requests SPEC §4.1).
const MaxFailedResponseSamplers = 8

// FailedResponseSampleDeadline bounds each sampler read.
const FailedResponseSampleDeadline = 50 * time.Millisecond

// FailedResponseSampleBytes bounds each sampler read (32 KiB per attempt);
// the sample never enters the outbox.
const FailedResponseSampleBytes = int64(safediag.MaxUpstreamErrorSampleBytes)

// runtimeSampledFailure is the async result of a bounded failed-response
// sampler.
type runtimeSampledFailure struct {
	mu       sync.Mutex
	done     bool
	fallback bool
	diag     attemptFailureDiagnostics
}

func (sample *runtimeSampledFailure) markFallback(source string, stage string, code string, detail string) {
	sample.mu.Lock()
	defer sample.mu.Unlock()
	sample.done = true
	sample.fallback = true
	sample.diag = attemptFailureDiagnostics{Source: source, Stage: stage, Code: code, Detail: detail}
}

func (sample *runtimeSampledFailure) markExtracted(diag attemptFailureDiagnostics) {
	sample.mu.Lock()
	defer sample.mu.Unlock()
	sample.done = true
	sample.diag = diag
}

// result returns the diagnostic only when the sampler completed; otherwise it
// reports unavailable so the telemetry sealer can use a generic fallback
// without waiting for the sampler.
func (sample *runtimeSampledFailure) result() (attemptFailureDiagnostics, bool) {
	if sample == nil {
		return attemptFailureDiagnostics{}, false
	}
	sample.mu.Lock()
	defer sample.mu.Unlock()
	if !sample.done || sample.fallback {
		return attemptFailureDiagnostics{}, false
	}
	return sample.diag, true
}

// failedResponseSampler reads at most 32 KiB of a failed upstream response
// with a fixed 50 ms deadline, extracts the safe diagnostic, and exclusively
// closes the response body. It never blocks the next launch: the caller does
// not wait for it; telemetry sealing uses whatever completed.
type failedResponseSampler struct {
	ingressID   string
	response    *http.Response
	contentType string
	extraRules  []safediag.SensitiveNameRule
	result      *runtimeSampledFailure
	release     func()
}

type failedResponseSamplerLimiter struct {
	mu     sync.Mutex
	counts map[string]int
}

func (limiter *failedResponseSamplerLimiter) acquire(ingressID string) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.counts == nil {
		limiter.counts = make(map[string]int)
	}
	if limiter.counts[ingressID] >= MaxFailedResponseSamplers {
		return false
	}
	limiter.counts[ingressID]++
	return true
}

func (limiter *failedResponseSamplerLimiter) release(ingressID string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.counts[ingressID] <= 1 {
		delete(limiter.counts, ingressID)
		return
	}
	limiter.counts[ingressID]--
}

func newFailedResponseSampler(ingressID string, response *http.Response, contentType string, extraRules []safediag.SensitiveNameRule) *failedResponseSampler {
	return &failedResponseSampler{
		ingressID:   ingressID,
		response:    response,
		contentType: contentType,
		extraRules:  extraRules,
		result:      &runtimeSampledFailure{},
	}
}

// run executes the bounded sample and closes the response body exactly once.
func (sampler *failedResponseSampler) run() {
	if sampler == nil {
		return
	}
	var closeOnce sync.Once
	closeBody := func() {
		closeOnce.Do(func() {
			if sampler.response != nil && sampler.response.Body != nil {
				_ = sampler.response.Body.Close()
			}
		})
	}
	defer func() {
		closeBody()
		if sampler.release != nil {
			sampler.release()
		}
	}()
	if sampler.response == nil || sampler.response.Body == nil {
		return
	}
	deadline := time.AfterFunc(FailedResponseSampleDeadline, closeBody)
	defer deadline.Stop()
	limited := &io.LimitedReader{R: sampler.response.Body, N: FailedResponseSampleBytes}
	raw, err := io.ReadAll(limited)
	if err != nil {
		sampler.result.markFallback(errorSourceUpstream, failureStageUpstreamResponse, "", "")
		return
	}
	extraction := safediag.ExtractProviderErrorEnvelope(raw, sampler.contentType, sampler.extraRules...)
	if !extraction.Recognized {
		// Unrecognized failure body: the stable code fallback is applied at
		// persistence, and the detail falls back to a bounded generic safe
		// message so a non-2xx failure never persists a null error_detail
		// (Requests/Audit SPEC §4.2 safe fallback).
		fallbackDetail := fmt.Sprintf("upstream returned HTTP %d", sampler.response.StatusCode)
		sampler.result.markFallback(errorSourceUpstream, failureStageUpstreamResponse, "", fallbackDetail)
		return
	}
	code := extraction.Code
	if code == "" {
		code = safediag.HTTPFallbackCode(sampler.response.StatusCode)
	}
	sampler.result.markExtracted(attemptFailureDiagnostics{
		Source:    errorSourceUpstream,
		Stage:     failureStageUpstreamResponse,
		Code:      code,
		Detail:    extraction.Detail,
		Redacted:  extraction.Redacted,
		Truncated: extraction.Truncated,
	})
}

// startFailedResponseSampler begins the bounded failed-response sampler for an
// intermediate retry/failover non-2xx response. The sampler reads at most
// 32 KiB with a 50 ms deadline and exclusively owns the failed response body
// close. The next launch never waits for it; telemetry sealing uses whatever
// completed or falls back to generic status. It MUST only be called for
// responses that will NOT be passed through to the client (the final selected
// response keeps its body for passthrough).
func (s *Service) startFailedResponseSampler(ctx context.Context, plan requestPlan, outcome *executionOutcome) {
	if outcome == nil || outcome.Response == nil || outcome.Attempt.Sampler != nil {
		return
	}
	ingressID := runtimeIngressRequestIDFromContext(ctx)
	s.failedResponseSamplerOnce.Do(func() {
		s.failedResponseSamplers = &failedResponseSamplerLimiter{}
	})
	if !s.failedResponseSamplers.acquire(ingressID) {
		return
	}
	contentType := strings.ToLower(strings.TrimSpace(outcome.Response.Header.Get("Content-Type")))
	sampler := newFailedResponseSampler(
		ingressID,
		outcome.Response,
		contentType,
		planBlocklistSensitiveRules(plan),
	)
	sampler.release = func() { s.failedResponseSamplers.release(ingressID) }
	outcome.Attempt.Sampler = sampler
	go sampler.run()
}

// planBlocklistSensitiveRules converts the request-time effective Header
// Blocklist into extra sensitive-name rules for runtime scrubbing. Every
// consumer remains at least as strict as the outbound forwarding policy.
func planBlocklistSensitiveRules(plan requestPlan) []safediag.SensitiveNameRule {
	rules := make([]safediag.SensitiveNameRule, 0, len(plan.BlocklistRules))
	for _, rule := range plan.BlocklistRules {
		rules = append(rules, safediag.SensitiveNameRule{MatchType: rule.MatchType, Pattern: rule.Pattern})
	}
	return rules
}

func (s *Service) isHedgeLoserCancellation(ctx context.Context, err error) bool {
	return err != nil && errors.Is(err, context.Canceled) && errors.Is(context.Cause(ctx), errHedgeLoserCanceled)
}
