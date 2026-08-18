package loadbalance

import (
	"time"
)

type RuntimeStateTransition struct {
	PreviousState         RuntimeConnectionState
	CurrentState          RuntimeConnectionState
	RecoveryEventEligible bool
}

func (s *LocalRuntimeStateStore) RecordRuntimeSuccess(profileID int, modelConfigID int, connectionID int, strategy RuntimeStrategy, responseHeadersLatencyMS int, observedAt time.Time) RuntimeStateTransition {
	if s == nil || profileID <= 0 || connectionID <= 0 {
		return RuntimeStateTransition{}
	}
	nowAt := observedAt.UTC()
	state := s.ensureConnection(profileID, modelConfigID, connectionID, nowAt)
	state.mu.Lock()
	defer state.mu.Unlock()

	previousState := state.snapshotLocked()
	latencyMS := max(responseHeadersLatencyMS, 1)
	state.state.CycleRetryAttempts = 0
	state.state.CumulativeRetryAttempts = 0
	state.state.NextRetryAt = nil
	state.state.LastRetryDelayMS = 0
	state.state.BanMode = "off"
	state.state.BannedUntilAt = nil
	state.state.LastFailureKind = nil
	state.state.LastSuccessAt = timePointer(nowAt)
	state.state.LastSuccessResponseHeadersLatencyMS = intPointer(latencyMS)
	state.updatedAt = nowAt
	return RuntimeStateTransition{
		PreviousState:         previousState,
		CurrentState:          state.snapshotLocked(),
		RecoveryEventEligible: shouldRecordRuntimeRecoveryEvent(previousState),
	}
}

func (s *LocalRuntimeStateStore) RecordRuntimeFailoverHTTPFailure(profileID int, modelConfigID int, connectionID int, strategy RuntimeStrategy, observedAt time.Time) RuntimeStateTransition {
	return s.recordRuntimeFailure(profileID, modelConfigID, connectionID, strategy, observedAt, runtimeFailureKindTransientHTTP)
}

func (s *LocalRuntimeStateStore) RecordRuntimeTransportFailure(profileID int, modelConfigID int, connectionID int, strategy RuntimeStrategy, observedAt time.Time) RuntimeStateTransition {
	return s.recordRuntimeFailure(profileID, modelConfigID, connectionID, strategy, observedAt, runtimeFailureKindConnectError)
}

func (s *LocalRuntimeStateStore) recordRuntimeFailure(profileID int, modelConfigID int, connectionID int, strategy RuntimeStrategy, observedAt time.Time, failureKind string) RuntimeStateTransition {
	if s == nil || profileID <= 0 || connectionID <= 0 {
		return RuntimeStateTransition{}
	}
	nowAt := observedAt.UTC()
	state := s.ensureConnection(profileID, modelConfigID, connectionID, nowAt)
	state.mu.Lock()
	defer state.mu.Unlock()

	state.refreshAvailabilityLocked(nowAt)
	previousState := state.snapshotLocked()
	policy := strategy.FeedbackPolicy()
	cycleRetryAttempts := state.state.CycleRetryAttempts + 1
	cumulativeRetryAttempts := state.state.CumulativeRetryAttempts + 1
	delayMS := retryDelayMilliseconds(policy, cycleRetryAttempts)
	var nextRetryAt *time.Time
	if delayMS > 0 {
		nextRetry := nowAt.Add(time.Duration(delayMS) * time.Millisecond)
		nextRetryAt = &nextRetry
	}

	banMode := "off"
	var bannedUntilAt *time.Time
	if activeBanMode, activeBannedUntilAt := activeBanState(state.state, nowAt); activeBanMode != "" {
		banMode = activeBanMode
		bannedUntilAt = cloneTimePointer(activeBannedUntilAt)
	}
	if cumulativeRetryAttemptsReachedBanThreshold(policy, cumulativeRetryAttempts) {
		banMode = normalizeBanMode(policy.BanMode)
		nextRetryAt = nil
		switch banMode {
		case "temporary":
			bannedUntil := nowAt.Add(time.Duration(maxInt(policy.BanDurationSeconds, 0)) * time.Second)
			bannedUntilAt = &bannedUntil
		case "until_reset":
			bannedUntilAt = nil
		default:
			banMode = "off"
			bannedUntilAt = nil
		}
	}

	state.state.CycleRetryAttempts = cycleRetryAttempts
	state.state.CumulativeRetryAttempts = cumulativeRetryAttempts
	state.state.NextRetryAt = cloneTimePointer(nextRetryAt)
	state.state.LastRetryDelayMS = delayMS
	state.state.BanMode = banMode
	state.state.BannedUntilAt = cloneTimePointer(bannedUntilAt)
	state.state.LastFailureKind = stringPointer(failureKind)
	state.updatedAt = nowAt
	return RuntimeStateTransition{PreviousState: previousState, CurrentState: state.snapshotLocked()}
}

func cumulativeRetryAttemptsReachedBanThreshold(policy runtimeFeedbackPolicy, cumulativeRetryAttempts int) bool {
	return normalizeBanMode(policy.BanMode) != "off" && cumulativeRetryAttempts >= policy.BanCumulativeRetryAttemptThreshold
}

func activeBanState(state RuntimeConnectionState, referenceNow time.Time) (string, *time.Time) {
	nowAt := referenceNow.UTC()
	banMode := normalizeBanMode(state.BanMode)
	switch banMode {
	case "until_reset":
		return banMode, nil
	case "temporary":
		if state.BannedUntilAt != nil && state.BannedUntilAt.After(nowAt) {
			return banMode, cloneTimePointer(state.BannedUntilAt)
		}
	}
	return "", nil
}
