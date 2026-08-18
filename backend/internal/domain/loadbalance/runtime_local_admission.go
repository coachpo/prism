package loadbalance

import (
	"sort"
	"strings"
	"time"
)

type RuntimeConnectionRef struct {
	ConnectionID  int
	ModelConfigID int
}

type RuntimeConnectionAttemptInput struct {
	ProfileID     int
	ModelConfigID int
	ConnectionID  int
	Admission     RuntimeConnectionAdmission
	Policy        runtimeAdmissionPolicy
	IsStreaming   bool
	ObservedAt    time.Time
}

type RuntimeConnectionAttemptHandle struct {
	state            *localRuntimeConnectionState
	countedNonStream bool
	countedStream    bool
}

type RuntimeConnectionAttemptDecision struct {
	Handle          RuntimeConnectionAttemptHandle
	Skipped         bool
	AdmissionReason string
	AdmissionState  *RuntimeConnectionState
	UnbannedRecord  *RuntimeConnectionState
}

func (s *LocalRuntimeStateStore) SnapshotConnectionStates(profileID int, refs []RuntimeConnectionRef) map[int]RuntimeConnectionState {
	if len(refs) == 0 || s == nil {
		return map[int]RuntimeConnectionState{}
	}
	profile := s.profileState(profileID)
	profile.mu.Lock()
	states := make([]*localRuntimeConnectionState, 0, len(refs))
	for _, ref := range refs {
		profile.registerConnectionLocked(ref.ModelConfigID, ref.ConnectionID)
		if state := profile.connections[ref.ConnectionID]; state != nil {
			states = append(states, state)
		}
	}
	profile.mu.Unlock()

	snapshots := make(map[int]RuntimeConnectionState, len(states))
	for _, state := range states {
		state.mu.Lock()
		snapshots[state.state.ConnectionID] = state.snapshotLocked()
		state.mu.Unlock()
	}
	return snapshots
}

