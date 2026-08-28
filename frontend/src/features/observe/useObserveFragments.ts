import { useCallback, useEffect, useRef, useState } from "react";

import { ApiError } from "@/lib/api/request";
import {
  observe,
  type QueryContextResponse,
  type UsageSummaryResponse,
  type DashboardNowResponse,
} from "@/lib/api/observability";
import type { ObserveScope } from "@/features/observe/observeSearch";
import { getStaticMessages } from "@/i18n/staticMessages";

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
  return {
    phase: "loading",
    data: null,
    stale: false,
    error: null,
    retryAfterMs: null,
  };
}

/**
 * Maps an unknown error into the fragment error surface: extracts the safe
 * message and preserves the server Retry-After for 503 overload responses so
 * panels can render "暂不可用，请稍后重试" guidance.
 */
export function fragmentErrorFrom(err: unknown): {
  error: string;
  retryAfterMs: number | null;
} {
  if (err instanceof ApiError) {
    return { error: err.message, retryAfterMs: err.retryAfterMs };
  }
  return {
    error: err instanceof Error ? err.message : String(err),
    retryAfterMs: null,
  };
}

/**
 * Observe fragment state: query-context, window summary and Now strip load
 * independently. A failure in one fragment never blanks the others and never
 * produces synthetic zeros (per O-P0-1).
 */
export function useObserveFragments(preset: string) {
  const [queryContextSnapshot, setQueryContextSnapshot] = useState<{
    key: string;
    fragment: FragmentState<QueryContextResponse>;
  }>(() => ({ key: preset, fragment: initialFragment() }));
  const [summarySnapshot, setSummarySnapshot] = useState<{
    key: string;
    fragment: FragmentState<UsageSummaryResponse>;
  }>(() => ({ key: preset, fragment: initialFragment() }));
  const [nowSnapshot, setNowSnapshot] = useState<{
    key: string;
    fragment: FragmentState<DashboardNowResponse>;
  }>(() => ({ key: preset, fragment: initialFragment() }));
  const queryContext = visibleFragment(queryContextSnapshot, preset);
  const summary = visibleFragment(summarySnapshot, preset);
  const now = visibleFragment(nowSnapshot, preset);
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
        setQueryContextSnapshot({
          key: preset,
          fragment: readyFragment(context),
        });
        void observe
          .usageSummary(context.query_context, signal)
          .then((summaryData) => {
            if (generationRef.current !== generation || signal.aborted) return;
            setSummarySnapshot({
              key: preset,
              fragment: readyFragment(summaryData),
            });
          })
          .catch((error: unknown) => {
            if (generationRef.current !== generation || signal.aborted) return;
            setSummarySnapshot((previous) =>
              failFragmentRead(previous, preset, error),
            );
          });
      })
      .catch((error: unknown) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setQueryContextSnapshot((previous) =>
          failFragmentRead(previous, preset, error),
        );
      });
    void observe
      .dashboardNow(signal)
      .then((nowData) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setNowSnapshot({
          key: preset,
          fragment: readyFragment(nowData),
        });
      })
      .catch((error: unknown) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setNowSnapshot((previous) =>
          failFragmentRead(previous, preset, error),
        );
      });
  }, [preset]);

  useEffect(() => {
    refresh();
    return () => abortRef.current?.abort();
  }, [refresh]);

  return { queryContext, summary, now, refresh };
}

type FragmentSnapshot<T> = {
  key: string;
  fragment: FragmentState<T>;
};

function visibleFragment<T>(
  snapshot: FragmentSnapshot<T>,
  key: string,
): FragmentState<T> {
  return snapshot.key === key ? snapshot.fragment : initialFragment<T>();
}

function readyFragment<T>(data: T): FragmentState<T> {
  return {
    phase: "ready",
    data,
    stale: false,
    error: null,
    retryAfterMs: null,
  };
}

function failFragmentRead<T>(
  snapshot: FragmentSnapshot<T>,
  key: string,
  error: unknown,
): FragmentSnapshot<T> {
  const previous =
    snapshot.key === key ? snapshot.fragment : initialFragment<T>();
  return {
    key,
    fragment: {
      ...previous,
      phase: "error",
      stale: previous.data !== null,
      error: describeError(error),
      retryAfterMs: error instanceof ApiError ? error.retryAfterMs : null,
    },
  };
}

/** Query-context lane used only by Trend and Errors. Window KPIs and Activity
 * keep their independent ingress context, so changing analysis scope cannot
 * silently change the page's one-request-per-row surfaces. */
export function useObserveAnalysisContext(
  preset: string,
  scope: ObserveScope,
): FragmentState<QueryContextResponse> & { refresh: () => void } {
  const [reloadToken, setReloadToken] = useState(0);
  const key = `${preset}:${scope}:${reloadToken}`;
  const [snapshot, setSnapshot] = useState<{
    key: string;
    fragment: FragmentState<QueryContextResponse>;
  }>(() => ({ key, fragment: initialFragment() }));
  const fragment =
    snapshot.key === key ? snapshot.fragment : initialFragment<QueryContextResponse>();
  useEffect(() => {
    let active = true;
    const controller = new AbortController();
    void observe
      .queryContext({ preset, scope }, controller.signal)
      .then((data) => {
        if (active && !controller.signal.aborted) {
          if (data.scope !== scope || data.caliber?.scope !== scope) {
            setSnapshot({
              key,
              fragment: {
                phase: "error",
                data: null,
                stale: false,
                error: getStaticMessages().observe.queryContextUnavailable,
                retryAfterMs: null,
              },
            });
            return;
          }
          setSnapshot({
            key,
            fragment: {
              phase: "ready",
              data,
              stale: false,
              error: null,
              retryAfterMs: null,
            },
          });
        }
      })
      .catch((error: unknown) => {
        if (!active || controller.signal.aborted) return;
        const mapped = fragmentErrorFrom(error);
        setSnapshot({
          key,
          fragment: {
            phase: "error",
            data: null,
            stale: false,
            error: mapped.error,
            retryAfterMs: mapped.retryAfterMs,
          },
        });
      });
    return () => {
      active = false;
      controller.abort();
    };
  }, [preset, reloadToken, scope, key]);
  const refresh = useCallback(() => setReloadToken((value) => value + 1), []);
  return { ...fragment, refresh };
}

export function describeError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
