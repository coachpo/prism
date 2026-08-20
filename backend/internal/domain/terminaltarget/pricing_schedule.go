package terminaltarget

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// PricingScheduleDecision is deliberately separate from routing eligibility:
// zero pricing windows are invalid configuration, not unrestricted routing.
type PricingScheduleDecision int

const (
	PricingScheduleUnconfigured PricingScheduleDecision = iota
	PricingScheduleUnresolved
	PricingScheduleInWindow
	PricingScheduleOutOfWindow
)

type PricingWallClock struct {
	Weekday int
	Minute  int
	Valid   bool
}

type CompiledPricingSchedule struct {
	Timezone string
	Location *time.Location
	Windows  []Window
	State    PricingScheduleDecision
}

func CompilePricingSchedule(timezone string, windows []Window) CompiledPricingSchedule {
	tz := strings.TrimSpace(timezone)
	if len(windows) == 0 {
		return CompiledPricingSchedule{Timezone: tz, State: PricingScheduleUnconfigured}
	}
	compiled := CompileRoutingSchedule(tz, windows)
	if compiled.Unresolved || compiled.Location == nil {
		return CompiledPricingSchedule{Timezone: tz, Location: compiled.Location, Windows: append([]Window(nil), compiled.Windows...), State: PricingScheduleUnresolved}
	}
	return CompiledPricingSchedule{Timezone: tz, Location: compiled.Location, Windows: append([]Window(nil), compiled.Windows...), State: PricingScheduleOutOfWindow}
}

func (s CompiledPricingSchedule) DecideAt(referenceNow time.Time) (PricingScheduleDecision, PricingWallClock) {
	if referenceNow.IsZero() || s.State == PricingScheduleUnconfigured || s.State == PricingScheduleUnresolved || s.Location == nil {
		return PricingScheduleUnresolved, PricingWallClock{}
	}
	local := referenceNow.In(s.Location)
	wall := PricingWallClock{Weekday: isoWeekday(local), Minute: local.Hour()*60 + local.Minute(), Valid: true}
	compiled := CompiledRoutingSchedule{Timezone: s.Timezone, Location: s.Location, Windows: s.Windows}
	if compiled.IsOpenAt(referenceNow) {
		return PricingScheduleInWindow, wall
	}
	return PricingScheduleOutOfWindow, wall
}

// PricingWindowsDigest is the Go/SQL canonical digest contract for normalized
// schedule rows: sorted mask,start,end tuples joined by LF, SHA-256 hex.
func PricingWindowsDigest(windows []Window) string {
	ordered := normalizeWindows(windows)
	parts := make([]string, 0, len(ordered))
	for _, window := range ordered {
		parts = append(parts, fmt.Sprintf("%d,%d,%d", window.WeekdayMask, window.StartMinute, window.EndMinute))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:])
}
