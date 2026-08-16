export type Locale = "zh-CN";

export const DEFAULT_LOCALE: Locale = "zh-CN";

export function getCurrentLocale(): Locale {
  return DEFAULT_LOCALE;
}

export function formatNumber(
  value: number,
  locale: Locale = getCurrentLocale(),
  options?: Intl.NumberFormatOptions,
): string {
  return new Intl.NumberFormat(locale, options).format(value);
}

export function formatCompactNumber(value: number, locale: Locale = getCurrentLocale()): string {
  const absoluteValue = Math.abs(value);
  const units: Array<[number, string]> = [
    [1_000_000_000, "B"],
    [1_000_000, "M"],
    [1_000, "K"],
  ];

  for (const [unitValue, suffix] of units) {
    if (absoluteValue >= unitValue) {
      const scaledValue = value / unitValue;
      const maximumFractionDigits = Math.abs(scaledValue) < 10 ? 1 : 0;
      return `${formatNumber(scaledValue, locale, { maximumFractionDigits })}${suffix}`;
    }
  }

  return formatNumber(value, locale, { maximumFractionDigits: 0 });
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
      hour12: false,
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
