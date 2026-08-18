package stats

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

type DashboardAggregateInvalidation struct {
	ProfileID int
	All       bool
}

type DashboardAggregateInvalidationListener func(DashboardAggregateInvalidation)

type DashboardAggregateStore struct {
	mu        sync.RWMutex
	snapshots map[int]DashboardAggregateSnapshot
	listeners []DashboardAggregateInvalidationListener
}

func NewDashboardAggregateStore() *DashboardAggregateStore {
	return &DashboardAggregateStore{snapshots: map[int]DashboardAggregateSnapshot{}}
}

func (s *DashboardAggregateStore) LoadProfile(profileID int) (DashboardAggregateSnapshot, bool) {
	if s == nil || profileID <= 0 {
		return DashboardAggregateSnapshot{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot, ok := s.snapshots[profileID]
	return snapshot, ok
}

func (s *DashboardAggregateStore) LoadFreshProfile(profileID int, isFresh func(DashboardAggregateSnapshot) bool) (DashboardAggregateSnapshot, bool) {
	snapshot, ok := s.LoadProfile(profileID)
	if !ok {
		return DashboardAggregateSnapshot{}, false
	}
	if isFresh != nil && !isFresh(snapshot) {
		return DashboardAggregateSnapshot{}, false
	}
	return snapshot, true
}

func (s *DashboardAggregateStore) StoreProfile(snapshot DashboardAggregateSnapshot) {
	if s == nil || snapshot.ProfileID <= 0 {
		return
	}
	snapshot = ensureDashboardAggregateSnapshotRevision(snapshot)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snapshot.ProfileID] = snapshot
}

func ensureDashboardAggregateSnapshotRevision(snapshot DashboardAggregateSnapshot) DashboardAggregateSnapshot {
	if strings.TrimSpace(snapshot.SnapshotRevision) == "" {
		snapshot.SnapshotRevision = newDashboardSnapshotRevision(snapshot.GeneratedAt)
	}
	return snapshot
}

const dashboardRevisionAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var dashboardRevisionGenerator = struct {
	sync.Mutex
	lastTimestampMS uint64
	lastEntropy     [10]byte
}{}

func newDashboardSnapshotRevision(referenceNow time.Time) string {
	referenceNow = referenceNow.UTC()
	if referenceNow.IsZero() {
		referenceNow = time.Now().UTC()
	}
	timestampMS := uint64(referenceNow.UnixMilli())
	entropy := randomDashboardRevisionEntropy()

	dashboardRevisionGenerator.Lock()
	defer dashboardRevisionGenerator.Unlock()
	if timestampMS <= dashboardRevisionGenerator.lastTimestampMS {
		timestampMS = dashboardRevisionGenerator.lastTimestampMS
		entropy = dashboardRevisionGenerator.lastEntropy
		if !incrementDashboardRevisionEntropy(&entropy) {
			timestampMS++
			entropy = randomDashboardRevisionEntropy()
		}
	}
	dashboardRevisionGenerator.lastTimestampMS = timestampMS
	dashboardRevisionGenerator.lastEntropy = entropy
	return encodeDashboardRevisionULID(timestampMS, entropy)
}

func randomDashboardRevisionEntropy() [10]byte {
	var entropy [10]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic(fmt.Sprintf("generate dashboard snapshot revision entropy: %v", err))
	}
	return entropy
}

func incrementDashboardRevisionEntropy(entropy *[10]byte) bool {
	for index := len(entropy) - 1; index >= 0; index-- {
		entropy[index]++
		if entropy[index] != 0 {
			return true
		}
	}
	return false
}

func encodeDashboardRevisionULID(timestampMS uint64, entropy [10]byte) string {
	var payload [16]byte
	payload[0] = byte(timestampMS >> 40)
	payload[1] = byte(timestampMS >> 32)
	payload[2] = byte(timestampMS >> 24)
	payload[3] = byte(timestampMS >> 16)
	payload[4] = byte(timestampMS >> 8)
	payload[5] = byte(timestampMS)
	copy(payload[6:], entropy[:])

	value := new(big.Int).SetBytes(payload[:])
	base := big.NewInt(32)
	mod := new(big.Int)
	var encoded [26]byte
	for index := len(encoded) - 1; index >= 0; index-- {
		value.DivMod(value, base, mod)
		encoded[index] = dashboardRevisionAlphabet[mod.Int64()]
	}
	return string(encoded[:])
}

func (s *DashboardAggregateStore) InvalidateProfile(profileID int) {
	s.invalidateProfile(profileID, true)
}

func (s *DashboardAggregateStore) InvalidateAll() {
	s.invalidateAll(true)
}

func (s *DashboardAggregateStore) invalidateProfile(profileID int, notify bool) {
	if s == nil || profileID <= 0 {
		return
	}
	s.mu.Lock()
	delete(s.snapshots, profileID)
	listeners := s.invalidationListenersLocked(notify)
	s.mu.Unlock()
	notifyDashboardAggregateInvalidation(listeners, DashboardAggregateInvalidation{ProfileID: profileID})
}

func (s *DashboardAggregateStore) invalidateAll(notify bool) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.snapshots = map[int]DashboardAggregateSnapshot{}
	listeners := s.invalidationListenersLocked(notify)
	s.mu.Unlock()
	notifyDashboardAggregateInvalidation(listeners, DashboardAggregateInvalidation{All: true})
}

func (s *DashboardAggregateStore) invalidationListenersLocked(notify bool) []DashboardAggregateInvalidationListener {
	if !notify || len(s.listeners) == 0 {
		return nil
	}
	return append([]DashboardAggregateInvalidationListener{}, s.listeners...)
}

func notifyDashboardAggregateInvalidation(listeners []DashboardAggregateInvalidationListener, invalidation DashboardAggregateInvalidation) {
	for _, listener := range listeners {
		listener(invalidation)
	}
}
