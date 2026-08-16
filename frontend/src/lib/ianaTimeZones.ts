/**
 * Shared IANA timezone helpers.
 *
 * Deliberately constants and pure functions only, never a shared
 * `<TimezoneSelect>` component. The settings page binds its select to
 * `timezone_preference`, which only affects how timestamps are displayed; a
 * Terminal Target's routing schedule binds a routing clock that changes which
 * upstream serves traffic. Sharing a component would couple the two meanings
 * and invite one to be changed while the operator was thinking about the other.
 */

/** Every timezone this runtime can resolve, or an empty list when unsupported. */
export function supportedTimezones(): string[] {
  try {
    return Intl.supportedValuesOf("timeZone");
  } catch {
    return [];
  }
}

export function isSupportedTimezone(timezone: string): boolean {
  if (!timezone) return false;
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: timezone });
    return true;
  } catch {
    return false;
  }
}

/** Offset in minutes east of UTC for a zone at a given instant. */
export function timeZoneOffsetMinutes(timezone: string, instantMs: number): number {
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: timezone,
      timeZoneName: "longOffset",
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    }).formatToParts(new Date(instantMs));
    const offsetPart = parts.find((part) => part.type === "timeZoneName")?.value ?? "";
    const match = offsetPart.match(/GMT([+-])(\d{2}):(\d{2})/);
    if (!match) {
      return 0;
    }
    const sign = match[1] === "-" ? -1 : 1;
    return sign * (Number(match[2]) * 60 + Number(match[3]));
  } catch {
    return 0;
  }
}

/** `Asia/Shanghai (UTC+08:00)`, falling back to the bare name. */
export function timezoneLabel(timezone: string): string {
  try {
    const offset = timeZoneOffsetMinutes(timezone, Date.now());
    const sign = offset >= 0 ? "+" : "-";
    const abs = Math.abs(offset);
    const hours = Math.floor(abs / 60);
    const minutes = abs % 60;
    return `${timezone} (UTC${sign}${String(hours).padStart(2, "0")}:${String(minutes).padStart(2, "0")})`;
  } catch {
    return timezone;
  }
}
