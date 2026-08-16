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

export type DiagnosticsStageName = "model_targets" | "terminal_targets";

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

export interface DiagnosticsStrategyResult {
  id: number;
  type: string;
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

export interface DiagnosticsStage {
  stage: DiagnosticsStageName;
  order: number;
  entered_when: string;
  targets: DiagnosticsTarget[];
}

export interface DiagnosticsOperationCoverage {
  operation_name: string;
  accepted: boolean;
  capability_covered: boolean;
  statically_routable: boolean;
  resolved_stage: DiagnosticsStageName | null;
  compatible_access_target_ids: number[];
  access_target_ids: number[];
}

export interface RoutingDiagnosticsResult {
  model_config_id: number;
  strategy: DiagnosticsStrategyResult;
  accepted_operations: string[];
  stages: DiagnosticsStage[];
  operation_coverage: DiagnosticsOperationCoverage[];
  configuration_warnings: ConfigurationWarning[];
}

export type RoutingOperationGroupStatus =
  | "not_accepted"
  | "routable"
  | "compatible_but_ineligible"
  | "uncovered";

export interface RoutingOperationGroup {
  group: "chat_completions" | "responses";
  status: RoutingOperationGroupStatus;
}

export interface RoutingSummary {
  enabled_access_target_count: number;
  total_access_target_count: number;
  coverage: DiagnosticsCoverage;
  operation_groups: RoutingOperationGroup[];
  single_truncated_stages: DiagnosticsStageName[];
  warning_codes: string[];
}

export interface RoutingDiagnosticsPreviewRequest {
  openai_accepted_format?: string | null;
  loadbalance_strategy_id?: number | null;
  is_enabled?: boolean | null;
}

export const CONFIGURATION_WARNING_CODES = {
  targetIncompatible: "openai_target_incompatible",
  targetPartialCoverage: "openai_target_partial_coverage",
  operationUncovered: "openai_operation_uncovered",
  singleTruncatesTargets: "single_strategy_truncates_targets",
} as const;
