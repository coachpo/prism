// The draft codec has to agree with the backend validator, or the operator
// hits "the form saved it and the server returned 422". The full-week cases use
// the same inputs as the Go CoversFullWeek tests for exactly that reason.
import { describe, expect, it } from "vitest";

import {
  decodeEndMinute,
  encodeEndMinute,
  parseRoutingScheduleDraft,
  parseWallClockTime,
  routingScheduleCoversFullWeek,
  routingScheduleDraftFromSchedule,
} from "@/pages/model-detail/routingScheduleDraft";

function draftWindow(overrides: Partial<{ weekdayMask: number; start: string; end: string; endsNextDay: boolean }> = {}) {
  return { id: "w1", weekdayMask: 31, start: "09:00", end: "18:00", endsNextDay: false, ...overrides };
}

describe("cross-midnight encoding", () => {
  it("round-trips a same-day window", () => {
    expect(encodeEndMinute(1080, false)).toBe(1080);
    expect(decodeEndMinute(1080)).toEqual({ minuteOfDay: 1080, endsNextDay: false });
  });

  it("encodes next-day 00:00 as 1440 rather than a same-day 24:00 that HH:mm cannot express", () => {
    expect(encodeEndMinute(0, true)).toBe(1440);
    expect(decodeEndMinute(1440)).toEqual({ minuteOfDay: 0, endsNextDay: true });
  });

  it("round-trips a window that crosses midnight", () => {
    const decoded = decodeEndMinute(1800);
    expect(decoded).toEqual({ minuteOfDay: 360, endsNextDay: true });
    expect(encodeEndMinute(decoded.minuteOfDay, decoded.endsNextDay)).toBe(1800);
  });
});

describe("parseWallClockTime", () => {
  it("rejects anything that is not strict HH:mm", () => {
    expect(parseWallClockTime("9:00")).toBeNull();
    expect(parseWallClockTime("24:00")).toBeNull();
    expect(parseWallClockTime("09:60")).toBeNull();
    expect(parseWallClockTime("")).toBeNull();
  });

  it("accepts the boundary values", () => {
    expect(parseWallClockTime("00:00")).toBe(0);
    expect(parseWallClockTime("23:59")).toBe(1439);
  });
});

// Ported verbatim from TestRoutingScheduleCoversFullWeek in
// backend/internal/domain/terminaltarget/routing_schedule_test.go. The two
// implementations must agree on every one of these, otherwise the form accepts
// a schedule the API then rejects with 422.
describe("routingScheduleCoversFullWeek matches the backend table", () => {
  const day = (mask: number, start: number, end: number) => ({
    weekday_mask: mask,
    start_minute: start,
    end_minute: end,
  });
  const cases: Array<[string, ReturnType<typeof day>[], boolean]> = [
    ["canonical 7x24", [day(127, 0, 1440)], true],
    ["shifted 7x24", [day(127, 1, 1441)], true],
    ["day plus night join", [day(127, 360, 1320), day(127, 1320, 1800)], true],
    ["wrap tail completes the week", [day(127, 0, 1439), day(127, 1439, 1440)], true],
    ["missing one day", [day(126, 0, 1440)], false],
    ["missing one minute", [day(127, 0, 1439)], false],
    ["single Sunday overflow", [day(64, 1380, 1500)], false],
    ["zero windows", [], false],
  ];
  it.each(cases)("%s", (_name, windows, want) => {
    expect(routingScheduleCoversFullWeek(windows)).toBe(want);
  });
});

describe("parseRoutingScheduleDraft", () => {
  it("resolves a disabled draft to the clear value", () => {
    expect(parseRoutingScheduleDraft({ enabled: false, timezone: "", windows: [] })).toEqual({ value: null, error: null });
  });

  it("reports the same blocking reasons the server would", () => {
    expect(parseRoutingScheduleDraft({ enabled: true, timezone: "Asia/Shanghai", windows: [] }).error).toEqual({
      reason: "no_windows",
    });
    expect(parseRoutingScheduleDraft({ enabled: true, timezone: "", windows: [draftWindow()] }).error).toEqual({
      reason: "timezone_required",
    });
    expect(
      parseRoutingScheduleDraft({ enabled: true, timezone: "Asia/Shanghai", windows: [draftWindow({ end: "09:00" })] }).error,
    ).toEqual({ reason: "end_minute_not_after_start", windowIndex: 0 });
    expect(
      parseRoutingScheduleDraft({
        enabled: true,
        timezone: "Asia/Shanghai",
        windows: [draftWindow(), { ...draftWindow(), id: "w2" }],
      }).error,
    ).toEqual({ reason: "duplicate_window", windowIndex: 1 });
    expect(
      parseRoutingScheduleDraft({
        enabled: true,
        timezone: "Asia/Shanghai",
        windows: [draftWindow({ weekdayMask: 127, start: "00:00", end: "00:00", endsNextDay: true })],
      }).error,
    ).toEqual({ reason: "covers_full_week" });
  });

  it("emits the wire shape for a valid draft", () => {
    const parsed = parseRoutingScheduleDraft({
      enabled: true,
      timezone: " Asia/Shanghai ",
      windows: [draftWindow({ start: "22:00", end: "06:00", endsNextDay: true })],
    });
    expect(parsed.error).toBeNull();
    expect(parsed.value).toEqual({
      timezone: "Asia/Shanghai",
      windows: [{ weekday_mask: 31, start_minute: 1320, end_minute: 1800 }],
    });
  });

  it("round-trips a stored schedule back through the draft", () => {
    const schedule = { timezone: "Europe/Berlin", windows: [{ weekday_mask: 96, start_minute: 1320, end_minute: 1800 }] };
    const parsed = parseRoutingScheduleDraft(routingScheduleDraftFromSchedule(schedule));
    expect(parsed.value).toEqual(schedule);
  });
});
