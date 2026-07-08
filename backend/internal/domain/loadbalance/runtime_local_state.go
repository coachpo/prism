package loadbalance

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
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

type RuntimeStateTransition struct {
	PreviousState         RuntimeConnectionState
	CurrentState          RuntimeConnectionState
	RecoveryEventEligible bool
}

type RuntimeRoundRobinCursorSource interface {
	ClaimRoundRobinCursor(profileID int, modelConfigID int, connectionCount int) int
}

type RuntimeTargetRoundRobinCursorSource interface {
	ClaimRoundRobinTargetCursor(profileID int, sourceModelConfigID int, strategyID int, targetSetHash string, targetCount int) int
}

type LocalRuntimeStateStore struct {
	mu       sync.RWMutex
	profiles map[int]*localRuntimeProfileState
}

type localRuntimeProfileState struct {
	mu               sync.RWMutex
	connections      map[int]*localRuntimeConnectionState
	connectionModels map[int]int
	modelConnections map[int]map[int]struct{}
	roundRobin       map[int]*localRoundRobinCursor
	targetRoundRobin map[localTargetRoundRobinCursorKey]*localRoundRobinCursor
}

type localRoundRobinCursor struct {
	next atomic.Uint64
}

type localTargetRoundRobinCursorKey struct {
	sourceModelConfigID int
	strategyID          int
	targetSetHash       string
}

type localRuntimeConnectionState struct {
	mu            sync.Mutex
	modelConfigID int
	createdAt     time.Time
	updatedAt     time.Time
	state         RuntimeConnectionState
}

