/**
 * Routing-schedule draft state and its conversion to and from the wire shape.
 *
 * Pure: no React, no i18n, no clock. In particular this never re-implements
 * "is the window open right now" or "when does it next open" — those live in
 * the backend domain package and are delivered as `routing_schedule_state`. A
 * second implementation of window and DST arithmetic would drift from the one
 * routing actually uses, and the drift would only show up as traffic going
 * somewhere the UI said it would not.
 *
 * The validation order and reasons mirror the backend validator, so a draft the
 * UI accepts is one the API accepts. Any divergence surfaces to the operator as
 * "the form let me save it and the server rejected it".
 */
import type { RoutingSchedule, RoutingScheduleWindow } from "@/lib/types/routing";

export const ROUTING_SCHEDULE_MAX_WINDOWS = 32;
export const MINUTES_PER_DAY = 1440;
const MINUTES_PER_WEEK = 7 * MINUTES_PER_DAY;

export interface RoutingScheduleWindowDraft {
  /** Stable row identity. Never use the array index: deleting a middle row would shift state onto its neighbour. */
  id: string;
  weekdayMask: number;
  start: string;
  end: string;
  endsNextDay: boolean;
}

export interface RoutingScheduleDraft {
  enabled: boolean;
  timezone: string;
  windows: RoutingScheduleWindowDraft[];
}

export type RoutingScheduleDraftReason =
  | "no_windows"
  | "too_many_windows"
  | "timezone_required"
  | "invalid_time"
  | "end_minute_not_after_start"
  | "span_exceeds_one_day"
  | "duplicate_window"
  | "weekday_mask_out_of_range"
  | "covers_full_week";

export interface RoutingScheduleDraftError {
  reason: RoutingScheduleDraftReason;
  windowIndex?: number;
}

let draftRowCounter = 0;

export function newRoutingScheduleWindowDraft(): RoutingScheduleWindowDraft {
  draftRowCounter += 1;
  return { id: `routing-window-${draftRowCounter}`, weekdayMask: 31, start: "09:00", end: "18:00", endsNextDay: false };
}

export function emptyRoutingScheduleDraft(): RoutingScheduleDraft {
  return { enabled: false, timezone: "", windows: [] };
}

/**
 * `end` above 1440 means the window continues into the next day.
 *
 * A same-day 24:00 is deliberately encoded as next-day 00:00: "HH:mm" tops out
 * at 23:59, so the only way to express it is through the next-day flag, and
 * decoding then re-encoding reproduces the server's value exactly.
 */
export function encodeEndMinute(minuteOfDay: number, endsNextDay: boolean): number {
  return endsNextDay ? minuteOfDay + MINUTES_PER_DAY : minuteOfDay;
}

export function decodeEndMinute(value: number): { minuteOfDay: number; endsNextDay: boolean } {
  return value >= MINUTES_PER_DAY
    ? { minuteOfDay: value - MINUTES_PER_DAY, endsNextDay: true }
    : { minuteOfDay: value, endsNextDay: false };
}

/**
 * Strict `HH:mm` parse. No repository code uses `<input type="time">` today, so
 * this is the first such input and cannot assume the browser normalises the
 * value for us.
 */
export function parseWallClockTime(value: string): number | null {
  const match = value.match(/^(\d{2}):(\d{2})$/);
  if (!match) return null;
  const hours = Number(match[1]);
  const minutes = Number(match[2]);
  if (hours > 23 || minutes > 59) return null;
  return hours * 60 + minutes;
}

export function formatWallClockTime(minuteOfDay: number): string {
  const hours = Math.floor(minuteOfDay / 60);
  const minutes = minuteOfDay % 60;
  return `${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")}`;
}

/**
 * Whether the windows together leave no uncovered minute in the week.
 *
 * The bitmap is circular: a window that runs past midnight projects onto the
 * following day modulo the week, so a genuine round-the-clock pair such as
 * 06:00-22:00 plus 22:00-06:00 is recognised instead of being reported as
 * having a hole at the week boundary. This must agree with the backend, which
 * refuses a schedule that covers the whole week (an operator meaning "always
 * available" should clear the schedule instead).
 */
