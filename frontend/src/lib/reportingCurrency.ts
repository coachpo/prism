import { api } from "@/lib/api";

export interface ReportingCurrency {
  code: string;
  symbol: string;
}

export interface ReportingCurrencyLike {
  code?: string | null;
  symbol?: string | null;
  report_currency_code?: string | null;
  report_currency_symbol?: string | null;
}

export type ReportingCurrencyTrust = "verified" | "fallback";

export interface ReportingCurrencyState {
  currency: ReportingCurrency;
  trust: ReportingCurrencyTrust;
}

export const DEFAULT_REPORTING_CURRENCY: ReportingCurrency = {
  code: "USD",
  symbol: "$",
};

export const DEFAULT_REPORTING_CURRENCY_STATE: ReportingCurrencyState = {
  currency: DEFAULT_REPORTING_CURRENCY,
  trust: "fallback",
};

const reportingCurrencyCache = new Map<string, ReportingCurrencyState>();
const reportingCurrencyRequestCache = new Map<string, Promise<ReportingCurrencyState>>();

let activeReportingCurrencyCacheKey: string | null = null;
let activeReportingCurrencyState = DEFAULT_REPORTING_CURRENCY_STATE;

function buildReportingCurrencyState(
  currency: ReportingCurrencyLike | ReportingCurrency | null | undefined,
  trust: ReportingCurrencyTrust,
): ReportingCurrencyState {
  return {
    currency: normalizeReportingCurrency(currency),
    trust,
  };
}

export function normalizeReportingCurrency(
  currency?: ReportingCurrencyLike | null,
): ReportingCurrency {
  const rawCode = currency?.code ?? currency?.report_currency_code;
  const normalizedCode = rawCode?.trim().toUpperCase() || DEFAULT_REPORTING_CURRENCY.code;
  const rawSymbol = currency?.symbol ?? currency?.report_currency_symbol;

  return {
    code: normalizedCode,
    symbol: typeof rawSymbol === "string" ? rawSymbol : DEFAULT_REPORTING_CURRENCY.symbol,
  };
}

function syncActiveReportingCurrency(cacheKey: string, currencyState: ReportingCurrencyState) {
  if (activeReportingCurrencyCacheKey === cacheKey) {
    activeReportingCurrencyState = currencyState;
  }
}

function getCachedReportingCurrencyState(cacheKey: string): ReportingCurrencyState | null {
  const cachedCurrencyState = reportingCurrencyCache.get(cacheKey) ?? null;

  if (cachedCurrencyState) {
    syncActiveReportingCurrency(cacheKey, cachedCurrencyState);
  }

  return cachedCurrencyState;
}

function buildFallbackReportingCurrencyState(cacheKey: string): ReportingCurrencyState {
  const cachedCurrencyState = reportingCurrencyCache.get(cacheKey);
  const fallbackCurrencyState = cachedCurrencyState
    ? {
        currency: cachedCurrencyState.currency,
        trust: "fallback" as const,
      }
    : DEFAULT_REPORTING_CURRENCY_STATE;

  reportingCurrencyCache.set(cacheKey, fallbackCurrencyState);
  syncActiveReportingCurrency(cacheKey, fallbackCurrencyState);
  return fallbackCurrencyState;
}

export function getReportingCurrencyState(
  cacheKey: string,
  forceRefresh = false,
): Promise<ReportingCurrencyState> {
  if (!forceRefresh) {
    const cachedCurrencyState = getCachedReportingCurrencyState(cacheKey);
    if (cachedCurrencyState) {
      return Promise.resolve(cachedCurrencyState);
    }
  }

  if (!forceRefresh) {
    const inFlightRequest = reportingCurrencyRequestCache.get(cacheKey);
    if (inFlightRequest) {
      return inFlightRequest;
    }
  }

  const loadPromise = api.settings.costing
    .get()
    .then((settings) => primeReportingCurrencyState(cacheKey, settings))
    .catch(() => buildFallbackReportingCurrencyState(cacheKey))
    .finally(() => {
      if (reportingCurrencyRequestCache.get(cacheKey) === loadPromise) {
        reportingCurrencyRequestCache.delete(cacheKey);
      }
    });

  reportingCurrencyRequestCache.set(cacheKey, loadPromise);
  return loadPromise;
}

export function getReportingCurrency(
  cacheKey: string,
  forceRefresh = false,
): Promise<ReportingCurrency> {
  return getReportingCurrencyState(cacheKey, forceRefresh).then((currencyState) => currencyState.currency);
}

export function primeReportingCurrencyState(
  cacheKey: string,
  currency: ReportingCurrencyLike,
): ReportingCurrencyState {
  const currencyState = buildReportingCurrencyState(currency, "verified");
  reportingCurrencyCache.set(cacheKey, currencyState);
  syncActiveReportingCurrency(cacheKey, currencyState);
  return currencyState;
}

export function primeReportingCurrency(
  cacheKey: string,
  currency: ReportingCurrencyLike,
): ReportingCurrency {
  return primeReportingCurrencyState(cacheKey, currency).currency;
}

export function setActiveReportingCurrencyState(
  cacheKey?: string | null,
): ReportingCurrencyState {
  activeReportingCurrencyCacheKey = cacheKey ?? null;
  activeReportingCurrencyState = cacheKey
    ? reportingCurrencyCache.get(cacheKey) ?? DEFAULT_REPORTING_CURRENCY_STATE
    : DEFAULT_REPORTING_CURRENCY_STATE;
  return activeReportingCurrencyState;
}

export function setActiveReportingCurrency(
  cacheKey?: string | null,
): ReportingCurrency {
  return setActiveReportingCurrencyState(cacheKey).currency;
}

export function getActiveReportingCurrencyState(): ReportingCurrencyState {
  return activeReportingCurrencyState;
}

export function getActiveReportingCurrency(): ReportingCurrency {
  return activeReportingCurrencyState.currency;
}

export function clearReportingCurrency(cacheKey?: string) {
  if (cacheKey === undefined) {
    reportingCurrencyCache.clear();
    reportingCurrencyRequestCache.clear();
    activeReportingCurrencyCacheKey = null;
    activeReportingCurrencyState = DEFAULT_REPORTING_CURRENCY_STATE;
    return;
  }

  reportingCurrencyCache.delete(cacheKey);
  reportingCurrencyRequestCache.delete(cacheKey);

  if (activeReportingCurrencyCacheKey === cacheKey) {
    activeReportingCurrencyState = DEFAULT_REPORTING_CURRENCY_STATE;
  }
}
