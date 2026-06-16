import type {
  ApiFamily,
  Connection,
  ModelAccessTarget,
  ModelAccessTargetModelMutation,
  ModelAccessTargetMutation,
  ModelConfig,
  ModelConfigListItem,
} from "@/lib/types";
import type {
  ManagedModelConfigCreate,
  ManagedModelConfigListItem,
  ManagedModelConfigUpdate,
} from "@/lib/api/management";

export type SubmitEventLike = Pick<Event, "preventDefault">;

export interface ModelFormData {
  api_family: ApiFamily;
  model_id: string;
  display_name: string;
  loadbalance_strategy_id: number | null;
  context_window_tokens: string;
  default_output_token_reserve: string;
  max_context_utilization: string;
  preferred_context_utilization_threshold: string;
  context_overflow_promotion_target_id: string;
  access_targets: ModelAccessTargetMutation[];
  is_enabled: boolean;
  last_auto_display_name?: string | null;
}

export type ModelFormValidationError =
  | "api_family_required"
  | "model_id_required"
  | "loadbalance_strategy_required"
  | "access_target_required"
  | "context_window_tokens_invalid"
  | "default_output_token_reserve_invalid"
  | "max_context_utilization_invalid"
  | "preferred_context_utilization_threshold_invalid"
  | "preferred_context_utilization_threshold_exceeds_max";

const DEFAULT_API_FAMILY: ApiFamily = "openai";
const DEFAULT_CONTEXT_WINDOW_TOKENS = "";
const DEFAULT_OUTPUT_TOKEN_RESERVE = "4096";
const DEFAULT_MAX_CONTEXT_UTILIZATION = "0.90";
const DEFAULT_PREFERRED_CONTEXT_UTILIZATION_THRESHOLD = "";
const DEFAULT_CONTEXT_OVERFLOW_PROMOTION_TARGET_ID = "";

export const DEFAULT_MODEL_FORM_DATA: ModelFormData = {
  api_family: DEFAULT_API_FAMILY,
  model_id: "",
  display_name: "",
  loadbalance_strategy_id: null,
  context_window_tokens: DEFAULT_CONTEXT_WINDOW_TOKENS,
  default_output_token_reserve: DEFAULT_OUTPUT_TOKEN_RESERVE,
  max_context_utilization: DEFAULT_MAX_CONTEXT_UTILIZATION,
  preferred_context_utilization_threshold: DEFAULT_PREFERRED_CONTEXT_UTILIZATION_THRESHOLD,
  context_overflow_promotion_target_id: DEFAULT_CONTEXT_OVERFLOW_PROMOTION_TARGET_ID,
  access_targets: [],
  is_enabled: false,
  last_auto_display_name: "",
};

export function resolveModelApiFamily(
  model: Pick<ModelConfigListItem, "api_family"> | Pick<ModelConfig, "api_family">,
): ApiFamily {
  return model.api_family;
}

function shouldAutoSyncDisplayName(formData: ModelFormData): boolean {
  const displayName = formData.display_name ?? "";
  return displayName.trim() === "" || displayName === (formData.last_auto_display_name ?? "");
}

function stringifyCapabilityValue(value: number | null): string {
  return value === null ? "" : String(value);
}

function parsePositiveIntegerField(value: string): number | null {
  if (value.trim() === "") {
    return null;
  }
  const parsedValue = Number(value);
  return Number.isInteger(parsedValue) && parsedValue > 0 ? parsedValue : null;
}

function parseRequiredPositiveIntegerField(value: string): number | null {
  const parsedValue = parsePositiveIntegerField(value);
  return parsedValue === null ? null : parsedValue;
}

function parseOptionalUtilizationField(value: string): number | null {
  if (value.trim() === "") {
    return null;
  }
  const parsedValue = Number(value);
  return Number.isFinite(parsedValue) && parsedValue > 0 && parsedValue <= 1 ? parsedValue : null;
}

function normalizeOptionalModelIdField(value: string | null | undefined): string | null {
  const trimmedValue = value?.trim() ?? "";
  return trimmedValue === "" ? null : trimmedValue;
}

function parseRequiredUtilizationField(value: string): number | null {
  return parseOptionalUtilizationField(value);
}

export interface IndexedModelAccessTargetMutation {
  sourceIndex: number;
  target: ModelAccessTargetModelMutation;
}

export interface IndexedConnectionAccessTargetMutation {
  sourceIndex: number;
  target: Extract<ModelAccessTargetMutation, { target_type: "connection" }>;
}

export function isModelAccessTargetMutation(target: ModelAccessTargetMutation): target is ModelAccessTargetModelMutation {
  return target.target_type === "model";
}

export function getIndexedModelAccessTargets(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): IndexedModelAccessTargetMutation[] {
  return normalizeAccessTargetMutations(targets).flatMap((target, sourceIndex) => {
    if (!isModelAccessTargetMutation(target)) {
      return [];
    }
    return [{ sourceIndex, target }];
  });
}

