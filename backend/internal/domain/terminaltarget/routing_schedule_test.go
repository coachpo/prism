package terminaltarget

import (
	"testing"
	"time"
)

// Fixed reference clock: 2023-11-14T22:13:20Z = Tuesday (ISO 2 -> bit1),
// local minute 1333 in UTC.
var routingScheduleFixedNow = time.Unix(1_700_000_000, 0).UTC()

func TestCompileRoutingScheduleNormalizesAndIsolatesWindows(t *testing.T) {
	t.Run("zero windows means unrestricted", func(t *testing.T) {
		compiled := CompileRoutingSchedule("Asia/Shanghai", nil)
		if compiled.Configured() {
			t.Fatalf("expected zero windows to be unconfigured")
		}
		if compiled.Unresolved {
			t.Fatalf("expected zero windows to compile without Unresolved")
		}
		if compiled.Location != nil {
			t.Fatalf("expected no Location for an unconfigured schedule")
		}
		if compiled.Timezone != "Asia/Shanghai" {
			t.Fatalf("expected timezone to be kept, got %q", compiled.Timezone)
		}
	})

	t.Run("empty or Local timezone with windows is unresolved", func(t *testing.T) {
		windows := []Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 1440}}
		for _, tz := range []string{"", "Local"} {
			compiled := CompileRoutingSchedule(tz, windows)
			if !compiled.Configured() || !compiled.Unresolved {
				t.Fatalf("timezone %q: expected configured + unresolved", tz)
			}
		}
	})

	t.Run("unparseable timezone is unresolved", func(t *testing.T) {
		compiled := CompileRoutingSchedule("Not/AZone", []Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 1440}})
		if !compiled.Unresolved {
			t.Fatalf("expected unresolved")
		}
	})

	t.Run("structurally out of range window is unresolved", func(t *testing.T) {
		compiled := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 384, StartMinute: 0, EndMinute: 1440}})
		if !compiled.Unresolved {
			t.Fatalf("expected unresolved")
		}
	})

	t.Run("normalizes and deduplicates into a fresh slice", func(t *testing.T) {
		input := []Window{
			{WeekdayMask: 2, StartMinute: 60, EndMinute: 120},
			{WeekdayMask: 1, StartMinute: 30, EndMinute: 90},
			{WeekdayMask: 2, StartMinute: 60, EndMinute: 120},
		}
		compiled := CompileRoutingSchedule("UTC", input)
		if len(compiled.Windows) != 2 {
			t.Fatalf("expected 2 windows after dedupe, got %d", len(compiled.Windows))
		}
		if compiled.Windows[0].WeekdayMask != 1 || compiled.Windows[1].WeekdayMask != 2 {
			t.Fatalf("expected sort by weekday mask, got %+v", compiled.Windows)
		}
		input[0] = Window{WeekdayMask: 127, StartMinute: 0, EndMinute: 1440}
		if compiled.Windows[0].WeekdayMask == 127 {
			t.Fatalf("compiled schedule aliases caller storage")
		}
	})
}

