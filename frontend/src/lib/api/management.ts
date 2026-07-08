import type {
  Connection,
  ConnectionDropdownResponse,
  ConnectionReferencesResponse,
  Endpoint,
  EndpointCreate,
  EndpointModelsBatchParams,
  EndpointModelsBatchResponse,
  EndpointUpdate,
  LegacyLoadbalanceStrategyType,
  LoadbalanceBanMode,
  LoadbalanceBanPolicyFields,
  LoadbalanceStrategy,
  LoadbalanceStrategyCreate,
  LoadbalanceStrategyDefaultsResponse,
  LoadbalanceStrategySummary,
  LoadbalanceStrategyUpdate,
  ModelConfig,
  ModelConfigCreate,
  ModelConfigListItem,
  ModelConfigUpdate,
  ModelAccessTarget,
  ModelAccessTargetCreate,
  ModelAccessTargetUpdate,
  ModelConnectionCreate,
  ModelConnectionUpdate,
  PricingTemplate,
  PricingTemplateConnectionsResponse,
  PricingTemplateCreate,
  PricingTemplateUpdate,
} from "../types";
import { normalizeFailureStatusCodes } from "../loadbalanceRoutingPolicy";
import { request } from "./core";

export type ManagedModelConfigListItem = ModelConfigListItem;
export type ManagedModelConfig = ModelConfig;
export type ManagedModelConfigCreate = ModelConfigCreate;
export type ManagedModelConfigUpdate = ModelConfigUpdate;

type RawLoadbalanceBanPolicyFields = {
  legacy_strategy_type?: unknown;
  failure_status_codes?: unknown;
  ban_mode?: unknown;
  retry_base_delay_ms?: unknown;
  retry_backoff_multiplier?: unknown;
  retry_jitter_ratio?: unknown;
  retry_max_delay_ms?: unknown;
  cycle_retry_attempt_limit?: unknown;
  ban_cumulative_retry_attempt_threshold?: unknown;
  ban_duration_seconds?: unknown;
};

type RawLoadbalanceStrategySummary = RawLoadbalanceBanPolicyFields & {
  id: number;
  name: string;
};

type RawLoadbalanceStrategy = RawLoadbalanceStrategySummary & {
  profile_id: number;
  attached_model_count: number;
  created_at: string;
  updated_at: string;
};

type RawLoadbalanceStrategyDefaultsResponse = {
  items: RawLoadbalanceStrategy[];
  created_count: number;
  created_names: string[];
  existing_names: string[];
};

type RawModelAccessTarget = ModelAccessTarget;

type RawModelConfigListItem = Omit<ManagedModelConfigListItem, "loadbalance_strategy" | "access_targets"> & {
  loadbalance_strategy: RawLoadbalanceStrategySummary | null;
  access_targets: RawModelAccessTarget[];
};

type RawModelConfig = Omit<ManagedModelConfig, "loadbalance_strategy" | "access_targets"> & {
  loadbalance_strategy: RawLoadbalanceStrategySummary | null;
  access_targets: RawModelAccessTarget[];
};

type RawEndpointModelsBatchResponse = {
  items: Array<{
    endpoint_id: number;
    models: RawModelConfigListItem[];
  }>;
};

function unsupportedLoadbalanceStrategy(reason: string): never {
  throw new Error(`Unsupported loadbalance strategy contract from management API: ${reason}`);
}

function normalizeInteger(value: unknown, field: string) {
  if (typeof value !== "number" || !Number.isFinite(value) || !Number.isInteger(value)) {
    unsupportedLoadbalanceStrategy(field);
  }

  return value;
}

function normalizeNumber(value: unknown, field: string) {
  if (typeof value !== "number" || !Number.isFinite(value)) {
    unsupportedLoadbalanceStrategy(field);
  }

  return value;
}

function unsupportedManagementModel(reason: string): never {
  throw new Error(`Unsupported model contract from management API: ${reason}`);
}

