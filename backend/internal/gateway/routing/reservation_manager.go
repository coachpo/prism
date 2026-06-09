package routing

import (
	"context"
	"strings"
	"sync"
	"time"

	gatewaycore "github.com/coachpo/prism/backend/internal/gateway/core"
)

type InMemoryReservationManager struct {
	mu     sync.Mutex
	states map[string]*ReservationState
	Now    func() time.Time
}

type ReservationState struct {
	QPSWindowStartedAt *time.Time
	QPSCount           int
	RPMWindowStartedAt *time.Time
	RPMCount           int
	TPMWindowStartedAt *time.Time
	TPMCount           int
	IPMWindowStartedAt *time.Time
	IPMCount           int
	InFlight           int
}

type memoryReservation struct {
	manager    *InMemoryReservationManager
	upstreamID string
	released   bool
}

func (manager *InMemoryReservationManager) Reserve(ctx context.Context, candidate Candidate, request ReservationRequest) (ReservationResult, error) {
	if err := ctx.Err(); err != nil {
		return ReservationResult{}, err
	}
	request, err := NormalizeReservationRequest(request)
	if err != nil {
		return ReservationResult{}, err
	}
	upstreamID := strings.TrimSpace(candidate.UpstreamID)
	if upstreamID == "" {
		return ReservationResult{Rejected: true, Reason: gatewaycore.RouteReasonPolicyReject}, nil
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state := manager.stateLocked(upstreamID)
	nowAt := manager.now().UTC()
	if reason := state.reserve(candidate, request, nowAt); reason != gatewaycore.RouteReasonDirectMatch {
		return ReservationResult{Rejected: true, Reason: reason}, nil
	}
	return ReservationResult{Reservation: &memoryReservation{manager: manager, upstreamID: upstreamID}}, nil
}

func (manager *InMemoryReservationManager) Snapshot(upstreamID string) ReservationState {
	if manager == nil {
		return ReservationState{}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	state, ok := manager.states[strings.TrimSpace(upstreamID)]
	if !ok || state == nil {
		return ReservationState{}
	}
	return *state
}

func (manager *InMemoryReservationManager) stateLocked(upstreamID string) *ReservationState {
	if manager.states == nil {
		manager.states = make(map[string]*ReservationState)
	}
	state := manager.states[upstreamID]
	if state == nil {
		state = &ReservationState{}
		manager.states[upstreamID] = state
	}
	return state
}

func (manager *InMemoryReservationManager) now() time.Time {
	if manager != nil && manager.Now != nil {
		return manager.Now()
	}
	return time.Now()
}

func (reservation *memoryReservation) Release() {
	if reservation == nil || reservation.manager == nil || reservation.released {
		return
	}
	reservation.manager.mu.Lock()
	defer reservation.manager.mu.Unlock()
	reservation.released = true
	state := reservation.manager.states[strings.TrimSpace(reservation.upstreamID)]
	if state != nil && state.InFlight > 0 {
		state.InFlight--
	}
}

func (state *ReservationState) reserve(candidate Candidate, request ReservationRequest, nowAt time.Time) gatewaycore.RouteReason {
	if request.RequireQPS && limitExceeded(&state.QPSWindowStartedAt, &state.QPSCount, effectiveLimit(candidate.QPSLimit), 1, time.Second, nowAt) {
		return gatewaycore.RouteReasonQPSOverflow
	}
	if request.RequireRPM && limitExceeded(&state.RPMWindowStartedAt, &state.RPMCount, effectiveLimit(candidate.RPMLimit), 1, time.Minute, nowAt) {
		return gatewaycore.RouteReasonRPMOverflow
	}
	if request.RequireTPM && limitExceeded(&state.TPMWindowStartedAt, &state.TPMCount, effectiveLimit(candidate.TPMLimit), tokenReservationUnits(request), time.Minute, nowAt) {
		return gatewaycore.RouteReasonTPMOverflow
	}
	if request.RequireIPM && limitExceeded(&state.IPMWindowStartedAt, &state.IPMCount, effectiveLimit(candidate.IPMLimit), imageReservationUnits(request), time.Minute, nowAt) {
		return gatewaycore.RouteReasonIPMOverflow
	}
	if request.RequireConcurrency && candidate.MaxConcurrency > 0 && state.InFlight >= candidate.MaxConcurrency {
		return gatewaycore.RouteReasonConcurrencyOverflow
	}
	if request.RequireQPS {
		incrementWindow(&state.QPSWindowStartedAt, &state.QPSCount, 1, time.Second, nowAt)
	}
	if request.RequireRPM {
		incrementWindow(&state.RPMWindowStartedAt, &state.RPMCount, 1, time.Minute, nowAt)
	}
	if request.RequireTPM {
		incrementWindow(&state.TPMWindowStartedAt, &state.TPMCount, tokenReservationUnits(request), time.Minute, nowAt)
	}
	if request.RequireIPM {
		incrementWindow(&state.IPMWindowStartedAt, &state.IPMCount, imageReservationUnits(request), time.Minute, nowAt)
	}
	if request.RequireConcurrency && candidate.MaxConcurrency > 0 {
		state.InFlight++
	}
	return gatewaycore.RouteReasonDirectMatch
}

func limitExceeded(startedAt **time.Time, count *int, limit int, units int, window time.Duration, nowAt time.Time) bool {
	if limit <= 0 {
		return false
	}
	resetExpiredWindow(startedAt, count, window, nowAt)
	return *count+maxReservationUnits(units) > limit
}

func incrementWindow(startedAt **time.Time, count *int, units int, window time.Duration, nowAt time.Time) {
	resetExpiredWindow(startedAt, count, window, nowAt)
	if *startedAt == nil {
		cloned := nowAt.UTC()
		*startedAt = &cloned
	}
	*count += maxReservationUnits(units)
}

func resetExpiredWindow(startedAt **time.Time, count *int, window time.Duration, nowAt time.Time) {
	if startedAt == nil || count == nil {
		return
	}
	if *startedAt == nil || nowAt.UTC().Sub((*startedAt).UTC()) >= window || (*startedAt).After(nowAt.UTC()) {
		*startedAt = nil
		*count = 0
	}
}

func tokenReservationUnits(request ReservationRequest) int {
	return maxReservationUnits(request.InputTokens + request.OutputTokens)
}

func imageReservationUnits(request ReservationRequest) int {
	return maxReservationUnits(request.ImageCount)
}

func maxReservationUnits(units int) int {
	if units <= 0 {
		return 1
	}
	return units
}

func effectiveLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	return limit
}