func TestRoutingScheduleIsOpenAt(t *testing.T) {
	utc := func(hour, minute int) time.Time {
		return time.Date(2023, 11, 13, hour, minute, 0, 0, time.UTC) // Monday
	}
	schedule := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100}})

	t.Run("half-open boundaries", func(t *testing.T) {
		if !schedule.IsOpenAt(utc(16, 40)) { // start
			t.Fatalf("expected start minute open")
		}
		if !schedule.IsOpenAt(utc(18, 19)) { // end-1
			t.Fatalf("expected end-1 open")
		}
		if schedule.IsOpenAt(utc(18, 20)) { // end
			t.Fatalf("expected end minute closed")
		}
		if schedule.IsOpenAt(utc(16, 39)) { // start-1
			t.Fatalf("expected start-1 closed")
		}
	})

	t.Run("other weekdays closed", func(t *testing.T) {
		wednesday := time.Date(2023, 11, 15, 17, 0, 0, 0, time.UTC)
		if schedule.IsOpenAt(wednesday) {
			t.Fatalf("expected Wednesday closed for a Monday-only mask")
		}
	})

	t.Run("cross-midnight same day and overflow", func(t *testing.T) {
		cross := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 1, StartMinute: 1300, EndMinute: 1500}})
		if !cross.IsOpenAt(utc(23, 0)) { // Monday 23:00
			t.Fatalf("expected Monday 23:00 open")
		}
		if !cross.IsOpenAt(time.Date(2023, 11, 14, 0, 30, 0, 0, time.UTC)) { // Tuesday 00:30
			t.Fatalf("expected trailing part open via yesterday's bit")
		}
		if cross.IsOpenAt(time.Date(2023, 11, 14, 1, 0, 0, 0, time.UTC)) { // Tuesday 01:00 = end
			t.Fatalf("expected end exclusive")
		}
	})

	t.Run("mask binds to opening day", func(t *testing.T) {
		sundayOnly := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 64, StartMinute: 1380, EndMinute: 1500}})
		saturday := time.Date(2023, 11, 18, 23, 0, 0, 0, time.UTC) // Saturday 23:00
		if sundayOnly.IsOpenAt(saturday) {
			t.Fatalf("expected Saturday 23:00 closed (window opens Sunday)")
		}
		sunday := time.Date(2023, 11, 19, 23, 0, 0, 0, time.UTC) // Sunday 23:00
		if !sundayOnly.IsOpenAt(sunday) {
			t.Fatalf("expected Sunday 23:00 open")
		}
		monday0030 := time.Date(2023, 11, 20, 0, 30, 0, 0, time.UTC) // Monday 00:30
		if !sundayOnly.IsOpenAt(monday0030) {
			t.Fatalf("expected Sunday->Monday wrap open")
		}
		monday0230 := time.Date(2023, 11, 20, 2, 30, 0, 0, time.UTC)
		if sundayOnly.IsOpenAt(monday0230) {
			t.Fatalf("expected Monday 02:30 closed")
		}
	})

	t.Run("union gap closed, adjacent and overlapping joined", func(t *testing.T) {
		gapped := CompileRoutingSchedule("UTC", []Window{
			{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100},
			{WeekdayMask: 1, StartMinute: 1200, EndMinute: 1300},
		})
		if gapped.IsOpenAt(utc(19, 10)) {
			t.Fatalf("expected gap closed")
		}
		adjacent := CompileRoutingSchedule("UTC", []Window{
			{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100},
			{WeekdayMask: 1, StartMinute: 1100, EndMinute: 1200},
		})
		if !adjacent.IsOpenAt(utc(19, 10)) {
			t.Fatalf("expected adjacent join open")
		}
		overlapping := CompileRoutingSchedule("UTC", []Window{
			{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1200},
			{WeekdayMask: 1, StartMinute: 1100, EndMinute: 1300},
		})
		if !overlapping.IsOpenAt(utc(19, 10)) {
			t.Fatalf("expected overlapping windows open")
		}
	})

	t.Run("multi-day mask", func(t *testing.T) {
		tueWed := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 2 | 4, StartMinute: 1000, EndMinute: 1100}})
		if !tueWed.IsOpenAt(time.Date(2023, 11, 14, 17, 0, 0, 0, time.UTC)) || !tueWed.IsOpenAt(time.Date(2023, 11, 15, 17, 0, 0, 0, time.UTC)) {
			t.Fatalf("expected both Tuesday and Wednesday open")
		}
	})

	t.Run("UTC instant converted into local zone", func(t *testing.T) {
		shanghai := CompileRoutingSchedule("Asia/Shanghai", []Window{{WeekdayMask: 2, StartMinute: 0, EndMinute: 1440}})
		if !shanghai.IsOpenAt(time.Date(2023, 11, 14, 15, 0, 0, 0, time.UTC)) { // Tuesday 23:00 local
			t.Fatalf("expected Tuesday 23:00 Shanghai open")
		}
		if shanghai.IsOpenAt(time.Date(2023, 11, 14, 16, 30, 0, 0, time.UTC)) { // Wednesday 00:30 local
			t.Fatalf("expected Wednesday 00:30 Shanghai closed")
		}
	})

	t.Run("unconfigured, unresolved, and zero value always false", func(t *testing.T) {
		if CompileRoutingSchedule("UTC", nil).IsOpenAt(routingScheduleFixedNow) {
			t.Fatalf("expected zero-window schedule false")
		}
		if CompileRoutingSchedule("Not/AZone", []Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 1440}}).IsOpenAt(routingScheduleFixedNow) {
			t.Fatalf("expected unresolved schedule false")
		}
		var zero CompiledRoutingSchedule
		if zero.IsOpenAt(routingScheduleFixedNow) || zero.Configured() {
			t.Fatalf("expected zero value false and unconfigured")
		}
	})
}

