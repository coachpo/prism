import { useMemo, useState as useStateLocal } from "react";
import { Checkbox } from "@/components/ui/checkbox";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useLocale } from "@/i18n/useLocale";
import { formatTimestampForLocale, getCurrentLocale } from "@/i18n/format";

/**
 * Timezone-aware expiry field (Proxy Key SPEC §11.3/§11.6).
 *
 * The wall-clock date/time is parsed in the Settings timezone and submitted as
 * an RFC3339 instant. DST gaps block submission with a field error; DST
 * overlaps resolve unambiguously (earlier occurrence by default, with a
 * notice). Create supports "never expires"; edit supports preserve/set/clear.
 */

export interface ResolvedExpiryInput {
  /** RFC3339 instant, or null for never-expires (create) / clear (edit). */
  instant: string | null;
  /** undefined when the edit field is omitted (preserve). */
  preserved: boolean;
  gapError: boolean;
  overlapNotice: boolean;
}

export interface ProxyKeyExpiryFieldProps {
  mode: "create" | "edit";
  timezone: string | null;
  timezoneLoading: boolean;
  /** Current RFC3339 instant (null = never/cleared). */
  currentInstant: string | null;
  disabled?: boolean;
  onChange: (value: ResolvedExpiryInput) => void;
  onClearWallClock?: () => void;
}

