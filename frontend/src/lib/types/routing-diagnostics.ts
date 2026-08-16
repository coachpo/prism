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
  | "structural_error";

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
}
