package loadbalance

import "fmt"

// CloneForObservation copies existing state without registration, expiry,
// cursor claims, or feedback. Rows and cursors are sampled independently;
// callers must describe this as a non-atomic sample, never live admission.
// The returned private store may be used by the real planner without writing
// any shared runtime state. A size failure returns no misleading partial clone.
func (s *LocalRuntimeStateStore) CloneForObservation(profileID, limit int) (*LocalRuntimeStateStore, error) {
	out := NewLocalRuntimeStateStore()
	if s == nil {
		return out, nil
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return out, nil
	}
	profile.mu.RLock()
	defer profile.mu.RUnlock()
	if len(profile.connections)+len(profile.targetRoundRobin)+len(profile.roundRobin) > limit {
		return nil, fmt.Errorf("runtime observation exceeds %d state entries", limit)
	}
	target := out.profileState(profileID)
	for id, state := range profile.connections {
		state.mu.Lock()
		copied := &localRuntimeConnectionState{modelConfigID: state.modelConfigID, createdAt: state.createdAt, updatedAt: state.updatedAt, state: cloneRuntimeConnectionState(state.state)}
		state.mu.Unlock()
		target.connections[id] = copied
	}
	for key, cursor := range profile.roundRobin {
		copied := &localRoundRobinCursor{}
		copied.next.Store(cursor.next.Load())
		target.roundRobin[key] = copied
	}
	for key, cursor := range profile.targetRoundRobin {
		copied := &localRoundRobinCursor{}
		copied.next.Store(cursor.next.Load())
		target.targetRoundRobin[key] = copied
	}
	return out, nil
}