func TestRoutingScheduleDecideAt(t *testing.T) {
	window := []Window{{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100}}
	inWindow := time.Date(2023, 11, 13, 17, 0, 0, 0, time.UTC)
	outOfWindow := time.Date(2023, 11, 13, 20, 0, 0, 0, time.UTC)

	t.Run("zero windows and zero value are unrestricted", func(t *testing.T) {
		if got := CompileRoutingSchedule("UTC", nil).DecideAt(inWindow); got != RoutingScheduleUnrestricted {
			t.Fatalf("expected Unrestricted, got %v", got)
		}
		var zero CompiledRoutingSchedule
		if got := zero.DecideAt(inWindow); got != RoutingScheduleUnrestricted {
			t.Fatalf("expected zero value Unrestricted, got %v", got)
		}
	})

	t.Run("unresolved beats everything", func(t *testing.T) {
		unresolved := CompileRoutingSchedule("Not/AZone", window)
		if got := unresolved.DecideAt(inWindow); got != RoutingScheduleUnresolved {
			t.Fatalf("expected Unresolved, got %v", got)
		}
		noLocation := CompiledRoutingSchedule{Windows: window}
		if got := noLocation.DecideAt(inWindow); got != RoutingScheduleUnresolved {
			t.Fatalf("expected windows-without-Location Unresolved, got %v", got)
		}
	})

	t.Run("open and closed", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", window)
		if got := schedule.DecideAt(time.Date(2023, 11, 13, 17, 0, 0, 0, time.UTC)); got != RoutingScheduleOpen {
			t.Fatalf("expected Open, got %v", got)
		}
		if got := schedule.DecideAt(outOfWindow); got != RoutingScheduleClosed {
			t.Fatalf("expected Closed, got %v", got)
		}
	})
}

