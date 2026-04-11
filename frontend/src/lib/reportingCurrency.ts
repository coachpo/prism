import { api } from "@/lib/api";

export interface ReportingCurrency {
  code: string;
  symbol: string;
}

interface ReportingCurrencyLike {
  code?: string | null;
  symbol?: string | null;
  report_currency_code?: string | null;
  report_currency_symbol?: string | null;
}

export const DEFAULT_REPORTING_CURRENCY: ReportingCurrency = {
  code: "USD",
  symbol: "$",
};

const reportingCurrencyCache = new Map<string, ReportingCurrency>();
const reportingCurrencyRequestCache = new Map<string, Promise<ReportingCurrency>>();

let activeReportingCurrencyCacheKey: string | null = null;
let activeReportingCurrency = DEFAULT_REPORTING_CURRENCY;

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

function syncActiveReportingCurrency(cacheKey: string, currency: ReportingCurrency) {
  if (activeReportingCurrencyCacheKey === cacheKey) {
    activeReportingCurrency = currency;
  }
}

function getCachedReportingCurrency(cacheKey: string): ReportingCurrency | null {
  const cachedCurrency = reportingCurrencyCache.get(cacheKey) ?? null;

  if (cachedCurrency) {
    syncActiveReportingCurrency(cacheKey, cachedCurrency);
  }

  return cachedCurrency;
}

function failOpenReportingCurrency(cacheKey: string): ReportingCurrency {
  const cachedCurrency = getCachedReportingCurrency(cacheKey);
  if (cachedCurrency) {
    return cachedCurrency;
  }

  reportingCurrencyCache.set(cacheKey, DEFAULT_REPORTING_CURRENCY);
  syncActiveReportingCurrency(cacheKey, DEFAULT_REPORTING_CURRENCY);
  return DEFAULT_REPORTING_CURRENCY;
}

export function getReportingCurrency(
  cacheKey: string,
  forceRefresh = false,
): Promise<ReportingCurrency> {
  if (!forceRefresh) {
    const cachedCurrency = getCachedReportingCurrency(cacheKey);
    if (cachedCurrency) {
      return Promise.resolve(cachedCurrency);
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
    .then((settings) => primeReportingCurrency(cacheKey, settings))
    .catch(() => failOpenReportingCurrency(cacheKey))
    .finally(() => {
      if (reportingCurrencyRequestCache.get(cacheKey) === loadPromise) {
        reportingCurrencyRequestCache.delete(cacheKey);
      }
    });

  reportingCurrencyRequestCache.set(cacheKey, loadPromise);
  return loadPromise;
}

export function primeReportingCurrency(
  cacheKey: string,
  currency: ReportingCurrencyLike,
): ReportingCurrency {
  const normalizedCurrency = normalizeReportingCurrency(currency);
  reportingCurrencyCache.set(cacheKey, normalizedCurrency);
  syncActiveReportingCurrency(cacheKey, normalizedCurrency);
  return normalizedCurrency;
}

export function setActiveReportingCurrency(
  cacheKey?: string | null,
): ReportingCurrency {
  activeReportingCurrencyCacheKey = cacheKey ?? null;
  activeReportingCurrency = cacheKey
    ? reportingCurrencyCache.get(cacheKey) ?? DEFAULT_REPORTING_CURRENCY
    : DEFAULT_REPORTING_CURRENCY;
  return activeReportingCurrency;
}

export function getActiveReportingCurrency(): ReportingCurrency {
  return activeReportingCurrency;
}

export function clearReportingCurrency(cacheKey?: string) {
  if (cacheKey === undefined) {
    reportingCurrencyCache.clear();
    reportingCurrencyRequestCache.clear();
    activeReportingCurrencyCacheKey = null;
    activeReportingCurrency = DEFAULT_REPORTING_CURRENCY;
    return;
  }

  reportingCurrencyCache.delete(cacheKey);
  reportingCurrencyRequestCache.delete(cacheKey);

  if (activeReportingCurrencyCacheKey === cacheKey) {
    activeReportingCurrency = DEFAULT_REPORTING_CURRENCY;
  }
}
