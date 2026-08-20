package terminaltarget

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Routing schedule per Terminal Target (a row in the connections table). A
// connection that owns at least one routing window only participates in
// routing while the request instant falls inside one of its windows,
// evaluated against the connection's own IANA timezone. Zero window rows
// means "no time restriction" and is the only encoding of 7x24 availability;
// a window set whose union covers the whole week is rejected by the write
// path so that "always available" has exactly one representation.
//
// weekday_mask is a 7-bit ISO weekday bitmap (bit0=Monday .. bit6=Sunday) and
// names the day on which the window OPENS. end_minute > 1440 means the window
// runs past local midnight into the following day; the trailing part is never
// re-encoded on the following day's bit. Boundaries are half-open
// [start_minute, end_minute) at minute granularity.
//
// The timezone here is the Terminal Target's own routing clock. It is
// unrelated to user_settings.timezone_preference, which only affects
// timestamp display and never changes routing.

const (
	// RoutingScheduleMaxWindows bounds the window rows of one connection.
	RoutingScheduleMaxWindows = 32
	// RoutingScheduleMaxTimezoneLength bounds the IANA timezone column
	// (connections.routing_schedule_timezone is varchar(100)).
	RoutingScheduleMaxTimezoneLength = 100
	// RoutingScheduleMaxStartMinute is the largest start minute of a window.
	RoutingScheduleMaxStartMinute = 1439
	// RoutingScheduleMaxEndMinute must stay byte-for-byte identical to the
	// migration CHECK upper bound (end_minute BETWEEN 1 AND 2880). The bound
	// is deliberately loose: start <= 1439 and span <= 1440 make end >= 2880
	// unreachable. Keeping the same literal in both layers prevents a
	// "database accepts, application 422s" crack.
	RoutingScheduleMaxEndMinute = 2880
	// RoutingScheduleMaxSpanMinutes bounds one window to at most 24 hours.
	RoutingScheduleMaxSpanMinutes = 1440
	// routingScheduleWeekMinutes is 7 * 1440, the full-week bitmap width.
	routingScheduleWeekMinutes = 10080
	// routingScheduleScanDays is the scan horizon for NextOpenAt/NextCloseAt
	// (8 days, so a Sunday-only window crossing a spring-forward day is
	// found). The horizon bound and the candidate-day loop must both use this
	// constant.
	routingScheduleScanDays = 8
	// routingScheduleZoneinfoProbe distinguishes a mistyped timezone name
	// from a missing zoneinfo database: Go's LoadLocation returns byte-for-
	// byte identical errors for both, and the probe name is present in every
	// zoneinfo installation.
	routingScheduleZoneinfoProbe = "America/New_York"
)

// Window is one half-open [StartMinute, EndMinute) routing interval on the
// ISO weekdays named by WeekdayMask. WeekdayMask uses int (not uint8) so a
// value like 384 cannot silently truncate to 128 during validation.
type Window struct {
	WeekdayMask int
	StartMinute int
	EndMinute   int
}

// CompiledRoutingSchedule is the immutable, compile-once representation of a
// connection's routing windows. All methods use value receivers and never
// mutate Windows: planning snapshots are shared across requests through
// atomic.Pointer with only shallow map copies, so any in-place write would be
// a cross-request data race that no test can detect.
type CompiledRoutingSchedule struct {
	Timezone   string
	Location   *time.Location
	Windows    []Window
	Unresolved bool
}

// RoutingScheduleDecision is the only eligibility conclusion type of this
// package. Every caller should call DecideAt instead of IsOpenAt.
type RoutingScheduleDecision int

const (
	// RoutingScheduleUnrestricted means no windows are configured: the
	// connection is always routable, byte-for-byte the pre-feature behavior.
	RoutingScheduleUnrestricted RoutingScheduleDecision = iota
	// RoutingScheduleOpen means windows are configured and referenceNow is
	// inside one of them.
	RoutingScheduleOpen
	// RoutingScheduleClosed means windows are configured and referenceNow is
	// outside all of them.
	RoutingScheduleClosed
	// RoutingScheduleUnresolved means the timezone is missing, unparseable,
	// or a window is structurally out of range. The failure is confined to
	// this single connection (the connection is excluded from routing).
	RoutingScheduleUnresolved
)

