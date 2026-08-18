package loadbalance

import (
	"strings"
	"sync"
	"time"
)

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

// ResetConnection clears only retry/ban cooldown fields on the connection's
// process-local state. It preserves admission windows, in-flight counts,
// last-success facts, response-header latency and round-robin cursors.
func (s *LocalRuntimeStateStore) ResetConnection(profileID int, connectionID int) (*CurrentStateItem, bool) {
	cleared, item := s.ResetConnectionCooldown(profileID, connectionID, time.Now().UTC())
	return item, cleared
}

// ResetConnectionCooldown applies the narrow cooldown reset used by both the
// management endpoint and the Observe current-state action. The bool reports
// whether a cooldown was cleared; the returned item is the post-reset state
// when the process has an observed row.
func (s *LocalRuntimeStateStore) ResetConnectionCooldown(profileID int, connectionID int, referenceNow time.Time) (bool, *CurrentStateItem) {
	if s == nil || profileID <= 0 || connectionID <= 0 {
		return false, nil
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return false, nil
	}
	profile.mu.RLock()
	state := profile.connections[connectionID]
	profile.mu.RUnlock()
	if state == nil {
		return false, nil
	}
	nowAt := referenceNow.UTC()
	state.mu.Lock()
	defer state.mu.Unlock()
	cleared := state.clearCooldownLocked()
	if cleared {
		state.updatedAt = nowAt
	}
	state.refreshAvailabilityLocked(nowAt)
	item := currentStateItemFromLocalStateLocked(state, nowAt)
	return cleared, &item
}

func (state *localRuntimeConnectionState) clearCooldownLocked() bool {
	cleared := false
	if state.state.CycleRetryAttempts != 0 {
		state.state.CycleRetryAttempts = 0
		cleared = true
	}
	if state.state.CumulativeRetryAttempts != 0 {
		state.state.CumulativeRetryAttempts = 0
		cleared = true
	}
	if state.state.NextRetryAt != nil {
		state.state.NextRetryAt = nil
		cleared = true
	}
	if state.state.LastRetryDelayMS != 0 {
		state.state.LastRetryDelayMS = 0
		cleared = true
	}
	if !strings.EqualFold(strings.TrimSpace(state.state.BanMode), "off") {
		state.state.BanMode = "off"
		cleared = true
	}
	if state.state.BannedUntilAt != nil {
		state.state.BannedUntilAt = nil
		cleared = true
	}
	if state.state.LastFailureKind != nil {
		state.state.LastFailureKind = nil
		cleared = true
	}
	return cleared
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

func (state *localRuntimeConnectionState) snapshotLocked() RuntimeConnectionState {
	return cloneRuntimeConnectionState(state.state)
}

func currentStateItemFromLocalStateLocked(state *localRuntimeConnectionState, nowAt time.Time) CurrentStateItem {
	snapshot := state.snapshotLocked()
	return CurrentStateItem{
		ConnectionID:                        snapshot.ConnectionID,
		WindowStartedAt:                     cloneTimePointer(snapshot.WindowStartedAt),
		WindowRequestCount:                  snapshot.WindowRequestCount,
		InFlightNonStream:                   snapshot.InFlightNonStream,
		InFlightStream:                      snapshot.InFlightStream,
		CycleRetryAttempts:                  snapshot.CycleRetryAttempts,
		CumulativeRetryAttempts:             snapshot.CumulativeRetryAttempts,
		NextRetryAt:                         cloneTimePointer(snapshot.NextRetryAt),
		LastRetryDelayMS:                    snapshot.LastRetryDelayMS,
		BanMode:                             snapshot.BanMode,
		BannedUntilAt:                       cloneTimePointer(snapshot.BannedUntilAt),
		LastFailureKind:                     cloneStringPointer(snapshot.LastFailureKind),
		LastSuccessAt:                       cloneTimePointer(snapshot.LastSuccessAt),
		LastSuccessResponseHeadersLatencyMS: cloneIntPointer(snapshot.LastSuccessResponseHeadersLatencyMS),
		State:                               deriveCurrentState(snapshot.BanMode, snapshot.BannedUntilAt, snapshot.NextRetryAt, nowAt),
		CreatedAt:                           state.createdAt.UTC(),
		UpdatedAt:                           state.updatedAt.UTC(),
	}
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
	cloned.LastSuccessResponseHeadersLatencyMS = cloneIntPointer(source.LastSuccessResponseHeadersLatencyMS)
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