func TestRoutingScheduleIsOpenAtAcrossDST(t *testing.T) {
	newYork := func(year int, month time.Month, day, hour, minute int) time.Time {
		return time.Date(year, month, day, hour, minute, 0, 0, time.FixedZone("EST", -5*3600))
	}
	springForward := time.Date(2026, 3, 8, 0, 0, 0, 0, mustLoadLocation(t, "America/New_York"))

	t.Run("gap-adjacent window", func(t *testing.T) {
		schedule := CompileRoutingSchedule("America/New_York", []Window{{WeekdayMask: 64, StartMinute: 30, EndMinute: 120}})
		if !schedule.IsOpenAt(newYork(2026, 3, 8, 1, 30)) {
			t.Fatalf("expected 01:30 EST open")
		}
		if schedule.IsOpenAt(time.Date(2026, 3, 8, 3, 30, 0, 0, springForward.Location())) {
			t.Fatalf("expected 03:30 EDT closed (window ended at 02:00 EST)")
		}
	})

	t.Run("window spanning the spring-forward instant", func(t *testing.T) {
		schedule := CompileRoutingSchedule("America/New_York", []Window{{WeekdayMask: 64, StartMinute: 60, EndMinute: 240}})
		if !schedule.IsOpenAt(newYork(2026, 3, 8, 1, 30)) {
			t.Fatalf("expected 01:30 EST open")
		}
		if !schedule.IsOpenAt(time.Date(2026, 3, 8, 3, 30, 0, 0, springForward.Location())) {
			t.Fatalf("expected 03:30 EDT open (wall clock inside window)")
		}
	})

	t.Run("entire window inside the gap", func(t *testing.T) {
		schedule := CompileRoutingSchedule("America/New_York", []Window{{WeekdayMask: 64, StartMinute: 120, EndMinute: 180}})
		if schedule.IsOpenAt(time.Date(2026, 3, 8, 6, 59, 0, 0, time.UTC)) {
			t.Fatalf("expected 01:59 EST closed")
		}
		if schedule.IsOpenAt(time.Date(2026, 3, 8, 7, 0, 0, 0, time.UTC)) {
			t.Fatalf("expected 03:00 EDT closed")
		}
	})

	t.Run("fall-back day passes twice", func(t *testing.T) {
		schedule := CompileRoutingSchedule("America/New_York", []Window{{WeekdayMask: 64, StartMinute: 60, EndMinute: 120}})
		if !schedule.IsOpenAt(time.Date(2026, 11, 1, 1, 30, 0, 0, time.FixedZone("EDT", -4*3600))) { // 01:30 EDT = 05:30 UTC
			t.Fatalf("expected first 01:30 open")
		}
		if !schedule.IsOpenAt(time.Date(2026, 11, 1, 1, 30, 0, 0, time.FixedZone("EST", -5*3600))) { // 01:30 EST = 06:30 UTC
			t.Fatalf("expected second 01:30 open")
		}
	})
}

func TestRoutingScheduleNextOpenAt(t *testing.T) {
	utc := func(day, hour, minute int) time.Time {
		return time.Date(2023, 11, day, hour, minute, 0, 0, time.UTC)
	}

	t.Run("already open returns now", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100}})
		now := utc(13, 17, 0)
		next, ok := schedule.NextOpenAt(now)
		if !ok || !next.Equal(now) {
			t.Fatalf("expected now, got %v ok=%v", next, ok)
		}
	})

	t.Run("later today and next week same slot", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 2, StartMinute: 1400, EndMinute: 1440}})
		next, ok := schedule.NextOpenAt(routingScheduleFixedNow)
		if !ok || !next.Equal(utc(14, 23, 20)) {
			t.Fatalf("expected 23:20 today, got %v ok=%v", next, ok)
		}
		mondayOnly := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 1, StartMinute: 1300, EndMinute: 1400}})
		next, ok = mondayOnly.NextOpenAt(routingScheduleFixedNow)
		if !ok || !next.Equal(time.Date(2023, 11, 20, 21, 40, 0, 0, time.UTC)) {
			t.Fatalf("expected next Monday 21:40, got %v ok=%v", next, ok)
		}
	})

	t.Run("five-minute window is not skipped", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 2, StartMinute: 1340, EndMinute: 1345}})
		next, ok := schedule.NextOpenAt(routingScheduleFixedNow)
		if !ok || !next.Equal(utc(14, 22, 20)) {
			t.Fatalf("expected 22:20, got %v ok=%v", next, ok)
		}
	})

	t.Run("spring-forward Sunday-only window needs the 8-day horizon", func(t *testing.T) {
		schedule := CompileRoutingSchedule("America/New_York", []Window{{WeekdayMask: 64, StartMinute: 135, EndMinute: 165}})
		base := time.Date(2026, 3, 8, 0, 30, 0, 0, mustLoadLocation(t, "America/New_York"))
		next, ok := schedule.NextOpenAt(base)
		if !ok {
			t.Fatalf("expected a next open within the horizon")
		}
		expected := base.Add(168*time.Hour + 45*time.Minute)
		if !next.Equal(expected) {
			t.Fatalf("expected %v (base+168h45m), got %v", expected, next)
		}
		if !schedule.IsOpenAt(next) {
			t.Fatalf("back-check failed: %v not open", next)
		}
	})

	t.Run("unresolved and nil Location report unknown", func(t *testing.T) {
		if _, ok := CompileRoutingSchedule("Not/AZone", []Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 1440}}).NextOpenAt(routingScheduleFixedNow); ok {
			t.Fatalf("expected unresolved to report unknown")
		}
		noLocation := CompiledRoutingSchedule{Windows: []Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 1440}}}
		if _, ok := noLocation.NextOpenAt(routingScheduleFixedNow); ok {
			t.Fatalf("expected nil Location to report unknown without panicking")
		}
	})
}

