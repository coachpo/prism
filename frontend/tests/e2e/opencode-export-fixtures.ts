import { createHash } from "node:crypto";
import type {
  OpenCodeExportModelRow,
  OpenCodeExportRenderResponse,
  OpenCodeExportSourceResponse,
} from "../../src/lib/types";

const metadata = {
  name: "Catalog GPT X",
  reasoning: true,
  tool_call: true,
  modalities_input: ["text"],
  modalities_output: ["text"],
  limit_context: 200000,
  limit_output: 16000,
};

const pricedModel: OpenCodeExportModelRow = {
  readiness: { status: "ready", blocking_reasons: [], repair_path: "/route/models/3" },
  model_config_id: 3,
  model_id: "codex/gpt-x",
  api_family: "openai",
  display_name: "Codex GPT X",
  is_enabled: true,
  direct_request_enabled: true,
  selectable: true,
  openai_accepted_format: "dual_native",
  npm: "@ai-sdk/openai",
  api_path: "/v1",
  source_metadata: metadata,
  override_metadata: { reasoning: false, limit_output: 8000 },
  merged_metadata: {
    ...metadata,
    name: "Codex GPT X",
    reasoning: false,
    limit_output: 8000,
  },
  metadata_provenance: {
    name: "prism_display_name",
    reasoning: "models_dev_override",
    tool_call: "models_dev_source",
    modalities_input: "models_dev_source",
    modalities_output: "models_dev_source",
    limit_context: "models_dev_source",
    limit_output: "models_dev_override",
  },
  missing_metadata: ["family", "release_date", "attachment", "temperature", "limit_input"],
  metadata_issues: [],
  targets: [{
    terminal_target_id: 11,
    position: 0,
    endpoint_id: 21,
    endpoint_name: "primary",
    openai_text_capability: "dual_native",
    pricing: {
      terminal_target_id: 11,
      template_kind: "tiered",
      currency_code: "USD",
      pricing_unit: "PER_1M",
      tier_threshold: 200000,
      base_card: {
        input_price: "3", output_price: "15", cached_input_price: "0.3",
        cache_creation_price: "3.75", reasoning_price: "15",
      },
      above_card: {
        input_price: "6", output_price: "30", cached_input_price: "0.6",
        cache_creation_price: "7.5", reasoning_price: "30",
      },
    },
  }],
  price_risk: { exportable: false, warning_codes: ["price_tier_unrepresentable"] },
  warnings: ["price_tier_unrepresentable", "metadata_incomplete"],
};

export const openCodeSource: OpenCodeExportSourceResponse = {
  target_version: "1.18.27",
  source_digest: "d".repeat(64),
  models: [pricedModel, {
    ...pricedModel,
    readiness: { status: "blocked", blocking_reasons: ["invalid_metadata_limits"], repair_path: "/route/models/4" },
    model_config_id: 4,
    model_id: "limits-missing",
    display_name: "Limits Missing",
    selectable: false,
    unselectable_reason: "invalid_metadata_limits",
    source_metadata: {},
    override_metadata: {},
    merged_metadata: { name: "Limits Missing" },
    metadata_provenance: { name: "prism_display_name" },
    missing_metadata: ["limit_context", "limit_output"],
    metadata_issues: [
      { field: "limit_context", reason: "missing", source: "none" },
      { field: "limit_output", reason: "missing", source: "none" },
    ],
  }],
};

/** UI fixture only; actual renderer and client exchange evidence has its own Go harness. */
export function openCodeRenderPayload(apiKey?: string): OpenCodeExportRenderResponse {
  const content = `${JSON.stringify({
    $schema: "https://opencode.ai/config.json",
    provider: {
      prism: {
        name: "Prism",
        ...(!apiKey ? { env: ["PRISM_API_KEY"] } : {}),
        ...(apiKey ? { options: { apiKey } } : {}),
        models: {
          "codex/gpt-x": {
            name: "Codex GPT X",
            provider: { npm: "@ai-sdk/openai", api: "http://127.0.0.1:8000/v1" },
            reasoning: false,
            tool_call: true,
            modalities: { input: ["text"], output: ["text"] },
            limit: { context: 200000, output: 8000 },
          },
        },
      },
    },
  }, null, 2)}\n`;
  return {
    target_version: "1.18.27",
    content,
    content_sha256: createHash("sha256").update(content, "utf8").digest("hex"),
    file_name: "opencode-prism.json",
    mime_type: "application/json;charset=utf-8",
    source_digest: openCodeSource.source_digest,
    model_results: [{
      model_config_id: 3, model_id: "codex/gpt-x", cost_exported: false,
      warning_codes: ["price_tier_unrepresentable", "metadata_incomplete"],
    }],
    warnings: ["price_tier_unrepresentable", "metadata_incomplete"],
  };
}
