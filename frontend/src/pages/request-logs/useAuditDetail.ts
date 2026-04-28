import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "@/lib/api";
import type { AuditLogDetail } from "@/lib/types";
import {
  resolveRequestAuditCaptureMode,
  type AuditDetailState,
  type RequestAuditCaptureMode,
} from "./requestLogAuditState";

const MAX_RETRIES = 5;
const RETRY_DELAY_MS = 1000;

interface UseAuditDetailParams {
  requestLogId: number | null;
  auditEnabledAtRequest: boolean;
  auditCaptureBodiesAtRequest: boolean;
  enabled: boolean;
}

export function useAuditDetail({
  requestLogId,
  auditEnabledAtRequest,
  auditCaptureBodiesAtRequest,
  enabled,
}: UseAuditDetailParams) {
  const [audits, setAudits] = useState<AuditLogDetail[]>([]);
  const [loading, setLoading] = useState(false);
  const [state, setState] = useState<AuditDetailState | null>(null);
  const [loadedRequestLogId, setLoadedRequestLogId] = useState<number | null>(null);
  const activeLogIdRef = useRef<number | null>(null);
  const isActive = enabled && requestLogId !== null;
  const captureMode = resolveRequestAuditCaptureMode({
    audit_enabled_at_request: auditEnabledAtRequest,
    audit_capture_bodies_at_request: auditCaptureBodiesAtRequest,
  });
  const hasLoadedCurrentRequest = requestLogId !== null && loadedRequestLogId === requestLogId;

  const fetchAudits = useCallback(async (logId: number, nextState: RequestAuditCaptureMode) => {
    activeLogIdRef.current = logId;
    setLoadedRequestLogId(logId);
    setLoading(true);
    setState(null);
    setAudits([]);

    for (let attempt = 0; attempt < MAX_RETRIES; attempt += 1) {
      if (activeLogIdRef.current !== logId) {
        return;
      }

      try {
        const listResult = await api.audit.listForRequestLog(logId, { limit: 20 });

        if (activeLogIdRef.current !== logId) {
          return;
        }

        if (listResult.items.length === 0) {
          if (attempt < MAX_RETRIES - 1) {
            await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY_MS));
            continue;
          }

          setState("load_failed");
          setLoading(false);
          return;
        }

        const details = await Promise.all(listResult.items.map((item) => api.audit.get(item.id)));

        if (activeLogIdRef.current !== logId) {
          return;
        }

        setAudits(details);
        setState(nextState);
        setLoading(false);
        return;
      } catch {
        if (activeLogIdRef.current !== logId) {
          return;
        }

        if (attempt < MAX_RETRIES - 1) {
          await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY_MS));
          continue;
        }

        setState("load_failed");
        setLoading(false);
        return;
      }
    }
  }, []);

  useEffect(() => {
    if (!isActive || requestLogId === null || captureMode === "disabled") {
      activeLogIdRef.current = null;
      return;
    }

    const fetchTimeoutId = setTimeout(() => {
      void fetchAudits(requestLogId, captureMode);
    }, 0);

    return () => {
      clearTimeout(fetchTimeoutId);
      activeLogIdRef.current = null;
    };
  }, [captureMode, fetchAudits, isActive, requestLogId]);

  if (!isActive) {
    return { audits: [], loading: false, state: null as AuditDetailState | null };
  }

  if (captureMode === "disabled") {
    return { audits: [], loading: false, state: "disabled" as const };
  }

  return {
    audits: hasLoadedCurrentRequest ? audits : [],
    loading: !hasLoadedCurrentRequest || loading,
    state: hasLoadedCurrentRequest ? state ?? captureMode : captureMode,
  };
}
