import type {
  ExportSourceResponse,
  ExportRenderResponse,
  PiBindingResponse,
  PiRefreshPreviewResponse,
} from "@/features/models/export/exportTypes";
import { request } from "./request";

export type { ExportSourceResponse, ExportRenderResponse };

export function fetchModelExportSource(
  signal?: AbortSignal,
): Promise<ExportSourceResponse> {
  return request<ExportSourceResponse>(`/api/models/export/source`, {
    cache: "no-store",
    signal,
  });
}

export interface PiRenderRequest {
  expected_source_digest: string;
  model_config_ids: number[];
  base_url: string;
  provider_id: string;
  credential: {
    include: boolean;
    api_key?: string;
  };
  selections?: Record<
    number,
    { provider_id: string; model_id: string; api: string } | null
  >;
}

/**
 * Renders one deterministic Pi models.json file. The call goes through a plain fetch
 * on purpose: responses may embed the final plaintext Prism proxy key manually
 * entered by the operator, and those bytes must never enter any query cache or
 * persistent store.
 */
export function renderModelExport(
  body: PiRenderRequest,
): Promise<ExportRenderResponse> {
  return request<ExportRenderResponse>(`/api/models/export/render`, {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export interface PiBindRequest {
  provider_id?: string;
  catalog_model_id?: string;
  expected_catalog_revision: string;
}

export function bindModelPi(
  modelConfigId: number,
  body: PiBindRequest,
): Promise<PiBindingResponse> {
  return request<PiBindingResponse>(
    `/api/models/${modelConfigId}/pi/bind`,
    {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );
}

export function refreshModelPiPreview(
  modelConfigId: number,
): Promise<PiRefreshPreviewResponse> {
  return request<PiRefreshPreviewResponse>(
    `/api/models/${modelConfigId}/pi/refresh/preview`,
    {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    },
  );
}

export function refreshModelPiCommit(
  modelConfigId: number,
  expectedCatalogRevision: string,
): Promise<PiBindingResponse> {
  return request<PiBindingResponse>(
    `/api/models/${modelConfigId}/pi/refresh/commit`,
    {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        expected_catalog_revision: expectedCatalogRevision,
      }),
    },
  );
}

export type PiOverrideFieldValue =
  | string
  | boolean
  | number
  | string[]
  | Record<string, string | null>
  | Record<string, unknown>
  | null;

export function putModelPiOverride(
  modelConfigId: number,
  fields: Record<string, PiOverrideFieldValue>,
): Promise<PiBindingResponse> {
  return request<PiBindingResponse>(
    `/api/models/${modelConfigId}/pi/override`,
    {
      method: "PUT",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(fields),
    },
  );
}

export function clearModelPiOverride(
  modelConfigId: number,
): Promise<PiBindingResponse> {
  return request<PiBindingResponse>(
    `/api/models/${modelConfigId}/pi/override`,
    { method: "DELETE", cache: "no-store" },
  );
}

export function unbindModelPi(
  modelConfigId: number,
): Promise<PiBindingResponse> {
  return request<PiBindingResponse>(`/api/models/${modelConfigId}/pi`, {
    method: "DELETE",
    cache: "no-store",
  });
}
