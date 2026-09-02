// Shared row/list fixtures for the exit-mapping projection and cell suites.
// One builder set keeps the list cell, projection, and identity-flag tests on
// identical shapes, so a DTO drift fails all of them together.
import type { ManagedModelConfigListItem } from "@/lib/api/models";
import type {
  Connection,
  ModelAccessTarget,
  ModelRoutingSummary,
} from "@/lib/types";

export const ENTRY_MODEL_ID = "Entry-A";

export function routingSummary(
  overrides: Partial<ModelRoutingSummary> = {},
): ModelRoutingSummary {
  return {
    enabled_access_target_count: 0,
    total_access_target_count: 0,
    openai_mode: null,
    coverage: "full",
    operation_groups: [],
    single_truncated_access_target_ids: [],
    warning_codes: [],
    ...overrides,
  };
}

export function terminalTargetRow(
  id: number,
  position: number,
  options: {
    isEnabled?: boolean;
    endpointName?: string | null;
    upstreamModelId?: string | null;
  } = {},
): ModelAccessTarget {
  // SAFETY: test fixture builds a partially-populated wire shape; only the
  // fields read by the projection/cell under test (endpoint + upstream id)
  // are populated, matching what the backend list endpoint returns for a
  // Terminal Target row.
  const connection = {
    id: id + 900,
    profile_id: 1,
    api_family: "openai",
    endpoint_id: id + 100,
    endpoint:
      options.endpointName === undefined
        ? {
            id: id + 100,
            name: `endpoint-${id}`,
            base_url: "https://x",
            has_api_key: false,
            api_key_fingerprint: null,
            api_key_updated_at: null,
            config_revision: 1,
            created_at: "t",
            updated_at: "t",
          }
        : options.endpointName === null
          ? undefined
          : {
              id: id + 100,
              name: options.endpointName,
              base_url: "https://x",
              has_api_key: false,
              api_key_fingerprint: null,
              api_key_updated_at: null,
              config_revision: 1,
              created_at: "t",
              updated_at: "t",
            },
    is_active: true,
    priority: position,
    name: `conn-${id}`,
    auth_type: null,
    upstream_model_id:
      options.upstreamModelId === undefined
        ? ENTRY_MODEL_ID
        : options.upstreamModelId,
    custom_headers: null,
    custom_headers_redacted: null,
    custom_request_parameters: null,
    routing_schedule: null,
    routing_schedule_state: null,
    openai_text_capability: null,
    openai_image_capability: null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    pricing_template: null,
    created_at: "t",
    updated_at: "t",
  } as unknown as Connection;
  return {
    id,
    target_type: "connection",
    target_model_id: null,
    connection_id: connection.id,
    terminal_target_id: connection.id,
    position,
    is_enabled: options.isEnabled ?? true,
    target_model: null,
    connection,
    terminal_target: connection,
    created_at: "t",
    updated_at: "t",
  };
}

export function modelTargetRow(
  id: number,
  position: number,
  options: {
    isEnabled?: boolean;
    logicalModelId?: string | null;
    summaryModelId?: string | null;
  } = {},
): ModelAccessTarget {
  return {
    id,
    target_type: "model",
    target_model_id:
      options.logicalModelId === undefined
        ? "child-model"
        : options.logicalModelId,
    connection_id: null,
    terminal_target_id: null,
    position,
    is_enabled: options.isEnabled ?? true,
    // SAFETY: target_model is a summary object in the wire payload; the
    // fixture supplies the two fields the projection reads (id + model_id).
    target_model:
      options.summaryModelId === undefined
        ? null
        : options.summaryModelId === null
          ? null
          : ({
              id: id + 500,
              model_id: options.summaryModelId,
            } as unknown as ModelAccessTarget["target_model"]),
    connection: null,
    terminal_target: null,
    created_at: "t",
    updated_at: "t",
  };
}

export function entryModelListItem(
  accessTargets: ModelAccessTarget[],
): ManagedModelConfigListItem {
  // SAFETY: the list item fixture leaves most wire fields unset; the suites
  // only read identity/order/routing-summary fields populated above.
  return {
    id: 1,
    profile_id: 1,
    api_family: "openai",
    model_id: ENTRY_MODEL_ID,
    display_name: null,
    openai_accepted_format: null,
    openai_image_operations: null,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: accessTargets,
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    // Counts derive from the rows so the cell takes the exit-mapping branch,
    // not the zero-targets branch; suites that need a different summary
    // overwrite routing_summary explicitly.
    routing_summary: routingSummary({
      enabled_access_target_count: accessTargets.filter(
        (target) => target.is_enabled,
      ).length,
      total_access_target_count: accessTargets.length,
    }),
    created_at: "t",
    updated_at: "t",
  } as unknown as ManagedModelConfigListItem;
}
