import { createContext } from "react";
import type { Locale } from "./format";
import type { Messages } from "./messages/en";

export interface LocaleContextValue {
  locale: Locale;
  messages: Messages;
  setLocale: (locale: Locale) => void;
}

export const LocaleContext = createContext<LocaleContextValue | undefined>(undefined);
