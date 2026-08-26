import { useCallback, useEffect, useState } from "react";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import {
  getRequestLogAuditErrorMessage,
  IDLE_LIST,
  type ListLaneState,
  type RequestLaneState,
  type RequestLogAuditWindow,
} from "./requestLogAuditLanes";

interface UseRequestLogAuditListInput {
  auditWindow: RequestLogAuditWindow | null;
  cursor?: string;
  request: RequestLaneState;
  requestId: string | null;
}

export function useRequestLogAuditList({
  auditWindow,
  cursor,
  request,
  requestId,
}: UseRequestLogAuditListInput) {
  const messages = getStaticMessages();
  const [list, setList] = useState<ListLaneState>(IDLE_LIST);
  const [listNonce, setListNonce] = useState(0);
  const normalizedCursor = cursor?.trim() ?? "";

  useEffect(() => {
    if (
      request.phase !== "ready" ||
      requestId === null ||
      request.captureMode === "disabled" ||
      !auditWindow
    ) {
      // The list lane must reset when request capture/window gating closes it.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setList(IDLE_LIST);
      return;
    }
    let cancelled = false;
    setList((current) => ({
      ...current,
      phase: "loading",
      items: normalizedCursor ? [] : current.items,
      error: null,
    }));
    void (async () => {
      try {
        const page = await api.audit.listForRequestLog(requestId, {
          from: auditWindow.from,
          to: auditWindow.to,
          limit: 20,
          cursor: normalizedCursor || undefined,
        });
        if (cancelled) return;
        setList({
          phase: page.items.length === 0 ? "empty" : "ready",
          items: page.items,
          nextCursor: page.next_cursor,
          hasMore: page.has_more,
          error: null,
        });
      } catch (error) {
        if (cancelled) return;
        setList({
          phase: "error",
          items: [],
          nextCursor: null,
          hasMore: false,
          error: getRequestLogAuditErrorMessage(
            error,
            messages.requestLogs.auditListLoadFailed,
          ),
        });
      }
    })();
    return () => {
      cancelled = true;
    };
    // Request readiness and the derived window are the committed request lane.
  }, [
    auditWindow,
    messages.requestLogs.auditListLoadFailed,
    normalizedCursor,
    request.captureMode,
    request.phase,
    request.request,
    requestId,
    listNonce,
  ]);

  const retryList = useCallback(() => {
    setListNonce((current) => current + 1);
  }, []);

  return { list, retryList };
}
