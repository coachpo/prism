import { formatNumber, getCurrentLocale, type Locale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";
import {
  getActiveReportingCurrency,
  getActiveReportingCurrencyState,
  type ReportingCurrency,
  type ReportingCurrencyState,
  type ReportingCurrencyTrust,
} from "@/lib/reportingCurrency";

const MICRO_FACTOR = 1_000_000;

export interface SpendTrustInput {
  costMicros?: number | null;
  priced?: boolean | null;
  unpricedReason?: string | null;
  pricedRequestCount?: number | null;
  unpricedRequestCount?: number | null;
}

export type SpendTrustState = ReportingCurrencyTrust | "unpriced";

export interface SpendTrustContext {
  currency: ReportingCurrency;
  currencyTrust: ReportingCurrencyTrust;
  spendTrust: SpendTrustState;
}

function getPricingUnitLabels(): Record<string, string> {
  return {
    PER_1M: getStaticMessages().costingUi.per1mTokens,
  };
}

function getUnpricedReasonLabels(): Record<string, string> {
  return {
    PRICING_DISABLED: getStaticMessages().costingUi.pricingDisabled,
    MISSING_PRICE_DATA: getStaticMessages().costingUi.missingPriceData,
    MISSING_ENDPOINT: getStaticMessages().costingUi.missingEndpoint,
    MISSING_TOKEN_USAGE: getStaticMessages().costingUi.missingTokenUsage,
    STREAM_USAGE_UNAVAILABLE: getStaticMessages().costingUi.streamUsageUnavailable,
  };
}

function getFxRateSourceLabels(): Record<string, string> {
  return {
    ENDPOINT_SPECIFIC: getStaticMessages().costingUi.endpointSpecificRate,
    DEFAULT_1_TO_1: getStaticMessages().costingUi.default1To1,
  };
}

function formatEnumLabel(
  value: string | null | undefined,
  labels: Record<string, string>,
  fallback = "-"
): string {
  if (value === null || value === undefined || value === "") {
    return fallback;
  }
  return labels[value] ?? fallback;
}

function hasOwnProperty<T extends object>(value: T, key: keyof SpendTrustInput): boolean {
  return Object.prototype.hasOwnProperty.call(value, key);
}

function normalizeOptionalCount(value: number | null | undefined): number | null {
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

export function hasUnpricedSpendContext(input: SpendTrustInput = {}): boolean {
  if (input.unpricedReason !== null && input.unpricedReason !== undefined && input.unpricedReason !== "") {
    return true;
  }

  if (input.priced === false) {
    return true;
  }

  if (input.priced === true) {
    return false;
  }

  if (hasOwnProperty(input, "pricedRequestCount") || hasOwnProperty(input, "unpricedRequestCount")) {
    const pricedRequestCount = normalizeOptionalCount(input.pricedRequestCount);
    const unpricedRequestCount = normalizeOptionalCount(input.unpricedRequestCount);
    return (pricedRequestCount ?? 0) <= 0 && (unpricedRequestCount ?? 0) > 0;
  }

  if (hasOwnProperty(input, "costMicros")) {
    return input.costMicros === null || input.costMicros === undefined;
  }

  return false;
}

export function resolveSpendTrustState(
  input: SpendTrustInput = {},
  currencyState: Pick<ReportingCurrencyState, "trust"> = getActiveReportingCurrencyState(),
): SpendTrustState {
  return hasUnpricedSpendContext(input) ? "unpriced" : currencyState.trust;
}

export function getActiveSpendTrustContext(
  input: SpendTrustInput = {},
): SpendTrustContext {
  const activeCurrencyState = getActiveReportingCurrencyState();

  return {
    currency: activeCurrencyState.currency,
    currencyTrust: activeCurrencyState.trust,
    spendTrust: resolveSpendTrustState(input, activeCurrencyState),
  };
}

export function microsToDecimal(micros: number | null | undefined): number {
  if (micros === null || micros === undefined) {
    return 0;
  }
  return micros / MICRO_FACTOR;
}

export function formatMoneyMicros(
  micros: number | null | undefined,
  symbol?: string,
  code?: string,
  minimumFractionDigits = 2,
  maximumFractionDigits = 6,
  locale: Locale = getCurrentLocale(),
): string {
  if (micros === null || micros === undefined) {
    return "-";
  }
  const value = microsToDecimal(micros);
  const activeCurrency = getActiveReportingCurrency();
  const resolvedSymbol = symbol ?? activeCurrency.symbol;
  const resolvedCode =
    symbol === undefined ? code ?? activeCurrency.code : code;
  const formatted = formatNumber(value, locale, {
    minimumFractionDigits,
    maximumFractionDigits,
  });
  return `${resolvedSymbol}${formatted}${resolvedCode ? ` ${resolvedCode}` : ""}`;
}

export function formatTokenCount(
  value: number | null | undefined,
  locale: Locale = getCurrentLocale(),
): string {
  if (value === null || value === undefined) {
    return "-";
  }
  return formatNumber(value, locale);
}

export function isValidCurrencyCode(value: string): boolean {
  return /^[A-Z]{3}$/.test(value.trim().toUpperCase());
}

export function isValidPositiveDecimalString(value: string): boolean {
  if (!value.trim()) {
    return false;
  }
  const parsed = Number(value);
  if (Number.isNaN(parsed) || !Number.isFinite(parsed)) {
    return false;
  }
  return parsed > 0;
}

export function formatPricingUnitLabel(value: string | null | undefined): string {
  return formatEnumLabel(value, getPricingUnitLabels());
}

export function formatUnpricedReasonLabel(value: string | null | undefined): string {
  return formatEnumLabel(value, getUnpricedReasonLabels());
}

export function formatFxRateSourceLabel(value: string | null | undefined): string {
  return formatEnumLabel(value, getFxRateSourceLabels());
}