function normalizeModelAccessTarget(target: RawModelAccessTarget): ModelAccessTarget {
  const obsoleteTarget = target as unknown as Record<string, unknown>;
  const obsoleteTargetPriority = "target_priority";

  if (Object.hasOwn(obsoleteTarget, "weight")) {
    unsupportedManagementModel("access_targets.weight");
  }

  if (Object.hasOwn(obsoleteTarget, obsoleteTargetPriority)) {
    unsupportedManagementModel(`access_targets.${obsoleteTargetPriority}`);
  }

  return {
    id: target.id,
    target_type: target.target_type,
    target_model_id: target.target_model_id,
    connection_id: target.connection_id,
    terminal_target_id: target.terminal_target_id,
    position: target.position,
    is_enabled: target.is_enabled,
    target_model: target.target_model,
    connection: target.connection,
    terminal_target: target.terminal_target,
    created_at: target.created_at,
    updated_at: target.updated_at,
  };
}

function normalizeLegacyStrategyType(value: unknown): LegacyLoadbalanceStrategyType {
  if (
    value === "single" ||
    value === "fill-first" ||
    value === "round-robin"
  ) {
    return value;
  }

  unsupportedLoadbalanceStrategy("legacy_strategy_type");
}

function normalizeBanMode(value: unknown): LoadbalanceBanMode {
  if (value === "off" || value === "temporary" || value === "until_reset") {
    return value;
  }

  unsupportedLoadbalanceStrategy("ban_mode");
}

function normalizeStatusCodes(value: unknown) {
  if (!Array.isArray(value) || value.some((statusCode) => typeof statusCode !== "number")) {
    unsupportedLoadbalanceStrategy("failure_status_codes");
  }

  return normalizeFailureStatusCodes(value);
}

const removedRetryAttemptField = ["retry", "max", "attempts"].join("_");

function rejectRemovedLoadbalanceBanPolicyFields(strategy: RawLoadbalanceBanPolicyFields) {
  if (Object.hasOwn(strategy, removedRetryAttemptField)) {
    unsupportedLoadbalanceStrategy(removedRetryAttemptField);
  }
}

function normalizeLoadbalanceBanPolicyFields(
  strategy: RawLoadbalanceBanPolicyFields,
): LoadbalanceBanPolicyFields {
  rejectRemovedLoadbalanceBanPolicyFields(strategy);

  return {
    legacy_strategy_type: normalizeLegacyStrategyType(strategy.legacy_strategy_type),
    failure_status_codes: normalizeStatusCodes(strategy.failure_status_codes),
    ban_mode: normalizeBanMode(strategy.ban_mode),
    retry_base_delay_ms: normalizeInteger(
      strategy.retry_base_delay_ms,
      "retry_base_delay_ms",
    ),
    retry_backoff_multiplier: normalizeNumber(
      strategy.retry_backoff_multiplier,
      "retry_backoff_multiplier",
    ),
    retry_jitter_ratio: normalizeNumber(strategy.retry_jitter_ratio, "retry_jitter_ratio"),
    retry_max_delay_ms: normalizeInteger(strategy.retry_max_delay_ms, "retry_max_delay_ms"),
    cycle_retry_attempt_limit: normalizeInteger(
      strategy.cycle_retry_attempt_limit,
      "cycle_retry_attempt_limit",
    ),
    ban_cumulative_retry_attempt_threshold: normalizeInteger(
      strategy.ban_cumulative_retry_attempt_threshold,
      "ban_cumulative_retry_attempt_threshold",
    ),
    ban_duration_seconds: normalizeInteger(
      strategy.ban_duration_seconds,
      "ban_duration_seconds",
    ),
  };
}

function normalizeLoadbalanceStrategySummary(strategy: RawLoadbalanceStrategySummary | null): LoadbalanceStrategySummary | null {
  if (!strategy) {
    return null;
  }

  return {
    id: strategy.id,
    name: strategy.name,
    ...normalizeLoadbalanceBanPolicyFields(strategy),
  };
}

