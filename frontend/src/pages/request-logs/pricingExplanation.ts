import { getStaticMessages } from "@/i18n/staticMessages";

export interface UnpricedCauseInput {
  pricingStatus: string | null | undefined;
  unpricedReason: string | null | undefined;
  streamOutcome: string | null | undefined;
}

function isStreamRow(outcome: string | null | undefined): boolean {
  return outcome !== null && outcome !== undefined && outcome !== "" && outcome !== "not_streaming";
}

/** Returns the causal sentence for an unpriced row, or null when the row is
 *  not unpriced or the reason carries no additional explanation. */
export function describeUnpricedCause(input: UnpricedCauseInput): string | null {
  if (input.pricingStatus !== "unpriced") return null;
  const m = getStaticMessages().requestLogs;
  switch (input.unpricedReason) {
    case "MISSING_TOKEN_USAGE":
      return isStreamRow(input.streamOutcome) ? m.unpricedCauseStreamNoUsage : m.unpricedCauseNonStreamNoUsage;
    case "STREAM_USAGE_UNAVAILABLE":
      return m.unpricedCauseStreamTruncated;
    case "PRICING_DISABLED":
      return m.unpricedCausePricingDisabled;
    case "MISSING_PRICE_DATA":
      return m.unpricedCauseMissingPriceData;
    default:
      return null;
  }
}

export interface TokenComponentInput {
  input: number | null;
  output: number | null;
  total: number | null;
  cacheRead: number | null;
  cacheCreation: number | null;
  reasoning: number | null;
}

export type TokenComponentCoverage =
  | { kind: "unavailable" }
  | { kind: "total_only"; uncategorized: number }
  | { kind: "balanced" }
  | { kind: "residual"; uncategorized: number };

/** Classifies how well the disjoint components reconstruct the provider total.
 *  "total_only" means every component is NULL while a total exists (upstream
 *  reported a bare total); "residual" means the components sum to something
 *  other than the total. */
export function classifyTokenComponents(usage: TokenComponentInput): TokenComponentCoverage {
  if (usage.total === null) return { kind: "unavailable" };
  const parts = [usage.input, usage.output, usage.cacheRead, usage.cacheCreation, usage.reasoning];
  if (parts.every((value) => value === null)) return { kind: "total_only", uncategorized: usage.total };
  const sum = parts.reduce<number>((acc, value) => acc + (value ?? 0), 0);
  const residual = usage.total - sum;
  return residual === 0 ? { kind: "balanced" } : { kind: "residual", uncategorized: residual };
}