// Validation reasons exposed through the management API error envelope.
const (
	RoutingScheduleReasonNoWindows                   = "no_windows"
	RoutingScheduleReasonTooManyWindows              = "too_many_windows"
	RoutingScheduleReasonTimezoneRequired            = "timezone_required"
	RoutingScheduleReasonTimezoneTooLong             = "timezone_too_long"
	RoutingScheduleReasonTimezoneNotAllowed          = "timezone_not_allowed"
	RoutingScheduleReasonTimezoneUnknown             = "timezone_unknown"
	RoutingScheduleReasonTimezoneDatabaseUnavailable = "timezone_database_unavailable"
	RoutingScheduleReasonWeekdayMaskOutOfRange       = "weekday_mask_out_of_range"
	RoutingScheduleReasonStartMinuteOutOfRange       = "start_minute_out_of_range"
	RoutingScheduleReasonEndMinuteOutOfRange         = "end_minute_out_of_range"
	RoutingScheduleReasonEndMinuteNotAfterStart      = "end_minute_not_after_start"
	RoutingScheduleReasonSpanExceedsOneDay           = "span_exceeds_one_day"
	RoutingScheduleReasonDuplicateWindow             = "duplicate_window"
	RoutingScheduleReasonCoversFullWeek              = "covers_full_week"
)

// RoutingScheduleValidationError describes why a routing schedule was
// rejected. Path points at the shallowest locatable failure position inside
// the field (for example "routing_schedule.windows[2]"). Index is the
// offending window's position when the failure is window-scoped, -1
// otherwise. Limit is only populated for limit-class failures.
type RoutingScheduleValidationError struct {
	Reason string
	Path   string
	Index  int
	Limit  int
}

func (err *RoutingScheduleValidationError) Error() string {
	if err == nil {
		return ""
	}
	message := fmt.Sprintf("invalid routing schedule: %s", err.Reason)
	if err.Path != "" {
		message += " at " + err.Path
	}
	if err.Index >= 0 {
		message += fmt.Sprintf(" (window %d)", err.Index)
	}
	if err.Limit > 0 {
		message += fmt.Sprintf(" (limit %d)", err.Limit)
	}
	return message
}

// CompileRoutingSchedule compiles a timezone plus raw window rows into an
// immutable schedule. It never returns an error: a missing/unparseable
// timezone or a structurally out-of-range window compiles to
// Unresolved=true, which confines the failure to the single connection
// (DecideAt reports RoutingScheduleUnresolved and routing excludes it). This
// is the only fail-closed point; runtime loading never calls validation.
func CompileRoutingSchedule(timezone string, windows []Window) CompiledRoutingSchedule {
	tz := strings.TrimSpace(timezone)
	if len(windows) == 0 {
		return CompiledRoutingSchedule{Timezone: tz}
	}
	// LoadLocation("") silently returns UTC and LoadLocation("Local") returns
	// the server's local zone; both must be rejected before calling it.
	if tz == "" || tz == "Local" {
		return CompiledRoutingSchedule{Timezone: tz, Windows: normalizeWindows(windows), Unresolved: true}
	}
	location, err := time.LoadLocation(tz)
	if err != nil || location == nil {
		return CompiledRoutingSchedule{Timezone: tz, Windows: normalizeWindows(windows), Unresolved: true}
	}
	for _, w := range windows {
		if w.WeekdayMask < 1 || w.WeekdayMask > 127 ||
			w.StartMinute < 0 || w.StartMinute > RoutingScheduleMaxStartMinute ||
			w.EndMinute < 1 || w.EndMinute > RoutingScheduleMaxEndMinute ||
			w.EndMinute <= w.StartMinute || w.EndMinute-w.StartMinute > RoutingScheduleMaxSpanMinutes {
			return CompiledRoutingSchedule{Timezone: tz, Windows: normalizeWindows(windows), Unresolved: true}
		}
	}
	return CompiledRoutingSchedule{Timezone: tz, Location: location, Windows: normalizeWindows(windows)}
}

