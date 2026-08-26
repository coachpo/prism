import { useRequestLogAuditDetail } from "./useRequestLogAuditDetail";
import { useRequestLogAuditList } from "./useRequestLogAuditList";
import { useRequestLogAuditRequest } from "./useRequestLogAuditRequest";
import type {
  AuditLogDetail,
  AuditLogListItem,
  DetailLanePhase,
  DetailLaneState,
  ListLanePhase,
  ListLaneState,
  RequestLanePhase,
  RequestLaneState,
} from "./requestLogAuditLanes";

interface UseDedicatedRequestLogAuditParams {
  cursor?: string;
  requestId: string | null;
  selectedAuditId: number | null;
  selectedAuditParamPresent: boolean;
  selectedAuditParamLabel: string | null;
}

export interface DedicatedRequestLogAuditState {
  request: RequestLaneState;
  list: ListLaneState;
  detail: DetailLaneState;
  retryRequest: () => void;
  retryList: () => void;
  retryDetail: () => void;
}

export function useDedicatedRequestLogAudit({
  cursor,
  requestId,
  selectedAuditId,
  selectedAuditParamPresent,
  selectedAuditParamLabel,
}: UseDedicatedRequestLogAuditParams): DedicatedRequestLogAuditState {
  const request = useRequestLogAuditRequest({ requestId });
  const list = useRequestLogAuditList({
    auditWindow: request.auditWindow,
    cursor,
    request: request.request,
    requestId,
  });
  const detail = useRequestLogAuditDetail({
    list: list.list,
    selectedAuditId,
    selectedAuditParamLabel,
    selectedAuditParamPresent,
  });

  return {
    request: request.request,
    list: list.list,
    detail: detail.detail,
    retryRequest: request.retryRequest,
    retryList: list.retryList,
    retryDetail: detail.retryDetail,
  };
}

export type {
  AuditLogDetail,
  AuditLogListItem,
  DetailLanePhase,
  DetailLaneState,
  ListLanePhase,
  ListLaneState,
  RequestLanePhase,
  RequestLaneState,
};
