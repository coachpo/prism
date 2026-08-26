import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "@/lib/api";
import { isReferenceIntegrityError } from "@/lib/api/endpointErrors";
import { ApiError } from "@/lib/api/request";
import type {
  EndpointReferenceDetail,
  EndpointReferenceItem,
  EndpointReferenceSummary,
} from "@/lib/types";
import type { EndpointReferenceGeneration } from "./useEndpointReferenceSummaries";

export type EndpointReferenceDetailState =
  | { status: "idle" }
  /** An in-flight read. `previous` carries the committed page an append extends, so the list never collapses while loading. */
  | {
      status: "loading";
      generation: number;
      previous?: EndpointReferencePagedSnapshot;
    }
  | {
      status: "ready";
      value: EndpointReferencePagedSnapshot;
      generation: number;
      receivedAt: number;
    }
  | {
      status: "stale";
      value: EndpointReferencePagedSnapshot;
      error: ApiError;
      generation: number;
      receivedAt: number;
    }
  | { status: "error"; error: ApiError; generation: number };

export interface EndpointReferencePagedSnapshot {
  summary: EndpointReferenceSummary;
  loaded_items: EndpointReferenceItem[];
  total_count: number;
  next_cursor: string | null;
  reference_snapshot_hash: string;
}

function initialDetailState(): EndpointReferenceDetailState {
  return { status: "idle" };
}

function pageToSnapshot(
  detail: EndpointReferenceDetail,
): EndpointReferencePagedSnapshot {
  return {
    summary: detail.summary,
    loaded_items: detail.reference_page.items,
    total_count: detail.reference_page.total_count,
    next_cursor: detail.reference_page.next_cursor,
    reference_snapshot_hash: detail.reference_page.reference_snapshot_hash,
  };
}

function asReferenceError(error: unknown): ApiError {
  return error instanceof ApiError
    ? error
    : new ApiError(
        error instanceof Error ? error.message : "Failed to load references",
        0,
        null,
      );
}

/**
 * Append one reference page to the accumulated list, deduplicating by stable
 * connection identity. Overlapping cursor pages must never render one
 * referencing model twice.
 */
export function mergeReferenceItems(
  existing: readonly EndpointReferenceItem[],
  incoming: readonly EndpointReferenceItem[],
): EndpointReferenceItem[] {
  const seen = new Set(existing.map((item) => item.connection_id));
  return [
    ...existing,
    ...incoming.filter((item) => {
      if (seen.has(item.connection_id)) return false;
      seen.add(item.connection_id);
      return true;
    }),
  ];
}

