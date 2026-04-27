import type {
  ApiFamily,
  ModelConfig,
  ModelConfigCreate,
  ModelConfigListItem,
  ModelConfigUpdate,
  ModelType,
  ProxyTarget,
  Vendor,
} from "@/lib/types";

export type SubmitEventLike = Pick<Event, "preventDefault">;
export interface ModelFormData {
  vendor_id: number | null;
  api_family: ApiFamily;
  model_id: string;
  display_name: string;
  model_type: ModelType;
  proxy_targets: ProxyTarget[];
  loadbalance_strategy_id: number | null;
  is_enabled: boolean;
  last_auto_display_name?: string | null;
}

const DEFAULT_API_FAMILY: ApiFamily = "openai";

export function resolveModelApiFamily(
  model: Pick<ModelConfigListItem, "api_family"> | Pick<ModelConfig, "api_family">,
): ApiFamily {
  return model.api_family;
}

function resolveModelVendorId(
  model: Pick<ModelConfigListItem, "vendor_id"> | Pick<ModelConfig, "vendor_id">,
): number | null {
  return model.vendor_id ?? null;
}

export const DEFAULT_MODEL_FORM_DATA: ModelFormData = {
  vendor_id: null,
  api_family: DEFAULT_API_FAMILY,
  model_id: "",
  display_name: "",
  model_type: "native",
  proxy_targets: [],
  loadbalance_strategy_id: null,
  is_enabled: true,
  last_auto_display_name: "",
};

function shouldAutoSyncDisplayName(formData: ModelFormData): boolean {
  const displayName = formData.display_name ?? "";
  return displayName.trim() === "" || displayName === (formData.last_auto_display_name ?? "");
}

export function normalizeProxyTargets(proxyTargets: ProxyTarget[] | null | undefined): ProxyTarget[] {
  const seenTargetIds = new Set<string>();

  return (proxyTargets ?? [])
    .map((target) => ({
      target_model_id: target.target_model_id.trim(),
      position: target.position,
    }))
    .filter((target) => {
      if (!target.target_model_id || seenTargetIds.has(target.target_model_id)) {
        return false;
      }

      seenTargetIds.add(target.target_model_id);
      return true;
    })
    .map((target, index) => ({
      target_model_id: target.target_model_id,
      position: index,
    }));
}

function getNormalizedRoutingState(formData: ModelFormData) {
  if (formData.model_type === "native") {
    return {
      model_type: "native" as const,
      proxy_targets: [] as [],
      loadbalance_strategy_id: formData.loadbalance_strategy_id ?? null,
    };
  }

  return {
    model_type: "proxy" as const,
    proxy_targets: normalizeProxyTargets(formData.proxy_targets),
    loadbalance_strategy_id: null,
  };
}

export function moveProxyTarget(proxyTargets: ProxyTarget[], fromIndex: number, toIndex: number): ProxyTarget[] {
  if (
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= proxyTargets.length ||
    toIndex >= proxyTargets.length ||
    fromIndex === toIndex
  ) {
    return normalizeProxyTargets(proxyTargets);
  }

  const nextTargets = [...normalizeProxyTargets(proxyTargets)];
  const [movedTarget] = nextTargets.splice(fromIndex, 1);

  if (!movedTarget) {
    return normalizeProxyTargets(proxyTargets);
  }

  nextTargets.splice(toIndex, 0, movedTarget);
  return normalizeProxyTargets(nextTargets);
}

export function appendProxyTarget(proxyTargets: ProxyTarget[], targetModelId: string): ProxyTarget[] {
  return normalizeProxyTargets([
    ...normalizeProxyTargets(proxyTargets),
    { target_model_id: targetModelId, position: proxyTargets.length },
  ]);
}

export function removeProxyTarget(proxyTargets: ProxyTarget[], targetModelId: string): ProxyTarget[] {
  return normalizeProxyTargets(
    normalizeProxyTargets(proxyTargets).filter((target) => target.target_model_id !== targetModelId),
  );
}