// Configured reports whether the connection owns at least one window row.
// Unresolved configurations still report Configured() == true: a broken
// configuration is still a configuration.
func (s CompiledRoutingSchedule) Configured() bool {
	return len(s.Windows) > 0
}

// DecideAt is the only eligibility entry point for a compiled schedule. The
// branch order is authoritative and must not be reordered: Unresolved beats
// everything, zero windows means unrestricted, and only then do windows
// decide Open versus Closed. IsOpenAt must never be used directly as a gate
// — an unrestricted schedule is always IsOpenAt == false, so a gate built on
// IsOpenAt would fail every existing connection closed.
func (s CompiledRoutingSchedule) DecideAt(referenceNow time.Time) RoutingScheduleDecision {
	if s.Unresolved || (len(s.Windows) > 0 && s.Location == nil) {
		return RoutingScheduleUnresolved
	}
	if len(s.Windows) == 0 {
		return RoutingScheduleUnrestricted
	}
	if s.IsOpenAt(referenceNow) {
		return RoutingScheduleOpen
	}
	return RoutingScheduleClosed
}

// IsOpenAt is a low-level predicate: does referenceNow fall inside some
// configured window? It deliberately returns false for unconfigured (zero
// windows), Unresolved, and nil-Location schedules — none of those have any
// window that could contain now. It does not express eligibility; use
// DecideAt for that.
func (s CompiledRoutingSchedule) IsOpenAt(referenceNow time.Time) bool {
	if len(s.Windows) == 0 || s.Unresolved || s.Location == nil {
		return false
	}
	local := referenceNow.In(s.Location)
	iso := isoWeekday(local)
	minute := local.Hour()*60 + local.Minute()
	// The day on which a window that runs past midnight must have opened.
	// Written ((iso + 5) % 7) + 1 — plain iso-1 underflows on Monday.
	yesterday := ((iso + 5) % 7) + 1
	for _, w := range s.Windows {
		if w.WeekdayMask&(1<<(iso-1)) != 0 && minute >= w.StartMinute && minute < w.EndMinute {
			return true
		}
		if w.EndMinute > 1440 && w.WeekdayMask&(1<<(yesterday-1)) != 0 && minute < w.EndMinute-1440 {
			return true
		}
	}
	return false
}

// NextOpenAt returns the earliest instant >= referenceNow at which the
// schedule is open, or (zero, false) when the schedule is unconfigured,
// unresolved, or has no open instant within the 8-day scan horizon. An
// already-open schedule returns referenceNow itself.
func (s CompiledRoutingSchedule) NextOpenAt(referenceNow time.Time) (time.Time, bool) {
	if len(s.Windows) == 0 || s.Unresolved || s.Location == nil {
		return time.Time{}, false
	}
	if s.IsOpenAt(referenceNow) {
		return referenceNow, true
	}
	limit := referenceNow.Add(routingScheduleScanDays * 24 * time.Hour)
	candidates := s.windowTransitionCandidates(referenceNow, false)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	for _, candidate := range candidates {
		if !candidate.Before(referenceNow) && !candidate.After(limit) && s.IsOpenAt(candidate) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

// NextCloseAt returns the earliest instant > referenceNow at which the
// schedule stops being open (the end of the current window), or (zero, false)
// when the schedule is not currently open or no close instant exists within
// the scan horizon. Adjacent windows that seamlessly join are skipped, so the
// result is the true end of the joined span.
func (s CompiledRoutingSchedule) NextCloseAt(referenceNow time.Time) (time.Time, bool) {
	if !s.IsOpenAt(referenceNow) {
		return time.Time{}, false
	}
	limit := referenceNow.Add(routingScheduleScanDays * 24 * time.Hour)
	candidates := s.windowTransitionCandidates(referenceNow, true)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Before(candidates[j]) })
	for _, candidate := range candidates {
		if candidate.After(referenceNow) && !candidate.After(limit) && !s.IsOpenAt(candidate) {
			return candidate, true
		}
	}
	return time.Time{}, false
}

