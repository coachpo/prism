export const DASHBOARD_TAB_OPTIONS = ["overview", "analytics"] as const;
export type DashboardTab = (typeof DASHBOARD_TAB_OPTIONS)[number];

export const DEFAULTS = {
  tab: "overview" as DashboardTab,
} as const;

export interface DashboardPageState {
  tab: DashboardTab;
}

function parseEnum<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
  const normalized = value == null ? "" : String(value);

  if (normalized && (allowed as readonly string[]).includes(normalized)) {
    return normalized as T;
  }

  return fallback;
}

export function parsePageSearch(search: Record<string, unknown>): DashboardPageState {
  return {
    tab: parseEnum(search.tab, DASHBOARD_TAB_OPTIONS, DEFAULTS.tab),
  };
}

export function parsePageState(params: URLSearchParams): DashboardPageState {
  return parsePageSearch(Object.fromEntries(params));
}

export function stateToSearch(state: DashboardPageState): Record<string, string> {
  return { tab: state.tab };
}

export function stateToParams(state: DashboardPageState): URLSearchParams {
  return new URLSearchParams(stateToSearch(state));
}
