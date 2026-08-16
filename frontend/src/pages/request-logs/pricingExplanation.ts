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

export interface CacheReadShareInput {
  input: number | null;
  cacheRead: number | null;
  cacheCreation: number | null;
  operationName: string | null;
}

/** Operations whose cache components are not comparable under the disjoint
 *  cache-basis semantics: count_tokens writes the total into both input and
 *  cache_read (double counting), image operations never report cache
 *  components. */
const CACHE_BASIS_INELIGIBLE_OPERATIONS = new Set([
  "anthropic.count_tokens",
  "gemini.count_tokens",
  "openai.images.generations",
  "openai.images.edits",
]);

/** The reasons a share cannot be shown stay separate: "we know this operation
 *  is not comparable", "we cannot tell which operation this was", "upstream
 *  never reported the components" and "there were no prompt tokens" are four
 *  different facts about the request, and collapsing them into one em dash
 *  would leave the reader unable to tell them apart. */
export type CacheReadShare =
  | { kind: "value"; share: number }
  | { kind: "incomparable_operation" }
  | { kind: "indeterminate_operation" }
  | { kind: "components_missing" }
  | { kind: "no_prompt_tokens" };

/** Single-request share of prompt tokens served from cache:
 *  cacheRead / (input + cacheRead + cacheCreation). input and cacheRead must
 *  both be measured — a null excludes the row, while cacheCreation is
 *  structurally absent for OpenAI/Gemini and coalesces to zero (the two are
 *  not interchangeable). A zero denominator (all three components measured as
 *  0) yields no ratio, never a zero-value stand-in.
 *
 *  Operation eligibility is judged before the components: an operation known
 *  to be incomparable stays incomparable even when every component is present.
 *  A null operation_name is indeterminate and excluded the same way, which is
 *  a deliberate tradeoff rather than a pass-through. */
export function cacheReadShare(usage: CacheReadShareInput): CacheReadShare {
  if (usage.operationName === null) return { kind: "indeterminate_operation" };
  if (CACHE_BASIS_INELIGIBLE_OPERATIONS.has(usage.operationName)) {
    return { kind: "incomparable_operation" };
  }
  if (usage.input === null || usage.cacheRead === null) return { kind: "components_missing" };
  const denominator = usage.input + usage.cacheRead + (usage.cacheCreation ?? 0);
  if (denominator <= 0) return { kind: "no_prompt_tokens" };
  return { kind: "value", share: usage.cacheRead / denominator };
}
