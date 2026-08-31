import type { UsageSummaryResponse } from "@/lib/api/observability";

/**
 * Requests inside the window that have no measured rate: unmeasurable,
 * operation-level not-applicable, and historical unknown rows. Surfaced on
 * the KPI so a shrinking sample is explainable instead of silently
 * disappearing.
 */
export function unmeasuredOutputRateCount(
  summary: UsageSummaryResponse,
): number {
  const counts = summary.output_rate_state_counts;
  if (!counts) return 0;
  return counts.unmeasurable + counts.not_applicable + counts.unknown;
}
