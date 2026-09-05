import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import {
  DEFAULT_REPORTING_CURRENCY_STATE,
  getReportingCurrencyState,
  primeReportingCurrencyState,
  setActiveReportingCurrencyState,
  type ReportingCurrency,
  type ReportingCurrencyState,
} from "@/lib/reportingCurrency";
import { useLocale } from "@/i18n/useLocale";
import { OperatorCallout, OperatorRetryButton } from "@/shared/design-system";

export interface ReportingCurrencySource {
  code?: string | null;
  symbol?: string | null;
  report_currency_code?: string | null;
  report_currency_symbol?: string | null;
}

interface ReportingCurrencyContextValue {
  ready: boolean;
  /** The read settled on the fallback currency: amounts are not in the instance's reporting currency. */
  degraded: boolean;
  currency: ReportingCurrency;
  currencyState: ReportingCurrencyState;
  refresh: () => Promise<ReportingCurrencyState>;
  prime: (currency: ReportingCurrencySource) => ReportingCurrencyState;
}

const ReportingCurrencyContext = createContext<ReportingCurrencyContextValue | undefined>(undefined);

const pinnedReportingCurrencyCacheKey = "profile:1";

/**
 * One global currency read must not decide whether the console renders at all.
 * The provider is transparent: children mount immediately against the default
 * currency (`trust: "fallback"`, which the honesty layer already reads as "not
 * verified"), and a settled read re-renders them with the real one.
 */
export function ReportingCurrencyProvider({ children }: { children: ReactNode }) {
  const cacheKey = pinnedReportingCurrencyCacheKey;
  const requestIdRef = useRef(0);
  const [readyCacheKey, setReadyCacheKey] = useState<string | null>(null);
  const [currencyState, setCurrencyState] = useState<ReportingCurrencyState>(
    DEFAULT_REPORTING_CURRENCY_STATE,
  );
  const isReady = cacheKey === null || readyCacheKey === cacheKey;
  const exposedCurrencyState = cacheKey === null ? DEFAULT_REPORTING_CURRENCY_STATE : currencyState;
  const exposedCurrency = exposedCurrencyState.currency;

  const loadCurrency = useCallback(async (nextCacheKey: string | null, forceRefresh = false) => {
    const requestId = ++requestIdRef.current;

    if (nextCacheKey === null) {
      const fallbackCurrencyState = setActiveReportingCurrencyState(null);

      await Promise.resolve();

      if (requestId !== requestIdRef.current) {
        return fallbackCurrencyState;
      }

      setReadyCacheKey(null);
      setCurrencyState(fallbackCurrencyState);
      return fallbackCurrencyState;
    }

    setActiveReportingCurrencyState(nextCacheKey);

    try {
      const nextCurrencyState = await getReportingCurrencyState(nextCacheKey, forceRefresh);

      if (requestId !== requestIdRef.current) {
        return nextCurrencyState;
      }

      setActiveReportingCurrencyState(nextCacheKey);
      setReadyCacheKey(nextCacheKey);
      setCurrencyState(nextCurrencyState);
      return nextCurrencyState;
    } catch {
      const fallbackCurrencyState = setActiveReportingCurrencyState(nextCacheKey);

      if (requestId === requestIdRef.current) {
        setReadyCacheKey(nextCacheKey);
        setCurrencyState(fallbackCurrencyState);
      }

      return fallbackCurrencyState;
    }
  }, []);

  useEffect(() => {
    const requestId = ++requestIdRef.current;

    void (async () => {
      if (cacheKey === null) {
        const fallbackCurrencyState = setActiveReportingCurrencyState(null);

        await Promise.resolve();

        if (requestId !== requestIdRef.current) {
          return;
        }

        setReadyCacheKey(null);
        setCurrencyState(fallbackCurrencyState);
        return;
      }

      setActiveReportingCurrencyState(cacheKey);

      try {
        const nextCurrencyState = await getReportingCurrencyState(cacheKey);

        if (requestId !== requestIdRef.current) {
          return;
        }

        setActiveReportingCurrencyState(cacheKey);
        setReadyCacheKey(cacheKey);
        setCurrencyState(nextCurrencyState);
      } catch {
        const fallbackCurrencyState = setActiveReportingCurrencyState(cacheKey);

        if (requestId !== requestIdRef.current) {
          return;
        }

        setReadyCacheKey(cacheKey);
        setCurrencyState(fallbackCurrencyState);
      }
    })();
  }, [cacheKey]);

  useEffect(() => {
    return () => {
      requestIdRef.current += 1;
      setActiveReportingCurrencyState(null);
    };
  }, []);

  const refresh = useCallback(() => loadCurrency(cacheKey, true), [cacheKey, loadCurrency]);

  const prime = useCallback(
    (nextCurrency: ReportingCurrencySource) => {
      requestIdRef.current += 1;

      if (cacheKey === null) {
        const fallbackCurrencyState = setActiveReportingCurrencyState(null);
        setReadyCacheKey(null);
        setCurrencyState(fallbackCurrencyState);
        return fallbackCurrencyState;
      }

      const primedCurrencyState = primeReportingCurrencyState(cacheKey, nextCurrency);
      setActiveReportingCurrencyState(cacheKey);
      setReadyCacheKey(cacheKey);
      setCurrencyState(primedCurrencyState);
      return primedCurrencyState;
    },
    [cacheKey],
  );

  const value = useMemo<ReportingCurrencyContextValue>(
    () => ({
      ready: isReady,
      // Only a settled read can be degraded; before that the fallback currency
      // is just the not-yet-loaded state.
      degraded: isReady && exposedCurrencyState.trust === "fallback",
      currency: exposedCurrency,
      currencyState: exposedCurrencyState,
      refresh,
      prime,
    }),
    [exposedCurrency, exposedCurrencyState, isReady, prime, refresh],
  );

  return (
    <ReportingCurrencyContext.Provider value={value}>
      {children}
    </ReportingCurrencyContext.Provider>
  );
}

/**
 * The degraded notice for a costing read that failed. Amounts stay on screen —
 * withholding them would be a different (and false) claim — but the notice
 * names the currency they are actually denominated in.
 */
export function ReportingCurrencyDegradedNotice() {
  const { currency, degraded, refresh } = useReportingCurrencyContext();
  const { messages } = useLocale();

  if (!degraded) {
    return null;
  }

  return (
    <OperatorCallout
      intent="warning"
      data-testid="reporting-currency-degraded"
      title={messages.common.reportingCurrencyFallbackTitle}
      description={messages.common.reportingCurrencyFallbackDescription(currency.code)}
      action={
        <OperatorRetryButton onClick={() => void refresh()}>
          {messages.common.retry}
        </OperatorRetryButton>
      }
    />
  );
}

// eslint-disable-next-line react-refresh/only-export-components
export function useReportingCurrencyContext() {
  const context = useContext(ReportingCurrencyContext);
  if (context === undefined) {
    throw new Error("useReportingCurrencyContext must be used within a ReportingCurrencyProvider");
  }
  return context;
}
