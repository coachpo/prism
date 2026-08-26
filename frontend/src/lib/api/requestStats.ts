import type { ChainResponse, RequestLogDetail } from "../types/request-logs";
import type {
  ProxyApiKeyFilterOptionsResponse,
  RequestLogListResponse,
  StatsRequestParams,
} from "../types";
import { buildQuery, request } from "./request";

function buildStatsQuery(params?: StatsRequestParams) {
  return buildQuery(
    params as
      | Record<string, string | number | boolean | null | undefined>
      | undefined,
  );
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