function timeZoneOffsetMinutes(timezone: string, instantMs: number): number {
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

function wallClockToInstant(timezone: string, year: number, month: number, day: number, hour: number, minute: number): number {
  // Iterate to converge on the offset (DST transitions shift the wall clock).
  let candidate = Date.UTC(year, month - 1, day, hour, minute);
  for (let iteration = 0; iteration < 3; iteration += 1) {
    const offsetMs = timeZoneOffsetMinutes(timezone, candidate) * 60_000;
    const adjusted = Date.UTC(year, month - 1, day, hour, minute) - offsetMs;
    if (Math.abs(adjusted - candidate) < 60_000) {
      return adjusted;
    }
    candidate = adjusted;
  }
  return candidate;
}

function formatWallClock(timezone: string, instantMs: number): { year: number; month: number; day: number; hour: number; minute: number } {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: timezone,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(new Date(instantMs));
  const get = (type: string) => Number(parts.find((part) => part.type === type)?.value ?? 0);
  return { year: get("year"), month: get("month"), day: get("day"), hour: get("hour") % 24, minute: get("minute") };
}

function timezoneLabel(timezone: string): string {
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

function parseWallClockInput(value: string): { year: number; month: number; day: number; hour: number; minute: number } | null {
  // datetime-local value: YYYY-MM-DDTHH:mm
  const match = value.match(/^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/);
  if (!match) {
    return null;
  }
  return {
    year: Number(match[1]),
    month: Number(match[2]),
    day: Number(match[3]),
    hour: Number(match[4]),
    minute: Number(match[5]),
  };
}

export function ProxyKeyExpiryField({
  mode,
  timezone,
  timezoneLoading,
  currentInstant,
  disabled = false,
  onChange,
}: ProxyKeyExpiryFieldProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const effectiveTimezone = timezone ?? null;
  const tzUnavailable = !timezoneLoading && timezone === null;

  const currentWallClock = useMemo(() => {
    if (!effectiveTimezone || !currentInstant) {
      return "";
    }
    const parsedInstant = new Date(currentInstant).getTime();
    if (Number.isNaN(parsedInstant)) {
      return "";
    }
    const wall = formatWallClock(effectiveTimezone, parsedInstant);
    return `${wall.year}-${String(wall.month).padStart(2, "0")}-${String(wall.day).padStart(2, "0")}T${String(wall.hour).padStart(2, "0")}:${String(wall.minute).padStart(2, "0")}`;
  }, [currentInstant, effectiveTimezone]);

  const [wallClock, setWallClock] = useStateLocal(currentWallClock);
  const [editMode, setEditMode] = useStateLocal<"preserve" | "set" | "clear">(mode === "create" ? "set" : "preserve");
  const [neverExpires, setNeverExpires] = useStateLocal(mode === "create" && currentInstant === null);
  const [error, setError] = useStateLocal<string | null>(null);
  const [overlap, setOverlap] = useStateLocal<string | null>(null);

  const resolve = (nextWallClock: string, nextMode: "preserve" | "set" | "clear", nextNever: boolean): void => {
    setError(null);
    setOverlap(null);
    if (mode === "create" && nextNever) {
      onChange({ instant: null, preserved: false, gapError: false, overlapNotice: false });
      return;
    }
    if (mode === "edit" && nextMode === "preserve") {
      onChange({ instant: undefined as never, preserved: true, gapError: false, overlapNotice: false });
      return;
    }
    if (mode === "edit" && nextMode === "clear") {
      onChange({ instant: null, preserved: false, gapError: false, overlapNotice: false });
      return;
    }
    if (!effectiveTimezone || !nextWallClock) {
      onChange({ instant: null, preserved: mode === "edit" && nextMode === "preserve", gapError: false, overlapNotice: false });
      return;
    }
    const wall = parseWallClockInput(nextWallClock);
    if (!wall) {
      setError(copy.expiryInvalidFormat);
      onChange({ instant: null, preserved: false, gapError: true, overlapNotice: false });
      return;
    }
    const instant = wallClockToInstant(effectiveTimezone, wall.year, wall.month, wall.day, wall.hour, wall.minute);
    // Round-trip check: a DST gap wall time does not exist in the zone.
    const roundTrip = formatWallClock(effectiveTimezone, instant);
    if (
      roundTrip.year !== wall.year ||
      roundTrip.month !== wall.month ||
      roundTrip.day !== wall.day ||
      roundTrip.hour !== wall.hour ||
      roundTrip.minute !== wall.minute
    ) {
      setError(copy.expiryDstGap);
      onChange({ instant: null, preserved: false, gapError: true, overlapNotice: false });
      return;
    }
    // DST overlap: the same wall time maps to two instants (one hour apart).
    // Resolve to the earlier occurrence and surface an explicit notice by
    // checking whether an adjacent instant round-trips to the same wall time.
    const wallMatches = (candidateMs: number) => {
      const candidateWall = formatWallClock(effectiveTimezone, candidateMs);
      return (
        candidateWall.year === wall.year &&
        candidateWall.month === wall.month &&
        candidateWall.day === wall.day &&
        candidateWall.hour === wall.hour &&
        candidateWall.minute === wall.minute
      );
    };
    const overlapDetected = wallMatches(instant - 60 * 60 * 1000) || wallMatches(instant + 60 * 60 * 1000);
    if (overlapDetected) {
      setOverlap(copy.expiryDstOverlapNotice);
    }
    onChange({
      instant: new Date(instant).toISOString(),
      preserved: false,
      gapError: false,
      overlapNotice: overlapDetected,
    });
  };

  const handleWallClockChange = (value: string) => {
    setWallClock(value);
    resolve(value, editMode, neverExpires);
  };

  const handleEditModeChange = (value: "preserve" | "set" | "clear") => {
    setEditMode(value);
    resolve(wallClock, value, neverExpires);
  };

  const handleNeverExpiresChange = (checked: boolean) => {
    setNeverExpires(checked);
    resolve(wallClock, editMode, checked);
  };

  const zoneLabel = effectiveTimezone ? timezoneLabel(effectiveTimezone) : null;
  const showWallClock = mode === "create" ? !neverExpires : editMode === "set";

  return (
    <FieldGroup className="gap-2">
      {zoneLabel ? (
        <p className="text-xs text-muted-foreground">
          {copy.expiryTimezoneLabel} {zoneLabel}
        </p>
      ) : tzUnavailable ? (
        <p className="text-xs text-degraded">{copy.expiryTimezoneUnavailable}</p>
      ) : null}

      {mode === "create" ? (
        <label className="flex items-center gap-2 text-sm">
          <Checkbox checked={neverExpires} onCheckedChange={(checked) => handleNeverExpiresChange(checked === true)} disabled={disabled || timezoneLoading} />
          {copy.neverExpires}
        </label>
      ) : (
        <div className="flex items-center gap-2 text-sm">
          <label className="flex items-center gap-1.5">
            <input
              type="radio"
              name="proxy-key-expiry-mode"
              checked={editMode === "preserve"}
              onChange={() => handleEditModeChange("preserve")}
              disabled={disabled}
            />
            {copy.expiryPreserve}
          </label>
          <label className="flex items-center gap-1.5">
            <input
              type="radio"
              name="proxy-key-expiry-mode"
              checked={editMode === "set"}
              onChange={() => handleEditModeChange("set")}
              disabled={disabled || tzUnavailable}
            />
            {copy.expirySet}
          </label>
          <label className="flex items-center gap-1.5">
            <input
              type="radio"
              name="proxy-key-expiry-mode"
              checked={editMode === "clear"}
              onChange={() => handleEditModeChange("clear")}
              disabled={disabled}
            />
            {copy.clearExpiry}
          </label>
        </div>
      )}

      {showWallClock ? (
        <Field>
          <FieldLabel htmlFor="proxy-key-expiry-datetime">{copy.expiresAt}</FieldLabel>
          <Input
            id="proxy-key-expiry-datetime"
            name="proxy-key-expiry-datetime"
            type="datetime-local"
            value={wallClock}
            onChange={(event) => handleWallClockChange(event.target.value)}
            disabled={disabled || tzUnavailable}
            aria-invalid={error ? true : undefined}
            aria-describedby={error ? "proxy-key-expiry-error" : overlap ? "proxy-key-expiry-overlap" : undefined}
          />
          {error ? (
            <p id="proxy-key-expiry-error" className="text-sm text-destructive" role="alert">
              {error}
            </p>
          ) : null}
          {overlap ? (
            <p id="proxy-key-expiry-overlap" className="text-sm text-degraded">
              {overlap}
            </p>
          ) : null}
        </Field>
      ) : null}

      <FieldDescription>
        {mode === "edit" ? copy.expiryEditDescription : copy.expiresAtDescription}
      </FieldDescription>

      {/* Edit preserve mode keeps current instant untouched. */}
      {mode === "edit" && editMode === "preserve" && currentInstant ? (
        <p className="text-xs text-muted-foreground">
          {copy.expiryCurrentValue} {effectiveTimezone ? formatTimestampInZone(currentInstant, effectiveTimezone) : currentInstant}
        </p>
      ) : null}
    </FieldGroup>
  );
}

function formatTimestampInZone(isoString: string, timezone: string): string {
  return formatTimestampForLocale(getCurrentLocale(), timezone, isoString, {
    year: "numeric",
    month: "numeric",
    day: "numeric",
    hour: "numeric",
    minute: "numeric",
    hour12: false,
  });
}
