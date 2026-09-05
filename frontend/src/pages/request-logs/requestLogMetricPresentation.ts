import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import { formatMoneyMicros } from "@/lib/costing";

/** The four persisted output-rate evidence states (Requests SPEC §8). */
export type TokenRateState =
  | "measured"
  | "unmeasurable"
  | "not_applicable"
  | "unknown";

/** One request's authoritative output-rate evidence from the backend. */
export interface TokenRateEvidence {
  rateTps: number | null | undefined;
  state: string | null | undefined;
  reason: string | null | undefined;
}

export function formatCost(
  micros: number | null,
  symbol: string | null,
): string {
  if (micros === null) return "—";
  return formatMoneyMicros(
    micros,
    symbol ?? undefined,
    undefined,
    2,
    6,
    getCurrentLocale(),
  );
}

export function formatTokens(tokens: number | null): string {
  if (tokens === null) return "—";
  return formatNumber(tokens, getCurrentLocale());
}

/**
 * 毫秒时长的唯一写法。同一屏出现「10,329 ms」与「10,329ms」两种写法时，
 * 读者会怀疑两个数不是一回事，所以单位与数值的间隔只在这里决定一次。
 */
export function formatDurationMs(durationMs: number | null | undefined): string {
  if (
    durationMs === null ||
    durationMs === undefined ||
    !Number.isFinite(durationMs)
  ) {
    return "—";
  }

  return `${formatNumber(durationMs, getCurrentLocale())} ms`;
}

export function formatTtft(ttftMs: number | null | undefined): string {
  return formatDurationMs(ttftMs);
}

/**
 * Formats the backend-authoritative output rate. Only persisted measured
 * evidence may render a number: the tok/s value is never recomputed in the
 * browser from tokens and durations, so buffered bursts, non-streaming
 * responses, Images, failures, and historical rows (state unknown) all stay
 * missing instead of becoming fabricated rates. A measured zero is a real
 * zero and renders as 0.0 tok/s.
 */
export function formatTokenRate(
  rateTps: number | null | undefined,
  state: string | null | undefined,
): string {
  if (
    state !== "measured" ||
    rateTps === null ||
    rateTps === undefined ||
    !Number.isFinite(rateTps)
  ) {
    return "—";
  }

  return `${formatNumber(rateTps, getCurrentLocale(), {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  })} tok/s`;
}

/**
 * Resolves the honest-missing explanation for a request without a measured
 * rate. The backend persists one reason code per unmeasured verdict; unknown
 * state keeps a generic fallback so legacy rows stay explainable even if a
 * reason is absent.
 */
export function tokenRateMissingReason(
  evidence: TokenRateEvidence,
  reasonLabels: Record<string, string>,
  fallback: string,
): string {
  if (evidence.reason && reasonLabels[evidence.reason]) {
    return reasonLabels[evidence.reason];
  }
  if (evidence.state === "not_applicable") {
    return reasonLabels["not_applicable"] ?? fallback;
  }
  if (evidence.state === "unknown") {
    return reasonLabels["unknown"] ?? fallback;
  }
  return fallback;
}

/** Shared localized explanation for every Requests rate consumer. */
export function describeTokenRateMissing(evidence: TokenRateEvidence): string {
  const messages = getStaticMessages();
  const copy = messages.observe;
  return tokenRateMissingReason(
    evidence,
    {
      unmeasurable_incomplete_stream: copy.outputRateReasonIncompleteStream,
      unmeasurable_missing_output_usage:
        copy.outputRateReasonMissingOutputUsage,
      unmeasurable_no_output_events: copy.outputRateReasonNoOutputEvents,
      unmeasurable_single_output_event:
        copy.outputRateReasonSingleOutputEvent,
      unmeasurable_output_span_below_threshold:
        copy.outputRateReasonSpanBelowThreshold,
      unmeasurable_reasoning_tokens_unaligned:
        copy.outputRateReasonReasoningUnaligned,
      unmeasurable_non_success_status:
        copy.outputRateReasonNonSuccessStatus,
      not_applicable_non_stream: copy.outputRateReasonNotApplicableNonStream,
      not_applicable_image_operation:
        copy.outputRateReasonNotApplicableImageOperation,
      not_applicable_non_text_operation:
        copy.outputRateReasonNotApplicableNonTextOperation,
      unknown_missing_evidence: copy.outputRateReasonUnknownLegacy,
      unknown_inconsistent_evidence:
        copy.outputRateReasonUnknownInconsistent,
      not_applicable: messages.honesty.notMeasured,
      unknown: copy.outputRateReasonUnknownLegacy,
    },
    messages.honesty.noValue,
  );
}
