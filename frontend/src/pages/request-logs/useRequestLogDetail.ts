import { useCallback, useEffect, useRef, useState } from "react";
import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/core";
import type { RequestLogDetail } from "@/lib/types";

interface UseRequestLogDetailParams {
  requestId: number | null;
  enabled: boolean;
}

export function useRequestLogDetail({ requestId, enabled }: UseRequestLogDetailParams) {
  const messages = getStaticMessages();
  const [request, setRequest] = useState<RequestLogDetail | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notFound, setNotFound] = useState(false);
  const activeRequestIdRef = useRef<number | null>(null);

  const fetchDetail = useCallback(
    async (targetRequestId: number) => {
      activeRequestIdRef.current = targetRequestId;
      setLoading(true);
      setError(null);
      setNotFound(false);
      setRequest(null);

      try {
        const detail = await api.stats.requestDetail(targetRequestId);
        if (activeRequestIdRef.current !== targetRequestId) {
          return;
        }

        setRequest(detail);
      } catch (err) {
        if (activeRequestIdRef.current !== targetRequestId) {
          return;
        }

        setRequest(null);
        if (err instanceof ApiError && err.status === 404) {
          setNotFound(true);
          return;
        }

        setError(err instanceof Error ? err.message : messages.requestLogs.loadFailed);
      } finally {
        if (activeRequestIdRef.current === targetRequestId) {
          setLoading(false);
        }
      }
    },
    [messages.requestLogs.loadFailed],
  );

  useEffect(() => {
    if (!enabled || requestId === null) {
      activeRequestIdRef.current = null;
      setLoading(false);
      setError(null);
      setNotFound(false);
      setRequest(null);
      return;
    }

    const fetchTimeoutId = setTimeout(() => {
      void fetchDetail(requestId);
    }, 0);

    return () => {
      clearTimeout(fetchTimeoutId);
      activeRequestIdRef.current = null;
    };
  }, [enabled, fetchDetail, requestId]);

  const refresh = useCallback(() => {
    if (!enabled || requestId === null) {
      return;
    }

    void fetchDetail(requestId);
  }, [enabled, fetchDetail, requestId]);

  return {
    request,
    loading,
    error,
    notFound,
    refresh,
  };
}
