package connections

import (
	"time"

	"github.com/coachpo/prism/backend/internal/domain/terminaltarget"
)

// Routing schedule state values. "not_evaluated" is deliberately distinct from
// both "open" and "closed": when a connection cannot be routed for a reason
// that precedes the window check, claiming either would be a guess.
const (
	routingScheduleStatusOpen         = "open"
	routingScheduleStatusClosed       = "closed"
	routingScheduleStatusUnresolved   = "unresolved"
	routingScheduleStatusNotEvaluated = "not_evaluated"

	routingScheduleNotEvaluatedConnectionInactive = "connection_inactive"
)

// RoutingScheduleStatePayload is the server-computed state of one connection's
// routing schedule at a given instant. The frontend must not recompute it:
// window arithmetic involves IANA zones and DST, and a second implementation
// would drift from the one routing actually uses.
//
// The *_at keys are absent whenever the matching *_known flag is false. A typed
// nil pointer would marshal as JSON null and break that contract, so consumers
// read the flag rather than testing for key presence.
type RoutingScheduleStatePayload struct {
	Status             string  `json:"status"`
	NotEvaluatedReason *string `json:"not_evaluated_reason,omitempty"`
	Timezone           string  `json:"timezone"`
	EvaluatedAt        string  `json:"evaluated_at"`
	NextOpenAt         *string `json:"next_open_at,omitempty"`
	NextOpenAtKnown    bool    `json:"next_open_at_known"`
	NextCloseAt        *string `json:"next_close_at,omitempty"`
	NextCloseAtKnown   bool    `json:"next_close_at_known"`
}

// RoutingScheduleStateFor is the single state projection in the repository.
// Every surface that shows "is this leg on duty right now" calls it, so the
// three views (connection list, model detail target summary, routing health)
// cannot drift apart.
//
// The branch order matches the runtime gate and must not be reordered:
// unconfigured yields no state at all rather than a fabricated "open", and an
// inactive connection reports not_evaluated because is_active is checked before
// the schedule ever loads.
func RoutingScheduleStateFor(schedule terminaltarget.CompiledRoutingSchedule, isActive bool, now time.Time) *RoutingScheduleStatePayload {
	if !schedule.Configured() {
		return nil
	}
	state := &RoutingScheduleStatePayload{
		Timezone:    schedule.Timezone,
		EvaluatedAt: now.UTC().Format(time.RFC3339),
	}
	if !isActive {
		reason := routingScheduleNotEvaluatedConnectionInactive
		state.Status = routingScheduleStatusNotEvaluated
		state.NotEvaluatedReason = &reason
		return state
	}
	switch schedule.DecideAt(now) {
	case terminaltarget.RoutingScheduleUnresolved:
		state.Status = routingScheduleStatusUnresolved
	case terminaltarget.RoutingScheduleOpen:
		state.Status = routingScheduleStatusOpen
		// The open state carries its closing boundary so a rendered badge can
		// expire itself; without it an "open" badge keeps asserting a stale
		// conclusion for as long as the page stays mounted.
		if nextCloseAt, known := schedule.NextCloseAt(now); known {
			formatted := nextCloseAt.UTC().Format(time.RFC3339)
			state.NextCloseAt = &formatted
			state.NextCloseAtKnown = true
		}
	default:
		state.Status = routingScheduleStatusClosed
		if nextOpenAt, known := schedule.NextOpenAt(now); known {
			formatted := nextOpenAt.UTC().Format(time.RFC3339)
			state.NextOpenAt = &formatted
			state.NextOpenAtKnown = true
		}
	}
	return state
}

// RoutingScheduleStateForConfig compiles stored configuration and projects it in
// one step, so callers holding raw configuration never build a
// CompiledRoutingSchedule themselves (and never reach for IsOpenAt directly).
// It is exported for the models package, whose access-target responses expose
// the same connection through two JSON keys and must render both identically.
func RoutingScheduleStateForConfig(timezone *string, windows []terminaltarget.Window, isActive bool, now time.Time) *RoutingScheduleStatePayload {
	if timezone == nil && len(windows) == 0 {
		return nil
	}
	tz := ""
	if timezone != nil {
		tz = *timezone
	}
	return RoutingScheduleStateFor(terminaltarget.CompileRoutingSchedule(tz, windows), isActive, now)
}