export function routingScheduleCoversFullWeek(windows: RoutingScheduleWindow[]): boolean {
  if (windows.length === 0) return false;
  const covered = new Uint8Array(MINUTES_PER_WEEK);
  for (const window of windows) {
    for (let weekday = 0; weekday < 7; weekday += 1) {
      if ((window.weekday_mask & (1 << weekday)) === 0) continue;
      const base = weekday * MINUTES_PER_DAY;
      for (let minute = window.start_minute; minute < window.end_minute; minute += 1) {
        covered[(base + minute) % MINUTES_PER_WEEK] = 1;
      }
    }
  }
  return covered.every((value) => value === 1);
}

export function routingScheduleDraftFromSchedule(schedule: RoutingSchedule | null): RoutingScheduleDraft {
  if (!schedule) return emptyRoutingScheduleDraft();
  return {
    enabled: true,
    timezone: schedule.timezone,
    windows: schedule.windows.map((window) => {
      const decoded = decodeEndMinute(window.end_minute);
      draftRowCounter += 1;
      return {
        id: `routing-window-${draftRowCounter}`,
        weekdayMask: window.weekday_mask,
        start: formatWallClockTime(window.start_minute),
        end: formatWallClockTime(decoded.minuteOfDay),
        endsNextDay: decoded.endsNextDay,
      };
    }),
  };
}

/**
 * Converts a draft into the wire value, or reports the first blocking problem.
 *
 * A disabled draft resolves to `null`, which is the clear semantics: the field
 * is sent as JSON null and the server drops the timezone and every window row.
 */
export function parseRoutingScheduleDraft(
  draft: RoutingScheduleDraft,
): { value: RoutingSchedule | null; error: null } | { value: null; error: RoutingScheduleDraftError } {
  if (!draft.enabled) return { value: null, error: null };
  if (draft.windows.length === 0) return { value: null, error: { reason: "no_windows" } };
  if (draft.windows.length > ROUTING_SCHEDULE_MAX_WINDOWS) {
    return { value: null, error: { reason: "too_many_windows" } };
  }
  if (!draft.timezone.trim()) return { value: null, error: { reason: "timezone_required" } };

  const windows: RoutingScheduleWindow[] = [];
  for (let index = 0; index < draft.windows.length; index += 1) {
    const row = draft.windows[index];
    if (row.weekdayMask < 1 || row.weekdayMask > 127) {
      return { value: null, error: { reason: "weekday_mask_out_of_range", windowIndex: index } };
    }
    const start = parseWallClockTime(row.start);
    const endOfDay = parseWallClockTime(row.end);
    if (start === null || endOfDay === null) {
      return { value: null, error: { reason: "invalid_time", windowIndex: index } };
    }
    const end = encodeEndMinute(endOfDay, row.endsNextDay);
    if (end <= start) {
      return { value: null, error: { reason: "end_minute_not_after_start", windowIndex: index } };
    }
    if (end - start > MINUTES_PER_DAY) {
      return { value: null, error: { reason: "span_exceeds_one_day", windowIndex: index } };
    }
    windows.push({ weekday_mask: row.weekdayMask, start_minute: start, end_minute: end });
  }

  const seen = new Set<string>();
  for (let index = 0; index < windows.length; index += 1) {
    const key = `${windows[index].weekday_mask}:${windows[index].start_minute}:${windows[index].end_minute}`;
    if (seen.has(key)) return { value: null, error: { reason: "duplicate_window", windowIndex: index } };
    seen.add(key);
  }

  if (routingScheduleCoversFullWeek(windows)) {
    return { value: null, error: { reason: "covers_full_week" } };
  }
  return { value: { timezone: draft.timezone.trim(), windows }, error: null };
}
