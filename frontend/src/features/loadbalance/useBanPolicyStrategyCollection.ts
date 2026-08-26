import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "@/lib/api";
import {
  getSharedLoadbalanceStrategies,
  setSharedLoadbalanceStrategies,
} from "@/lib/referenceData";
import type { LoadbalanceStrategy } from "@/lib/types";
import {
  initialStrategyFragment,
  type FragmentState,
} from "./strategyFragmentState";

const STRATEGIES_QUERY_KEY = "loadbalance:strategies:v1";

function sortStrategies(strategies: LoadbalanceStrategy[]) {
  return [...strategies].sort((left, right) => {
    if (left.is_default !== right.is_default) return left.is_default ? -1 : 1;
    return left.id - right.id;
  });
}

function readErrorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

export function useBanPolicyStrategyCollection(revision: number) {
  const [strategiesFragment, setStrategiesFragment] = useState<
    FragmentState<LoadbalanceStrategy[]>
  >(() => initialStrategyFragment<LoadbalanceStrategy[]>(STRATEGIES_QUERY_KEY));
  const requestGeneration = useRef(0);

  const commitStrategies = useCallback(
    (updater: (current: LoadbalanceStrategy[]) => LoadbalanceStrategy[]) => {
      setStrategiesFragment((current) => {
        const next = sortStrategies(updater(current.data ?? []));
        setSharedLoadbalanceStrategies(revision, next);
        return {
          ...current,
          phase: next.length === 0 ? "empty" : "ready",
          data: next,
          stale: false,
          error: null,
        };
      });
    },
    [revision],
  );

  const replaceStrategies = useCallback(
    (strategies: LoadbalanceStrategy[], preserveCacheOrder = false) => {
      const next = sortStrategies(strategies);
      setStrategiesFragment({
        phase: next.length === 0 ? "empty" : "ready",
        data: next,
        stale: false,
        lastSuccessfulAt: new Date().toISOString(),
        error: null,
        semanticQueryKey: STRATEGIES_QUERY_KEY,
      });
      setSharedLoadbalanceStrategies(
        revision,
        preserveCacheOrder ? strategies : next,
      );
    },
    [revision],
  );

  const loadStrategy = useCallback(
    (strategyId: number) => api.loadbalanceStrategies.get(strategyId),
    [],
  );

  const refreshStrategiesAfterMutation = useCallback(async () => {
    const refreshed = await getSharedLoadbalanceStrategies(revision);
    replaceStrategies(refreshed, true);
  }, [replaceStrategies, revision]);

  const markReadError = useCallback((error: unknown, fallback: string) => {
    setStrategiesFragment((current) => ({
      ...current,
      phase: "error",
      stale: current.data !== null,
      error: readErrorMessage(error, fallback),
      lastSuccessfulAt: current.lastSuccessfulAt,
    }));
  }, []);

  const refreshStrategies = useCallback(async () => {
    const generation = ++requestGeneration.current;
    setStrategiesFragment((current) => ({
      ...current,
      phase: "loading",
      stale: current.data !== null,
      error: null,
    }));
    try {
      const next = sortStrategies(await getSharedLoadbalanceStrategies(revision));
      if (generation !== requestGeneration.current) return;
      setStrategiesFragment({
        phase: next.length === 0 ? "empty" : "ready",
        data: next,
        stale: false,
        lastSuccessfulAt: new Date().toISOString(),
        error: null,
        semanticQueryKey: STRATEGIES_QUERY_KEY,
      });
    } catch (error) {
      if (generation !== requestGeneration.current) return;
      setStrategiesFragment((current) => ({
        ...current,
        phase: "error",
        stale: current.data !== null,
        error: readErrorMessage(error, "Failed to load routing strategies"),
        lastSuccessfulAt: current.lastSuccessfulAt,
      }));
    }
  }, [revision]);

  useEffect(() => {
    void refreshStrategies();
  }, [refreshStrategies]);

  const defaultsCompleteness = useMemo(() => {
    const strategies = strategiesFragment.data ?? [];
    const canonicalNames = [
      "Default single routing",
      "Default fill-first routing",
      "Default round-robin routing",
    ];
    const existing = canonicalNames.filter((name) =>
      strategies.some((strategy) => strategy.name === name),
    );
    const missing = canonicalNames.filter((name) => !existing.includes(name));
    return {
      complete: missing.length === 0,
      missing,
      existingCount: existing.length,
    };
  }, [strategiesFragment.data]);

  return {
    commitStrategies,
    defaultsCompleteness,
    loadStrategy,
    markReadError,
    refreshStrategies,
    refreshStrategiesAfterMutation,
    replaceStrategies,
    strategiesFragment,
  };
}
