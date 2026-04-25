package loadbalance

import (
	"database/sql"
	"math"
	"strings"
	"time"
)

const (
	runtimeFailureKindTransientHTTP = "transient_http"
	runtimeFailureKindConnectError  = "connect_error"
)

type RuntimeConnectionState struct {
	ConnectionID        int
	CircuitState        string
	BanMode             string
	BannedUntilAt       *time.Time
	OpenUntilAt         *time.Time
	ProbeAvailableAt    *time.Time
	WindowStartedAt     *time.Time
	WindowRequestCount  int
	InFlightNonStream   int
	InFlightStream      int
	ConsecutiveFailures int
	LastFailureKind     *string
	LastCooldownSeconds float64
	MaxCooldownStrikes  int
	ProbeEligibleLogged bool
	LiveP95LatencyMS    *int
	LastLiveFailureKind *string
	LastLiveFailureAt   *time.Time
	LastLiveSuccessAt   *time.Time
}

type RuntimeConnectionAdmission struct {
	QPSLimit             *int
	MaxInFlightNonStream *int
	MaxInFlightStream    *int
}

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

func RequiresHalfOpenProbeLease(state RuntimeConnectionState, referenceNow time.Time) bool {
	nowAt := referenceNow.UTC()
	if state.ProbeAvailableAt == nil || state.ProbeAvailableAt.After(nowAt) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(state.CircuitState)) {
	case "open", "half_open":
		return true
	default:
		return false
	}
}

func (state RuntimeConnectionState) IsEligible(referenceNow time.Time) bool {
	nowAt := referenceNow.UTC()
	status := deriveCurrentState(state.BanMode, state.BannedUntilAt, state.OpenUntilAt, nowAt)
	if status == "banned" || status == "blocked" {
		return false
	}

	if state.ProbeAvailableAt == nil || !state.ProbeAvailableAt.After(nowAt) {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(state.CircuitState)) {
	case "open", "half_open":
		return false
	default:
		return true
	}
}

func feedbackOpenSeconds(policy runtimeFeedbackPolicy, strikeCount int) float64 {
	if !policy.Enabled || policy.BaseOpenSeconds <= 0 {
		return 0
	}
	multiplier := math.Pow(maxFloat(policy.BackoffMultiplier, 1), float64(maxInt(strikeCount, 1)-1))
	cooldownSeconds := float64(policy.BaseOpenSeconds) * multiplier
	if policy.MaxOpenSeconds > 0 && cooldownSeconds > float64(policy.MaxOpenSeconds) {
		cooldownSeconds = float64(policy.MaxOpenSeconds)
	}
	if cooldownSeconds < 0 {
		return 0
	}
	return cooldownSeconds
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
