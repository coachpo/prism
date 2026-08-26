import { formatNumber, getCurrentLocale } from "@/i18n/format";
import { formatMoneyMicros } from "@/lib/costing";

export function formatCost(micros: number | null, symbol: string | null): string {
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

export function formatTtft(ttftMs: number | null | undefined): string {
  if (ttftMs === null || ttftMs === undefined || !Number.isFinite(ttftMs)) {
    return "—";
  }

  return `${formatNumber(ttftMs, getCurrentLocale())}ms`;
}

export function formatTokenRate(
  outputTokens: number | null | undefined,
  ttftMs: number | null | undefined,
  completionDurationMs: number | null | undefined,
): string {
  if (
    outputTokens === null ||
    outputTokens === undefined ||
    !Number.isFinite(outputTokens) ||
    ttftMs === null ||
    ttftMs === undefined ||
    !Number.isFinite(ttftMs) ||
    completionDurationMs === null ||
    completionDurationMs === undefined ||
    !Number.isFinite(completionDurationMs)
  ) {
    return "—";
  }

  const decodeDurationMs = completionDurationMs - ttftMs;
  if (decodeDurationMs <= 0) return "—";

  const tokensPerSecond = (outputTokens * 1000) / decodeDurationMs;
  return `${formatNumber(tokensPerSecond, getCurrentLocale(), {
    minimumFractionDigits: 1,
    maximumFractionDigits: 1,
  })} tok/s`;
}
