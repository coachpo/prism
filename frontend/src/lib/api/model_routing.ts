import { request } from "./request";

export const modelRoutingDiagnostics = {
  get: (modelConfigId: number) => request<RoutingDiagnosticsResponse>(`/api/models/${modelConfigId}/routing-diagnostics`),
};

/**
 * Every read takes an optional signal. An abandoned panel that only ignores
 * its answer still holds a management admission slot until the query finishes,
 * so navigating away has to actually cancel, not just stop listening.
 */
export type RoutingDiagnosticTarget = {
  access_target_id: number;
  position: number;
  enabled_strategy_index: number | null;
  target_type: "model" | "connection";
  target_model_config_id: number | null;
  connection_id: number | null;
  target_mode: string | null;
  mode_match: boolean | null;
  operation_results: {
    operation_name: string;
    disposition: string;
    terminal_connection_ids: number[];
  }[];
};

export type RoutingDiagnosticRoute = {
  operation_name: string;
  accepted: boolean;
  configured_leaf_exists: boolean;
  statically_routable: boolean;
  access_target_ids: number[];
};

export type RoutingConfigurationWarning = {
  code: string;
  severity: "warning" | "danger";
  message: string;
  path: string;
  model_config_id: number;
  access_target_id: number | null;
  connection_id: number | null;
  operation_names: string[];
  details: Record<string, unknown>;
};

export type RoutingDiagnosticsResponse = {
  model_config_id: number;
  openai_accepted_format: string | null;
  strategy: { id: number; type: string } | null;
  accepted_operations: string[];
  targets: RoutingDiagnosticTarget[];
  operation_routes: RoutingDiagnosticRoute[];
  configuration_warnings: RoutingConfigurationWarning[];
};

// ---- Terminal Target batch copy (MC-B4) ----

export type TerminalTargetCopyResponse = {
  source_connection_id: number;
  items: {
    model_config_id: number;
    connection_summary: {
      id: number;
      name: string | null;
      endpoint_id: number;
      is_active: boolean;
      openai_text_capability: string | null;
      pricing_template: { id: number; name: string } | null;
      qps_limit: number | null;
      max_in_flight_non_stream: number | null;
      max_in_flight_stream: number | null;
      custom_header_count: number;
      custom_request_parameter_count: number;
    };
    access_target: { id: number; is_enabled: boolean; position: number };
  }[];
  configuration_warnings: RoutingConfigurationWarning[];
};

export const terminalTargetCopies = {
  create: (modelConfigId: number, connectionId: number, body: { destination_model_config_ids: number[]; enable_copies?: boolean }) =>
    request<TerminalTargetCopyResponse>(`/api/models/${modelConfigId}/connections/${connectionId}/copies`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
};
