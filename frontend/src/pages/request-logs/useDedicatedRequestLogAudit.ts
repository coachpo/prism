import { useEffect, useRef, useState } from "react";
import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/core";
import type { AuditLogDetail, AuditLogListItem, RequestLogDetail } from "@/lib/types";
import { deriveRequestLogAuditWindow } from "./requestLogAuditWindow";
import {
  resolveRequestAuditCaptureMode,
  type RequestAuditCaptureMode,
} from "./requestLogAuditState";

export type DedicatedRequestLogAuditStatus =
  | "invalid_request_id"
  | "request_loading"
  | "request_missing"
  | "request_error"
  | "disabled"
  | "invalid_timestamp"
  | "audit_list_loading"
  | "audit_list_error"
  | "no_audit_records"
  | "missing_audit"
  | "audit_detail_loading"
  | "audit_detail_error"
  | "ready";
interface UseDedicatedRequestLogAuditParams {
  requestId: number | null;
  selectedAuditId: number | null;
  selectedAuditParamPresent: boolean;
  selectedAuditParamLabel: string | null;
}

interface DedicatedRequestLogAuditState {
  auditItems: AuditLogListItem[];
  captureMode: RequestAuditCaptureMode | null;
  detail: AuditLogDetail | null;
  error: string | null;
  loadKey: string | null;
  missingAuditLabel: string | null;
  request: RequestLogDetail | null;
  selectedAuditId: number | null;
  status: DedicatedRequestLogAuditStatus;
}

const INITIAL_STATE: DedicatedRequestLogAuditState = {
  auditItems: [],
  captureMode: null,
  detail: null,
  error: null,
  loadKey: null,
  missingAuditLabel: null,
  request: null,
  selectedAuditId: null,
  status: "request_loading",
};
function getErrorMessage(error: unknown, fallback: string): string {
  return error instanceof Error ? error.message : fallback;
}

function isRequestMissing(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

function resolveSelectedAuditItem(
  items: AuditLogListItem[],
  selectedAuditId: number | null,
  selectedAuditParamPresent: boolean,
): AuditLogListItem | null {
  if (!selectedAuditParamPresent) {
    return items[0] ?? null;
  }

  if (selectedAuditId === null) {
    return null;
  }

  return items.find((item) => item.id === selectedAuditId) ?? null;
}

function buildAuditLoadKey(
  requestId: number | null,
  selectedAuditParamPresent: boolean,
  selectedAuditParamLabel: string | null,
): string | null {
  if (requestId === null) {
    return null;
  }

  return `${requestId}:${selectedAuditParamPresent ? selectedAuditParamLabel?.trim() ?? "" : "default"}`;
}

export function useDedicatedRequestLogAudit({
  requestId,
  selectedAuditId,
  selectedAuditParamPresent,
  selectedAuditParamLabel,
}: UseDedicatedRequestLogAuditParams): DedicatedRequestLogAuditState {
  const messages = getStaticMessages();
  const [state, setState] = useState<DedicatedRequestLogAuditState>(INITIAL_STATE);
  const activeLoadIdRef = useRef(0);
  const currentLoadKey = buildAuditLoadKey(requestId, selectedAuditParamPresent, selectedAuditParamLabel);

  useEffect(() => {
    if (requestId === null || currentLoadKey === null) {
      activeLoadIdRef.current += 1;
      return undefined;
    }

    const loadId = activeLoadIdRef.current + 1;
    activeLoadIdRef.current = loadId;

    const isCurrent = () => activeLoadIdRef.current === loadId;
    const load = async () => {
      let request: RequestLogDetail;

      try {
        request = await api.stats.requestDetail(requestId);
      } catch (error) {
        if (!isCurrent()) return;
        setState({
          ...INITIAL_STATE,
          error: isRequestMissing(error)
            ? null
            : getErrorMessage(error, messages.requestLogs.loadFailed),
          loadKey: currentLoadKey,
          status: isRequestMissing(error) ? "request_missing" : "request_error",
        });
        return;
      }

      if (!isCurrent()) return;

      const captureMode = resolveRequestAuditCaptureMode(request.routing);
      const requestState = {
        ...INITIAL_STATE,
        captureMode,
        loadKey: currentLoadKey,
        request,
      };
      if (captureMode === "disabled") {
        setState({
          ...requestState,
          status: "disabled",
        });
        return;
      }

      const auditWindow = deriveRequestLogAuditWindow(request.summary.created_at);
      if (!auditWindow) {
        setState({
          ...requestState,
          status: "invalid_timestamp",
        });
        return;
      }

      setState({
        ...requestState,
        status: "audit_list_loading",
      });

      let auditItems: AuditLogListItem[];
      try {
        const list = await api.audit.listForRequestLog(requestId, {
          from: auditWindow.from,
          to: auditWindow.to,
          limit: 20,
        });
        auditItems = list.items;
      } catch (error) {
        if (!isCurrent()) return;
        setState({
          ...requestState,
          error: getErrorMessage(error, messages.requestLogs.auditListLoadFailed),
          status: "audit_list_error",
        });
        return;
      }

      if (!isCurrent()) return;

      if (auditItems.length === 0) {
        setState({
          ...requestState,
          auditItems,
          status: "no_audit_records",
        });
        return;
      }

      const selectedAuditItem = resolveSelectedAuditItem(
        auditItems,
        selectedAuditId,
        selectedAuditParamPresent,
      );

      if (!selectedAuditItem) {
        setState({
          ...requestState,
          auditItems,
          missingAuditLabel: selectedAuditParamLabel,
          status: "missing_audit",
        });
        return;
      }

      const selectedState = {
        ...requestState,
        auditItems,
        selectedAuditId: selectedAuditItem.id,
      };
      setState({
        ...selectedState,
        status: "audit_detail_loading",
      });

      try {
        const detail = await api.audit.get(selectedAuditItem.id);
        if (!isCurrent()) return;
        setState({
          ...selectedState,
          detail,
          status: "ready",
        });
      } catch (error) {
        if (!isCurrent()) return;
        setState({
          ...selectedState,
          error: getErrorMessage(error, messages.requestLogs.auditDetailLoadFailed),
          status: "audit_detail_error",
        });
      }
    };

    void load();
    return () => {
      if (activeLoadIdRef.current === loadId) {
        activeLoadIdRef.current = loadId + 1;
      }
    };
  }, [
    currentLoadKey,
    messages.requestLogs.auditDetailLoadFailed,
    messages.requestLogs.auditListLoadFailed,
    messages.requestLogs.loadFailed,
    requestId,
    selectedAuditId,
    selectedAuditParamLabel,
    selectedAuditParamPresent,
  ]);

  if (requestId === null || currentLoadKey === null) {
    return {
      ...INITIAL_STATE,
      status: "invalid_request_id",
    };
  }

  if (state.loadKey !== currentLoadKey) {
    return {
      ...INITIAL_STATE,
      loadKey: currentLoadKey,
      status: "request_loading",
    };
  }

  return state;
}
