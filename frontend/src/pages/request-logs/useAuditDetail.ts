import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import type { AuditLogDetail } from "@/lib/types";
import {
  resolveRequestAuditCaptureMode,
  type AuditDetailState,
  type RequestAuditCaptureMode,
} from "./requestLogAuditState";

const MAX_RETRIES = 5;
const RETRY_DELAY_MS = 1000;
const AUDIT_WINDOW_MS = 12 * 60 * 60 * 1000;

interface RequestLogAuditWindow {
  from_time: string;
  to_time: string;
}

export function deriveRequestLogAuditWindow(requestCreatedAt: string | null): RequestLogAuditWindow | null {
  if (!requestCreatedAt) {
    return null;
  }

  const createdTime = Date.parse(requestCreatedAt);
  if (!Number.isFinite(createdTime)) {
    return null;
  }

  return {
    from_time: new Date(createdTime - AUDIT_WINDOW_MS).toISOString(),
    to_time: new Date(createdTime + AUDIT_WINDOW_MS).toISOString(),
  };
}

interface UseAuditDetailParams {
  requestLogId: number | null;
  requestCreatedAt: string | null;
  auditEnabledAtRequest: boolean;
  auditCaptureBodiesAtRequest: boolean;
  enabled: boolean;
}

export function useAuditDetail({
  requestLogId,
  requestCreatedAt,
  auditEnabledAtRequest,
  auditCaptureBodiesAtRequest,
  enabled,
}: UseAuditDetailParams) {
  const [audits, setAudits] = useState<AuditLogDetail[]>([]);
  const [loading, setLoading] = useState(false);
  const [state, setState] = useState<AuditDetailState | null>(null);
  const [loadedAuditKey, setLoadedAuditKey] = useState<string | null>(null);
  const activeAuditKeyRef = useRef<string | null>(null);
  const auditWindow = useMemo(() => deriveRequestLogAuditWindow(requestCreatedAt), [requestCreatedAt]);
  const currentAuditKey = requestLogId !== null && auditWindow
    ? `${requestLogId}:${auditWindow.from_time}:${auditWindow.to_time}`
    : null;
  const isActive = enabled && requestLogId !== null;
  const captureMode = resolveRequestAuditCaptureMode({
    audit_enabled_at_request: auditEnabledAtRequest,
    audit_capture_bodies_at_request: auditCaptureBodiesAtRequest,
  });
  const hasLoadedCurrentRequest = currentAuditKey !== null && loadedAuditKey === currentAuditKey;

  const fetchAudits = useCallback(async (
    logId: number,
    auditKey: string,
    fromTime: string,
    toTime: string,
    nextState: RequestAuditCaptureMode,
  ) => {
    activeAuditKeyRef.current = auditKey;
    setLoadedAuditKey(auditKey);
    setLoading(true);
    setState(null);
    setAudits([]);

    for (let attempt = 0; attempt < MAX_RETRIES; attempt += 1) {
      if (activeAuditKeyRef.current !== auditKey) {
        return;
      }

      try {
        const listResult = await api.audit.listForRequestLog(logId, {
          from_time: fromTime,
          to_time: toTime,
          limit: 20,
        });

        if (activeAuditKeyRef.current !== auditKey) {
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

        if (activeAuditKeyRef.current !== auditKey) {
          return;
        }

        setAudits(details);
        setState(nextState);
        setLoading(false);
        return;
      } catch {
        if (activeAuditKeyRef.current !== auditKey) {
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
    if (!isActive || requestLogId === null || captureMode === "disabled" || currentAuditKey === null || !auditWindow) {
      activeAuditKeyRef.current = null;
      return;
    }

    const fetchTimeoutId = setTimeout(() => {
      void fetchAudits(requestLogId, currentAuditKey, auditWindow.from_time, auditWindow.to_time, captureMode);
    }, 0);

    return () => {
      clearTimeout(fetchTimeoutId);
      activeAuditKeyRef.current = null;
    };
  }, [auditWindow, captureMode, currentAuditKey, fetchAudits, isActive, requestLogId]);

  if (!isActive) {
    return { audits: [], loading: false, state: null as AuditDetailState | null };
  }

  if (captureMode === "disabled") {
    return { audits: [], loading: false, state: "disabled" as const };
  }

  if (currentAuditKey === null || !auditWindow) {
    return { audits: [], loading: false, state: captureMode };
  }

  return {
    audits: hasLoadedCurrentRequest ? audits : [],
    loading: !hasLoadedCurrentRequest || loading,
    state: hasLoadedCurrentRequest ? state ?? captureMode : captureMode,
  };
}
