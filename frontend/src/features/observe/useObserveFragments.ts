import { useCallback, useEffect, useRef, useState } from "react";

import { useObserveReadCycle } from "./observeReadCycleContext";
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
  const { track } = useObserveReadCycle();
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
    setQueryContextSnapshot(previous => beginFragmentRead(previous, preset));
    setSummarySnapshot(previous => beginFragmentRead(previous, preset));
    setNowSnapshot(previous => beginFragmentRead(previous, preset));
    void track(observe
      .queryContext({ preset, scope: "ingress" }, signal))
      .then((context) => {
        if (generationRef.current !== generation || signal.aborted) return;
        setQueryContextSnapshot({
          key: preset,
          fragment: readyFragment(context),
        });
        void track(observe
          .usageSummary(context.query_context, signal))
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
        setSummarySnapshot(previous => failFragmentRead(previous, preset, error));
      });
    void track(observe
      .dashboardNow(signal))
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
  }, [preset, track]);

  useEffect(() => {
    let active = true;
    queueMicrotask(() => { if (active) refresh(); });
    return () => { active = false; abortRef.current?.abort(); };
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

function beginFragmentRead<T>(snapshot: FragmentSnapshot<T>, key: string): FragmentSnapshot<T> {
  const previous = snapshot.key === key ? snapshot.fragment : initialFragment<T>();
  return { key, fragment: { ...previous, phase: "loading", error: null } };
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
  const { track } = useObserveReadCycle();
  const [reloadToken, setReloadToken] = useState(0);
  const key = `${preset}:${scope}`;
  const [snapshot, setSnapshot] = useState<FragmentSnapshot<QueryContextResponse> & { token: number }>(
    () => ({ key, token: reloadToken, fragment: initialFragment() }),
  );
  const current = visibleFragment(snapshot, key);
  const fragment = snapshot.token === reloadToken ? current : { ...current, phase: "loading" as const };
  useEffect(() => {
    const controller = new AbortController();
    void track(observe.queryContext({ preset, scope }, controller.signal))
      .then(data => {
        if (controller.signal.aborted) return;
        if (data.scope !== scope || data.caliber?.scope !== scope) {
          throw new Error(getStaticMessages().observe.queryContextUnavailable);
        }
        setSnapshot({ key, token: reloadToken, fragment: readyFragment(data) });
      })
      .catch((error: unknown) => {
        if (!controller.signal.aborted) setSnapshot(previous => ({ ...failFragmentRead(previous, key, error), token: reloadToken }));
      });
    return () => controller.abort();
  }, [preset, reloadToken, scope, key, track]);
  const refresh = useCallback(() => setReloadToken(value => value + 1), []);
  return { ...fragment, refresh };
}

export function describeError(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error);
}
