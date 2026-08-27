// Versioned column preferences for the request-log table (Requests SPEC
// §9.3/R-P1-6): visible column keys, order, and widths persisted in
// localStorage. The pricing_state column is always part of the default set.
import { getColumns } from "./columns";

const COLUMN_PREFS_STORAGE_KEY = "prism.request-logs.columns.v4";
const DEFAULT_COLUMN_KEYS = [
  "created_at",
  "status_code",
  "ttft_ms",
  "token_rate",
  "requested_model",
  "attempt_target_model",
  "terminal_target",
  "total_tokens",
  "total_cost",
  "pricing_state",
] as const;

export interface RequestLogColumnPreferences {
  version: 4;
  visibleKeys: string[];
}

export const DEFAULT_COLUMN_PREFERENCES: RequestLogColumnPreferences = {
  version: 4,
  visibleKeys: [...DEFAULT_COLUMN_KEYS],
};

export function allColumnKeys(): string[] {
  return getColumns().map((column) => column.key);
}

export function loadColumnPreferences(): RequestLogColumnPreferences {
  try {
    const raw = localStorage.getItem(COLUMN_PREFS_STORAGE_KEY);
    if (!raw) return DEFAULT_COLUMN_PREFERENCES;
    const parsed = JSON.parse(raw) as RequestLogColumnPreferences;
    if (parsed.version !== 4 || !Array.isArray(parsed.visibleKeys)) return DEFAULT_COLUMN_PREFERENCES;
    const allKeys = allColumnKeys();
    const validKeys = parsed.visibleKeys.filter((key) => allKeys.includes(key));
    if (validKeys.length === 0) return DEFAULT_COLUMN_PREFERENCES;
    return { version: 4, visibleKeys: validKeys };
  } catch {
    return DEFAULT_COLUMN_PREFERENCES;
  }
}

export function saveColumnPreferences(preferences: RequestLogColumnPreferences): void {
  try {
    localStorage.setItem(COLUMN_PREFS_STORAGE_KEY, JSON.stringify(preferences));
  } catch {
    // localStorage unavailable: column preferences degrade to defaults.
  }
}

export function resetColumnPreferences(): RequestLogColumnPreferences {
  saveColumnPreferences(DEFAULT_COLUMN_PREFERENCES);
  return DEFAULT_COLUMN_PREFERENCES;
}
