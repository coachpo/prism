import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "@/lib/api";
import type { StrategyImpactListResponse } from "@/lib/types";
import { mergeStrategyImpactItems } from "./strategyImpact";
import {
  initialStrategyFragment,
  type FragmentState,
} from "./strategyFragmentState";

export interface StrategyImpactState {
  fragment: FragmentState<StrategyImpactListResponse>;
  expanded: boolean;
  /** The cursor of the most recent page attempt; a retry repeats it. */
  lastCursor?: string;
}

export function useStrategyImpactPager(strategyIds: readonly number[]) {
  const [impactStates, setImpactStates] = useState<
    Record<number, StrategyImpactState>
  >({});
  const impactLoadsInFlight = useRef<Set<number>>(new Set());
  const generationByStrategy = useRef<Record<number, number>>({});
  const lastRequested = useRef<number[]>([]);
  const uniqueIds = useMemo(
    () => Array.from(new Set(strategyIds)),
    [strategyIds],
  );

  useEffect(() => {
    const previous = lastRequested.current;
    lastRequested.current = uniqueIds;
    const removed = previous.filter((id) => !uniqueIds.includes(id));
    for (const id of removed) delete generationByStrategy.current[id];
    if (removed.length === 0) return;
    setImpactStates((current) => {
      const next = { ...current };
      for (const id of removed) delete next[id];
      return next;
    });
  }, [uniqueIds]);

  const loadImpactPage = useCallback(
    async (strategyId: number, cursor?: string) => {
      if (impactLoadsInFlight.current.has(strategyId)) return;
      impactLoadsInFlight.current.add(strategyId);
      const generation =
        (generationByStrategy.current[strategyId] ?? 0) + 1;
      generationByStrategy.current[strategyId] = generation;
      const queryKey = "loadbalance:impact:" + strategyId;
      const previousState = impactStates[strategyId];
      const previousData = cursor
        ? previousState?.fragment.data?.items
        : undefined;

      setImpactStates((states) => {
        const previous =
          states[strategyId]?.fragment ??
          initialStrategyFragment<StrategyImpactListResponse>(queryKey);
        return {
          ...states,
          [strategyId]: {
            expanded: true,
            lastCursor: cursor,
            fragment: {
              ...previous,
              phase: "loading",
              error: null,
            },
          },
        };
      });
      try {
        const response = await api.loadbalanceStrategies.impact(
          strategyId,
          cursor ? { limit: 25, cursor } : { limit: 25 },
        );
        setImpactStates((states) => {
          if (generationByStrategy.current[strategyId] !== generation) {
            return states;
          }
          const data: StrategyImpactListResponse = {
            ...response,
            items: mergeStrategyImpactItems(previousData, response.items),
          };
          return {
            ...states,
            [strategyId]: {
              expanded: true,
              lastCursor: cursor,
              fragment: {
                phase: data.items.length === 0 ? "empty" : "ready",
                data,
                stale: false,
                lastSuccessfulAt: new Date().toISOString(),
                error: null,
                semanticQueryKey: queryKey,
              },
            },
          };
        });
      } catch (error) {
        setImpactStates((states) => {
          if (generationByStrategy.current[strategyId] !== generation) {
            return states;
          }
          const previous =
            states[strategyId]?.fragment ??
            initialStrategyFragment<StrategyImpactListResponse>(queryKey);
          const hasLastGoodData =
            previousData !== undefined ||
            (previous.data !== null && previous.data.items.length > 0);
          return {
            ...states,
            [strategyId]: {
              expanded: true,
              lastCursor: cursor,
              fragment: {
                ...previous,
                phase: hasLastGoodData ? "ready" : "error",
                stale: hasLastGoodData,
                error:
                  error instanceof Error
                    ? error.message
                    : "Failed to load attached models",
              },
            },
          };
        });
      } finally {
        impactLoadsInFlight.current.delete(strategyId);
      }
    },
    [impactStates],
  );

  const toggleImpact = useCallback(
    async (strategyId: number) => {
      const current = impactStates[strategyId];
      if (current?.expanded && current.fragment.phase !== "error") {
        setImpactStates((states) => ({
          ...states,
          [strategyId]: { ...states[strategyId], expanded: false },
        }));
        return;
      }
      await loadImpactPage(strategyId);
    },
    [impactStates, loadImpactPage],
  );

  const retryImpact = useCallback(
    async (strategyId: number) => {
      const current = impactStates[strategyId];
      await loadImpactPage(strategyId, current?.lastCursor);
    },
    [impactStates, loadImpactPage],
  );

  const loadMoreImpact = useCallback(
    async (strategyId: number) => {
      const current = impactStates[strategyId];
      if (
        !current?.fragment.data?.next_cursor ||
        current.fragment.phase === "loading"
      ) {
        return;
      }
      await loadImpactPage(strategyId, current.fragment.data.next_cursor);
    },
    [impactStates, loadImpactPage],
  );

  return { impactStates, loadMoreImpact, retryImpact, toggleImpact };
}
