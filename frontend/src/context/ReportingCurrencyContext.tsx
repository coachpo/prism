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

export interface ReportingCurrencySource {
  code?: string | null;
  symbol?: string | null;
  report_currency_code?: string | null;
  report_currency_symbol?: string | null;
}

interface ReportingCurrencyContextValue {
  ready: boolean;
  currency: ReportingCurrency;
  currencyState: ReportingCurrencyState;
  refresh: () => Promise<ReportingCurrencyState>;
  prime: (currency: ReportingCurrencySource) => ReportingCurrencyState;
}

const ReportingCurrencyContext = createContext<ReportingCurrencyContextValue | undefined>(undefined);

const pinnedReportingCurrencyCacheKey = "profile:1";

export function ReportingCurrencyProvider({
  children,
  fallback = null,
}: {
  children: ReactNode;
  fallback?: ReactNode;
}) {
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
      currency: exposedCurrency,
      currencyState: exposedCurrencyState,
      refresh,
      prime,
    }),
    [exposedCurrency, exposedCurrencyState, isReady, prime, refresh],
  );

  return (
    <ReportingCurrencyContext.Provider value={value}>
      {isReady ? children : fallback}
    </ReportingCurrencyContext.Provider>
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
