import type {
  CatalogPricingCommitRequest,
  CatalogPricingCommitResponse,
  CatalogPricingPreviewRequest,
  CatalogPricingPreviewResponse,
  PricingSetupReadiness,
  PricingTemplate,
  PricingTemplateConnectionsResponse,
  PricingTemplateCreate,
  PricingTemplateImpact,
  PricingTemplateImportCommitRequest,
  PricingTemplateImportRequest,
  PricingTemplateImportResponse,
  PricingTemplateListPage,
  PricingTemplateRevision,
  PricingTemplateUpdate,
} from "../types";
import { buildQuery, request } from "./request";

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
