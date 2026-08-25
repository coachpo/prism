import type {
  Connection,
  ConnectionDropdownResponse,
  ConnectionReferencesResponse,
  Endpoint,
  EndpointCreate,
  EndpointReferenceBatchResponse,
  EndpointReferenceDetail,
  EndpointUpdate,
  EndpointVerifyRequest,
  EndpointVerifyResult,
  OrphanCleanupResponse,
  PricingTemplate,
  PricingTemplateListPage,
  PricingTemplateConnectionsResponse,
  PricingTemplateImpact,
  PricingTemplateImportCommitRequest,
  PricingTemplateRevision,
  PricingTemplateCreate,
  PricingTemplateImportRequest,
  PricingTemplateImportResponse,
  PricingTemplateUpdate,
  PricingSetupReadiness,
  CatalogPricingPreviewRequest,
  CatalogPricingPreviewResponse,
  CatalogPricingCommitRequest,
  CatalogPricingCommitResponse,
} from "../types";
import { buildQuery, request } from "./core";

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

export const connections = {
  list: () => request<Connection[]>("/api/connections"),
  get: (id: number) => request<Connection>(`/api/connections/${id}`),
  references: (id: number) =>
    request<ConnectionReferencesResponse>(`/api/connections/${id}/references`),
};

export const pricingTemplates = {
  list: () => request<PricingTemplate[]>("/api/pricing-templates"),
  listPage: (params?: { limit?: number; cursor?: string; q?: string }) => {
    const query = buildQuery(
      params as Record<string, string | number | null | undefined> | undefined,
    );
    return request<PricingTemplateListPage>(`/api/pricing-templates?${query}`);
  },
  setupReadiness: (generation: string) =>
    request<PricingSetupReadiness>(
      `/api/pricing-templates?limit=1&include=setup_readiness&expected_route_witness_generation=${encodeURIComponent(generation)}`,
    ),
  get: (id: number) => request<PricingTemplate>(`/api/pricing-templates/${id}`),
  create: (data: PricingTemplateCreate) =>
    request<PricingTemplate>("/api/pricing-templates", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  importTemplates: (data: PricingTemplateImportRequest) =>
    request<PricingTemplateImportResponse>("/api/pricing-templates/import", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  importCommit: (data: PricingTemplateImportCommitRequest) =>
    request<PricingTemplateImportResponse>(
      "/api/pricing-templates/import/commit",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
    ),
  update: (id: number, data: PricingTemplateUpdate) =>
    request<PricingTemplate>(`/api/pricing-templates/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  delete: (id: number) =>
    request<void>(`/api/pricing-templates/${id}`, { method: "DELETE" }),
  connections: (id: number) =>
    request<PricingTemplateConnectionsResponse>(
      `/api/pricing-templates/${id}/connections`,
    ),
  revisions: (id: number) =>
    request<PricingTemplateRevision[]>(
      `/api/pricing-templates/${id}/revisions`,
    ),
  impact: (id: number) =>
    request<PricingTemplateImpact>(`/api/pricing-templates/${id}/impact`),
  catalogPreview: (data: CatalogPricingPreviewRequest) =>
    request<CatalogPricingPreviewResponse>(
      "/api/pricing-templates/catalog/preview",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
    ),
  catalogCommit: (data: CatalogPricingCommitRequest) =>
    request<CatalogPricingCommitResponse>(
      "/api/pricing-templates/catalog/commit",
      {
        method: "POST",
        body: JSON.stringify(data),
      },
    ),
};