// windowTransitionCandidates collects every candidate transition instant over
// the 9 local calendar days starting at referenceNow's local date: one
// ZoneBounds candidate per day (a DST gap end is the earliest instant after
// the gap and cannot be produced by either construction below) plus, for
// every window opening on that day, both an absolute addition from local
// midnight and a wall-clock time.Date construction of the window's start (or
// end when closeTransition is set). The wall-clock construction normalizes
// hours >= 24 automatically, which is exactly what cross-midnight ends need.
func (s CompiledRoutingSchedule) windowTransitionCandidates(referenceNow time.Time, closeTransition bool) []time.Time {
	year, month, day := referenceNow.In(s.Location).Date()
	var candidates []time.Time
	for off := 0; off <= routingScheduleScanDays; off++ {
		midnight := time.Date(year, month, day+off, 0, 0, 0, 0, s.Location)
		if _, zoneEnd := midnight.ZoneBounds(); !zoneEnd.IsZero() {
			candidates = append(candidates, zoneEnd)
		}
		iso := isoWeekday(midnight)
		for _, w := range s.Windows {
			if w.WeekdayMask&(1<<(iso-1)) == 0 {
				continue
			}
			minute := w.StartMinute
			if closeTransition {
				minute = w.EndMinute
			}
			candidates = append(candidates,
				midnight.Add(time.Duration(minute)*time.Minute),
				time.Date(year, month, day+off, minute/60, minute%60, 0, 0, s.Location),
			)
		}
	}
	return candidates
}

// CoversFullWeek reports whether the schedule's windows cover all 10080
// minutes of the week. Cross-midnight segments are projected onto the
// following day and folded with % 10080 so a Sunday 22:00 overflow cannot
// fall out of the bitmap and misreport a true 7x24 as gapped.
func (s CompiledRoutingSchedule) CoversFullWeek() bool {
	return WindowsCoverFullWeek(s.Windows)
}

// WindowsCoverFullWeek is the timezone-free, clock-free full-week predicate
// over raw window rows; static diagnostics use it without compiling a
// schedule or reading any clock.
func WindowsCoverFullWeek(windows []Window) bool {
	if len(windows) == 0 {
		return false
	}
	var covered [routingScheduleWeekMinutes]bool
	for _, w := range windows {
		for day := 0; day < 7; day++ {
			if w.WeekdayMask&(1<<day) == 0 {
				continue
			}
			start := day*1440 + w.StartMinute
			end := day*1440 + w.EndMinute
			for minute := start; minute < end; minute++ {
				covered[minute%routingScheduleWeekMinutes] = true
			}
		}
	}
	for _, isCovered := range covered {
		if !isCovered {
			return false
		}
	}
	return true
}

