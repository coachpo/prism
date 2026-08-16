package loadbalance

import (
	"crypto/rand"
	"database/sql"
	"math"
	"math/big"
	"sync"
	"time"
)

const (
	runtimeFailureKindTransientHTTP = "transient_http"
	runtimeFailureKindConnectError  = "connect_error"
)

type RuntimeConnectionState struct {
	ConnectionID            int
	WindowStartedAt         *time.Time
	WindowRequestCount      int
	InFlightNonStream       int
	InFlightStream          int
	CycleRetryAttempts      int
	CumulativeRetryAttempts int
	NextRetryAt             *time.Time
	LastRetryDelayMS        int
	BanMode                 string
	BannedUntilAt           *time.Time
	LastFailureKind         *string
	LastSuccessAt           *time.Time
	LastSuccessResponseHeadersLatencyMS *int
}

type RuntimeConnectionAdmission struct {
	QPSLimit             *int
	MaxInFlightNonStream *int
	MaxInFlightStream    *int
}

var runtimeRetryJitter = struct {
	mu     sync.RWMutex
	offset func(maxOffsetMS int) int
}{offset: randomRuntimeRetryJitterOffset}

func FilterEligibleConnectionIDs(candidates []ConnectionOrderCandidate, states map[int]RuntimeConnectionState, referenceNow time.Time) []int {
	eligible := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		state, ok := states[candidate.ID]
		if ok && !state.IsEligible(referenceNow) {
			continue
		}
		eligible = append(eligible, candidate.ID)
	}
	return eligible
}

func AdmissionRejectionReason(state RuntimeConnectionState, admission RuntimeConnectionAdmission, policy runtimeAdmissionPolicy, isStream bool, referenceNow time.Time) string {
	if policy.RespectQPSLimit && admission.QPSLimit != nil && *admission.QPSLimit > 0 && state.WindowStartedAt != nil {
		windowExpiresAt := state.WindowStartedAt.UTC().Add(time.Second)
		nowAt := referenceNow.UTC()
		if windowExpiresAt.After(nowAt) && state.WindowRequestCount >= *admission.QPSLimit {
			return "qps_limit"
		}
	}
	if !policy.RespectInFlightLimits {
		return ""
	}
	if isStream {
		if admission.MaxInFlightStream != nil && *admission.MaxInFlightStream > 0 && state.InFlightStream >= *admission.MaxInFlightStream {
			return "max_in_flight_stream"
		}
		return ""
	}
	if admission.MaxInFlightNonStream != nil && *admission.MaxInFlightNonStream > 0 && state.InFlightNonStream >= *admission.MaxInFlightNonStream {
		return "max_in_flight_non_stream"
	}
	return ""
}

func (state RuntimeConnectionState) IsEligible(referenceNow time.Time) bool {
	return deriveCurrentState(state.BanMode, state.BannedUntilAt, state.NextRetryAt, referenceNow.UTC()) == "available"
}

func retryDelayMilliseconds(policy runtimeFeedbackPolicy, cycleAttempt int) int {
	if !policy.Enabled || policy.BaseDelayMS <= 0 {
		return 0
	}
	attempt := maxInt(cycleAttempt, 1)
	multiplier := math.Pow(maxFloat(policy.BackoffMultiplier, 1), float64(attempt-1))
	delayMS := int(math.Ceil(float64(policy.BaseDelayMS) * multiplier))
	if policy.MaxDelayMS > 0 && delayMS > policy.MaxDelayMS {
		delayMS = policy.MaxDelayMS
	}
	if delayMS < 0 {
		delayMS = 0
	}
	if policy.JitterRatio <= 0 || delayMS == 0 {
		return delayMS
	}
	maxOffsetMS := int(math.Round(float64(delayMS) * policy.JitterRatio))
	if maxOffsetMS <= 0 {
		return delayMS
	}
	delayMS += runtimeRetryJitterOffset(maxOffsetMS)
	if delayMS < 0 {
		return 0
	}
	if policy.MaxDelayMS > 0 && delayMS > policy.MaxDelayMS {
		return policy.MaxDelayMS
	}
	return delayMS
}

func runtimeRetryJitterOffset(maxOffsetMS int) int {
	if maxOffsetMS <= 0 {
		return 0
	}
	runtimeRetryJitter.mu.RLock()
	hook := runtimeRetryJitter.offset
	runtimeRetryJitter.mu.RUnlock()
	if hook == nil {
		return 0
	}
	return hook(maxOffsetMS)
}

func randomRuntimeRetryJitterOffset(maxOffsetMS int) int {
	span := int64(maxOffsetMS*2 + 1)
	if span <= 1 {
		return 0
	}
	value, err := rand.Int(rand.Reader, big.NewInt(span))
	if err != nil {
		return 0
	}
	return int(value.Int64()) - maxOffsetMS
}

func setRuntimeRetryJitterOffsetForTest(hook func(maxOffsetMS int) int) func() {
	runtimeRetryJitter.mu.Lock()
	previous := runtimeRetryJitter.offset
	if hook == nil {
		runtimeRetryJitter.offset = randomRuntimeRetryJitterOffset
	} else {
		runtimeRetryJitter.offset = hook
	}
	runtimeRetryJitter.mu.Unlock()
	return func() {
		runtimeRetryJitter.mu.Lock()
		runtimeRetryJitter.offset = previous
		runtimeRetryJitter.mu.Unlock()
	}
}

func nullableInt(value sql.NullInt32) *int {
	if !value.Valid {
		return nil
	}
	resolved := int(value.Int32)
	return &resolved
}

func nullableTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UTC()
}
