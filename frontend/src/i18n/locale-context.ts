import { createContext } from "react";
import type { Locale } from "./format";
import type { Messages } from "./messages";

export interface LocaleContextValue {
  locale: Locale;
  messages: Messages;
}

export const LocaleContext = createContext<LocaleContextValue | undefined>(undefined);
