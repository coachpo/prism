import type {
  Connection,
  ConnectionDropdownResponse,
  ConnectionReferencesResponse,
  Endpoint,
  EndpointCreate,
  EndpointModelsBatchParams,
  EndpointModelsBatchResponse,
  EndpointUpdate,
  HealthCheckResponse,
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
  Profile,
  ProfileActivateRequest,
  ProfileBootstrapResponse,
  ProfileCreate,
  ProfileUpdate,
  Vendor,
  VendorCreate,
  VendorModelUsageItem,
  VendorUpdate,
} from "../types";
import { normalizeFailureStatusCodes } from "../loadbalanceRoutingPolicy";
import { request } from "./core";

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

type RawModelAccessTarget = Omit<ModelAccessTarget, "weight" | "target_priority"> & {
  weight?: unknown;
  target_priority?: unknown;
};

type RawModelConfigListItem = Omit<ModelConfigListItem, "loadbalance_strategy" | "access_targets"> & {
  loadbalance_strategy: RawLoadbalanceStrategySummary | null;
  access_targets: RawModelAccessTarget[];
};

type RawModelConfig = Omit<ModelConfig, "loadbalance_strategy" | "access_targets"> & {
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

function normalizeOptionalPositiveInteger(value: unknown, field: string): number | null {
  if (value == null) {
    return null;
  }
  if (typeof value !== "number" || !Number.isFinite(value) || !Number.isInteger(value) || value <= 0) {
    unsupportedManagementModel(field);
  }
  return value;
}

function normalizeOptionalNonNegativeInteger(value: unknown, field: string): number | null {
  if (value == null) {
    return null;
  }
  if (typeof value !== "number" || !Number.isFinite(value) || !Number.isInteger(value) || value < 0) {
    unsupportedManagementModel(field);
  }
  return value;
}

function normalizeModelAccessTarget(target: RawModelAccessTarget): ModelAccessTarget {
  const weight = normalizeOptionalPositiveInteger(target.weight, "access_targets.weight");
  const targetPriority = normalizeOptionalNonNegativeInteger(
    target.target_priority,
    "access_targets.target_priority",
  );

  if (target.target_type === "model") {
    return {
      ...target,
      weight: weight ?? 1,
      target_priority: targetPriority ?? 0,
    };
  }

  return {
    ...target,
    weight: null,
    target_priority: null,
  };
}

function normalizeLegacyStrategyType(value: unknown): LegacyLoadbalanceStrategyType {
  if (
    value === "single" ||
    value === "fill-first" ||
    value === "round-robin" ||
    value === "cheapest_eligible_context"
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

function normalizeModelConfigListItem(model: RawModelConfigListItem): ModelConfigListItem {
  return {
    ...model,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(model.loadbalance_strategy),
    access_targets: model.access_targets.map(normalizeModelAccessTarget),
  };
}

function normalizeModelConfig(model: RawModelConfig): ModelConfig {
  return {
    ...model,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(model.loadbalance_strategy),
    access_targets: model.access_targets.map(normalizeModelAccessTarget),
  };
}

export const profiles = {
  bootstrap: () => request<ProfileBootstrapResponse>("/api/profiles/bootstrap"),
  list: () => request<Profile[]>("/api/profiles"),
  getActive: () => request<Profile>("/api/profiles/active"),
  create: (data: ProfileCreate) =>
    request<Profile>("/api/profiles", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  update: (id: number, data: ProfileUpdate) =>
    request<Profile>(`/api/profiles/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),
  delete: (id: number) => request<void>(`/api/profiles/${id}`, { method: "DELETE" }),
  activate: (id: number, payload: ProfileActivateRequest) =>
    request<Profile>(`/api/profiles/${id}/activate`, {
      method: "POST",
      body: JSON.stringify(payload),
    }),
};

export const vendors = {
  list: () => request<Vendor[]>("/api/vendors"),
  get: (id: number) => request<Vendor>(`/api/vendors/${id}`),
  create: (data: VendorCreate) =>
    request<Vendor>("/api/vendors", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  update: (id: number, data: VendorUpdate) =>
    request<Vendor>(`/api/vendors/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),
  models: (id: number) => request<VendorModelUsageItem[]>(`/api/vendors/${id}/models`),
  delete: (id: number) => request<void>(`/api/vendors/${id}`, { method: "DELETE" }),
};

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
  create: (data: ModelConfigCreate) =>
    request<RawModelConfig>("/api/models", {
      method: "POST",
      body: JSON.stringify(data),
    }).then(normalizeModelConfig),
  update: (id: number, data: ModelConfigUpdate) =>
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
      request<RawModelAccessTarget[]>(`/api/models/${modelConfigId}/targets/${targetId}`, {
        method: "PATCH",
        body: JSON.stringify({ position: toIndex }),
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
    healthCheck: (modelConfigId: number, connectionId: number) =>
      request<HealthCheckResponse>(`/api/models/${modelConfigId}/connections/${connectionId}/health`, {
        method: "POST",
      }),
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
