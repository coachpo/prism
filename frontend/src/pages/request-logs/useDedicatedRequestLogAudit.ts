import { useCallback } from "react";

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
  QueryCoverage,
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
  /** The most recent successful read across the three lanes, or null. */
  lastFetchedAt: string | null;
  /** True while any lane has a read in flight. */
  refreshing: boolean;
  /** Re-reads every lane that feeds this page, not a third of them. */
  refresh: () => void;
  retryRequest: () => void;
  retryList: () => void;
  retryDetail: () => void;
}

function latestFetchedAt(candidates: (string | null)[]): string | null {
  return candidates.reduce<string | null>(
    (latest, candidate) =>
      candidate !== null && (latest === null || candidate > latest)
        ? candidate
        : latest,
    null,
  );
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

  const refresh = useCallback(() => {
    request.retryRequest();
    list.retryList();
    detail.retryDetail();
  }, [detail, list, request]);

  return {
    request: request.request,
    list: list.list,
    detail: detail.detail,
    lastFetchedAt: latestFetchedAt([
      request.request.fetchedAt,
      list.list.fetchedAt,
      detail.detail.fetchedAt,
    ]),
    refreshing:
      request.request.phase === "loading" ||
      list.list.phase === "loading" ||
      detail.detail.phase === "loading",
    refresh,
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
  QueryCoverage,
  RequestLanePhase,
  RequestLaneState,
};