export function useEndpointReferenceDetails(
  endpointIds: readonly number[],
  generation: EndpointReferenceGeneration,
  replaceSummary: (
    endpointId: number,
    generation: number,
    summary: EndpointReferenceSummary,
  ) => void,
) {
  const [details, setDetails] = useState<
    Record<number, EndpointReferenceDetailState>
  >({});
  const detailLoadsInFlight = useRef<Set<number>>(new Set());
  const lastRequested = useRef<number[]>([]);

  const uniqueIds = useMemo(
    () => Array.from(new Set(endpointIds)),
    [endpointIds],
  );

  useEffect(() => {
    const previous = lastRequested.current;
    lastRequested.current = uniqueIds;
    const removed = previous.filter((id) => !uniqueIds.includes(id));
    setDetails((current) => {
      const next = { ...current };
      for (const id of removed) delete next[id];
      for (const id of uniqueIds) {
        if (!next[id]) next[id] = initialDetailState();
      }
      return removed.length > 0 || uniqueIds.some((id) => !current[id])
        ? next
        : current;
    });
  }, [uniqueIds]);

  const commitDetailIfCurrent = useCallback(
    (
      endpointId: number,
      itemGeneration: number,
      updater: (
        current: Record<number, EndpointReferenceDetailState>,
      ) => Record<number, EndpointReferenceDetailState>,
    ) => {
      setDetails((current) => {
        if (!generation.isCurrent(endpointId, itemGeneration)) return current;
        return updater(current);
      });
    },
    [generation],
  );

  // A disclosure read is also the authoritative summary replacement for its
  // Endpoint. The coordinator supplies the shared generation fence so a late
  // detail response cannot overwrite a newer batch or mutation snapshot.
  const loadDetail = useCallback(
    async (endpointId: number): Promise<EndpointReferenceDetail | null> => {
      const itemGeneration = generation.issue(endpointId);
      setDetails((current) => ({
        ...current,
        [endpointId]: { status: "loading", generation: itemGeneration },
      }));
      try {
        const detail = await api.endpoints.referencesDetail(endpointId);
        replaceSummary(endpointId, itemGeneration, detail.summary);
        commitDetailIfCurrent(endpointId, itemGeneration, (current) => ({
          ...current,
          [endpointId]: {
            status: "ready",
            value: pageToSnapshot(detail),
            generation: itemGeneration,
            receivedAt: Date.now(),
          },
        }));
        if (!generation.isCurrent(endpointId, itemGeneration)) return null;
        return detail;
      } catch (error) {
        const apiError = asReferenceError(error);
        commitDetailIfCurrent(endpointId, itemGeneration, (current) => {
          const previous = current[endpointId];
          if (
            previous &&
            (previous.status === "ready" || previous.status === "stale")
          ) {
            return {
              ...current,
              [endpointId]: {
                status: "stale",
                value: previous.value,
                error: apiError,
                generation: itemGeneration,
                receivedAt: previous.receivedAt,
              },
            };
          }
          return {
            ...current,
            [endpointId]: {
              status: "error",
              error: apiError,
              generation: itemGeneration,
            },
          };
        });
        return null;
      }
    },
    [commitDetailIfCurrent, generation, replaceSummary],
  );

  // Adopt a detail response already fetched by a delete preflight. Keeping
  // this path in the detail owner prevents the dialog and disclosure from
  // maintaining competing snapshots for one Endpoint.
  const adoptDetail = useCallback(
    (endpointId: number, detail: EndpointReferenceDetail) => {
      const itemGeneration = generation.issue(endpointId);
      const receivedAt = Date.now();
      replaceSummary(endpointId, itemGeneration, detail.summary);
      commitDetailIfCurrent(endpointId, itemGeneration, (current) => ({
        ...current,
        [endpointId]: {
          status: "ready",
          value: pageToSnapshot(detail),
          generation: itemGeneration,
          receivedAt,
        },
      }));
    },
    [commitDetailIfCurrent, generation, replaceSummary],
  );

  // Load more stays on the same opaque reference snapshot. A cursor/hash
  // mismatch discards accumulated pages and starts a fresh first page; a
  // transient failure keeps the prior page visible and retries that cursor.
  const loadMore = useCallback(
    async (endpointId: number): Promise<EndpointReferenceDetail | null> => {
      const current = details[endpointId];
      if (
        !current ||
        (current.status !== "ready" && current.status !== "stale") ||
        !current.value.next_cursor
      ) {
        return null;
      }
      if (detailLoadsInFlight.current.has(endpointId)) return null;

      detailLoadsInFlight.current.add(endpointId);
      const itemGeneration = generation.issue(endpointId);
      const snapshot = current.value;
      setDetails((previous) => ({
        ...previous,
        [endpointId]: {
          status: "loading",
          generation: itemGeneration,
          previous: snapshot,
        },
      }));

      try {
        const detail = await api.endpoints.referencesDetail(
          endpointId,
          snapshot.next_cursor ? { cursor: snapshot.next_cursor } : undefined,
        );
        if (
          detail.reference_page.reference_snapshot_hash !==
            snapshot.reference_snapshot_hash ||
          detail.reference_page.total_count !== snapshot.total_count
        ) {
          // Snapshot changed under us: discard accumulated pages and restart
          // from page one instead of leaving a permanent loading state.
          commitDetailIfCurrent(endpointId, itemGeneration, (previous) => ({
            ...previous,
            [endpointId]: { status: "loading", generation: itemGeneration },
          }));
          return await loadDetail(endpointId);
        }

        const merged: EndpointReferencePagedSnapshot = {
          summary: detail.summary,
          loaded_items: mergeReferenceItems(
            snapshot.loaded_items,
            detail.reference_page.items,
          ),
          total_count: detail.reference_page.total_count,
          next_cursor: detail.reference_page.next_cursor,
          reference_snapshot_hash:
            detail.reference_page.reference_snapshot_hash,
        };
        replaceSummary(endpointId, itemGeneration, detail.summary);
        commitDetailIfCurrent(endpointId, itemGeneration, (previous) => ({
          ...previous,
          [endpointId]: {
            status: "ready",
            value: merged,
            generation: itemGeneration,
            receivedAt: Date.now(),
          },
        }));
        if (!generation.isCurrent(endpointId, itemGeneration)) return null;
        return {
          endpoint_id: endpointId,
          summary: detail.summary,
          reference_page: {
            items: merged.loaded_items,
            total_count: merged.total_count,
            next_cursor: merged.next_cursor,
            reference_snapshot_hash: merged.reference_snapshot_hash,
          },
        };
      } catch (error) {
        const apiError = asReferenceError(error);
        if (
          isReferenceIntegrityError(error) ||
          (error instanceof ApiError && error.status === 409)
        ) {
          // Stale/cursor errors discard accumulated pages and restart.
          return await loadDetail(endpointId);
        }

        // Keep the committed rows and the failed cursor available for retry.
        commitDetailIfCurrent(endpointId, itemGeneration, (previous) => ({
          ...previous,
          [endpointId]: {
            status: "stale",
            value: snapshot,
            error: apiError,
            generation: itemGeneration,
            receivedAt: current.receivedAt,
          },
        }));
        return null;
      } finally {
        detailLoadsInFlight.current.delete(endpointId);
      }
    },
    [commitDetailIfCurrent, details, generation, loadDetail, replaceSummary],
  );

  const addEndpoint = useCallback((endpointId: number) => {
    setDetails((current) => ({
      ...current,
      [endpointId]: initialDetailState(),
    }));
  }, []);

  const removeEndpoint = useCallback((endpointId: number) => {
    setDetails((current) => {
      const next = { ...current };
      delete next[endpointId];
      return next;
    });
  }, []);

  return {
    addEndpoint,
    adoptDetail,
    details,
    loadDetail,
    loadMore,
    removeEndpoint,
  };
}

export function isReferenceDetailReady(
  detail: EndpointReferenceDetailState | undefined,
): detail is {
  status: "ready";
  value: EndpointReferencePagedSnapshot;
  generation: number;
  receivedAt: number;
} {
  return Boolean(detail && detail.status === "ready");
}
