import type {
  AdaptiveRoutingObjective,
  Connection,
  ConnectionCreate,
  LoadbalanceAutoRecovery,
  LoadbalanceBanMode,
  LoadbalanceRoutingPolicy,
  LoadbalanceStrategy,
  LoadbalanceStrategyCreate,
  LoadbalanceStrategyFamily,
  LoadbalanceStrategySummary,
  LoadbalanceStrategyUpdate,
  LegacyLoadbalanceStrategyType,
  ModelConnectionsBatchParams,
  ModelConnectionsBatchResponse,
  ConnectionDropdownResponse,
  ConnectionHealthCheckPreviewResponse,
  ConnectionOwnerResponse,
  ConnectionPricingTemplateUpdate,
  ConnectionUpdate,
  Endpoint,
  EndpointModelsBatchParams,
  EndpointModelsBatchResponse,
  EndpointCreate,
  EndpointUpdate,
  HealthCheckResponse,
  ModelConfig,
  ModelConfigCreate,
  ModelConfigListItem,
  ModelConfigUpdate,
  PricingTemplate,
  PricingTemplateConnectionsResponse,
  PricingTemplateCreate,
  PricingTemplateUpdate,
  Profile,
  ProfileActivateRequest,
  ProfileBootstrapResponse,
  ProfileCreate,
  ProfileUpdate,
  VendorModelUsageItem,
  Vendor,
  VendorCreate,
  VendorUpdate,
} from "../types";
import { normalizeFailureStatusCodes } from "../loadbalanceRoutingPolicy";
import { request } from "./core";

type RawLoadbalanceStrategySummary = {
  id: number;
  name: string;
  strategy_type?: unknown;
  legacy_strategy_type?: unknown;
  auto_recovery?: unknown;
  routing_policy?: unknown;
};

type RawLoadbalanceStrategy = {
  id: number;
  profile_id: number;
  name: string;
  strategy_type?: unknown;
  legacy_strategy_type?: unknown;
  auto_recovery?: unknown;
  routing_policy?: unknown;
  attached_model_count: number;
  created_at: string;
  updated_at: string;
};

type RawModelConfigListItem = Omit<ModelConfigListItem, "loadbalance_strategy"> & {
  loadbalance_strategy: RawLoadbalanceStrategySummary | null;
};

