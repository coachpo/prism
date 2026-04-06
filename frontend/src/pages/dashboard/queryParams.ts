export const DASHBOARD_TAB_OPTIONS = ["overview", "analytics"] as const;
export type DashboardTab = (typeof DASHBOARD_TAB_OPTIONS)[number];

export const DEFAULTS = {
  tab: "overview" as DashboardTab,
} as const;

export interface DashboardPageState {
  tab: DashboardTab;
}

function parseEnum<T extends string>(value: string | null, allowed: readonly T[], fallback: T): T {
  if (value && (allowed as readonly string[]).includes(value)) {
    return value as T;
  }

  return fallback;
}

export function parsePageState(params: URLSearchParams): DashboardPageState {
  return {
    tab: parseEnum(params.get("tab"), DASHBOARD_TAB_OPTIONS, DEFAULTS.tab),
  };
}

export function stateToParams(state: DashboardPageState): URLSearchParams {
  const params = new URLSearchParams();

  params.set("tab", state.tab);

  return params;
}
