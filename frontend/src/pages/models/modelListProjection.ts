import type {
  ModelConfig,
  ModelConfigListItem,
} from "@/lib/types";
import type { ManagedModelConfigListItem } from "@/lib/api/models";
import { getModelConnections } from "@/pages/model-detail/modelAccessTargetProjection";

export function toModelListItem(
  model: ModelConfig,
  existing?: ModelConfigListItem,
): ManagedModelConfigListItem {
  const connections = getModelConnections(model);
  return {
    id: model.id,
    profile_id: model.profile_id,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    openai_accepted_format: model.openai_accepted_format,
    openai_image_operations: model.openai_image_operations,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: model.loadbalance_strategy,
    access_targets: model.access_targets,
    is_enabled: model.is_enabled,
    connection_count: connections.length,
    active_connection_count: connections.filter(
      (connection) => connection.is_active,
    ).length,
    health_success_rate: existing?.health_success_rate ?? null,
    health_total_requests: existing?.health_total_requests ?? 0,
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
