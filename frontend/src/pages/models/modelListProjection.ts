import type {
  ModelConfig,
  ModelConfigListItem,
} from "@/lib/types";
import type { ManagedModelConfigListItem } from "@/lib/api/models";

export function toModelListItem(
  model: ModelConfig,
  existing: ModelConfigListItem,
): ManagedModelConfigListItem {
  return {
    id: model.id,
    profile_id: model.profile_id,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    openai_accepted_format: model.openai_accepted_format,
    openai_image_operations: model.openai_image_operations,
    direct_request_enabled: model.direct_request_enabled,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: model.loadbalance_strategy,
    access_targets: model.access_targets,
    is_enabled: model.is_enabled,
    connection_count: existing.connection_count,
    active_connection_count: existing.active_connection_count,
    health_success_rate: existing.health_success_rate,
    health_total_requests: existing.health_total_requests,
    routing_summary: existing.routing_summary,
    incoming_model_target_count: model.incoming_model_target_count,
    configuration_warnings: model.configuration_warnings,
    created_at: model.created_at,
    updated_at: model.updated_at,
  };
}

export function patchModelListItemFromDetail(
  models: ModelConfigListItem[],
  model: ModelConfig,
): ModelConfigListItem[] {
  return models.map((item) =>
    item.id === model.id ? toModelListItem(model, item) : item,
  );
}
