package loadbalance

import "math"

// RetryDelayBounds is the deterministic delay projection for one retry-cycle
// attempt: the nominal (jitter-free) delay plus the inclusive jitter offset
// range, before any random sampling. Preview projection and runtime sampling
// MUST share this helper; preview must never call the sampling wrapper and the
// frontend must never re-implement the formula.
type RetryDelayBounds struct {
	NominalDelayMS   int
	JitterMinDelayMS int
	JitterMaxDelayMS int
}

// RetryDelayBounds computes the deterministic bounds for cycleAttempt within a
// single retry cycle. The nominal delay is ceil(base * max(multiplier,1)^(attempt-1))
// and is saturation-capped to retry_max_delay_ms BEFORE integer conversion so a
// high multiplier/attempt combination can never overflow. The jitter offset
// bound is round(nominal * jitter_ratio); the range is clamped to
// [max(0, nominal-bound), min(max_delay, nominal+bound)]. A disabled policy or a
// zero base delay yields all-zero bounds.
func retryDelayBounds(policy runtimeFeedbackPolicy, cycleAttempt int) RetryDelayBounds {
	if !policy.Enabled || policy.BaseDelayMS <= 0 {
		return RetryDelayBounds{}
	}
	attempt := maxInt(cycleAttempt, 1)
	multiplier := math.Pow(maxFloat(policy.BackoffMultiplier, 1), float64(attempt-1))
	nominal := math.Ceil(float64(policy.BaseDelayMS) * multiplier)
	if policy.MaxDelayMS > 0 && nominal > float64(policy.MaxDelayMS) {
		nominal = float64(policy.MaxDelayMS)
	} else if nominal > 9e18 {
		// Defensive int64 saturation guard; unreachable with validated policies
		// (retry_max_delay_ms >= 1), kept so conversion can never be
		// implementation-defined.
		nominal = 9e18
	}
	nominalMS := int(nominal)
	if nominalMS < 0 {
		nominalMS = 0
	}
	if policy.JitterRatio <= 0 || nominalMS == 0 {
		return RetryDelayBounds{NominalDelayMS: nominalMS, JitterMinDelayMS: nominalMS, JitterMaxDelayMS: nominalMS}
	}
	offsetBound := int(math.Round(float64(nominalMS) * policy.JitterRatio))
	lower := nominalMS - offsetBound
	if lower < 0 {
		lower = 0
	}
	upper := nominalMS + offsetBound
	if policy.MaxDelayMS > 0 && upper > policy.MaxDelayMS {
		upper = policy.MaxDelayMS
	}
	return RetryDelayBounds{NominalDelayMS: nominalMS, JitterMinDelayMS: lower, JitterMaxDelayMS: upper}
}

const (
	// PreviewTerminationCycleExhausted reports that the cycle retry attempt
	// limit ended the simulated sequence.
	PreviewTerminationCycleExhausted = "cycle_exhausted"
	// PreviewTerminationBanTransition reports that the cumulative retry
	// threshold triggered a ban transition and ended the simulated sequence.
	PreviewTerminationBanTransition = "ban_transition"
	// PreviewTerminationFiveStepLimit reports that the five-step preview cap
	// truncated a longer cycle; has_more is true in that case.
	PreviewTerminationFiveStepLimit = "five_step_limit"

	// PreviewMaxSteps is the maximum number of consecutive failure feedback
	// steps a preview may show.
	PreviewMaxSteps = 5
)

// RetryPreviewBanTransition describes the ban transition triggered at a step.
type RetryPreviewBanTransition struct {
	Mode            string `json:"mode"`
	DurationSeconds int    `json:"duration_seconds"`
}

// RetryPreviewStep is one simulated consecutive failure feedback step.
type RetryPreviewStep struct {
	FailureOrdinal         int                        `json:"failure_ordinal"`
	CycleRetryAttempt      int                        `json:"cycle_retry_attempt"`
	CumulativeRetryAttempt int                        `json:"cumulative_retry_attempt"`
	NominalDelayMS         int                        `json:"nominal_delay_ms"`
	JitterMinDelayMS       int                        `json:"jitter_min_delay_ms"`
	JitterMaxDelayMS       int                        `json:"jitter_max_delay_ms"`
	CycleExhausted         bool                       `json:"cycle_exhausted"`
	BanTransition          *RetryPreviewBanTransition `json:"ban_transition"`
}

