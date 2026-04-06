import { useCallback, useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { parsePageState, stateToParams, type DashboardPageState, type DashboardTab } from "./queryParams";

export function useDashboardPageState() {
  const [searchParams, setSearchParams] = useSearchParams();
  const state = useMemo(() => parsePageState(searchParams), [searchParams]);

  useEffect(() => {
    const canonicalParams = stateToParams(state);

    if (canonicalParams.toString() !== searchParams.toString()) {
      setSearchParams(canonicalParams, { replace: true });
    }
  }, [searchParams, setSearchParams, state]);

  const update = useCallback(
    (patch: Partial<DashboardPageState>) => {
      setSearchParams(
        (prev) => {
          const current = parsePageState(prev);
          const next = { ...current, ...patch };
          return stateToParams(next);
        },
        { replace: true },
      );
    },
    [setSearchParams],
  );

  const setTab = useCallback((tab: DashboardTab) => update({ tab }), [update]);

  return {
    state,
    setTab,
  };
}
