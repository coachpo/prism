import type {
  ApiFamily,
  Connection,
  ModelAccessTarget,
  ModelAccessTargetModelMutation,
  ModelAccessTargetMutation,
  ModelConfig,
  ModelConfigListItem,
  ModelConfigUpdate,
  ModelConfigCreate,
  Vendor,
} from "@/lib/types";

export type SubmitEventLike = Pick<Event, "preventDefault">;

export interface ModelFormData {
  vendor_id: number | null;
  api_family: ApiFamily;
  model_id: string;
  display_name: string;
  loadbalance_strategy_id: number | null;
  access_targets: ModelAccessTargetMutation[];
  is_enabled: boolean;
  last_auto_display_name?: string | null;
}

export type ModelFormValidationError =
  | "api_family_required"
  | "loadbalance_strategy_required"
  | "access_target_required";

const DEFAULT_API_FAMILY: ApiFamily = "openai";

export const DEFAULT_MODEL_FORM_DATA: ModelFormData = {
  vendor_id: null,
  api_family: DEFAULT_API_FAMILY,
  model_id: "",
  display_name: "",
  loadbalance_strategy_id: null,
  access_targets: [],
  is_enabled: false,
  last_auto_display_name: "",
};

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

function shouldAutoSyncDisplayName(formData: ModelFormData): boolean {
  const displayName = formData.display_name ?? "";
  return displayName.trim() === "" || displayName === (formData.last_auto_display_name ?? "");
}

export function accessTargetKey(target: Pick<ModelAccessTargetMutation, "target_type" | "target_model_id" | "connection_id">): string | null {
  if (target.target_type === "model" && target.target_model_id?.trim()) {
    return `model:${target.target_model_id.trim()}`;
  }
  if (target.target_type === "connection" && typeof target.connection_id === "number") {
    return `connection:${target.connection_id}`;
  }
  return null;
}

export function accessTargetToMutation(target: ModelAccessTarget): ModelAccessTargetMutation | null {
  if (target.target_type === "model" && target.target_model_id) {
    return {
      target_type: "model",
      target_model_id: target.target_model_id,
      position: target.position,
      is_enabled: target.is_enabled,
    };
  }
  if (target.target_type === "connection" && target.connection_id !== null) {
    return {
      target_type: "connection",
      connection_id: target.connection_id,
      position: target.position,
      is_enabled: target.is_enabled,
    };
  }
  return null;
}

export function normalizeAccessTargetMutations(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): ModelAccessTargetMutation[] {
  const seen = new Set<string>();
  const normalized: ModelAccessTargetMutation[] = [];
  for (const target of targets ?? []) {
    const key = accessTargetKey(target);
    if (!key || seen.has(key)) {
      continue;
    }
    seen.add(key);
    if (target.target_type === "model") {
      normalized.push({
        target_type: "model",
        target_model_id: target.target_model_id.trim(),
        position: normalized.length,
        is_enabled: target.is_enabled ?? true,
      });
    } else {
      normalized.push({
        target_type: "connection",
        connection_id: target.connection_id,
        position: normalized.length,
        is_enabled: target.is_enabled ?? true,
      });
    }
  }
  return normalized;
}

export function moveAccessTarget(
  targets: ModelAccessTargetMutation[],
  fromIndex: number,
  toIndex: number,
): ModelAccessTargetMutation[] {
  const normalized = normalizeAccessTargetMutations(targets);
  if (
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= normalized.length ||
    toIndex >= normalized.length ||
    fromIndex === toIndex
  ) {
    return normalized;
  }
  const nextTargets = [...normalized];
  const [movedTarget] = nextTargets.splice(fromIndex, 1);
  if (!movedTarget) {
    return normalized;
  }
  nextTargets.splice(toIndex, 0, movedTarget);
  return normalizeAccessTargetMutations(nextTargets);
}

export function appendAccessTarget(
  targets: ModelAccessTargetMutation[],
  target: Omit<ModelAccessTargetMutation, "position">,
): ModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations([
    ...normalizeAccessTargetMutations(targets),
    { ...target, position: targets.length } as ModelAccessTargetMutation,
  ]);
}

export function removeAccessTarget(targets: ModelAccessTargetMutation[], index: number): ModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations(normalizeAccessTargetMutations(targets).filter((_, currentIndex) => currentIndex !== index));
}

export function setAccessTargetEnabled(
  targets: ModelAccessTargetMutation[],
  index: number,
  isEnabled: boolean,
): ModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations(
    normalizeAccessTargetMutations(targets).map((target, currentIndex) =>
      currentIndex === index ? { ...target, is_enabled: isEnabled } : target,
    ),
  );
}

type EditableModelFormSource = Pick<
  ModelConfig,
  "vendor_id" | "api_family" | "model_id" | "display_name" | "loadbalance_strategy_id" | "access_targets" | "is_enabled"
