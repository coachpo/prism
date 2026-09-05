import { useEffect, useState } from "react";
import { useLocale } from "@/i18n/useLocale";
import {
  clearUserTimezonePreference,
  formatTimestamp,
  formatTimestampWithZone,
  getUserTimezonePreference,
} from "@/lib/timezone";

function getBrowserTimezone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
  } catch {
    return "UTC";
  }
}

export function useTimezone() {
  const { locale } = useLocale();
  const [timezone, setTimezone] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const timezoneKey = "1:0";

  useEffect(() => {
    let mounted = true;

    const loadTimezone = async () => {
      setLoading(true);
      try {
        const tz = await getUserTimezonePreference(timezoneKey);
        if (!mounted) return;
        setTimezone(tz ?? getBrowserTimezone());
      } finally {
        if (mounted) {
          setLoading(false);
        }
      }
    };

    loadTimezone();

    return () => {
      mounted = false;
    };
  }, [timezoneKey]);

  const format = (isoString: string, options?: Intl.DateTimeFormatOptions) => {
    const effectiveTimezone = timezone ?? getBrowserTimezone();
    return formatTimestamp(isoString, effectiveTimezone, options, locale);
  };

  // 带偏移量后缀。新鲜度条与详情页的绝对时间用它：同屏并排着后端的 UTC 串时，
  // 不标时区就分不出「差三小时」是数据延迟还是换算差。
  const formatWithZone = (
    isoString: string,
    options?: Intl.DateTimeFormatOptions,
  ) => {
    const effectiveTimezone = timezone ?? getBrowserTimezone();
    return formatTimestampWithZone(isoString, effectiveTimezone, options, locale);
  };

  return {
    timezone,
    format,
    formatWithZone,
    loading,
    refresh: async () => {
      clearUserTimezonePreference(timezoneKey);
      const tz = await getUserTimezonePreference(timezoneKey, true);
      const effectiveTimezone = tz ?? getBrowserTimezone();
      setTimezone(effectiveTimezone);
      return effectiveTimezone;
    },
  };
}