function normalizeLoadbalanceStrategy(strategy: RawLoadbalanceStrategy): LoadbalanceStrategy {
  return {
    id: strategy.id,
    profile_id: strategy.profile_id,
    name: strategy.name,
    ...normalizeLoadbalanceBanPolicyFields(strategy),
    attached_model_count: strategy.attached_model_count,
    created_at: strategy.created_at,
    updated_at: strategy.updated_at,
  };
}

function normalizeModelConfigListItem(model: RawModelConfigListItem): ManagedModelConfigListItem {
  return {
    id: model.id,
    profile_id: model.profile_id,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    openai_accepted_format: model.openai_accepted_format,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(model.loadbalance_strategy),
    access_targets: model.access_targets.map(normalizeModelAccessTarget),
    is_enabled: model.is_enabled,
    connection_count: model.connection_count,
    active_connection_count: model.active_connection_count,
    health_success_rate: model.health_success_rate,
    health_total_requests: model.health_total_requests,
    created_at: model.created_at,
    updated_at: model.updated_at,
  };
}

function normalizeModelConfig(model: RawModelConfig): ManagedModelConfig {
  return {
    id: model.id,
    profile_id: model.profile_id,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    openai_accepted_format: model.openai_accepted_format,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(model.loadbalance_strategy),
    access_targets: model.access_targets.map(normalizeModelAccessTarget),
    is_enabled: model.is_enabled,
    created_at: model.created_at,
    updated_at: model.updated_at,
  };
}

