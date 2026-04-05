import { useContext, useMemo } from "react";
import { LocaleContext } from "./locale-context";
import {
  formatNumber,
  formatRelativeTimeFromNow,
  formatTimestampForLocale,
} from "./format";

export function useLocale() {
  const context = useContext(LocaleContext);
  if (!context) {
    throw new Error("useLocale must be used within a LocaleProvider");
  }

  const { locale, messages, setLocale } = context;

  return useMemo(
    () => ({
      locale,
      messages,
      setLocale,
      formatNumber: (value: number, options?: Intl.NumberFormatOptions) =>
        formatNumber(value, locale, options),
      formatRelativeTimeFromNow: (isoString: string, now?: number) =>
        formatRelativeTimeFromNow(isoString, locale, now),
      formatTimestamp: (
        timezone: string,
        isoString: string,
        options?: Intl.DateTimeFormatOptions,
      ) => formatTimestampForLocale(locale, timezone, isoString, options),
    }),
    [locale, messages, setLocale],
  );
}