func TestRoutingScheduleNextCloseAt(t *testing.T) {
	utc := func(day, hour, minute int) time.Time {
		return time.Date(2023, 11, day, hour, minute, 0, 0, time.UTC)
	}

	t.Run("inside a window returns its end", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100}})
		next, ok := schedule.NextCloseAt(utc(13, 17, 10))
		if !ok || !next.Equal(utc(13, 18, 20)) {
			t.Fatalf("expected 18:20, got %v ok=%v", next, ok)
		}
		assertCloseBoundary(t, schedule, next)
	})

	t.Run("adjacent windows join to the true end", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", []Window{
			{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100},
			{WeekdayMask: 1, StartMinute: 1100, EndMinute: 1200},
		})
		next, ok := schedule.NextCloseAt(utc(13, 17, 10))
		if !ok || !next.Equal(utc(13, 20, 0)) {
			t.Fatalf("expected joined end 20:00, got %v ok=%v", next, ok)
		}
		assertCloseBoundary(t, schedule, next)
	})

	t.Run("cross-midnight window ends on the next day", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 1, StartMinute: 1300, EndMinute: 1500}})
		next, ok := schedule.NextCloseAt(utc(13, 23, 0))
		if !ok || !next.Equal(utc(14, 1, 0)) {
			t.Fatalf("expected Tuesday 01:00, got %v ok=%v", next, ok)
		}
		assertCloseBoundary(t, schedule, next)
	})

	t.Run("outside a window or unresolved reports unknown", func(t *testing.T) {
		schedule := CompileRoutingSchedule("UTC", []Window{{WeekdayMask: 1, StartMinute: 1000, EndMinute: 1100}})
		if _, ok := schedule.NextCloseAt(utc(14, 20, 0)); ok {
			t.Fatalf("expected closed schedule to report unknown")
		}
		if _, ok := CompileRoutingSchedule("Not/AZone", []Window{{WeekdayMask: 1, StartMinute: 0, EndMinute: 1440}}).NextCloseAt(utc(14, 17, 0)); ok {
			t.Fatalf("expected unresolved to report unknown")
		}
	})
}

func assertCloseBoundary(t *testing.T, schedule CompiledRoutingSchedule, boundary time.Time) {
	t.Helper()
	if schedule.IsOpenAt(boundary) {
		t.Fatalf("boundary %v must be closed", boundary)
	}
	if !schedule.IsOpenAt(boundary.Add(-time.Minute)) {
		t.Fatalf("minute before boundary %v must be open", boundary)
	}
}