export const models = {
  list: () =>
    request<RawModelConfigListItem[]>("/api/models").then((models) =>
      models.map(normalizeModelConfigListItem),
    ),
  byEndpoints: (data: EndpointModelsBatchParams) =>
    request<RawEndpointModelsBatchResponse>("/api/models/by-endpoints", {
      method: "POST",
      body: JSON.stringify(data),
    }).then((response) => ({
      items: response.items.map((item) => ({
        ...item,
        models: item.models.map(normalizeModelConfigListItem),
      })),
    }) as EndpointModelsBatchResponse),
  byEndpoint: (endpointId: number) =>
    request<RawModelConfigListItem[]>(`/api/models/by-endpoint/${endpointId}`).then((models) =>
      models.map(normalizeModelConfigListItem),
    ),
  get: (id: number) =>
    request<RawModelConfig>(`/api/models/${id}`).then(normalizeModelConfig),
  create: (data: ManagedModelConfigCreate) =>
    request<RawModelConfig>("/api/models", {
      method: "POST",
      body: JSON.stringify(data),
    }).then(normalizeModelConfig),
  update: (id: number, data: ManagedModelConfigUpdate) =>
    request<RawModelConfig>(`/api/models/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }).then(normalizeModelConfig),
  delete: (id: number) => request<void>(`/api/models/${id}`, { method: "DELETE" }),
  targets: {
    list: (modelConfigId: number) =>
      request<RawModelAccessTarget[]>(`/api/models/${modelConfigId}/targets`).then((targets) =>
        targets.map(normalizeModelAccessTarget),
      ),
    create: (modelConfigId: number, data: ModelAccessTargetCreate) =>
      request<RawModelAccessTarget[]>(`/api/models/${modelConfigId}/targets`, {
        method: "POST",
        body: JSON.stringify(data),
      }).then((targets) => targets.map(normalizeModelAccessTarget)),
    update: (modelConfigId: number, targetId: number, data: ModelAccessTargetUpdate) =>
      request<RawModelAccessTarget[]>(`/api/models/${modelConfigId}/targets/${targetId}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }).then((targets) => targets.map(normalizeModelAccessTarget)),
    movePosition: (modelConfigId: number, targetId: number, toIndex: number) =>
      request<RawModelAccessTarget[]>(`/api/models/${modelConfigId}/targets/${targetId}/position`, {
        method: "PATCH",
        body: JSON.stringify({ to_index: toIndex }),
      }).then((targets) => targets.map(normalizeModelAccessTarget)),
    delete: (modelConfigId: number, targetId: number) =>
      request<RawModelAccessTarget[]>(`/api/models/${modelConfigId}/targets/${targetId}`, {
        method: "DELETE",
      }).then((targets) => targets.map(normalizeModelAccessTarget)),
  },
  connections: {
    list: (modelConfigId: number) => request<Connection[]>(`/api/models/${modelConfigId}/connections`),
    create: (modelConfigId: number, data: ModelConnectionCreate) =>
      request<Connection>(`/api/models/${modelConfigId}/connections`, {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (modelConfigId: number, connectionId: number, data: ModelConnectionUpdate) =>
      request<Connection>(`/api/models/${modelConfigId}/connections/${connectionId}`, {
        method: "PATCH",
        body: JSON.stringify(data),
      }),
    delete: (modelConfigId: number, connectionId: number) =>
      request<void>(`/api/models/${modelConfigId}/connections/${connectionId}`, { method: "DELETE" }),
  },
};

export const loadbalanceStrategies = {
  list: () =>
    request<RawLoadbalanceStrategy[]>("/api/loadbalance/strategies").then((strategies) =>
      strategies.map(normalizeLoadbalanceStrategy),
    ),
  createDefaults: () =>
    request<RawLoadbalanceStrategyDefaultsResponse>("/api/loadbalance/strategies/defaults", {
      method: "POST",
    }).then((response) => ({
      items: response.items.map(normalizeLoadbalanceStrategy),
      created_count: response.created_count,
      created_names: response.created_names,
      existing_names: response.existing_names,
    }) as LoadbalanceStrategyDefaultsResponse),
  get: (id: number) =>
    request<RawLoadbalanceStrategy>(`/api/loadbalance/strategies/${id}`).then(
      normalizeLoadbalanceStrategy,
    ),
  create: (data: LoadbalanceStrategyCreate) =>
    request<RawLoadbalanceStrategy>("/api/loadbalance/strategies", {
      method: "POST",
      body: JSON.stringify(data),
    }).then(normalizeLoadbalanceStrategy),
  update: (id: number, data: LoadbalanceStrategyUpdate) =>
    request<RawLoadbalanceStrategy>(`/api/loadbalance/strategies/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }).then(normalizeLoadbalanceStrategy),
  delete: (id: number) =>
    request<{ deleted: boolean }>(`/api/loadbalance/strategies/${id}`, {
      method: "DELETE",
    }),
};

export const endpoints = {
  list: () => request<Endpoint[]>("/api/endpoints"),
  connections: () => request<ConnectionDropdownResponse>("/api/endpoints/connections"),
  create: (data: EndpointCreate) =>
    request<Endpoint>("/api/endpoints", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  update: (id: number, data: EndpointUpdate) =>
    request<Endpoint>(`/api/endpoints/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  movePosition: (id: number, toIndex: number) =>
    request<Endpoint[]>(`/api/endpoints/${id}/position`, {
      method: "PATCH",
      body: JSON.stringify({ to_index: toIndex }),
    }),
  duplicate: (id: number) =>
    request<Endpoint>(`/api/endpoints/${id}/duplicate`, {
      method: "POST",
    }),
  delete: (id: number) => request<void>(`/api/endpoints/${id}`, { method: "DELETE" }),
};

export const connections = {
  list: () => request<Connection[]>("/api/connections"),
  get: (id: number) => request<Connection>(`/api/connections/${id}`),
  references: (id: number) =>
    request<ConnectionReferencesResponse>(`/api/connections/${id}/references`),
};

export const pricingTemplates = {
  list: () => request<PricingTemplate[]>("/api/pricing-templates"),
  get: (id: number) => request<PricingTemplate>(`/api/pricing-templates/${id}`),
  create: (data: PricingTemplateCreate) =>
    request<PricingTemplate>("/api/pricing-templates", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  update: (id: number, data: PricingTemplateUpdate) =>
    request<PricingTemplate>(`/api/pricing-templates/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  delete: (id: number) => request<void>(`/api/pricing-templates/${id}`, { method: "DELETE" }),
  connections: (id: number) =>
    request<PricingTemplateConnectionsResponse>(`/api/pricing-templates/${id}/connections`),
};
