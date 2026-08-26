import type {
 ExportPlatform,
 ExportRenderResponse,
 ExportSourceResponse,
 ManualEnhancementWire,
} from "@/features/models/export/exportTypes";
import { request } from "./core";

export interface ExportRenderRequestBody {
 expected_source_digest: string;
 model_config_ids: number[];
 base_url: string;
 provider_id: string;
 enhancements?: Record<number, ManualEnhancementWire | null>;
 credential: {
  include: boolean;
  api_key?: string;
 };
 default_model_config_id?: number;
}

export function fetchModelExportSource(
 platform: ExportPlatform,
 signal?: AbortSignal,
): Promise<ExportSourceResponse> {
 return request<ExportSourceResponse>(`/api/models/exports/${platform}/source`, {
  cache: "no-store",
  signal,
 });
}

/**
 * Renders one deterministic client file. The call goes through a plain fetch
 * on purpose: responses may embed the final plaintext Prism proxy key manually
 * entered by the operator, and those bytes must never enter any query cache or
 * persistent store. Upstream endpoint credentials are not part of this wire.
 * Callers hold the result in component memory only.
 */
export function renderModelExport(
 body: ExportRenderRequestBody,
 platform: ExportPlatform,
): Promise<ExportRenderResponse> {
 return request<ExportRenderResponse>(
  `/api/models/exports/${platform}/render`,
  {
   method: "POST",
   cache: "no-store",
   headers: { "Content-Type": "application/json" },
   body: JSON.stringify(body),
  },
 );
}
