import { formatNumber, getCurrentLocale, type Locale } from "@/i18n/format";
import { getStaticMessages } from "@/i18n/staticMessages";

const MICRO_FACTOR = 1_000_000;

function getPricingUnitLabels(): Record<string, string> {
  return {
    PER_1M: getStaticMessages().costingUi.per1mTokens,
  };
}

function getMissingSpecialTokenPolicyLabels(): Record<string, string> {
  return {
    MAP_TO_OUTPUT: getStaticMessages().costingUi.mapToOutputPrice,
    ZERO_COST: getStaticMessages().costingUi.zeroCost,
  };
}

function getUnpricedReasonLabels(): Record<string, string> {
  return {
    PRICING_DISABLED: getStaticMessages().costingUi.pricingDisabled,
    MISSING_PRICE_DATA: getStaticMessages().costingUi.missingPriceData,
    MISSING_ENDPOINT: getStaticMessages().costingUi.missingEndpoint,
    MISSING_TOKEN_USAGE: getStaticMessages().costingUi.missingTokenUsage,
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

export function microsToDecimal(micros: number | null | undefined): number {
  if (micros === null || micros === undefined) {
    return 0;
  }
  return micros / MICRO_FACTOR;
}

export function formatMoneyMicros(
  micros: number | null | undefined,
  symbol: string,
  code?: string,
  minimumFractionDigits = 2,
  maximumFractionDigits = 6,
  locale: Locale = getCurrentLocale(),
): string {
  if (micros === null || micros === undefined) {
    return "-";
  }
  const value = microsToDecimal(micros);
  const formatted = formatNumber(value, locale, {
    minimumFractionDigits,
    maximumFractionDigits,
  });
  return `${symbol}${formatted}${code ? ` ${code}` : ""}`;
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

export function formatMissingSpecialTokenPolicyLabel(
  value: string | null | undefined
): string {
  return formatEnumLabel(value, getMissingSpecialTokenPolicyLabels());
}

export function formatUnpricedReasonLabel(value: string | null | undefined): string {
  return formatEnumLabel(value, getUnpricedReasonLabels());
}

export function formatFxRateSourceLabel(value: string | null | undefined): string {
  return formatEnumLabel(value, getFxRateSourceLabels());
}