func NewLocalRuntimeStateStore() *LocalRuntimeStateStore {
	return &LocalRuntimeStateStore{profiles: map[int]*localRuntimeProfileState{}}
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
		state.refreshAvailabilityLocked(nowAt)
		item := currentStateItemFromLocalStateLocked(state, nowAt)
		state.mu.Unlock()
		if item.State == "banned" {
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

func (s *LocalRuntimeStateStore) RecordRuntimeSuccess(profileID int, modelConfigID int, connectionID int, strategy RuntimeStrategy, responseTimeMS int, observedAt time.Time) RuntimeStateTransition {
	if s == nil || profileID <= 0 || connectionID <= 0 {
		return RuntimeStateTransition{}
	}
	nowAt := observedAt.UTC()
	state := s.ensureConnection(profileID, modelConfigID, connectionID, nowAt)
	state.mu.Lock()
	defer state.mu.Unlock()

	previousState := state.snapshotLocked()
	latencyMS := max(responseTimeMS, 1)
	state.state.CycleRetryAttempts = 0
	state.state.CumulativeRetryAttempts = 0
	state.state.NextRetryAt = nil
	state.state.LastRetryDelayMS = 0
	state.state.BanMode = "off"
	state.state.BannedUntilAt = nil
	state.state.LastFailureKind = nil
	state.state.LastSuccessAt = timePointer(nowAt)
	state.state.LiveP95LatencyMS = intPointer(latencyMS)
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

func (s *LocalRuntimeStateStore) ClaimRoundRobinCursor(profileID int, modelConfigID int, connectionCount int) int {
	if s == nil || profileID <= 0 || modelConfigID <= 0 || connectionCount <= 0 {
		return 0
	}
	profile := s.profileState(profileID)
	profile.mu.Lock()
	cursor := profile.roundRobin[modelConfigID]
	if cursor == nil {
		cursor = &localRoundRobinCursor{}
		profile.roundRobin[modelConfigID] = cursor
	}
	profile.mu.Unlock()
	return int((cursor.next.Add(1) - 1) % uint64(connectionCount))
}

func (s *LocalRuntimeStateStore) ClaimRoundRobinTargetCursor(profileID int, sourceModelConfigID int, strategyID int, targetSetHash string, targetCount int) int {
	if s == nil || profileID <= 0 || sourceModelConfigID <= 0 || targetCount <= 0 || strings.TrimSpace(targetSetHash) == "" {
		return 0
	}
	profile := s.profileState(profileID)
	key := localTargetRoundRobinCursorKey{sourceModelConfigID: sourceModelConfigID, strategyID: strategyID, targetSetHash: strings.TrimSpace(targetSetHash)}
	profile.mu.Lock()
	cursor := profile.targetRoundRobin[key]
	if cursor == nil {
		cursor = &localRoundRobinCursor{}
		profile.targetRoundRobin[key] = cursor
	}
	profile.mu.Unlock()
	return int((cursor.next.Add(1) - 1) % uint64(targetCount))
}

func (s *LocalRuntimeStateStore) PeekRoundRobinCursor(profileID int, modelConfigID int, connectionCount int) int {
	if s == nil || profileID <= 0 || modelConfigID <= 0 || connectionCount <= 0 {
		return 0
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return 0
	}
	profile.mu.RLock()
	cursor := profile.roundRobin[modelConfigID]
	profile.mu.RUnlock()
	if cursor == nil {
		return 0
	}
	return int(cursor.next.Load() % uint64(connectionCount))
}

func (s *LocalRuntimeStateStore) ResetConnection(profileID int, connectionID int) bool {
	if s == nil || profileID <= 0 || connectionID <= 0 {
		return false
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return false
	}
	profile.mu.Lock()
	defer profile.mu.Unlock()
	if profile.connections[connectionID] == nil {
		return false
	}
	delete(profile.connections, connectionID)
	return true
}

func (s *LocalRuntimeStateStore) ResetRoundRobinCursor(profileID int, modelConfigID int) bool {
	if s == nil || profileID <= 0 || modelConfigID <= 0 {
		return false
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return false
	}
	profile.mu.Lock()
	defer profile.mu.Unlock()
	if profile.roundRobin[modelConfigID] == nil {
		return false
	}
	delete(profile.roundRobin, modelConfigID)
	return true
}

func (s *LocalRuntimeStateStore) ResetProfile(profileID int) {
	if s == nil || profileID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.profiles, profileID)
}

func (s *LocalRuntimeStateStore) ResetAll() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles = map[int]*localRuntimeProfileState{}
}

func (s *LocalRuntimeStateStore) SeedConnectionState(profileID int, modelConfigID int, connectionID int, seeded RuntimeConnectionState, createdAt time.Time, updatedAt time.Time) {
	if s == nil || profileID <= 0 || connectionID <= 0 {
		return
	}
	state := s.ensureConnection(profileID, modelConfigID, connectionID, updatedAt)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.state = normalizeSeededRuntimeConnectionState(seeded, connectionID)
	state.modelConfigID = modelConfigID
	state.createdAt = normalizeRuntimeTimestamp(createdAt, updatedAt)
	state.updatedAt = normalizeRuntimeTimestamp(updatedAt, state.createdAt)
}

func (s *LocalRuntimeStateStore) profileState(profileID int) *localRuntimeProfileState {
	s.mu.RLock()
	profile := s.profiles[profileID]
	s.mu.RUnlock()
	if profile != nil {
		return profile
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.profiles[profileID]; existing != nil {
		return existing
	}
	profile = &localRuntimeProfileState{
		connections:      map[int]*localRuntimeConnectionState{},
		connectionModels: map[int]int{},
		modelConnections: map[int]map[int]struct{}{},
		roundRobin:       map[int]*localRoundRobinCursor{},
		targetRoundRobin: map[localTargetRoundRobinCursorKey]*localRoundRobinCursor{},
	}
	s.profiles[profileID] = profile
	return profile
}

func (s *LocalRuntimeStateStore) lookupProfile(profileID int) (*localRuntimeProfileState, bool) {
	if s == nil || profileID <= 0 {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile := s.profiles[profileID]
	return profile, profile != nil
}

func (s *LocalRuntimeStateStore) ensureConnection(profileID int, modelConfigID int, connectionID int, observedAt time.Time) *localRuntimeConnectionState {
	profile := s.profileState(profileID)
	profile.mu.Lock()
	defer profile.mu.Unlock()
	profile.registerConnectionLocked(modelConfigID, connectionID)
	if state := profile.connections[connectionID]; state != nil {
		if modelConfigID > 0 {
			state.modelConfigID = modelConfigID
		}
		return state
	}
	nowAt := normalizeRuntimeTimestamp(observedAt, time.Time{})
	state := &localRuntimeConnectionState{
		modelConfigID: modelConfigID,
		createdAt:     nowAt,
		updatedAt:     nowAt,
		state: RuntimeConnectionState{
			ConnectionID: connectionID,
			BanMode:      "off",
		},
	}
	profile.connections[connectionID] = state
	return state
}

func (profile *localRuntimeProfileState) registerConnectionLocked(modelConfigID int, connectionID int) {
	if connectionID <= 0 || modelConfigID <= 0 {
		return
	}
	profile.connectionModels[connectionID] = modelConfigID
	connections := profile.modelConnections[modelConfigID]
	if connections == nil {
		connections = map[int]struct{}{}
		profile.modelConnections[modelConfigID] = connections
	}
	connections[connectionID] = struct{}{}
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

func (state *localRuntimeConnectionState) snapshotLocked() RuntimeConnectionState {
	return cloneRuntimeConnectionState(state.state)
}

func currentStateItemFromLocalStateLocked(state *localRuntimeConnectionState, nowAt time.Time) CurrentStateItem {
	snapshot := state.snapshotLocked()
	return CurrentStateItem{
		ConnectionID:            snapshot.ConnectionID,
		WindowStartedAt:         cloneTimePointer(snapshot.WindowStartedAt),
		WindowRequestCount:      snapshot.WindowRequestCount,
		InFlightNonStream:       snapshot.InFlightNonStream,
		InFlightStream:          snapshot.InFlightStream,
		CycleRetryAttempts:      snapshot.CycleRetryAttempts,
		CumulativeRetryAttempts: snapshot.CumulativeRetryAttempts,
		NextRetryAt:             cloneTimePointer(snapshot.NextRetryAt),
		LastRetryDelayMS:        snapshot.LastRetryDelayMS,
		BanMode:                 snapshot.BanMode,
		BannedUntilAt:           cloneTimePointer(snapshot.BannedUntilAt),
		LastFailureKind:         cloneStringPointer(snapshot.LastFailureKind),
		LastSuccessAt:           cloneTimePointer(snapshot.LastSuccessAt),
		LiveP95LatencyMS:        cloneIntPointer(snapshot.LiveP95LatencyMS),
		State:                   deriveCurrentState(snapshot.BanMode, snapshot.BannedUntilAt, snapshot.NextRetryAt, nowAt),
		CreatedAt:               state.createdAt.UTC(),
		UpdatedAt:               state.updatedAt.UTC(),
	}
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

func normalizeSeededRuntimeConnectionState(seeded RuntimeConnectionState, connectionID int) RuntimeConnectionState {
	state := cloneRuntimeConnectionState(seeded)
	state.ConnectionID = connectionID
	if strings.TrimSpace(state.BanMode) == "" {
		state.BanMode = "off"
	}
	return state
}

func normalizeRuntimeTimestamp(observedAt time.Time, fallback time.Time) time.Time {
	if !observedAt.IsZero() {
		return observedAt.UTC()
	}
	if !fallback.IsZero() {
		return fallback.UTC()
	}
	return time.Now().UTC()
}

func cloneRuntimeConnectionState(source RuntimeConnectionState) RuntimeConnectionState {
	cloned := source
	cloned.WindowStartedAt = cloneTimePointer(source.WindowStartedAt)
	cloned.NextRetryAt = cloneTimePointer(source.NextRetryAt)
	cloned.BannedUntilAt = cloneTimePointer(source.BannedUntilAt)
	cloned.LastFailureKind = cloneStringPointer(source.LastFailureKind)
	cloned.LastSuccessAt = cloneTimePointer(source.LastSuccessAt)
	cloned.LiveP95LatencyMS = cloneIntPointer(source.LiveP95LatencyMS)
	return cloned
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	resolved := value.UTC()
	return &resolved
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	resolved := *value
	return &resolved
}

func timePointer(value time.Time) *time.Time {
	resolved := value.UTC()
	return &resolved
}

func stringPointer(value string) *string {
	resolved := strings.TrimSpace(value)
	return &resolved
}
