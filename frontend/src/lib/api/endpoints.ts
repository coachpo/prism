import type {
  Endpoint,
  EndpointCreate,
  ConnectionDropdownResponse,
  EndpointReferenceBatchResponse,
  EndpointReferenceDetail,
  EndpointUpdate,
  EndpointVerifyRequest,
  EndpointVerifyResult,
  OrphanCleanupResponse,
} from "../types";
import { request } from "./request";

export const endpoints = {
  list: () => request<Endpoint[]>("/api/endpoints"),
  connections: () =>
    request<ConnectionDropdownResponse>("/api/endpoints/connections"),
  create: (data: EndpointCreate) =>
    request<Endpoint>("/api/endpoints", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  update: (id: number, data: EndpointUpdate) =>
    request<Endpoint>(`/api/endpoints/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  referencesBatch: (endpointIds: number[]) =>
    request<EndpointReferenceBatchResponse>("/api/endpoints/references/batch", {
      method: "POST",
      body: JSON.stringify({ endpoint_ids: endpointIds }),
    }),
  referencesDetail: (
    endpointId: number,
    params?: { limit?: number; cursor?: string },
  ) => {
    const query = new URLSearchParams();
    if (params?.limit != null) {
      query.set("limit", String(params.limit));
    }
    if (params?.cursor) {
      query.set("cursor", params.cursor);
    }
    const suffix = query.size > 0 ? `?${query.toString()}` : "";
    return request<EndpointReferenceDetail>(
      `/api/endpoints/${endpointId}/references${suffix}`,
    );
  },
  verify: (endpointId: number, data: EndpointVerifyRequest) =>
    request<EndpointVerifyResult>(`/api/endpoints/${endpointId}/verify`, {
      method: "POST",
      body: JSON.stringify(data),
    }),
  duplicate: (id: number) =>
    request<Endpoint>(`/api/endpoints/${id}/duplicate`, {
      method: "POST",
    }),
  orphanCleanup: (endpointId: number, connectionId: number) =>
    request<OrphanCleanupResponse>(
      `/api/endpoints/${endpointId}/orphan-connections/${connectionId}`,
      {
        method: "DELETE",
      },
    ),
  delete: (id: number) =>
    request<void>(`/api/endpoints/${id}`, { method: "DELETE" }),
};
