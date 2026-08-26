import { api } from "@/lib/api";
import { formatTimestampForLocale, getCurrentLocale, type Locale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

const timezonePreferenceCache = new Map<string, string | null>();
const timezonePreferenceRequestCache = new Map<string, Promise<string | null>>();

export async function getUserTimezonePreference(
  cacheKey: string,
  forceRefresh = false,
): Promise<string | null> {
  if (!forceRefresh && timezonePreferenceCache.has(cacheKey)) {
    return timezonePreferenceCache.get(cacheKey) ?? null;
  }

  if (!forceRefresh) {
    const inFlightRequest = timezonePreferenceRequestCache.get(cacheKey);
    if (inFlightRequest) {
      return inFlightRequest;
    }
  }

  const loadPromise = api.settings.costing
    .get()
    .then((settings) => {
      const preference = settings.timezone_preference ?? null;
      timezonePreferenceCache.set(cacheKey, preference);
      return preference;
    })
    .finally(() => {
      if (timezonePreferenceRequestCache.get(cacheKey) === loadPromise) {
        timezonePreferenceRequestCache.delete(cacheKey);
      }
    });

  timezonePreferenceRequestCache.set(cacheKey, loadPromise);
  return loadPromise;
}

export function clearUserTimezonePreference(cacheKey?: string) {
  if (cacheKey === undefined) {
    timezonePreferenceCache.clear();
    timezonePreferenceRequestCache.clear();
    return;
  }

  timezonePreferenceCache.delete(cacheKey);
  timezonePreferenceRequestCache.delete(cacheKey);
}

export function formatTimestamp(
  isoString: string,
  timezone: string,
  options?: Intl.DateTimeFormatOptions,
  locale: Locale = getCurrentLocale(),
): string {
  return formatTimestampForLocale(locale, timezone, isoString, options);
}

// Current-instant timezone preview (SPEC §11.2): the preview always uses the
// current clock instant and IANA current offset; the fixed 2026-02-27 source
// was removed. The caller supplies the instant so tests stay deterministic.
export const formatTimezoneOffset = (timezone: string, instant: Date = new Date()): string => {
  try {
    const parts = new Intl.DateTimeFormat("en-US", {
      timeZone: timezone,
      timeZoneName: "longOffset",
    }).formatToParts(instant);
    return parts.find((part) => part.type === "timeZoneName")?.value?.replace("GMT", "UTC") ?? "UTC";
  } catch {
    return "UTC";
  }
};

export const formatTimezonePreview = (timezone: string, instant: Date = new Date()): string => {
  const messages = getStaticMessages();
  try {
    const parts = new Intl.DateTimeFormat(getCurrentLocale(), {
      timeZone: timezone,
      year: "numeric",
      month: "2-digit",
      day: "2-digit",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    }).formatToParts(instant);

    const byType = new Map(parts.map((part) => [part.type, part.value]));
    const year = byType.get("year") ?? "0000";
    const month = byType.get("month") ?? "00";
    const day = byType.get("day") ?? "00";
    const hour = byType.get("hour") ?? "00";
    const minute = byType.get("minute") ?? "00";
    return `${year}-${month}-${day} ${hour}:${minute}`;
  } catch {
    return messages.settingsTimezone.unavailable;
  }
};