>;

export function getModelConnections(
  model: Pick<ModelConfig, "access_targets"> | Pick<ModelConfigListItem, "access_targets">,
): Connection[] {
  return model.access_targets
    .filter((target) => target.target_type === "connection" && target.connection)
    .sort((left, right) => left.position - right.position)
    .map((target) => ({ ...(target.connection as Connection), priority: target.position }));
}

export function createEditModelFormData(model: EditableModelFormSource): ModelFormData {
  const vendorId = resolveModelVendorId(model);
  const displayName = model.display_name || "";
  return {
    vendor_id: vendorId,
    api_family: resolveModelApiFamily(model),
    model_id: model.model_id,
    display_name: displayName,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    access_targets: normalizeAccessTargetMutations(
      model.access_targets.map(accessTargetToMutation).filter((target): target is ModelAccessTargetMutation => target !== null),
    ),
    is_enabled: model.is_enabled,
    last_auto_display_name: displayName === model.model_id ? model.model_id : displayName,
  };
}

export function createNewModelFormData(_vendors: Vendor[], loadbalanceStrategyId: number | null): ModelFormData {
  return {
    ...DEFAULT_MODEL_FORM_DATA,
    loadbalance_strategy_id: loadbalanceStrategyId,
  };
}

export function getAccessTargetOptionKeys(
  modelTargets: Pick<ModelConfigListItem, "model_id">[],
): Set<string> {
  return new Set(modelTargets.map((model) => `model:${model.model_id}`));
}

export function validateModelFormData(
  formData: ModelFormData,
  availableAccessTargetKeys?: Iterable<string>,
): ModelFormValidationError | null {
  if (!formData.api_family) {
    return "api_family_required";
  }
  if (formData.loadbalance_strategy_id === null) {
    return "loadbalance_strategy_required";
  }
  const normalizedTargets = normalizeAccessTargetMutations(formData.access_targets);
  const enabledTargets = normalizedTargets.filter((target) => target.is_enabled !== false);
  if (formData.is_enabled && enabledTargets.length === 0) {
    return "access_target_required";
  }
  if (!availableAccessTargetKeys) {
    return null;
  }
  const validKeys = new Set(availableAccessTargetKeys);
  return normalizedTargets.some((target) => {
    if (target.target_type !== "model") {
      return false;
    }
    const key = accessTargetKey(target);
    return !key || !validKeys.has(key);
  }) ? "access_target_required" : null;
}

function getRequiredLoadbalanceStrategyId(formData: ModelFormData): number {
  if (formData.loadbalance_strategy_id === null) {
    throw new Error("loadbalance_strategy_id is required");
  }
  return formData.loadbalance_strategy_id;
}

export function normalizeModelAccessTargetMutations(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): ModelAccessTargetModelMutation[] {
  return normalizeAccessTargetMutations(targets)
    .filter((target): target is ModelAccessTargetModelMutation => target.target_type === "model")
    .map((target, position) => ({ ...target, position }));
}

function getNormalizedRoutingState(formData: ModelFormData) {
  return {
    loadbalance_strategy_id: getRequiredLoadbalanceStrategyId(formData),
    access_targets: normalizeModelAccessTargetMutations(formData.access_targets),
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

export function setLoadbalanceStrategyIdOnForm(formData: ModelFormData, strategyId: number | null): ModelFormData {
  return { ...formData, loadbalance_strategy_id: strategyId };
}

export function setApiFamilyOnForm(formData: ModelFormData, apiFamily: ApiFamily): ModelFormData {
  if (formData.api_family === apiFamily) {
    return formData;
  }
  return {
    ...formData,
    api_family: apiFamily,
    access_targets: [],
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
  return { ...formData, display_name: displayName };
}

export function getAccessTargetModelsForApiFamily(
  models: ModelConfigListItem[],
  apiFamily: ApiFamily,
  excludedModelId?: string,
): ModelConfigListItem[] {
  return models.filter(
    (model) => model.api_family === apiFamily && (!excludedModelId || model.model_id !== excludedModelId),
  );
}

export function toModelListItem(model: ModelConfig, existing?: ModelConfigListItem): ModelConfigListItem {
  const connections = getModelConnections(model);
  return {
    id: model.id,
    profile_id: model.profile_id,
    vendor_id: resolveModelVendorId(model),
    vendor: model.vendor,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: model.loadbalance_strategy,
    access_targets: model.access_targets,
    is_enabled: model.is_enabled,
    connection_count: connections.length,
    active_connection_count: connections.filter((connection) => connection.is_active).length,
    health_success_rate: existing?.health_success_rate ?? null,
    health_total_requests: existing?.health_total_requests ?? 0,
    created_at: model.created_at,
    updated_at: model.updated_at,
  };
}
