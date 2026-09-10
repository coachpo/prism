import type {
  ClientReadiness,
  ExportPriceRisk,
  ExportRenderResponse,
  ExportSourceTargetRow,
} from "./model-export";

/** Safe, persisted models.dev leaves consumed by the OpenCode exporter. */
export interface OpenCodeExportMetadata {
  name?: string;
  family?: string;
  release_date?: string;
  attachment?: boolean;
  reasoning?: boolean;
  tool_call?: boolean;
  temperature?: boolean;
  modalities_input?: string[];
  modalities_output?: string[];
  limit_context?: number;
  limit_input?: number;
  limit_output?: number;
}

export interface OpenCodeExportModelRow {
  readiness: ClientReadiness;
  model_config_id: number;
  model_id: string;
  api_family: string;
  display_name: string | null;
  is_enabled: boolean;
  direct_request_enabled: boolean;
  selectable: boolean;
  unselectable_reason?: string;
  openai_accepted_format?: string;
  openai_image_operations?: string;
  npm: string;
  api_path: string;
  source_metadata: Record<string, unknown>;
  override_metadata: Record<string, unknown>;
  merged_metadata: OpenCodeExportMetadata;
  metadata_provenance: Record<string, string>;
  missing_metadata: string[];
  metadata_issues: Array<{ field: string; reason: string; source: string }>;
  targets: ExportSourceTargetRow[];
  price_risk: ExportPriceRisk;
  warnings?: string[];
}

export interface OpenCodeExportSourceResponse {
  target_version: string;
  source_digest: string;
  models: OpenCodeExportModelRow[];
  warnings?: string[];
}

export interface OpenCodeExportRenderRequest {
  expected_source_digest: string;
  model_config_ids: number[];
  base_url: string;
  provider_id: string;
  credential: { include: boolean; api_key?: string };
}

export type OpenCodeExportRenderResponse = ExportRenderResponse;