export function createEditModelFormData(model: ModelConfigListItem): ModelFormData {
  const vendorId = resolveModelVendorId(model);
  const displayName = model.display_name || "";

  return {
    vendor_id: vendorId,
    api_family: resolveModelApiFamily(model),
    model_id: model.model_id,
    display_name: displayName,
    model_type: model.model_type,
    proxy_targets: normalizeProxyTargets(model.proxy_targets),
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    is_enabled: model.is_enabled,
    last_auto_display_name: displayName === model.model_id ? model.model_id : displayName,
  };
}

export function createNewModelFormData(
  _vendors: Vendor[],
  loadbalanceStrategyId: number | null,
): ModelFormData {
  return {
    ...DEFAULT_MODEL_FORM_DATA,
    loadbalance_strategy_id: loadbalanceStrategyId,
  };
}

export function toModelCreatePayload(formData: ModelFormData): ModelConfigCreate {
  const normalizedDisplayName = formData.display_name?.trim() || formData.model_id.trim();

  return {
    vendor_id: formData.vendor_id ?? null,
    api_family: formData.api_family,
    model_id: formData.model_id,
    display_name: normalizedDisplayName,
    is_enabled: formData.is_enabled,
    ...getNormalizedRoutingState(formData),
  };
}

export function toModelUpdatePayload(formData: ModelFormData): ModelConfigUpdate {
  return {
    vendor_id: formData.vendor_id ?? null,
    api_family: formData.api_family,
    display_name: formData.display_name || null,
    model_id: formData.model_id,
    is_enabled: formData.is_enabled,
    ...getNormalizedRoutingState(formData),
  };
}

export function setModelTypeOnForm(
  formData: ModelFormData,
  modelType: "native" | "proxy",
  defaultLoadbalanceStrategyId: number | null,
): ModelFormData {
  return {
    ...formData,
    model_type: modelType,
    proxy_targets: [],
    loadbalance_strategy_id:
      modelType === "native"
        ? formData.loadbalance_strategy_id ?? defaultLoadbalanceStrategyId
        : null,
  };
}

export function setLoadbalanceStrategyIdOnForm(
  formData: ModelFormData,
  strategyId: number | null,
): ModelFormData {
  return { ...formData, loadbalance_strategy_id: strategyId };
}

export function setApiFamilyOnForm(formData: ModelFormData, apiFamily: ApiFamily): ModelFormData {
  if (formData.api_family === apiFamily) {
    return formData;
  }

  return {
    ...formData,
    api_family: apiFamily,
    proxy_targets: formData.model_type === "proxy" ? [] : formData.proxy_targets,
  };
}

export function setModelIdOnForm(formData: ModelFormData, modelId: string): ModelFormData {
  const autoDisplayName = modelId;

  return {
    ...formData,
    model_id: modelId,
    display_name: shouldAutoSyncDisplayName(formData) ? autoDisplayName : formData.display_name,
    last_auto_display_name: autoDisplayName,
  };
}

export function setDisplayNameOnForm(formData: ModelFormData, displayName: string): ModelFormData {
  return {
    ...formData,
    display_name: displayName,
  };
}

export function getNativeModelsForApiFamily(
  models: ModelConfigListItem[],
  apiFamily: ApiFamily,
  excludedModelId?: string,
): ModelConfigListItem[] {
  return models.filter(
    (model) =>
      model.model_type === "native" &&
      model.api_family === apiFamily &&
      (!excludedModelId || model.model_id !== excludedModelId),
  );
}

export function toModelListItem(
  model: ModelConfig,
  _existing?: ModelConfigListItem,
): ModelConfigListItem {
  return {
    id: model.id,
    vendor_id: resolveModelVendorId(model),
    vendor: model.vendor,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    model_type: model.model_type,
    proxy_targets: normalizeProxyTargets(model.proxy_targets),
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: model.loadbalance_strategy,
    is_enabled: model.is_enabled,
    connection_count: model.connections.length,
    active_connection_count: model.connections.filter((connection) => connection.is_active).length,
    health_success_rate: _existing?.health_success_rate ?? null,
    health_total_requests: _existing?.health_total_requests ?? 0,
    created_at: model.created_at,
    updated_at: model.updated_at,
  };
}