// RuntimeConnectionObservation is one existing runtime state key with its
// creation/update timestamps.
type RuntimeConnectionObservation struct {
	State     RuntimeConnectionState
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SnapshotConnectionObservations returns observations only for EXISTING state
// keys of the given connections; it never registers or ensures unobserved
// targets (a global current-state read must not manufacture observations).
func (s *LocalRuntimeStateStore) SnapshotConnectionObservations(profileID int, refs []RuntimeConnectionRef, referenceNow time.Time) map[int]RuntimeConnectionObservation {
	observations := map[int]RuntimeConnectionObservation{}
	if s == nil || profileID <= 0 || len(refs) == 0 {
		return observations
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return observations
	}
	profile.mu.RLock()
	states := make([]*localRuntimeConnectionState, 0, len(refs))
	for _, ref := range refs {
		if state := profile.connections[ref.ConnectionID]; state != nil {
			states = append(states, state)
		}
	}
	profile.mu.RUnlock()

	nowAt := referenceNow.UTC()
	for _, state := range states {
		state.mu.Lock()
		state.refreshAvailabilityLocked(nowAt)
		snapshot := state.snapshotLocked()
		observation := RuntimeConnectionObservation{State: snapshot, CreatedAt: state.createdAt.UTC(), UpdatedAt: state.updatedAt.UTC()}
		state.mu.Unlock()
		observations[snapshot.ConnectionID] = observation
	}
	return observations
}

func (s *LocalRuntimeStateStore) SnapshotConnectionState(profileID int, connectionID int) (RuntimeConnectionState, bool) {
	if s == nil || profileID <= 0 || connectionID <= 0 {
		return RuntimeConnectionState{}, false
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return RuntimeConnectionState{}, false
	}
	profile.mu.RLock()
	state := profile.connections[connectionID]
	profile.mu.RUnlock()
	if state == nil {
		return RuntimeConnectionState{}, false
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.refreshAvailabilityLocked(time.Now().UTC())
	return state.snapshotLocked(), true
}

func (s *LocalRuntimeStateStore) SnapshotCurrentState(profileID int, modelConfigID int, orderedConnectionIDs []int, referenceNow time.Time) []CurrentStateItem {
	if s == nil || profileID <= 0 || modelConfigID <= 0 || len(orderedConnectionIDs) == 0 {
		return []CurrentStateItem{}
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return []CurrentStateItem{}
	}
	profile.mu.RLock()
	states := make([]*localRuntimeConnectionState, 0, len(orderedConnectionIDs))
	for _, connectionID := range orderedConnectionIDs {
		if state := profile.connections[connectionID]; state != nil {
			states = append(states, state)
		}
	}
	profile.mu.RUnlock()

	nowAt := referenceNow.UTC()
	items := make([]CurrentStateItem, 0, len(states))
	for _, state := range states {
		state.mu.Lock()
		state.refreshAvailabilityLocked(nowAt)
		item := currentStateItemFromLocalStateLocked(state, nowAt)
		state.mu.Unlock()
		items = append(items, item)
	}
	return items
}

func (s *LocalRuntimeStateStore) SnapshotActiveBans(profileID int, referenceNow time.Time) []CurrentStateItem {
	if s == nil || profileID <= 0 {
		return []CurrentStateItem{}
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return []CurrentStateItem{}
	}
	profile.mu.RLock()
	states := make([]*localRuntimeConnectionState, 0, len(profile.connections))
	for _, state := range profile.connections {
		if state != nil {
			states = append(states, state)
		}
	}
	profile.mu.RUnlock()

	nowAt := referenceNow.UTC()
	items := make([]CurrentStateItem, 0, len(states))
	for _, state := range states {
		state.mu.Lock()
		item := currentStateItemFromLocalStateLocked(state, nowAt)
		state.mu.Unlock()
		if (item.BannedUntilAt != nil && item.BannedUntilAt.After(nowAt)) || strings.EqualFold(strings.TrimSpace(item.BanMode), "until_reset") {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(left, right int) bool {
		return items[left].ConnectionID < items[right].ConnectionID
	})
	return items
}

func (s *LocalRuntimeStateStore) TryBeginConnectionAttempt(input RuntimeConnectionAttemptInput) RuntimeConnectionAttemptDecision {
	if s == nil || input.ProfileID <= 0 || input.ConnectionID <= 0 {
		return RuntimeConnectionAttemptDecision{}
	}
	nowAt := input.ObservedAt.UTC()
	state := s.ensureConnection(input.ProfileID, input.ModelConfigID, input.ConnectionID, nowAt)
	state.mu.Lock()
	defer state.mu.Unlock()

	decision := RuntimeConnectionAttemptDecision{}
	if state.refreshAvailabilityLocked(nowAt) {
		record := state.snapshotLocked()
		if record.BanMode == "off" && record.BannedUntilAt == nil {
			decision.UnbannedRecord = &record
		}
	}
	snapshot := state.snapshotLocked()
	if !snapshot.IsEligible(nowAt) {
		return RuntimeConnectionAttemptDecision{Skipped: true}
	}
	if reason := state.admissionRejectionReasonLocked(input.Admission, input.Policy, input.IsStreaming, nowAt); reason != "" {
		admissionState := state.snapshotLocked()
		decision.AdmissionReason = reason
		decision.AdmissionState = &admissionState
		return decision
	}

	handle := RuntimeConnectionAttemptHandle{state: state}
	if input.Policy.RespectQPSLimit && input.Admission.QPSLimit != nil && *input.Admission.QPSLimit > 0 {
		state.advanceQPSWindowLocked(nowAt)
		state.state.WindowRequestCount++
	}
	if input.Policy.RespectInFlightLimits {
		switch {
		case input.IsStreaming && input.Admission.MaxInFlightStream != nil && *input.Admission.MaxInFlightStream > 0:
			state.state.InFlightStream++
			handle.countedStream = true
		case !input.IsStreaming && input.Admission.MaxInFlightNonStream != nil && *input.Admission.MaxInFlightNonStream > 0:
			state.state.InFlightNonStream++
			handle.countedNonStream = true
		}
	}
	state.updatedAt = nowAt
	decision.Handle = handle
	return decision
}

func (s *LocalRuntimeStateStore) FinishConnectionAttempt(handle RuntimeConnectionAttemptHandle, releasedAt time.Time) {
	if handle.state == nil {
		return
	}
	nowAt := releasedAt.UTC()
	handle.state.mu.Lock()
	defer handle.state.mu.Unlock()
	if handle.countedNonStream && handle.state.state.InFlightNonStream > 0 {
		handle.state.state.InFlightNonStream--
	}
	if handle.countedStream && handle.state.state.InFlightStream > 0 {
		handle.state.state.InFlightStream--
	}
	handle.state.updatedAt = nowAt
}

func (state *localRuntimeConnectionState) admissionRejectionReasonLocked(admission RuntimeConnectionAdmission, policy runtimeAdmissionPolicy, isStreaming bool, referenceNow time.Time) string {
	nowAt := referenceNow.UTC()
	if policy.RespectQPSLimit && admission.QPSLimit != nil && *admission.QPSLimit > 0 && state.state.WindowStartedAt != nil {
		windowStartedAt := state.state.WindowStartedAt.UTC()
		if windowStartedAt.After(nowAt) || nowAt.Sub(windowStartedAt) >= time.Second {
			state.state.WindowStartedAt = nil
			state.state.WindowRequestCount = 0
		} else if state.state.WindowRequestCount >= *admission.QPSLimit {
			return "qps_limit"
		}
	}
	if !policy.RespectInFlightLimits {
		return ""
	}
	if isStreaming {
		if admission.MaxInFlightStream != nil && *admission.MaxInFlightStream > 0 && state.state.InFlightStream >= *admission.MaxInFlightStream {
			return "max_in_flight_stream"
		}
		return ""
	}
	if admission.MaxInFlightNonStream != nil && *admission.MaxInFlightNonStream > 0 && state.state.InFlightNonStream >= *admission.MaxInFlightNonStream {
		return "max_in_flight_non_stream"
	}
	return ""
}

func (state *localRuntimeConnectionState) advanceQPSWindowLocked(referenceNow time.Time) {
	nowAt := referenceNow.UTC()
	if state.state.WindowStartedAt == nil {
		state.state.WindowStartedAt = timePointer(nowAt)
		state.state.WindowRequestCount = 0
		return
	}
	windowStartedAt := state.state.WindowStartedAt.UTC()
	if windowStartedAt.After(nowAt) || nowAt.Sub(windowStartedAt) >= time.Second {
		state.state.WindowStartedAt = timePointer(nowAt)
		state.state.WindowRequestCount = 0
	}
}

func (state *localRuntimeConnectionState) refreshAvailabilityLocked(referenceNow time.Time) bool {
	nowAt := referenceNow.UTC()
	changed := false
	if strings.EqualFold(strings.TrimSpace(state.state.BanMode), "temporary") {
		if state.state.BannedUntilAt == nil || !state.state.BannedUntilAt.After(nowAt) {
			state.state.BanMode = "off"
			state.state.BannedUntilAt = nil
			changed = true
		}
	}
	if state.state.NextRetryAt != nil && !state.state.NextRetryAt.After(nowAt) {
		state.state.CycleRetryAttempts = 0
		state.state.NextRetryAt = nil
		changed = true
	}
	if changed {
		state.updatedAt = nowAt
	}
	return changed
}