// RetryPreviewBanProjection reports the ban policy projection even when the ban
// threshold is not reached within the shown steps.
type RetryPreviewBanProjection struct {
	Mode                            string `json:"mode"`
	CumulativeRetryAttemptThreshold int    `json:"cumulative_retry_attempt_threshold"`
	TransitionAtCumulativeFailure   *int   `json:"transition_at_cumulative_failure"`
	DurationSeconds                 int    `json:"duration_seconds"`
}

// RetryPreviewResult is the deterministic, side-effect-free projection of the
// first consecutive failure feedback of one retry cycle starting from a clean
// state, with no success and no cooldown expiry in between.
type RetryPreviewResult struct {
	Steps                       []RetryPreviewStep        `json:"steps"`
	ShownStepCount              int                       `json:"shown_step_count"`
	HasMore                     bool                      `json:"has_more"`
	TerminationReason           string                    `json:"termination_reason"`
	CycleExhaustionAfterAttempt int                       `json:"cycle_exhaustion_after_attempt"`
	BanProjection               RetryPreviewBanProjection `json:"ban_projection"`
}

// PreviewRetryCycle simulates up to PreviewMaxSteps consecutive failure
// feedback steps and stops at the earliest of the five-step cap, the cycle
// retry attempt limit, or the cumulative ban threshold transition. It never
// reads or writes DB, runtime state, cache, events, generation or audit state.
func PreviewRetryCycle(policy runtimeFeedbackPolicy) RetryPreviewResult {
	cycleLimit := maxInt(policy.CycleRetryAttemptLimit, 1)
	threshold := maxInt(policy.BanCumulativeRetryAttemptThreshold, 0)
	projection := RetryPreviewBanProjection{
		Mode:                            normalizeBanMode(policy.BanMode),
		CumulativeRetryAttemptThreshold: threshold,
		DurationSeconds:                 maxInt(policy.BanDurationSeconds, 0),
	}
	if projection.Mode != "off" {
		transitionAt := threshold
		projection.TransitionAtCumulativeFailure = &transitionAt
	}

	steps := make([]RetryPreviewStep, 0, PreviewMaxSteps)
	for ordinal := 1; ordinal <= PreviewMaxSteps; ordinal++ {
		bounds := retryDelayBounds(policy, ordinal)
		cycleExhausted := ordinal >= cycleLimit
		var transition *RetryPreviewBanTransition
		if projection.Mode != "off" && ordinal >= threshold {
			transition = &RetryPreviewBanTransition{Mode: projection.Mode, DurationSeconds: projection.DurationSeconds}
		}
		steps = append(steps, RetryPreviewStep{
			FailureOrdinal:         ordinal,
			CycleRetryAttempt:      ordinal,
			CumulativeRetryAttempt: ordinal,
			NominalDelayMS:         bounds.NominalDelayMS,
			JitterMinDelayMS:       bounds.JitterMinDelayMS,
			JitterMaxDelayMS:       bounds.JitterMaxDelayMS,
			CycleExhausted:         cycleExhausted,
			BanTransition:          transition,
		})
		if transition != nil {
			return previewResult(steps, cycleLimit, projection, PreviewTerminationBanTransition, false)
		}
		if cycleExhausted {
			return previewResult(steps, cycleLimit, projection, PreviewTerminationCycleExhausted, false)
		}
	}
	return previewResult(steps, cycleLimit, projection, PreviewTerminationFiveStepLimit, true)
}

func previewResult(steps []RetryPreviewStep, cycleLimit int, projection RetryPreviewBanProjection, termination string, hasMore bool) RetryPreviewResult {
	return RetryPreviewResult{
		Steps:                       steps,
		ShownStepCount:              len(steps),
		HasMore:                     hasMore,
		TerminationReason:           termination,
		CycleExhaustionAfterAttempt: cycleLimit,
		BanProjection:               projection,
	}
}
