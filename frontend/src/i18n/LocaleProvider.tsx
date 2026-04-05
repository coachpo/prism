import {
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { DEFAULT_LOCALE, LOCALE_STORAGE_KEY, resolveInitialLocale, type Locale } from "./format";
import { enMessages, type Messages } from "./messages/en";
import { LocaleContext } from "./locale-context";
import { zhCNMessages } from "./messages/zh-CN";

const MESSAGES_BY_LOCALE: Record<Locale, Messages> = {
  en: enMessages,
  "zh-CN": zhCNMessages,
};

export function LocaleProvider({
  children,
  defaultLocale = DEFAULT_LOCALE,
}: {
  children: ReactNode;
  defaultLocale?: Locale;
}) {
  const [locale, setLocale] = useState<Locale>(() => {
    const storedLocale =
      typeof window !== "undefined" ? window.localStorage.getItem(LOCALE_STORAGE_KEY) : null;
    const browserLanguage = typeof navigator !== "undefined" ? navigator.language : null;

    return resolveInitialLocale({
      browserLanguage,
      defaultLocale,
      storedLocale,
    });
  });

  useEffect(() => {
    document.documentElement.lang = locale;
    window.localStorage.setItem(LOCALE_STORAGE_KEY, locale);
  }, [locale]);

  const value = useMemo(
    () => ({
      locale,
      messages: MESSAGES_BY_LOCALE[locale],
      setLocale,
    }),
    [locale],
  );

  return <LocaleContext.Provider value={value}>{children}</LocaleContext.Provider>;
}
