import type { ChainResponse, RequestLogDetail } from "../types/request-logs";
import type {
  ProxyApiKeyFilterOptionsResponse,
  RequestLogListResponse,
  StatsRequestParams,
} from "../types";
import { buildQuery, request } from "./request";

function buildStatsQuery(params?: StatsRequestParams) {
  const query = new URLSearchParams();
  if (!params) return "";

  for (const [key, value] of Object.entries(params)) {
    const values = Array.isArray(value) ? value : [value];
    for (const item of values) {
      if (item !== undefined && item !== null && item !== "") {
        query.append(key, String(item));
      }
    }
  }
  return query.toString();
}

export const requestStats = {
  requests: (params?: StatsRequestParams) => {
    const query = buildStatsQuery(params);
    return request<RequestLogListResponse>(
      `/api/stats/requests${query ? `?${query}` : ""}`,
    );
  },
  chains: (
    params?: StatsRequestParams & {
      view?: string;
      chain_cursor?: string;
      sort_by?: string;
      sort_order?: string;
    },
  ) => {
    const query = buildStatsQuery(params);
    return request<ChainResponse>(
      `/api/stats/requests${query ? `?${query}` : ""}`,
    );
  },
  exportCsv: async (params?: StatsRequestParams): Promise<Blob> => {
    const query = buildStatsQuery(params);
    return request<Blob>(
      `/api/stats/requests/export${query ? `?${query}` : ""}`,
      {
        headers: { Accept: "text/csv" },
      },
      { responseType: "blob" },
    );
  },
  requestDetail: (requestId: string) =>
    request<RequestLogDetail>(`/api/stats/requests/${requestId}`),
  proxyApiKeyFilterOptions: (params?: {
    q?: string;
    from_time?: string;
    to_time?: string;
    limit?: number;
    cursor?: string;
    selected_id?: number;
  }) => {
    const query = buildQuery(
      params as Record<string, string | number | undefined> | undefined,
    );
    return request<ProxyApiKeyFilterOptionsResponse>(
      `/api/stats/request-filter-options/proxy-api-keys${query ? `?${query}` : ""}`,
    );
  },
};
