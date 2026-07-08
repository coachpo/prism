import { useCallback, useEffect, useMemo } from "react";
import { useNavigate, useRouterState, useSearch } from "@tanstack/react-router";
import { parsePageSearch, stateToParams, stateToSearch, type DashboardPageState, type DashboardTab } from "./queryParams";

export function useDashboardPageState() {
  const search = useSearch({ from: "/observe" });
  const location = useRouterState({ select: (state) => state.location });
  const navigate = useNavigate();
  const state = useMemo(() => parsePageSearch({
    ...search,
    ...Object.fromEntries(new URLSearchParams(location.searchStr)),
  }), [location.searchStr, search]);

  useEffect(() => {
    if (location.pathname !== "/observe") {
      return;
    }

    const canonicalParams = stateToParams(state);

    if (canonicalParams.toString() !== new URLSearchParams(location.searchStr).toString()) {
      void navigate({ to: "/observe", search: () => stateToSearch(state), replace: true });
    }
  }, [location.pathname, location.searchStr, navigate, state]);

  const update = useCallback(
    (patch: Partial<DashboardPageState>) => {
      void navigate({ to: "/observe", search: () => stateToSearch({ ...state, ...patch }), replace: true });
    },
    [navigate, state],
  );

  const setTab = useCallback((tab: DashboardTab) => update({ tab }), [update]);

  return {
    state,
    setTab,
  };
}