// ValidateRoutingSchedule validates a timezone plus raw window rows on the
// management write path only; the runtime load path never calls it (the
// runtime compiles fail-closed to a single connection instead). The reason
// order is authoritative: the first failure wins. timezone_unknown versus
// timezone_database_unavailable are distinguished with the America/New_York
// probe because Go returns byte-for-byte identical LoadLocation errors for a
// mistyped name and a missing zoneinfo database.
func ValidateRoutingSchedule(timezone string, windows []Window) *RoutingScheduleValidationError {
	if len(windows) == 0 {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonNoWindows, Path: "routing_schedule", Index: -1}
	}
	if len(windows) > RoutingScheduleMaxWindows {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonTooManyWindows, Path: "routing_schedule", Limit: RoutingScheduleMaxWindows, Index: -1}
	}
	tz := strings.TrimSpace(timezone)
	if tz == "" {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonTimezoneRequired, Path: "routing_schedule.timezone", Index: -1}
	}
	if len(tz) > RoutingScheduleMaxTimezoneLength {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonTimezoneTooLong, Path: "routing_schedule.timezone", Limit: RoutingScheduleMaxTimezoneLength, Index: -1}
	}
	if tz == "Local" {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonTimezoneNotAllowed, Path: "routing_schedule.timezone", Index: -1}
	}
	if location, err := time.LoadLocation(tz); err != nil || location == nil {
		if _, probeErr := time.LoadLocation(routingScheduleZoneinfoProbe); probeErr != nil {
			return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonTimezoneDatabaseUnavailable, Path: "routing_schedule.timezone", Index: -1}
		}
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonTimezoneUnknown, Path: "routing_schedule.timezone", Index: -1}
	}
	seen := make(map[Window]struct{}, len(windows))
	for index, w := range windows {
		if validationErr := ValidateRoutingWindowFields(w.WeekdayMask, w.StartMinute, w.EndMinute); validationErr != nil {
			validationErr.Index = index
			validationErr.Path = fmt.Sprintf("routing_schedule.windows[%d]", index)
			return validationErr
		}
		if _, duplicate := seen[w]; duplicate {
			return &RoutingScheduleValidationError{
				Reason: RoutingScheduleReasonDuplicateWindow,
				Index:  index,
				Path:   fmt.Sprintf("routing_schedule.windows[%d]", index),
			}
		}
		seen[w] = struct{}{}
	}
	if WindowsCoverFullWeek(windows) {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonCoversFullWeek, Path: "routing_schedule.windows", Index: -1}
	}
	return nil
}

// ValidateRoutingWindowFields validates a single window's three fields in
// fixed order.
func ValidateRoutingWindowFields(mask, start, end int) *RoutingScheduleValidationError {
	if mask < 1 || mask > 127 {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonWeekdayMaskOutOfRange}
	}
	if start < 0 || start > RoutingScheduleMaxStartMinute {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonStartMinuteOutOfRange}
	}
	if end < 1 || end > RoutingScheduleMaxEndMinute {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonEndMinuteOutOfRange}
	}
	if end <= start {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonEndMinuteNotAfterStart}
	}
	if end-start > RoutingScheduleMaxSpanMinutes {
		return &RoutingScheduleValidationError{Reason: RoutingScheduleReasonSpanExceedsOneDay}
	}
	return nil
}

// normalizeWindows returns a freshly allocated, sorted, deduplicated copy of
// windows. It never sorts or mutates the caller's slice: the compiled
// schedule is shared across requests through a snapshot pointer, so any
// aliasing of caller storage would be a cross-request data race.
func normalizeWindows(windows []Window) []Window {
	normalized := make([]Window, 0, len(windows))
	copied := append([]Window(nil), windows...)
	sort.Slice(copied, func(i, j int) bool {
		if copied[i].WeekdayMask != copied[j].WeekdayMask {
			return copied[i].WeekdayMask < copied[j].WeekdayMask
		}
		if copied[i].StartMinute != copied[j].StartMinute {
			return copied[i].StartMinute < copied[j].StartMinute
		}
		return copied[i].EndMinute < copied[j].EndMinute
	})
	for _, w := range copied {
		if len(normalized) > 0 && normalized[len(normalized)-1] == w {
			continue
		}
		normalized = append(normalized, w)
	}
	return normalized
}

// isoWeekday converts time.Weekday (Sunday=0 .. Saturday=6) to ISO
// (Monday=1 .. Sunday=7).
func isoWeekday(t time.Time) int {
	weekday := int(t.Weekday())
	if weekday == 0 {
		return 7
	}
	return weekday
}

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
	if s.State == PricingScheduleUnconfigured || s.State == PricingScheduleUnresolved || s.Location == nil {
		return s.State, PricingWallClock{}
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