export function getIndexedConnectionAccessTargets(
  targets: readonly ModelAccessTargetMutation[] | null | undefined,
): IndexedConnectionAccessTargetMutation[] {
  return normalizeAccessTargetMutations(targets).flatMap((target, sourceIndex) => {
    if (target.target_type !== "connection") {
      return [];
    }
    return [{ sourceIndex, target }];
  });
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

type EditableModelFormSource = (
  | Pick<
      ModelConfig,
      |
        "api_family"
        | "model_id"
        | "display_name"
        | "loadbalance_strategy_id"
        | "context_window_tokens"
        | "default_output_token_reserve"
        | "max_context_utilization"
        | "preferred_context_utilization_threshold"
        | "access_targets"
        | "is_enabled"
    >
  | Pick<
      ModelConfigListItem,
      |
        "api_family"
        | "model_id"
        | "display_name"
        | "loadbalance_strategy_id"
        | "context_window_tokens"
        | "default_output_token_reserve"
        | "max_context_utilization"
        | "preferred_context_utilization_threshold"
        | "access_targets"
        | "is_enabled"
    >
) & {
  context_overflow_promotion_target_id?: string | null;
};

export function getModelConnections(
  model: Pick<ModelConfig, "access_targets"> | Pick<ModelConfigListItem, "access_targets">,
): Connection[] {
  return model.access_targets
    .filter((target) => target.target_type === "connection" && target.connection)
    .sort((left, right) => left.position - right.position)
    .map((target) => ({ ...(target.connection as Connection), priority: target.position }));
}

export function getEditModelConnectionOptions(
  model: Pick<ModelConfig, "access_targets"> | Pick<ModelConfigListItem, "access_targets"> | null,
): Connection[] {
  return model ? getModelConnections(model) : [];
}

export function createEditModelFormData(model: EditableModelFormSource): ModelFormData {
  const displayName = model.display_name || "";
  return {
    api_family: resolveModelApiFamily(model),
    model_id: model.model_id,
    display_name: displayName,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    context_window_tokens: stringifyCapabilityValue(model.context_window_tokens),
    default_output_token_reserve: stringifyCapabilityValue(model.default_output_token_reserve),
    max_context_utilization: stringifyCapabilityValue(model.max_context_utilization),
    preferred_context_utilization_threshold: stringifyCapabilityValue(model.preferred_context_utilization_threshold),
    context_overflow_promotion_target_id: model.context_overflow_promotion_target_id ?? "",
    access_targets: normalizeAccessTargetMutations(
      model.access_targets.map(accessTargetToMutation).filter((target): target is ModelAccessTargetMutation => target !== null),
    ),
    is_enabled: model.is_enabled,
    last_auto_display_name: displayName === model.model_id ? model.model_id : displayName,
  };
}

export function createNewModelFormData(loadbalanceStrategyId: number | null): ModelFormData {
  return {
    api_family: DEFAULT_MODEL_FORM_DATA.api_family,
    model_id: DEFAULT_MODEL_FORM_DATA.model_id,
    display_name: DEFAULT_MODEL_FORM_DATA.display_name,
    loadbalance_strategy_id: loadbalanceStrategyId,
    context_window_tokens: DEFAULT_MODEL_FORM_DATA.context_window_tokens,
    default_output_token_reserve: DEFAULT_MODEL_FORM_DATA.default_output_token_reserve,
    max_context_utilization: DEFAULT_MODEL_FORM_DATA.max_context_utilization,
    preferred_context_utilization_threshold: DEFAULT_MODEL_FORM_DATA.preferred_context_utilization_threshold,
    context_overflow_promotion_target_id: DEFAULT_MODEL_FORM_DATA.context_overflow_promotion_target_id,
    access_targets: [...DEFAULT_MODEL_FORM_DATA.access_targets],
    is_enabled: DEFAULT_MODEL_FORM_DATA.is_enabled,
    last_auto_display_name: DEFAULT_MODEL_FORM_DATA.last_auto_display_name,
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
  if (formData.model_id.trim() === "") {
    return "model_id_required";
  }
  if (formData.loadbalance_strategy_id === null) {
    return "loadbalance_strategy_required";
  }
  if (formData.context_window_tokens.trim() !== "" && parsePositiveIntegerField(formData.context_window_tokens) === null) {
    return "context_window_tokens_invalid";
  }
  if (parseRequiredPositiveIntegerField(formData.default_output_token_reserve) === null) {
    return "default_output_token_reserve_invalid";
  }
  const maxContextUtilization = parseRequiredUtilizationField(formData.max_context_utilization);
  if (maxContextUtilization === null) {
    return "max_context_utilization_invalid";
  }
  const preferredContextUtilizationThreshold = parseOptionalUtilizationField(
    formData.preferred_context_utilization_threshold,
  );
  if (
    formData.preferred_context_utilization_threshold.trim() !== ""
    && preferredContextUtilizationThreshold === null
  ) {
    return "preferred_context_utilization_threshold_invalid";
  }
  if (
    typeof preferredContextUtilizationThreshold === "number"
    && preferredContextUtilizationThreshold > maxContextUtilization
  ) {
    return "preferred_context_utilization_threshold_exceeds_max";
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

function getNormalizedCapabilityState(formData: ModelFormData) {
  const contextWindowTokens = parsePositiveIntegerField(formData.context_window_tokens);
  const defaultOutputTokenReserve = parseRequiredPositiveIntegerField(formData.default_output_token_reserve);
  const maxContextUtilization = parseRequiredUtilizationField(formData.max_context_utilization);
  const preferredContextUtilizationThreshold = parseOptionalUtilizationField(
    formData.preferred_context_utilization_threshold,
  );

  if (formData.context_window_tokens.trim() !== "" && contextWindowTokens === null) {
    throw new Error("context_window_tokens is invalid");
  }
  if (defaultOutputTokenReserve === null) {
    throw new Error("default_output_token_reserve is invalid");
  }
  if (maxContextUtilization === null) {
    throw new Error("max_context_utilization is invalid");
  }
  if (
    formData.preferred_context_utilization_threshold.trim() !== ""
    && preferredContextUtilizationThreshold === null
  ) {
    throw new Error("preferred_context_utilization_threshold is invalid");
  }
  if (
    typeof preferredContextUtilizationThreshold === "number"
    && preferredContextUtilizationThreshold > maxContextUtilization
  ) {
    throw new Error("preferred_context_utilization_threshold exceeds max_context_utilization");
  }

  return {
    context_window_tokens: contextWindowTokens,
    default_output_token_reserve: defaultOutputTokenReserve,
    max_context_utilization: maxContextUtilization,
    preferred_context_utilization_threshold: preferredContextUtilizationThreshold,
    context_overflow_promotion_target_id: normalizeOptionalModelIdField(
      formData.context_overflow_promotion_target_id,
    ),
  };
}

export function toModelCreatePayload(formData: ModelFormData): ManagedModelConfigCreate {
  const normalizedDisplayName = formData.display_name?.trim() || formData.model_id.trim();
  return {
    api_family: formData.api_family,
    model_id: formData.model_id,
    display_name: normalizedDisplayName,
    is_enabled: formData.is_enabled,
    ...getNormalizedRoutingState(formData),
    ...getNormalizedCapabilityState(formData),
  };
}

export function toModelUpdatePayload(formData: ModelFormData): ManagedModelConfigUpdate {
  return {
    api_family: formData.api_family,
    display_name: formData.display_name || null,
    model_id: formData.model_id,
    is_enabled: formData.is_enabled,
    ...getNormalizedRoutingState(formData),
    ...getNormalizedCapabilityState(formData),
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
    context_overflow_promotion_target_id: "",
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

type ApiFamilyModelOption = {
  api_family: ApiFamily;
  model_id: string;
  is_enabled?: boolean;
  facade_enabled?: boolean | null;
  is_facade?: boolean | null;
};

function isFacadeModelOption(model: ApiFamilyModelOption): boolean {
  return model.facade_enabled === true || model.is_facade === true;
}

export function getAccessTargetModelsForApiFamily<T extends ApiFamilyModelOption>(
  models: T[],
  apiFamily: ApiFamily,
  excludedModelId?: string,
): T[] {
  const normalizedExcludedModelId = excludedModelId?.trim() ?? "";
  return models.filter(
    (model) =>
      model.api_family === apiFamily
      && (normalizedExcludedModelId === "" || model.model_id !== normalizedExcludedModelId)
      && model.is_enabled !== false
      && !isFacadeModelOption(model),
  );
}

export function getPromotionTargetModelsForApiFamily<T extends ApiFamilyModelOption>(
  models: T[],
  apiFamily: ApiFamily,
  excludedModelId?: string,
): T[] {
  const normalizedExcludedModelId = excludedModelId?.trim() ?? "";
  return models.filter(
    (model) =>
      model.api_family === apiFamily
      && (normalizedExcludedModelId === "" || model.model_id !== normalizedExcludedModelId)
      && model.is_enabled !== false
      && !isFacadeModelOption(model),
  );
}

type ModelListItemSource = ModelConfig & {
  context_overflow_promotion_target_id?: string | null;
};

export function toModelListItem(
  model: ModelListItemSource,
  existing?: ModelConfigListItem,
): ManagedModelConfigListItem {
  const connections = getModelConnections(model);
  return {
    id: model.id,
    profile_id: model.profile_id,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: model.loadbalance_strategy,
    context_window_tokens: model.context_window_tokens,
    default_output_token_reserve: model.default_output_token_reserve,
    max_context_utilization: model.max_context_utilization,
    preferred_context_utilization_threshold: model.preferred_context_utilization_threshold,
    context_overflow_promotion_target_id: model.context_overflow_promotion_target_id ?? null,
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
