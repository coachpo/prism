// Static routing diagnostics types shared with the management API.
// The backend analyzer is authoritative; the frontend only presents.

export type DiagnosticsCoverage = "full" | "partial" | "none" | "not_applicable";

export type DiagnosticsDisposition =
  | "candidate"
  | "disabled"
  | "inactive"
  | "incompatible"
  | "no_eligible_leaf"
  | "truncated_by_single"
  | "structural_error"
  | "cycle"
  | "depth_exceeded";

export interface ConfigurationWarning {
  code: string;
  severity: "warning" | "danger";
  message: string;
  path: string;
  model_config_id: number | null;
  access_target_id: number | null;
  connection_id: number | null;
  operation_names: string[];
  details: Record<string, unknown> | null;
}

export interface DiagnosticsOperationResult {
  operation_name: string;
  disposition: DiagnosticsDisposition;
  terminal_connection_ids?: number[];
}

export interface DiagnosticsTarget {
  access_target_id: number;
  authored_stage_position: number;
  enabled_strategy_index: number | null;
  target_model_config_id?: number;
  connection_id?: number;
  coverage: DiagnosticsCoverage;
  supported_operations: string[];
  unsupported_accepted_operations: string[];
  operation_results: DiagnosticsOperationResult[];
  schedule?: DiagnosticsRoutingSchedule;
}

export interface DiagnosticsRoutingWindow {
  weekday_mask: number;
  start_minute: number;
  end_minute: number;
}

export interface DiagnosticsRoutingSchedule {
  timezone: string;
  windows: DiagnosticsRoutingWindow[];
  covers_full_week: boolean;
}

export interface DiagnosticsStage {
  stage: string;
  order: number;
  entered_when: string;
  targets: DiagnosticsTarget[];
}

export interface RoutingDiagnosticRoute {
  operation_name: string;
  accepted: boolean;
  configured_leaf_exists: boolean;
  statically_routable: boolean;
  resolved_stage: string | null;
  access_target_ids: number[];
}

export interface DiagnosticsOperationCoverage {
  operation_name: string;
  accepted: boolean;
  capability_covered: boolean;
  statically_routable: boolean;
  resolved_stage: string | null;
  compatible_access_target_ids: number[];
  access_target_ids: number[];
}

export interface RoutingDiagnosticsResponse {
  model_config_id: number;
  openai_accepted_format: string | null;
  strategy: { id: number; type: string };
  accepted_operations: string[];
  stages: DiagnosticsStage[];
  targets: DiagnosticsTarget[];
  operation_routes: RoutingDiagnosticRoute[];
  operation_coverage: DiagnosticsOperationCoverage[];
  configuration_warnings: ConfigurationWarning[];
}
