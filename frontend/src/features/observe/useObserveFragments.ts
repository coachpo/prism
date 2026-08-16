import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError } from "@/lib/api/core";
import { observe, type QueryContextResponse, type UsageSummaryResponse, type DashboardNowResponse } from "@/lib/api/observability";

export type FragmentPhase = "loading" | "ready" | "error";

export interface FragmentState<T> {
  phase: FragmentPhase;
  data: T | null;
  stale: boolean;
  error: string | null;
  /** Parsed Retry-After milliseconds when the server replied 503. */
  retryAfterMs: number | null;
}

function initialFragment<T>(): FragmentState<T> {
  return { phase: "loading", data: null, stale: false, error: null, retryAfterMs: null };
}

/**
 * Maps an unknown error into the fragment error surface: extracts the safe
 * message and preserves the server Retry-After for 503 overload responses so
 * panels can render "暂不可用，请稍后重试" guidance.
 */
export function fragmentErrorFrom(err: unknown): { error: string; retryAfterMs: number | null } {
  if (err instanceof ApiError) {
    return { error: err.message, retryAfterMs: err.retryAfterMs };
  }
  return { error: err instanceof Error ? err.message : String(err), retryAfterMs: null };
}

/**
 * Observe fragment state: query-context, window summary and Now strip load
 * independently. A failure in one fragment never blanks the others and never
 * produces synthetic zeros (per O-P0-1).
 */
export function useObserveFragments(preset: string) {
  const [queryContext, setQueryContext] = useState<FragmentState<QueryContextResponse>>(initialFragment);
  const [summary, setSummary] = useState<FragmentState<UsageSummaryResponse>>(initialFragment);
  const [now, setNow] = useState<FragmentState<DashboardNowResponse>>(initialFragment);
  const generationRef = useRef(0);

  const refresh = useCallback(() => {
    const generation = ++generationRef.current;
    void observe
      .queryContext({ preset })
      .then((context) => {
        if (generationRef.current !== generation) return;
        setQueryContext({ phase: "ready", data: context, stale: false, error: null, retryAfterMs: null });
        void observe
          .usageSummary(context.query_context)
          .then((summaryData) => {
            if (generationRef.current !== generation) return;
            setSummary({ phase: "ready", data: summaryData, stale: false, error: null, retryAfterMs: null });
          })
          .catch((error: unknown) => {
            if (generationRef.current !== generation) return;
            setSummary((previous) => ({ ...previous, phase: "error", stale: previous.data !== null, error: describeError(error), retryAfterMs: error instanceof ApiError ? error.retryAfterMs : null }));
          });
      })
      .catch((error: unknown) => {
        if (generationRef.current !== generation) return;
        setQueryContext((previous) => ({ ...previous, phase: "error", stale: previous.data !== null, error: describeError(error), retryAfterMs: error instanceof ApiError ? error.retryAfterMs : null }));
      });
    void observe
      .dashboardNow()
      .then((nowData) => {
        if (generationRef.current !== generation) return;
        setNow({ phase: "ready", data: nowData, stale: false, error: null, retryAfterMs: null });
      })
      .catch((error: unknown) => {
        if (generationRef.current !== generation) return;
        setNow((previous) => ({ ...previous, phase: "error", stale: previous.data !== null, error: describeError(error), retryAfterMs: error instanceof ApiError ? error.retryAfterMs : null }));
      });
  }, [preset]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { queryContext, summary, now, refresh };
}

export function describeError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
