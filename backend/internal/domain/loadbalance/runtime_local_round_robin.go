package loadbalance

import (
	"strings"
	"sync/atomic"
)

type RuntimeRoundRobinCursorSource interface {
	ClaimRoundRobinCursor(profileID int, modelConfigID int, connectionCount int) int
}

type RuntimeTargetRoundRobinCursorSource interface {
	ClaimRoundRobinTargetCursor(profileID int, sourceModelConfigID int, strategyID int, targetSetHash string, targetCount int) int
}

type localRoundRobinCursor struct {
	next atomic.Uint64
}

type localTargetRoundRobinCursorKey struct {
	sourceModelConfigID int
	strategyID          int
	targetSetHash       string
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

func (s *LocalRuntimeStateStore) ResetRoundRobinCursor(profileID int, modelConfigID int) bool {
	if s == nil || profileID <= 0 || modelConfigID <= 0 {
		return false
	}
	profile, ok := s.lookupProfile(profileID)
	if !ok {
		return false
	}
	profile.mu.RLock()
	cursor := profile.roundRobin[modelConfigID]
	profile.mu.RUnlock()
	if cursor == nil {
		return false
	}
	cursor.next.Store(0)
	return true
}
