import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError } from "@/lib/api/core";
import { observe, type QueryContextResponse, type UsageSummaryResponse, type DashboardNowResponse } from "@/lib/api/observability";
import type { ObserveScope } from "@/features/observe/observeSearch";

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
  const abortRef = useRef<AbortController | null>(null);

  const refresh = useCallback(() => {
    const generation = ++generationRef.current;
    // A superseded read is cancelled, not merely ignored: it holds a server
    // admission slot for as long as it runs, and that slot is what a later
    // read gets rejected for.
    abortRef.current?.abort();
    const controller = new AbortController();
    abortRef.current = controller;
    const signal = controller.signal;
    void observe
      .queryContext({ preset, scope: "ingress" }, signal)
      .then((context) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setQueryContext({ phase: "ready", data: context, stale: false, error: null, retryAfterMs: null });
        void observe
          .usageSummary(context.query_context, signal)
          .then((summaryData) => {
            if (generationRef.current !== generation || signal.aborted) return;
            setSummary({ phase: "ready", data: summaryData, stale: false, error: null, retryAfterMs: null });
          })
          .catch((error: unknown) => {
            if (generationRef.current !== generation || signal.aborted) return;
            setSummary((previous) => ({ ...previous, phase: "error", stale: previous.data !== null, error: describeError(error), retryAfterMs: error instanceof ApiError ? error.retryAfterMs : null }));
          });
      })
      .catch((error: unknown) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setQueryContext((previous) => ({ ...previous, phase: "error", stale: previous.data !== null, error: describeError(error), retryAfterMs: error instanceof ApiError ? error.retryAfterMs : null }));
      });
    void observe
      .dashboardNow(signal)
      .then((nowData) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setNow({ phase: "ready", data: nowData, stale: false, error: null, retryAfterMs: null });
      })
      .catch((error: unknown) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setNow((previous) => ({ ...previous, phase: "error", stale: previous.data !== null, error: describeError(error), retryAfterMs: error instanceof ApiError ? error.retryAfterMs : null }));
      });
  }, [preset]);

  useEffect(() => {
    refresh();
    return () => abortRef.current?.abort();
  }, [refresh]);

  return { queryContext, summary, now, refresh };
}

/** Query-context lane used only by Trend and Errors. Window KPIs and Activity
 * keep their independent ingress context, so changing analysis scope cannot
 * silently change the page's one-request-per-row surfaces. */
export function useObserveAnalysisContext(
  preset: string,
  scope: ObserveScope,
): FragmentState<QueryContextResponse> & { refresh: () => void } {
  const [fragment, setFragment] = useState<FragmentState<QueryContextResponse>>(
    initialFragment,
  );
  const [reloadToken, setReloadToken] = useState(0);
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    queueMicrotask(() => {
      if (!active) return;
      setFragment((previous) => ({
        ...previous,
        phase: "loading",
        stale: previous.data !== null,
        error: null,
        retryAfterMs: null,
      }));
    });
    void observe
      .queryContext({ preset, scope }, controller.signal)
      .then((data) => {
        if (active && !controller.signal.aborted) {
          setFragment({
            phase: "ready",
            data,
            stale: false,
            error: null,
            retryAfterMs: null,
          });
        }
      })
      .catch((error: unknown) => {
        if (!active || controller.signal.aborted) return;
        const mapped = fragmentErrorFrom(error);
        setFragment((previous) => ({
          ...previous,
          phase: "error",
          stale: previous.data !== null,
          error: mapped.error,
          retryAfterMs: mapped.retryAfterMs,
        }));
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [preset, reloadToken, scope]);
  const refresh = useCallback(() => setReloadToken((value) => value + 1), []);
  return { ...fragment, refresh };
}

export function describeError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
