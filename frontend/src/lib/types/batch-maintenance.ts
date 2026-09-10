export type BatchMaintenanceAction = "model_limits" | "model_strategy" | "target_pricing";
export interface BatchMaintenanceRequest {
  items: Array<{ id: number; context_limit?: number; output_limit?: number }>;
  reference_id?: number;
  client?: "opencode";
  confirm_manual_overrides: boolean;
  preview_token?: string;
}
export interface BatchMaintenanceResponse {
  action: BatchMaintenanceAction;
  preview_token: string;
  can_apply: boolean;
  applied: boolean;
  items: Array<{
    id: number;
    label: string;
    before: Record<string, unknown>;
    after: Record<string, unknown>;
    error?: string;
    manual_override: boolean;
  }>;
}
