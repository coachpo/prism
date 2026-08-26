import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { api } from "@/lib/api";
import { isReferenceIntegrityError } from "@/lib/api/endpointErrors";
import { ApiError } from "@/lib/api/request";
import type { EndpointReferenceSummary } from "@/lib/types";

// Unknown/stale never equals zero: callers must not coerce non-ready state to
// an empty summary. The generation object is supplied by the reference
// coordinator so detail reads can fence batch results for the same Endpoint.

export type EndpointReferenceSummaryState =
  | { status: "loading"; generation: number }
  | {
      status: "ready";
      value: EndpointReferenceSummary;
      generation: number;
      receivedAt: number;
    }
  | {
      status: "stale";
      value: EndpointReferenceSummary;
      error: ApiError;
      generation: number;
      receivedAt: number;
    }
  | { status: "error"; error: ApiError; generation: number };

export interface EndpointReferenceGeneration {
  issue: (endpointId: number) => number;
  issueBatch: (endpointIds: readonly number[]) => Record<number, number>;
  isCurrent: (endpointId: number, generation: number) => boolean;
  remove: (endpointId: number) => void;
}

const BATCH_CHUNK_SIZE = 100;
const MAX_CONCURRENT_BATCHES = 3;

function initialSummaryState(): EndpointReferenceSummaryState {
  return { status: "loading", generation: 0 };
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

export function summaryFor(
  summary: EndpointReferenceSummaryState | undefined,
): EndpointReferenceSummary | null {
  if (!summary) return null;
  if (summary.status === "ready" || summary.status === "stale") {
    return summary.value;
  }
  return null;
}

export function useEndpointReferenceSummaries(
  endpointIds: readonly number[],
  generation: EndpointReferenceGeneration,
) {
  const [summaries, setSummaries] = useState<
    Record<number, EndpointReferenceSummaryState>
  >({});
  const [inFlight, setInFlight] = useState(false);
  const [retryNonce, setRetryNonce] = useState(0);
  const lastRequested = useRef<number[]>([]);

  const uniqueIds = useMemo(
    () => Array.from(new Set(endpointIds)),
    [endpointIds],
  );

  // Each chunk replaces all of its current-generation rows in one state
  // update. A late chunk can therefore neither partially replace a newer
  // generation nor leave a mixed fresh/error view for one response.
  const fetchChunk = useCallback(
    async (ids: number[], generations: Record<number, number>) => {
      try {
        const response = await api.endpoints.referencesBatch(ids);
        const byId = new Map(
          response.items.map((item) => [item.endpoint_id, item.summary]),
        );
        const now = Date.now();
        setSummaries((current) => {
          const next = { ...current };
          for (const id of ids) {
            const itemGeneration = generations[id];
            if (!generation.isCurrent(id, itemGeneration)) continue;
            const summary = byId.get(id);
            if (!summary) {
              next[id] = {
                status: "error",
                error: new ApiError("Missing reference summary item", 500, {
                  code: "reference_missing_item",
                }),
                generation: itemGeneration,
              };
            } else {
              next[id] = {
                status: "ready",
                value: summary,
                generation: itemGeneration,
                receivedAt: now,
              };
            }
          }
          return next;
        });
      } catch (error) {
        const apiError = asReferenceError(error);
        setSummaries((current) => {
          const next = { ...current };
          for (const id of ids) {
            const itemGeneration = generations[id];
            if (!generation.isCurrent(id, itemGeneration)) continue;
            const previous = current[id];
            if (
              previous &&
              (previous.status === "ready" || previous.status === "stale")
            ) {
              next[id] = {
                status: "stale",
                value: previous.value,
                error: apiError,
                generation: itemGeneration,
                receivedAt: previous.receivedAt,
              };
            } else {
              next[id] = {
                status: "error",
                error: apiError,
                generation: itemGeneration,
              };
            }
          }
          return next;
        });
      }
    },
    [generation],
  );

  // Visible Endpoint membership owns batch reconciliation. Detail membership
  // is reconciled by the detail owner, so a summary refresh cannot erase an
  // expanded snapshot.
  useEffect(() => {
    const previous = lastRequested.current;
    lastRequested.current = uniqueIds;
    const removed = previous.filter((id) => !uniqueIds.includes(id));
    for (const id of removed) {
      generation.remove(id);
    }
    if (removed.length > 0) {
      setSummaries((current) => {
        const next = { ...current };
        for (const id of removed) delete next[id];
        return next;
      });
    }

    setSummaries((current) => {
      const next = { ...current };
      let changed = false;
      for (const id of uniqueIds) {
        if (!next[id]) {
          next[id] = initialSummaryState();
          changed = true;
        }
      }
      return changed ? next : current;
    });

    const generations = generation.issueBatch(uniqueIds);
    const chunks: number[][] = [];
    for (let index = 0; index < uniqueIds.length; index += BATCH_CHUNK_SIZE) {
      chunks.push(uniqueIds.slice(index, index + BATCH_CHUNK_SIZE));
    }
    if (chunks.length === 0) {
      setInFlight(false);
      return;
    }

    setInFlight(true);
    let active = 0;
    let cursor = 0;
    const startNext = () => {
      while (active < MAX_CONCURRENT_BATCHES && cursor < chunks.length) {
        const chunk = chunks[cursor];
        cursor += 1;
        active += 1;
        void fetchChunk(chunk, generations).finally(() => {
          active -= 1;
          if (cursor < chunks.length) {
            startNext();
          } else if (active === 0) {
            setInFlight(false);
          }
        });
      }
    };
    startNext();
    // The joined key intentionally tracks membership rather than array
    // identity; the generation object fences all asynchronous results.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [uniqueIds.join(","), generation, fetchChunk, retryNonce]);

  const replaceFromDetail = useCallback(
    (
      endpointId: number,
      itemGeneration: number,
      summary: EndpointReferenceSummary,
    ) => {
      if (!generation.isCurrent(endpointId, itemGeneration)) return;
      setSummaries((current) => {
        if (!generation.isCurrent(endpointId, itemGeneration)) return current;
        return {
          ...current,
          [endpointId]: {
            status: "ready",
            value: summary,
            generation: itemGeneration,
            receivedAt: Date.now(),
          },
        };
      });
    },
    [generation],
  );

  const addEndpoint = useCallback(
    (endpointId: number) => {
      const itemGeneration = generation.issue(endpointId);
      setSummaries((current) => ({
        ...current,
        [endpointId]: { status: "loading", generation: itemGeneration },
      }));
    },
    [generation],
  );

  const removeEndpoint = useCallback(
    (endpointId: number) => {
      generation.remove(endpointId);
      setSummaries((current) => {
        const next = { ...current };
        delete next[endpointId];
        return next;
      });
    },
    [generation],
  );

  const retry = useCallback(() => {
    const retryGenerations = generation.issueBatch(uniqueIds);
    setSummaries((current) => {
      const next = { ...current };
      for (const id of uniqueIds) {
        next[id] = {
          status: "loading",
          generation: retryGenerations[id],
        };
      }
      return next;
    });
    setRetryNonce((current) => current + 1);
  }, [generation, uniqueIds]);

  const hasUnknownOrStale = useMemo(
    () =>
      uniqueIds.some((id) => {
        const summary = summaries[id];
        return !summary || summary.status !== "ready";
      }),
    [summaries, uniqueIds],
  );

  const hasIntegrityError = useMemo(
    () =>
      uniqueIds.some((id) => {
        const summary = summaries[id];
        return (
          summary?.status === "error" && isReferenceIntegrityError(summary.error)
        );
      }),
    [summaries, uniqueIds],
  );

  const hasReferenceError = useMemo(
    () =>
      uniqueIds.some((id) => {
        const summary = summaries[id];
        return summary?.status === "error" || summary?.status === "stale";
      }),
    [summaries, uniqueIds],
  );

  return {
    addEndpoint,
    hasIntegrityError,
    hasReferenceError,
    hasUnknownOrStale,
    inFlight,
    removeEndpoint,
    replaceFromDetail,
    retry,
    summaries,
  };
}

export function isReferenceSummaryFreshReady(
  summary: EndpointReferenceSummaryState | undefined,
): summary is {
  status: "ready";
  value: EndpointReferenceSummary;
  generation: number;
  receivedAt: number;
} {
  return Boolean(summary && summary.status === "ready");
}
