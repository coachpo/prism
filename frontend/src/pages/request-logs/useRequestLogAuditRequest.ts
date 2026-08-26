import { useCallback, useEffect, useMemo, useState } from "react";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { RequestLogDetail } from "@/lib/types/request-logs";
import { deriveRequestLogAuditWindow } from "./requestLogAuditWindow";
import { resolveRequestAuditCaptureMode } from "./requestLogAuditState";
import {
  getRequestLogAuditErrorMessage,
  IDLE_REQUEST,
  isRequestLogAuditMissing,
  type RequestLaneState,
  type RequestLogAuditWindow,
} from "./requestLogAuditLanes";

interface UseRequestLogAuditRequestInput {
  requestId: string | null;
}

function parseAuditWindow(
  request: RequestLogDetail,
): RequestLogAuditWindow | null {
  const summaryCreated = request.summary?.created_at;
  if (!summaryCreated) return null;
  try {
    return deriveRequestLogAuditWindow(summaryCreated);
  } catch {
    return null;
  }
}

export function useRequestLogAuditRequest({
  requestId,
}: UseRequestLogAuditRequestInput) {
  const messages = getStaticMessages();
  const [request, setRequest] = useState<RequestLaneState>(IDLE_REQUEST);
  const [requestNonce, setRequestNonce] = useState(0);
  const auditWindow = useMemo(
    () =>
      request.phase === "ready" && request.request !== null
        ? parseAuditWindow(request.request)
        : null,
    [request.phase, request.request],
  );

  useEffect(() => {
    if (requestId === null) {
      // Resetting the lane when its route identity disappears is the state
      // boundary for this hook, not an external-system synchronization.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setRequest(IDLE_REQUEST);
      return;
    }
    let cancelled = false;
    setRequest({
      phase: "loading",
      request: null,
      captureMode: null,
      error: null,
    });
    void (async () => {
      try {
        const response = await api.stats.requestDetail(requestId);
        if (cancelled) return;
        const captureMode = resolveRequestAuditCaptureMode(response.routing);
        if (captureMode === "disabled") {
          setRequest({ phase: "disabled", request: response, captureMode, error: null });
          return;
        }
        if (!parseAuditWindow(response)) {
          setRequest({
            phase: "invalid_timestamp",
            request: response,
            captureMode,
            error: null,
          });
          return;
        }
        setRequest({ phase: "ready", request: response, captureMode, error: null });
      } catch (error) {
        if (cancelled) return;
        if (isRequestLogAuditMissing(error)) {
          setRequest({ ...IDLE_REQUEST, phase: "missing" });
          return;
        }
        setRequest({
          phase: "error",
          request: null,
          captureMode: null,
          error: getRequestLogAuditErrorMessage(
            error,
            messages.requestLogs.loadFailed,
          ),
        });
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [messages.requestLogs.loadFailed, requestId, requestNonce]);

  const retryRequest = useCallback(() => {
    setRequestNonce((current) => current + 1);
  }, []);

  return {
    auditWindow,
    request,
    retryRequest,
  };
}
