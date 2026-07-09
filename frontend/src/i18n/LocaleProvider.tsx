import {
  useEffect,
  type ReactNode,
} from "react";
import { DEFAULT_LOCALE } from "./format";
import { LocaleContext } from "./locale-context";
import { zhCNMessages } from "./messages";

const LOCALE_CONTEXT_VALUE = {
  locale: DEFAULT_LOCALE,
  messages: zhCNMessages,
};

export function LocaleProvider({
  children,
}: {
  children: ReactNode;
}) {
  useEffect(() => {
    document.documentElement.lang = DEFAULT_LOCALE;
  }, []);

  return <LocaleContext.Provider value={LOCALE_CONTEXT_VALUE}>{children}</LocaleContext.Provider>;
}
