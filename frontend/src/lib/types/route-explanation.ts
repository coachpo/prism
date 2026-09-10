export interface RouteExplanation {
  generation: string;
  published_at: string;
  observed_at: string;
  sample_completed_at: string;
  model_config_id: number;
  operation: string;
  completeness: "sampled" | "partial";
  boundary: string;
  planner_error?: string;
  exclusions: Array<{ model_config_id:number; path:string[]; target_type:string; reason:string }>;
  candidates: Array<{
    path: string[];
    strategy: string;
    model_config_id: number;
    model_id: string;
    terminal_target_id: number;
    planner_position: number | null;
    schedule: string;
    runtime_observed: boolean;
    reason: string;
    capacity: string;
  }>;
}
