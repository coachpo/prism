import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import type { AuditLogDetail } from "@/lib/types";

const MAX_RETRIES = 5;
const RETRY_DELAY_MS = 1000;

export type AuditDetailErrorKind = "capture_unavailable" | "load_failed";

interface UseAuditDetailParams {
  requestLogId: number | null;
  enabled: boolean;
}

export function useAuditDetail({ requestLogId, enabled }: UseAuditDetailParams) {
  const [audits, setAudits] = useState<AuditLogDetail[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<AuditDetailErrorKind | null>(null);
  const [loadedRequestLogId, setLoadedRequestLogId] = useState<number | null>(null);
  const activeLogIdRef = useRef<number | null>(null);
  const isActive = enabled && requestLogId !== null;
  const hasCurrentRequestState = requestLogId !== null && loadedRequestLogId === requestLogId;

  const fetchAudits = useCallback(async (logId: number) => {
    activeLogIdRef.current = logId;
    setLoadedRequestLogId(logId);
    setLoading(true);
    setError(null);
    setAudits([]);

    for (let attempt = 0; attempt < MAX_RETRIES; attempt++) {
      if (activeLogIdRef.current !== logId) return;

      try {
        const listResult = await api.audit.list({
          request_log_id: logId,
          limit: 20,
        });

        if (activeLogIdRef.current !== logId) return;

        if (listResult.items.length === 0) {
          if (attempt < MAX_RETRIES - 1) {
            await new Promise((r) => setTimeout(r, RETRY_DELAY_MS));
            continue;
          }
          setError("capture_unavailable");
          setLoading(false);
          return;
        }

        const detailResults = await Promise.allSettled(
          listResult.items.map((item) => api.audit.get(item.id))
        );

        if (activeLogIdRef.current !== logId) return;

        const details = detailResults
          .filter((r): r is PromiseFulfilledResult<AuditLogDetail> => r.status === "fulfilled")
          .map((r) => r.value);

        setAudits(details);
        setLoading(false);
        return;
      } catch {
        if (activeLogIdRef.current !== logId) return;
        if (attempt < MAX_RETRIES - 1) {
          await new Promise((r) => setTimeout(r, RETRY_DELAY_MS));
          continue;
        }
        setError("load_failed");
        setLoading(false);
        return;
      }
    }
  }, []);

  useEffect(() => {
    if (!isActive || requestLogId === null) {
      activeLogIdRef.current = null;
      return;
    }

    const fetchTimeoutId = setTimeout(() => {
      void fetchAudits(requestLogId);
    }, 0);

    return () => {
      clearTimeout(fetchTimeoutId);
      activeLogIdRef.current = null;
    };
  }, [fetchAudits, isActive, requestLogId]);

  return {
    audits: isActive && hasCurrentRequestState ? audits : [],
    error: isActive && hasCurrentRequestState ? error : null,
    loading: isActive ? !hasCurrentRequestState || loading : false,
  };
}
