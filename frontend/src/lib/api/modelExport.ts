import type {
  ExportRenderResponse,
  ExportSourceResponse,
  PiBindingResponse,
  PiBindRequest,
  PiCatalogSearchRequest,
  PiCatalogSearchResponse,
  PiModelReadResponse,
  PiOverrideFieldValue,
  PiRefreshCommitRequest,
  PiRefreshPreviewResponse,
  PiRenderRequest,
} from "@/lib/types";
import { request } from "./request";

/**
 * Single-model Pi management read: model identity, one catalog evidence
 * block, live exact-candidate evidence, and the persisted binding — no
 * targets, pricing, digest, or credential.
 */
export function fetchModelPi(
  modelConfigId: number,
): Promise<PiModelReadResponse> {
  return request<PiModelReadResponse>(`/api/models/${modelConfigId}/pi`, {
    cache: "no-store",
  });
}

export function fetchModelExportSource(
  signal?: AbortSignal,
): Promise<ExportSourceResponse> {
  return request<ExportSourceResponse>(`/api/models/exports/pi/source`, {
    cache: "no-store",
    signal,
  });
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
  return request<ExportRenderResponse>(`/api/models/exports/pi/render`, {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

/**
 * Runs one bounded pi.dev directory model-id search through Prism's backend.
 * The browser never contacts pi.dev: this returns the same trusted catalog
 * evidence the bind gate re-verifies server-side.
 */
export function searchModelPiCatalog(
  modelConfigId: number,
  body: PiCatalogSearchRequest,
  signal?: AbortSignal,
): Promise<PiCatalogSearchResponse> {
  return request<PiCatalogSearchResponse>(
    `/api/models/${modelConfigId}/pi/search`,
    {
      method: "POST",
      cache: "no-store",
      signal,
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );
}

export function bindModelPi(
  modelConfigId: number,
  body: PiBindRequest,
): Promise<PiBindingResponse> {
  return request<PiBindingResponse>(`/api/models/${modelConfigId}/pi/bind`, {
    method: "POST",
    cache: "no-store",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
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
  expected: PiRefreshCommitRequest,
): Promise<PiBindingResponse> {
  return request<PiBindingResponse>(
    `/api/models/${modelConfigId}/pi/refresh/commit`,
    {
      method: "POST",
      cache: "no-store",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(expected),
    },
  );
}

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
