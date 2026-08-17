package runtime

import (
	"context"
	"errors"
	"strings"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

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
// Blocklist into scrubber extra rules so diagnostics are at least as strict
// as the outbound forwarding policy.
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
