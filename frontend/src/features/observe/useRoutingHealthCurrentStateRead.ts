import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "@/lib/api";
import type {
  GlobalCurrentStateResponse,
  LoadbalanceCurrentStateResetResponse,
} from "@/lib/types";
import {
  beginPagedRead,
  commitPagedRead,
  failPagedRead,
  initialPagedListState,
  type PageReadKind,
  type PagedListState,
} from "@/shared/table/paginationStates";
import {
  type RoutingHealthSearch,
  type RoutingHealthSearchUpdater,
} from "./routingHealthSearch";

interface UseRoutingHealthCurrentStateReadInput {
  loadFailedMessage: string;
  onSearchChange: RoutingHealthSearchUpdater;
  search: RoutingHealthSearch;
}

export function useRoutingHealthCurrentStateRead({
  loadFailedMessage,
  onSearchChange,
  search,
}: UseRoutingHealthCurrentStateReadInput) {
  const modelId = (search.runtime_model_id as string) || undefined;
  const states = useMemo(
    () =>
      Array.isArray(search.runtime_state)
        ? (search.runtime_state as string[])
        : search.runtime_state
          ? [search.runtime_state as string]
          : [],
    [search.runtime_state],
  );
  const endpointId = (search.runtime_endpoint_id as string) || undefined;
  const targetId = (search.runtime_terminal_target_id as string) || undefined;
  const cursor = (search.runtime_cursor as string) || undefined;
  const semanticKey = JSON.stringify({
    modelId,
    states: [...states].sort(),
    endpointId,
    targetId,
    cursor,
  });
  const [fragment, setFragment] = useState<
    PagedListState<GlobalCurrentStateResponse>
  >(() => initialPagedListState<GlobalCurrentStateResponse>());
  const [cursorStack, setCursorStack] = useState<string[]>([]);
  const generation = useRef(0);
  const abortRef = useRef<AbortController | null>(null);

  const load = useCallback(
    async (kind: PageReadKind) => {
      const current = ++generation.current;
      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;
      setFragment((state) => beginPagedRead(state, kind));
      try {
        const response = await api.loadbalance.listCurrentState(
          {
            model_id: modelId,
            state: states.length > 0 ? (states as never) : undefined,
            endpoint_id: endpointId ? Number(endpointId) : undefined,
            terminal_target_id: targetId ? Number(targetId) : undefined,
            limit: 50,
            cursor,
          },
          controller.signal,
        );
        if (current !== generation.current || controller.signal.aborted) return;
        const phase = response.items.length === 0 ? "empty" : "ready";
        setFragment((state) =>
          commitPagedRead(state, response, phase, semanticKey),
        );
      } catch (error) {
        if (current !== generation.current || controller.signal.aborted) return;
        setFragment((state) =>
          failPagedRead(
            state,
            error instanceof Error ? error.message : loadFailedMessage,
          ),
        );
      }
    },
    [
      cursor,
      endpointId,
      loadFailedMessage,
      modelId,
      semanticKey,
      states,
      targetId,
    ],
  );

  useEffect(() => {
    void load(fragment.data === null ? "initial" : "replace");
    return () => abortRef.current?.abort();
    // The committed fragment is intentionally excluded: the read kind is
    // selected before the request changes it.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [load]);

  const updateSearch = useCallback(
    (patch: RoutingHealthSearch) => {
      setCursorStack([]);
      onSearchChange({ ...patch, runtime_cursor: undefined });
    },
    [onSearchChange],
  );

  const goPreviousPage = useCallback(() => {
    const nextStack = cursorStack.slice(0, -1);
    setCursorStack(nextStack);
    onSearchChange({ runtime_cursor: nextStack.at(-1) });
  }, [cursorStack, onSearchChange]);

  const goNextPage = useCallback(() => {
    const next = fragment.data?.next_cursor;
    if (!next) return;
    setCursorStack((stack) => [...stack, next]);
    onSearchChange({ runtime_cursor: next });
  }, [fragment.data?.next_cursor, onSearchChange]);

  const applyResetSnapshot = useCallback(
    (
      targetIdValue: number,
      response: LoadbalanceCurrentStateResetResponse,
    ) => {
      setFragment((current) => {
        if (!current.data || !response.state) return current;
        const snapshot = response.state;
        return {
          ...current,
          data: {
            ...current.data,
            items: current.data.items.map((item) => {
              if (item.terminal_target.id !== targetIdValue) return item;
              return {
                ...item,
                observation_state: "observed" as const,
                state: snapshot.state,
                available: snapshot.state === "available",
                cycle_retry_attempts: snapshot.cycle_retry_attempts,
                cumulative_retry_attempts:
                  snapshot.cumulative_retry_attempts,
                next_retry_at: snapshot.next_retry_at,
                last_retry_delay_ms: snapshot.last_retry_delay_ms,
                ban_mode: snapshot.ban_mode,
                banned_until_at: snapshot.banned_until_at,
                last_failure_kind: snapshot.last_failure_kind,
                updated_at: snapshot.updated_at,
              };
            }),
          },
        };
      });
    },
    [],
  );

  return {
    applyResetSnapshot,
    cursor,
    cursorStack,
    endpointId,
    fragment,
    goNextPage,
    goPreviousPage,
    load,
    modelId,
    states,
    targetId,
    updateSearch,
  };
}
