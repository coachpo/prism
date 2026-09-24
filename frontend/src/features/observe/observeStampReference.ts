import { createContext, useContext } from "react";

import type { DashboardNowResponse, UsageSummaryResponse } from "@/lib/api/observability";
import type { FragmentState } from "./useObserveFragments";

/**
 * The time the page freshness bar states. A block stamp that agrees with it
 * says the same thing twice; a block read at another time — a tab opened
 * later, a chart re-based after load, a last-good fragment kept after a failed
 * refresh — still carries its own. Pages without a freshness bar provide no
 * value and every stamp renders.
 */
const ObservePageGeneratedAtContext = createContext<string | null>(null);

export const ObservePageGeneratedAtProvider = ObservePageGeneratedAtContext.Provider;

export function useObservePageGeneratedAt(): string | null {
  return useContext(ObservePageGeneratedAtContext);
}

/** The freshness bar reports the current-usage read, else the window summary. */
export function observePageGeneratedAt(
  nowFragment: FragmentState<DashboardNowResponse>,
  summaryFragment: FragmentState<UsageSummaryResponse>,
): string | null {
  return nowFragment.data?.generated_at ?? summaryFragment.data?.generated_at ?? null;
}

// Reads started by one refresh finish seconds apart; a minute apart is another read.
const SAME_READ_WINDOW_MS = 60_000;

export function stampRepeatsPage(
  generatedAt: string,
  pageGeneratedAt: string | null,
): boolean {
  if (!pageGeneratedAt) return false;
  const stamp = Date.parse(generatedAt);
  const page = Date.parse(pageGeneratedAt);
  if (Number.isNaN(stamp) || Number.isNaN(page)) return false;
  return Math.abs(stamp - page) < SAME_READ_WINDOW_MS;
}
