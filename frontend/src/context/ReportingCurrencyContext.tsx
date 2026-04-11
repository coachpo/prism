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
import { useProfileContext } from "@/context/ProfileContext";
import {
  DEFAULT_REPORTING_CURRENCY,
  getReportingCurrency,
  primeReportingCurrency,
  setActiveReportingCurrency,
  type ReportingCurrency,
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
  refresh: () => Promise<ReportingCurrency>;
  prime: (currency: ReportingCurrencySource) => ReportingCurrency;
}

const ReportingCurrencyContext = createContext<ReportingCurrencyContextValue | undefined>(undefined);

function buildReportingCurrencyCacheKey(selectedProfileId: number | null) {
  if (selectedProfileId === null) {
    return null;
  }

  return `profile:${selectedProfileId}`;
}

export function ReportingCurrencyProvider({
  children,
  fallback = null,
}: {
  children: ReactNode;
  fallback?: ReactNode;
}) {
  const { selectedProfileId } = useProfileContext();
  const cacheKey = useMemo(
    () => buildReportingCurrencyCacheKey(selectedProfileId),
    [selectedProfileId],
  );
  const requestIdRef = useRef(0);
  const [readyCacheKey, setReadyCacheKey] = useState<string | null>(null);
  const [currency, setCurrency] = useState<ReportingCurrency>(DEFAULT_REPORTING_CURRENCY);
  const isReady = cacheKey === null || readyCacheKey === cacheKey;
  const exposedCurrency = cacheKey === null ? DEFAULT_REPORTING_CURRENCY : currency;

  const loadCurrency = useCallback(async (nextCacheKey: string | null, forceRefresh = false) => {
    const requestId = ++requestIdRef.current;

    if (nextCacheKey === null) {
      const fallbackCurrency = setActiveReportingCurrency(null);

      await Promise.resolve();

      if (requestId !== requestIdRef.current) {
        return fallbackCurrency;
      }

      setReadyCacheKey(null);
      setCurrency(fallbackCurrency);
      return fallbackCurrency;
    }

    setActiveReportingCurrency(nextCacheKey);

    try {
      const nextCurrency = await getReportingCurrency(nextCacheKey, forceRefresh);

      if (requestId !== requestIdRef.current) {
        return nextCurrency;
      }

      setActiveReportingCurrency(nextCacheKey);
      setReadyCacheKey(nextCacheKey);
      setCurrency(nextCurrency);
      return nextCurrency;
    } catch {
      const fallbackCurrency = setActiveReportingCurrency(nextCacheKey);

      if (requestId === requestIdRef.current) {
        setReadyCacheKey(nextCacheKey);
        setCurrency(fallbackCurrency);
      }

      return fallbackCurrency;
    }
  }, []);

  useEffect(() => {
    const requestId = ++requestIdRef.current;

    void (async () => {
      if (cacheKey === null) {
        const fallbackCurrency = setActiveReportingCurrency(null);

        await Promise.resolve();

        if (requestId !== requestIdRef.current) {
          return;
        }

        setReadyCacheKey(null);
        setCurrency(fallbackCurrency);
        return;
      }

      setActiveReportingCurrency(cacheKey);

      try {
        const nextCurrency = await getReportingCurrency(cacheKey);

        if (requestId !== requestIdRef.current) {
          return;
        }

        setActiveReportingCurrency(cacheKey);
        setReadyCacheKey(cacheKey);
        setCurrency(nextCurrency);
      } catch {
        const fallbackCurrency = setActiveReportingCurrency(cacheKey);

        if (requestId !== requestIdRef.current) {
          return;
        }

        setReadyCacheKey(cacheKey);
        setCurrency(fallbackCurrency);
      }
    })();
  }, [cacheKey]);

  useEffect(() => {
    return () => {
      requestIdRef.current += 1;
      setActiveReportingCurrency(null);
    };
  }, []);

  const refresh = useCallback(() => loadCurrency(cacheKey, true), [cacheKey, loadCurrency]);

  const prime = useCallback(
    (nextCurrency: ReportingCurrencySource) => {
      requestIdRef.current += 1;

      if (cacheKey === null) {
        const fallbackCurrency = setActiveReportingCurrency(null);
        setReadyCacheKey(null);
        setCurrency(fallbackCurrency);
        return fallbackCurrency;
      }

      const primedCurrency = primeReportingCurrency(cacheKey, nextCurrency);
      setActiveReportingCurrency(cacheKey);
      setReadyCacheKey(cacheKey);
      setCurrency(primedCurrency);
      return primedCurrency;
    },
    [cacheKey],
  );

  const value = useMemo<ReportingCurrencyContextValue>(
    () => ({
      ready: isReady,
      currency: exposedCurrency,
      refresh,
      prime,
    }),
    [exposedCurrency, isReady, prime, refresh],
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
