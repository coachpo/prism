package runtime

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coachpo/prism/backend/internal/domain/safediag"
)

// Attempt lifecycle constants (Requests SPEC §3.4/§4.5, Observe SPEC §3.5).
const (
	attemptTriggerInitial         = "initial"
	attemptTriggerRetrySameTarget = "retry_same_target"
	attemptTriggerHedge           = "hedge"
	attemptTriggerFailover        = "failover"

	attemptResultCompleted          = "completed"
	attemptResultHTTPError          = "http_error"
	attemptResultStreamError        = "stream_error"
	attemptResultTransportError     = "transport_error"
	attemptResultCancelled          = "cancelled"
	attemptResultClientDisconnected = "client_disconnected"
	attemptResultUnknown            = "unknown"

	errorSourcePrism     = "prism"
	errorSourceUpstream  = "upstream"
	errorSourceTransport = "transport"
	errorSourceClient    = "client"
	errorSourceUnknown   = "unknown"

	failureStageRouting          = "routing"
	failureStageAdmission        = "admission"
	failureStageUpstreamConnect  = "upstream_connect"
	failureStageUpstreamResponse = "upstream_response"
	failureStageStream           = "stream"
	failureStageUnknown          = "unknown"

	// MaxLaunchedUpstreamAttempts is the executor hard safety bound (64 per
	// ingress). Reaching it terminates further launches with a gateway 503 and
	// typed attempt_budget_exhausted; no 65th upstream row is ever constructed.
	MaxLaunchedUpstreamAttempts = 64

	// MaxFailedResponseSamplers bounds concurrent intermediate failed-response
	// sampler goroutines per ingress (Requests SPEC §4.1).
	MaxFailedResponseSamplers = 8

	// FailedResponseSampleDeadline bounds each sampler read.
	FailedResponseSampleDeadline = 50 * time.Millisecond

	// FailedResponseSampleBytes bounds each sampler read (32 KiB per attempt);
	// the sample never enters the outbox.
	FailedResponseSampleBytes = int64(safediag.MaxUpstreamErrorSampleBytes)
)

// attemptFailureDiagnostics carries the safe failure projection for one
// attempt. Raw samples never enter these fields.
type attemptFailureDiagnostics struct {
	Source    string
	Stage     string
	Code      string
	Detail    string
	Redacted  bool
	Truncated bool
}

// runtimeAttemptLifecycle is frozen at the launch site before provider
// transport begins.
type runtimeAttemptLifecycle struct {
	LaunchOrdinal  int
	AttemptTrigger string
}

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

// safeTransportDiagnostic builds the bounded transport diagnostic from a
// sanitized typed error string. It never includes raw provider bytes.
func safeTransportDiagnostic(err error) attemptFailureDiagnostics {
	if err == nil {
		return attemptFailureDiagnostics{}
	}
	message := err.Error()
	scrubbed := safediag.ScrubValue(message, safediag.ScrubOptions{MaxBytes: safediag.MaxErrorDetailBytes})
	return attemptFailureDiagnostics{
		Source:    errorSourceTransport,
		Stage:     failureStageUpstreamConnect,
		Code:      safediag.CodeTransportError,
		Detail:    scrubbed.Value,
		Redacted:  scrubbed.Redacted,
		Truncated: scrubbed.Truncated,
	}
}

// safeStreamDiagnostic builds the bounded stream diagnostic. kind is the
// typed stream_error_kind (may be empty); detail is the raw stream error text
// which is scrubbed here before persistence.
func safeStreamDiagnostic(source string, stage string, kind string, outcome string, rawDetail string) attemptFailureDiagnostics {
	code := ""
	if strings.TrimSpace(kind) != "" {
		code = safediag.StreamKindFallbackCode(strings.TrimSpace(kind))
	} else if strings.TrimSpace(outcome) != "" {
		code = safediag.StreamOutcomeFallbackCode(strings.TrimSpace(outcome))
	}
	scrubbed := safediag.ScrubValue(rawDetail, safediag.ScrubOptions{MaxBytes: safediag.MaxStreamErrorDetailBytes})
	return attemptFailureDiagnostics{
		Source:    source,
		Stage:     stage,
		Code:      code,
		Detail:    scrubbed.Value,
		Redacted:  scrubbed.Redacted,
		Truncated: scrubbed.Truncated,
	}
}

// stableFallbackCode returns the stable error code for an HTTP failure.
func stableHTTPErrorCode(statusCode int, providerCode string) string {
	if code := safediag.AdoptProviderCode(providerCode); code != "" {
		return code
	}
	return safediag.HTTPFallbackCode(statusCode)
}

// validateAttemptTrigger ensures only the four fixed trigger values are
// persisted for new upstream rows.
func validateAttemptTrigger(trigger string) bool {
	switch trigger {
	case attemptTriggerInitial, attemptTriggerRetrySameTarget, attemptTriggerHedge, attemptTriggerFailover:
		return true
	default:
		return false
	}
}

// formatAttemptBudgetError builds the typed launch-safety error.
func formatAttemptBudgetError(modelID string) error {
	return &domainError{
		StatusCode: http.StatusServiceUnavailable,
		ErrorCode:  safediag.CodeAttemptBudgetExhausted,
		Detail:     fmt.Sprintf("Launch safety bound reached for model '%s': maximum of %d upstream attempts per ingress.", modelID, MaxLaunchedUpstreamAttempts),
		Fields:     map[string]any{"max_launched_upstream_attempts": MaxLaunchedUpstreamAttempts},
	}
}