type RawModelConfig = Omit<ModelConfig, "loadbalance_strategy"> & {
  loadbalance_strategy: RawLoadbalanceStrategySummary | null;
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

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
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

function normalizeBoolean(value: unknown, field: string) {
  if (typeof value !== "boolean") {
    unsupportedLoadbalanceStrategy(field);
  }

  return value;
}

function normalizeStrategyFamily(value: unknown): LoadbalanceStrategyFamily {
  if (value === "legacy" || value === "adaptive") {
    return value;
  }

  unsupportedLoadbalanceStrategy("strategy_type");
}

function normalizeLegacyStrategyType(value: unknown): LegacyLoadbalanceStrategyType {
  if (value === "single" || value === "fill-first" || value === "round-robin") {
    return value;
  }

  unsupportedLoadbalanceStrategy("legacy_strategy_type");
}

function normalizeRoutingPolicy(value: unknown): LoadbalanceRoutingPolicy {
  if (!isRecord(value)) {
    unsupportedLoadbalanceStrategy("routing_policy");
  }

  if (value.kind !== "adaptive") {
    unsupportedLoadbalanceStrategy("routing_policy.kind");
  }

  if (
    value.routing_objective !== "maximize_availability" &&
    value.routing_objective !== "minimize_latency"
  ) {
    unsupportedLoadbalanceStrategy("routing_policy.routing_objective");
  }

  const hedge = value.hedge;
  if (!isRecord(hedge)) {
    unsupportedLoadbalanceStrategy("routing_policy.hedge");
  }

  const circuitBreaker = value.circuit_breaker;
  if (!isRecord(circuitBreaker)) {
    unsupportedLoadbalanceStrategy("routing_policy.circuit_breaker");
  }

  const admission = value.admission;
  if (!isRecord(admission)) {
    unsupportedLoadbalanceStrategy("routing_policy.admission");
  }

  const banMode = circuitBreaker.ban_mode;
  if (banMode !== "off" && banMode !== "temporary" && banMode !== "manual") {
    unsupportedLoadbalanceStrategy("routing_policy.circuit_breaker.ban_mode");
  }

  return {
    kind: "adaptive",
    routing_objective: value.routing_objective as AdaptiveRoutingObjective,
    deadline_budget_ms: normalizeInteger(value.deadline_budget_ms, "routing_policy.deadline_budget_ms"),
    hedge: {
      enabled: normalizeBoolean(hedge.enabled, "routing_policy.hedge.enabled"),
      delay_ms: normalizeInteger(hedge.delay_ms, "routing_policy.hedge.delay_ms"),
      max_additional_attempts: normalizeInteger(
        hedge.max_additional_attempts,
        "routing_policy.hedge.max_additional_attempts",
      ),
    },
    circuit_breaker: {
      failure_status_codes: normalizeStatusCodes(circuitBreaker.failure_status_codes),
      base_open_seconds: normalizeNumber(
        circuitBreaker.base_open_seconds,
        "routing_policy.circuit_breaker.base_open_seconds",
      ),
      failure_threshold: normalizeInteger(
        circuitBreaker.failure_threshold,
        "routing_policy.circuit_breaker.failure_threshold",
      ),
      backoff_multiplier: normalizeNumber(
        circuitBreaker.backoff_multiplier,
        "routing_policy.circuit_breaker.backoff_multiplier",
      ),
      max_open_seconds: normalizeInteger(
        circuitBreaker.max_open_seconds,
        "routing_policy.circuit_breaker.max_open_seconds",
      ),
      jitter_ratio: normalizeNumber(
        circuitBreaker.jitter_ratio,
        "routing_policy.circuit_breaker.jitter_ratio",
      ),
      ban_mode: banMode as LoadbalanceBanMode,
      max_open_strikes_before_ban: normalizeInteger(
        circuitBreaker.max_open_strikes_before_ban,
        "routing_policy.circuit_breaker.max_open_strikes_before_ban",
      ),
      ban_duration_seconds: normalizeInteger(
        circuitBreaker.ban_duration_seconds,
        "routing_policy.circuit_breaker.ban_duration_seconds",
      ),
    },
    admission: {
      respect_qps_limit: normalizeBoolean(
        admission.respect_qps_limit,
        "routing_policy.admission.respect_qps_limit",
      ),
      respect_in_flight_limits: normalizeBoolean(
        admission.respect_in_flight_limits,
        "routing_policy.admission.respect_in_flight_limits",
      ),
    },
  };
}

function normalizeStatusCodes(value: unknown) {
  if (!Array.isArray(value) || value.some((statusCode) => typeof statusCode !== "number")) {
    unsupportedLoadbalanceStrategy("auto_recovery.status_codes");
  }

  return normalizeFailureStatusCodes(value);
}

function normalizeAutoRecovery(
  value: unknown,
): LoadbalanceAutoRecovery {
  if (!isRecord(value)) {
    unsupportedLoadbalanceStrategy("auto_recovery");
  }

  if (value.mode === "disabled") {
    return { mode: "disabled" };
  }

  if (value.mode !== "enabled") {
    unsupportedLoadbalanceStrategy("auto_recovery.mode");
  }

  const cooldown = value.cooldown;
  if (!isRecord(cooldown)) {
    unsupportedLoadbalanceStrategy("auto_recovery.cooldown");
  }

  const ban = value.ban;
  if (!isRecord(ban)) {
    unsupportedLoadbalanceStrategy("auto_recovery.ban");
  }

  const normalizedBanMode = ban.mode;
  if (normalizedBanMode === "off") {
    return {
      mode: "enabled",
      status_codes: normalizeStatusCodes(value.status_codes),
      cooldown: {
        base_seconds: normalizeInteger(cooldown.base_seconds, "auto_recovery.cooldown.base_seconds"),
        failure_threshold: normalizeInteger(cooldown.failure_threshold, "auto_recovery.cooldown.failure_threshold"),
        backoff_multiplier: normalizeNumber(cooldown.backoff_multiplier, "auto_recovery.cooldown.backoff_multiplier"),
        max_cooldown_seconds: normalizeInteger(cooldown.max_cooldown_seconds, "auto_recovery.cooldown.max_cooldown_seconds"),
        jitter_ratio: normalizeNumber(cooldown.jitter_ratio, "auto_recovery.cooldown.jitter_ratio"),
      },
      ban: {
        mode: "off",
      },
    };
  }

  if (normalizedBanMode === "manual") {
    return {
      mode: "enabled",
      status_codes: normalizeStatusCodes(value.status_codes),
      cooldown: {
        base_seconds: normalizeInteger(cooldown.base_seconds, "auto_recovery.cooldown.base_seconds"),
        failure_threshold: normalizeInteger(cooldown.failure_threshold, "auto_recovery.cooldown.failure_threshold"),
        backoff_multiplier: normalizeNumber(cooldown.backoff_multiplier, "auto_recovery.cooldown.backoff_multiplier"),
        max_cooldown_seconds: normalizeInteger(cooldown.max_cooldown_seconds, "auto_recovery.cooldown.max_cooldown_seconds"),
        jitter_ratio: normalizeNumber(cooldown.jitter_ratio, "auto_recovery.cooldown.jitter_ratio"),
      },
      ban: {
        mode: "manual",
        max_cooldown_strikes_before_ban: normalizeInteger(
          ban.max_cooldown_strikes_before_ban,
          "auto_recovery.ban.max_cooldown_strikes_before_ban",
        ),
      },
    };
  }

  if (normalizedBanMode === "temporary") {
    return {
      mode: "enabled",
      status_codes: normalizeStatusCodes(value.status_codes),
      cooldown: {
        base_seconds: normalizeInteger(cooldown.base_seconds, "auto_recovery.cooldown.base_seconds"),
        failure_threshold: normalizeInteger(cooldown.failure_threshold, "auto_recovery.cooldown.failure_threshold"),
        backoff_multiplier: normalizeNumber(cooldown.backoff_multiplier, "auto_recovery.cooldown.backoff_multiplier"),
        max_cooldown_seconds: normalizeInteger(cooldown.max_cooldown_seconds, "auto_recovery.cooldown.max_cooldown_seconds"),
        jitter_ratio: normalizeNumber(cooldown.jitter_ratio, "auto_recovery.cooldown.jitter_ratio"),
      },
      ban: {
        mode: "temporary",
        max_cooldown_strikes_before_ban: normalizeInteger(
          ban.max_cooldown_strikes_before_ban,
          "auto_recovery.ban.max_cooldown_strikes_before_ban",
        ),
        ban_duration_seconds: normalizeInteger(
          ban.ban_duration_seconds,
          "auto_recovery.ban.ban_duration_seconds",
        ),
      },
    };
  }

  unsupportedLoadbalanceStrategy("auto_recovery.ban.mode");
}

function normalizeLoadbalanceStrategySummary(strategy: RawLoadbalanceStrategySummary | null): LoadbalanceStrategySummary | null {
  if (!strategy) {
    return null;
  }

  const strategyFamily = normalizeStrategyFamily(strategy.strategy_type);

  if (strategyFamily === "legacy") {
    return {
      id: strategy.id,
      name: strategy.name,
      strategy_type: "legacy",
      legacy_strategy_type: normalizeLegacyStrategyType(strategy.legacy_strategy_type),
      auto_recovery: normalizeAutoRecovery(strategy.auto_recovery),
    };
  }

  return {
    id: strategy.id,
    name: strategy.name,
    strategy_type: "adaptive",
    routing_policy: normalizeRoutingPolicy(strategy.routing_policy),
  };
}

function normalizeLoadbalanceStrategy(strategy: RawLoadbalanceStrategy): LoadbalanceStrategy {
  const strategyFamily = normalizeStrategyFamily(strategy.strategy_type);

  if (strategyFamily === "legacy") {
    return {
      id: strategy.id,
      profile_id: strategy.profile_id,
      name: strategy.name,
      strategy_type: "legacy",
      legacy_strategy_type: normalizeLegacyStrategyType(strategy.legacy_strategy_type),
      auto_recovery: normalizeAutoRecovery(strategy.auto_recovery),
      attached_model_count: strategy.attached_model_count,
      created_at: strategy.created_at,
      updated_at: strategy.updated_at,
    };
  }

  return {
    id: strategy.id,
    profile_id: strategy.profile_id,
    name: strategy.name,
    strategy_type: "adaptive",
    routing_policy: normalizeRoutingPolicy(strategy.routing_policy),
    attached_model_count: strategy.attached_model_count,
    created_at: strategy.created_at,
    updated_at: strategy.updated_at,
  };
}

function normalizeModelConfigListItem(model: RawModelConfigListItem): ModelConfigListItem {
  return {
    ...model,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(model.loadbalance_strategy),
  };
}

function normalizeModelConfig(model: RawModelConfig): ModelConfig {
  return {
    ...model,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(model.loadbalance_strategy),
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
};

export const loadbalanceStrategies = {
  list: () =>
    request<RawLoadbalanceStrategy[]>("/api/loadbalance/strategies").then((strategies) =>
      strategies.map(normalizeLoadbalanceStrategy),
    ),
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
  list: (modelConfigId: number) =>
    request<Connection[]>(`/api/models/${modelConfigId}/connections`),
  byModels: (data: ModelConnectionsBatchParams) =>
    request<ModelConnectionsBatchResponse>("/api/models/connections/batch", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  create: (modelConfigId: number, data: ConnectionCreate) =>
    request<Connection>(`/api/models/${modelConfigId}/connections`, {
      method: "POST",
      body: JSON.stringify(data),
    }),
  update: (id: number, data: ConnectionUpdate) =>
    request<Connection>(`/api/connections/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  movePriority: (modelConfigId: number, connectionId: number, toIndex: number) =>
    request<Connection[]>(
      `/api/models/${modelConfigId}/connections/${connectionId}/priority`,
      {
        method: "PATCH",
        body: JSON.stringify({ to_index: toIndex }),
      }
    ),
  delete: (id: number) => request<void>(`/api/connections/${id}`, { method: "DELETE" }),
  healthCheck: (id: number) =>
    request<HealthCheckResponse>(`/api/connections/${id}/health-check`, {
      method: "POST",
    }),
  healthCheckPreview: (modelConfigId: number, data: ConnectionCreate) =>
    request<ConnectionHealthCheckPreviewResponse>(
      `/api/models/${modelConfigId}/connections/health-check-preview`,
      {
        method: "POST",
        body: JSON.stringify(data),
      }
    ),
  owner: (id: number) => request<ConnectionOwnerResponse>(`/api/connections/${id}/owner`),
  setPricingTemplate: (id: number, data: ConnectionPricingTemplateUpdate) =>
    request<Connection>(`/api/connections/${id}/pricing-template`, {
      method: "PUT",
      body: JSON.stringify(data),
    }),
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
