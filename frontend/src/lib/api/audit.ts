import type {
  AuditLogDetail,
  AuditLogListResponse,
  AuditLogParams,
} from "../types";
import { buildQuery, request } from "./request";

type RequestLogAuditParams = Required<Pick<AuditLogParams, "from" | "to">> & { anchor_id?: number }
  & Pick<AuditLogParams, "limit" | "cursor">;

export const audit = {
  list: (params?: AuditLogParams) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<AuditLogListResponse>(`/api/audit/logs${query ? `?${query}` : ""}`);
  },
  listForRequestLog: (requestLogId: string, params: RequestLogAuditParams) => {
    const query = buildQuery({ ...params, request_log_id: requestLogId } as Record<string, string | number | boolean | null | undefined>);
    return request<AuditLogListResponse>(`/api/audit/logs${query ? `?${query}` : ""}`);
  },
  get: (id: number) => request<AuditLogDetail>(`/api/audit/logs/${id}`),
};