func TestRoutingScheduleCoversFullWeek(t *testing.T) {
	day := func(mask, start, end int) Window {
		return Window{WeekdayMask: mask, StartMinute: start, EndMinute: end}
	}
	cases := []struct {
		name    string
		windows []Window
		want    bool
	}{
		{"canonical 7x24", []Window{day(127, 0, 1440)}, true},
		{"shifted 7x24", []Window{day(127, 1, 1441)}, true},
		{"day plus night join", []Window{day(127, 360, 1320), day(127, 1320, 1800)}, true},
		{"wrap tail completes the week", []Window{day(127, 0, 1439), day(127, 1439, 1440)}, true},
		{"missing one day", []Window{day(126, 0, 1440)}, false},
		{"missing one minute", []Window{day(127, 0, 1439)}, false},
		{"single Sunday overflow", []Window{day(64, 1380, 1500)}, false},
		{"zero windows", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WindowsCoverFullWeek(tc.windows); got != tc.want {
				t.Fatalf("WindowsCoverFullWeek = %v, want %v", got, tc.want)
			}
			if got := CompileRoutingSchedule("UTC", tc.windows).CoversFullWeek(); got != tc.want {
				t.Fatalf("CompiledRoutingSchedule.CoversFullWeek = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateRoutingSchedule(t *testing.T) {
	valid := []Window{{WeekdayMask: 1, StartMinute: 540, EndMinute: 1080}}
	day := func(mask, start, end int) Window {
		return Window{WeekdayMask: mask, StartMinute: start, EndMinute: end}
	}
	cases := []struct {
		name     string
		timezone string
		windows  []Window
		reason   string
		index    int
		limit    int
	}{
		{"no windows", "UTC", nil, RoutingScheduleReasonNoWindows, -1, 0},
		{"too many windows", "UTC", manyWindows(33), RoutingScheduleReasonTooManyWindows, -1, RoutingScheduleMaxWindows},
		{"timezone required", "", valid, RoutingScheduleReasonTimezoneRequired, -1, 0},
		{"timezone too long", repeatedA(101), valid, RoutingScheduleReasonTimezoneTooLong, -1, RoutingScheduleMaxTimezoneLength},
		{"timezone not allowed", "Local", valid, RoutingScheduleReasonTimezoneNotAllowed, -1, 0},
		{"timezone unknown", "Not/AZone", valid, RoutingScheduleReasonTimezoneUnknown, -1, 0},
		{"weekday mask out of range", "UTC", []Window{day(0, 0, 60)}, RoutingScheduleReasonWeekdayMaskOutOfRange, 0, 0},
		{"start minute out of range", "UTC", []Window{day(1, 1440, 1500)}, RoutingScheduleReasonStartMinuteOutOfRange, 0, 0},
		{"end minute out of range", "UTC", []Window{day(1, 0, 0)}, RoutingScheduleReasonEndMinuteOutOfRange, 0, 0},
		{"end not after start", "UTC", []Window{day(1, 60, 60)}, RoutingScheduleReasonEndMinuteNotAfterStart, 0, 0},
		{"span exceeds one day", "UTC", []Window{day(1, 0, 1441)}, RoutingScheduleReasonSpanExceedsOneDay, 0, 0},
		{"duplicate window", "UTC", []Window{day(1, 60, 120), day(1, 60, 120)}, RoutingScheduleReasonDuplicateWindow, 1, 0},
		{"covers full week", "UTC", []Window{day(127, 0, 1440)}, RoutingScheduleReasonCoversFullWeek, -1, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateRoutingSchedule(tc.timezone, tc.windows)
			if err == nil {
				t.Fatalf("expected reason %q", tc.reason)
			}
			if err.Reason != tc.reason {
				t.Fatalf("expected reason %q, got %q", tc.reason, err.Reason)
			}
			if err.Index != tc.index {
				t.Fatalf("expected index %d, got %d", tc.index, err.Index)
			}
			if err.Limit != tc.limit {
				t.Fatalf("expected limit %d, got %d", tc.limit, err.Limit)
			}
		})
	}

	t.Run("valid schedule passes", func(t *testing.T) {
		if err := ValidateRoutingSchedule("UTC", valid); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		if err := ValidateRoutingSchedule("Asia/Shanghai", []Window{day(64, 1380, 1500)}); err != nil {
			t.Fatalf("expected cross-midnight window to pass, got %v", err)
		}
	})
}

func manyWindows(count int) []Window {
	windows := make([]Window, 0, count)
	for i := 0; i < count; i++ {
		windows = append(windows, Window{WeekdayMask: 1, StartMinute: i % 1440, EndMinute: (i%1440 + 60) % 1441})
	}
	return windows
}

func repeatedA(repeat int) string {
	out := make([]byte, repeat)
	for i := range out {
		out[i] = 'a'
	}
	return string(out)
}

func mustLoadLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("load location %s: %v", name, err)
	}
	return location
}
