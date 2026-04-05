export type Locale = "en" | "zh-CN";

export const DEFAULT_LOCALE: Locale = "en";
export const LOCALE_STORAGE_KEY = "prism.locale";

export function normalizeLocale(candidate: string | null | undefined): Locale {
  const normalized = candidate?.trim().toLowerCase();

  if (normalized === "zh-cn" || normalized?.startsWith("zh")) {
    return "zh-CN";
  }

  return DEFAULT_LOCALE;
}

export function resolveInitialLocale({
  browserLanguage,
  defaultLocale = DEFAULT_LOCALE,
  storedLocale,
}: {
  browserLanguage?: string | null;
  defaultLocale?: Locale;
  storedLocale?: string | null;
} = {}): Locale {
  if (storedLocale) {
    return normalizeLocale(storedLocale);
  }

  if (browserLanguage) {
    return normalizeLocale(browserLanguage);
  }

  return defaultLocale;
}

export function getCurrentLocale(): Locale {
  if (typeof document !== "undefined") {
    return normalizeLocale(document.documentElement.lang);
  }

  if (typeof navigator !== "undefined") {
    return normalizeLocale(navigator.language);
  }

  return DEFAULT_LOCALE;
}

export function formatNumber(
  value: number,
  locale: Locale = getCurrentLocale(),
  options?: Intl.NumberFormatOptions,
): string {
  return new Intl.NumberFormat(locale, options).format(value);
}

export function compareStringsForLocale(
  left: string,
  right: string,
  locale: Locale = getCurrentLocale(),
): number {
  return new Intl.Collator(locale).compare(left, right);
}

export function formatTimestampForLocale(
  locale: Locale,
  timezone: string,
  isoString: string,
  options?: Intl.DateTimeFormatOptions,
): string {
  if (!isoString) {
    return "-";
  }

  try {
    const date = new Date(isoString);
    return new Intl.DateTimeFormat(locale, {
      timeZone: timezone,
      year: "numeric",
      month: "numeric",
      day: "numeric",
      hour: "numeric",
      minute: "numeric",
      second: "numeric",
      hour12: locale === "en",
      ...options,
    }).format(date);
  } catch {
    return isoString;
  }
}

export function formatDateForLocale(
  locale: Locale,
  isoString: string,
  options?: Intl.DateTimeFormatOptions,
): string {
  if (!isoString) {
    return "";
  }

  try {
    const date = new Date(isoString);
    return new Intl.DateTimeFormat(locale, options).format(date);
  } catch {
    return "";
  }
}

export function formatRelativeTimeFromNow(
  isoString: string,
  locale: Locale,
  now = Date.now(),
): string {
  const timestamp = new Date(isoString);
  if (Number.isNaN(timestamp.getTime())) {
    return "";
  }

  const delta = timestamp.getTime() - now;
  const absoluteDelta = Math.abs(delta);
  const formatter = new Intl.RelativeTimeFormat(locale, {
    numeric: "auto",
    style: "short",
  });

  if (absoluteDelta < 60_000) {
    return formatter.format(0, "minute");
  }

  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ["year", 31_536_000_000],
    ["month", 2_592_000_000],
    ["week", 604_800_000],
    ["day", 86_400_000],
    ["hour", 3_600_000],
    ["minute", 60_000],
  ];

  for (const [unit, size] of units) {
    if (absoluteDelta >= size) {
      return formatter.format(Math.round(delta / size), unit);
    }
  }

  return formatter.format(0, "minute");
}
