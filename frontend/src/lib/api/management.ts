import type {
  CatalogBindingRequest,
  CatalogOverridePatch,
  Connection,
  LegacyLoadbalanceStrategyType,
  ModelCatalogCandidatesResponse,
  ModelCatalogMatchPreviewResponse,
  ModelCatalogRefreshPreviewResponse,
  ModelCatalogResponse,
  LoadbalanceBanMode,
  LoadbalanceBanPolicyFields,
  LoadbalanceStrategy,
  LoadbalanceStrategyCreate,
  LoadbalanceStrategyDefaultsResponse,
  LoadbalanceStrategySummary,
  LoadbalanceStrategyUpdate,
  StrategyImpactListResponse,
  StrategyPreviewDraft,
  StrategyPreviewResponse,
  StrategySetDefaultRequest,
  StrategySetDefaultResponse,
  ModelConfig,
  ModelConfigCompositeCreate,
  ModelConfigCreate,
  ModelConfigListItem,
  ModelConfigUpdate,
  ModelAccessTarget,
  ModelAccessTargetCreate,
  ModelAccessTargetUpdate,
  ModelConnectionCreate,
  ModelConnectionUpdate,
  ConfigurationWarning,
  ModelRouteReadinessEnvelope,
  ModelRouteReadinessSummary,
} from "../types";
import { normalizeFailureStatusCodes } from "../loadbalanceRoutingPolicy";
import { buildQuery, request } from "./core";

export type ManagedModelConfigListItem = ModelConfigListItem;
export type ManagedModelConfig = ModelConfig;
export type ManagedModelConfigCreate = ModelConfigCreate;
/** Composite create: optionally carries initial_terminal_target (model + first target in one transaction). */
export type ManagedModelConfigCompositeCreate = ModelConfigCompositeCreate;
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
  is_default?: boolean;
};

type RawLoadbalanceStrategy = RawLoadbalanceStrategySummary & {
  profile_id: number;
  attached_model_count: number;
  created_at: string;
  updated_at: string;
};

type RawModelAccessTarget = ModelAccessTarget;

type RawModelConfigListItem = Omit<
  ManagedModelConfigListItem,
  "loadbalance_strategy" | "access_targets"
> & {
  loadbalance_strategy: RawLoadbalanceStrategySummary | null;
  access_targets: RawModelAccessTarget[];
};

type RawModelConfigListReadinessItem = RawModelConfigListItem & {
  route_readiness?: ModelRouteReadinessSummary;
};

type RawModelConfig = Omit<
  ManagedModelConfig,
  "loadbalance_strategy" | "access_targets"
> & {
  loadbalance_strategy: RawLoadbalanceStrategySummary | null;
  access_targets: RawModelAccessTarget[];
};

function unsupportedLoadbalanceStrategy(reason: string): never {
  throw new Error(
    `Unsupported loadbalance strategy contract from management API: ${reason}`,
  );
}

function normalizeInteger(value: unknown, field: string) {
  if (
    typeof value !== "number" ||
    !Number.isFinite(value) ||
    !Number.isInteger(value)
  ) {
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

function normalizeModelAccessTarget(
  target: RawModelAccessTarget,
): ModelAccessTarget {
  // SAFETY: RawModelAccessTarget is a typed contract, but retired wire fields
  // (weight/target_priority) can only be detected by probing the raw payload
  // as an untyped record; Object.hasOwn never reads the values.
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

type ModelMutationEnvelope = {
  model: RawModelConfig;
  configuration_warnings: ConfigurationWarning[];
};

type AccessTargetMutationEnvelope = {
  access_targets: RawModelAccessTarget[];
  configuration_warnings: ConfigurationWarning[];
};

export interface ConnectionMutationAccessTarget {
  id: number;
  target_type: string;
  connection_id: number | null;
  terminal_target_id: number | null;
  position: number;
  is_enabled: boolean;
}

type ConnectionMutationEnvelope = {
  connection: Connection;
  access_targets: ConnectionMutationAccessTarget[];
  configuration_warnings: ConfigurationWarning[];
};

type DeletedConnectionMutationEnvelope = {
  deleted: boolean;
  access_targets: Array<{
    id: number;
    target_type: string;
    connection_id: number | null;
    terminal_target_id: number | null;
    position: number;
    is_enabled: boolean;
  }>;
  configuration_warnings: ConfigurationWarning[];
};

export interface TerminalTargetCopyRequest {
  destination_model_config_ids: number[];
  enable_copies?: boolean;
}

export interface TerminalTargetCopyResponse {
  source_connection_id: number;
  items: Array<{
    model_config_id: number;
    connection_summary: {
      id: number;
      name: string | null;
      endpoint_id: number;
      is_active: boolean;
      openai_text_capability: string | null;
      pricing_template: { id: number; name: string } | null;
      qps_limit: number | null;
      max_in_flight_non_stream: number | null;
      max_in_flight_stream: number | null;
      custom_header_count: number;
      custom_request_parameter_count: number;
    };
    access_target: {
      id: number;
      target_type: string;
      connection_id: number | null;
      terminal_target_id: number | null;
      position: number;
      is_enabled: boolean;
    };
  }>;
  configuration_warnings: ConfigurationWarning[];
}

function normalizeTargetMutationEnvelope(
  envelope: AccessTargetMutationEnvelope,
): {
  access_targets: ModelAccessTarget[];
  configuration_warnings: ConfigurationWarning[];
} {
  return {
    access_targets: envelope.access_targets.map(normalizeModelAccessTarget),
    configuration_warnings: envelope.configuration_warnings ?? [],
  };
}

function normalizeConnectionMutationEnvelope(
  envelope: ConnectionMutationEnvelope,
): {
  connection: Connection;
  access_targets: ConnectionMutationAccessTarget[];
  configuration_warnings: ConfigurationWarning[];
} {
  return {
    connection: envelope.connection,
    access_targets: envelope.access_targets ?? [],
    configuration_warnings: envelope.configuration_warnings ?? [],
  };
}

function normalizeLegacyStrategyType(
  value: unknown,
): LegacyLoadbalanceStrategyType {
  if (value === "single" || value === "fill-first" || value === "round-robin") {
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
  if (
    !Array.isArray(value) ||
    value.some((statusCode) => typeof statusCode !== "number")
  ) {
    unsupportedLoadbalanceStrategy("failure_status_codes");
  }

  return normalizeFailureStatusCodes(value);
}

const removedRetryAttemptField = ["retry", "max", "attempts"].join("_");

function rejectRemovedLoadbalanceBanPolicyFields(
  strategy: RawLoadbalanceBanPolicyFields,
) {
  if (Object.hasOwn(strategy, removedRetryAttemptField)) {
    unsupportedLoadbalanceStrategy(removedRetryAttemptField);
  }
}

function normalizeLoadbalanceBanPolicyFields(
  strategy: RawLoadbalanceBanPolicyFields,
): LoadbalanceBanPolicyFields {
  rejectRemovedLoadbalanceBanPolicyFields(strategy);

  return {
    legacy_strategy_type: normalizeLegacyStrategyType(
      strategy.legacy_strategy_type,
    ),
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
    retry_jitter_ratio: normalizeNumber(
      strategy.retry_jitter_ratio,
      "retry_jitter_ratio",
    ),
    retry_max_delay_ms: normalizeInteger(
      strategy.retry_max_delay_ms,
      "retry_max_delay_ms",
    ),
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

function normalizeLoadbalanceStrategySummary(
  strategy: RawLoadbalanceStrategySummary | null,
): LoadbalanceStrategySummary | null {
  if (!strategy) {
    return null;
  }

  return {
    id: strategy.id,
    name: strategy.name,
    ...normalizeLoadbalanceBanPolicyFields(strategy),
  };
}

function normalizeLoadbalanceStrategy(
  strategy: RawLoadbalanceStrategy,
): LoadbalanceStrategy {
  return {
    id: strategy.id,
    profile_id: strategy.profile_id,
    name: strategy.name,
    is_default: strategy.is_default === true,
    ...normalizeLoadbalanceBanPolicyFields(strategy),
    attached_model_count: strategy.attached_model_count,
    created_at: strategy.created_at,
    updated_at: strategy.updated_at,
  };
}

function normalizeModelConfigListItem(
  model: RawModelConfigListItem,
): ManagedModelConfigListItem {
  return {
    id: model.id,
    profile_id: model.profile_id,
    api_family: model.api_family,
    model_id: model.model_id,
    display_name: model.display_name,
    openai_accepted_format: model.openai_accepted_format,
    openai_image_operations: model.openai_image_operations,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(
      model.loadbalance_strategy,
    ),
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
    openai_image_operations: model.openai_image_operations,
    loadbalance_strategy_id: model.loadbalance_strategy_id,
    loadbalance_strategy: normalizeLoadbalanceStrategySummary(
      model.loadbalance_strategy,
    ),
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
  routeReadiness: () =>
    request<ModelRouteReadinessEnvelope<RawModelConfigListReadinessItem>>(
      "/api/models?include=route_readiness",
    ).then((response) => ({
      items: response.items.map((model) => ({
        ...normalizeModelConfigListItem(model),
        route_readiness: model.route_readiness,
      })),
      route_readiness: response.route_readiness,
    })),
  byEndpoint: (endpointId: number) =>
    request<RawModelConfigListItem[]>(
      `/api/models/by-endpoint/${endpointId}`,
    ).then((models) => models.map(normalizeModelConfigListItem)),
  get: (id: number) =>
    request<RawModelConfig>(`/api/models/${id}`).then(normalizeModelConfig),
  create: (
    data: ManagedModelConfigCreate | ManagedModelConfigCompositeCreate,
  ) =>
    request<ModelMutationEnvelope>(`/api/models`, {
      method: "POST",
      body: JSON.stringify(data),
    }).then((response) => ({
      model: normalizeModelConfig(response.model),
      configuration_warnings: response.configuration_warnings ?? [],
    })),
  update: (id: number, data: ManagedModelConfigUpdate) =>
    request<ModelMutationEnvelope>(`/api/models/${id}`, {
      method: "PUT",
      body: JSON.stringify(data),
    }).then((response) => ({
      model: normalizeModelConfig(response.model),
      configuration_warnings: response.configuration_warnings ?? [],
    })),
  delete: (id: number) =>
    request<void>(`/api/models/${id}`, { method: "DELETE" }),
  targets: {
    list: (modelConfigId: number) =>
      request<RawModelAccessTarget[]>(
        `/api/models/${modelConfigId}/targets`,
      ).then((targets) => targets.map(normalizeModelAccessTarget)),
    create: (modelConfigId: number, data: ModelAccessTargetCreate) =>
      request<AccessTargetMutationEnvelope>(
        `/api/models/${modelConfigId}/targets`,
        {
          method: "POST",
          body: JSON.stringify(data),
        },
      ).then(normalizeTargetMutationEnvelope),
    update: (
      modelConfigId: number,
      targetId: number,
      data: ModelAccessTargetUpdate,
    ) =>
      request<AccessTargetMutationEnvelope>(
        `/api/models/${modelConfigId}/targets/${targetId}`,
        {
          method: "PATCH",
          body: JSON.stringify(data),
        },
      ).then(normalizeTargetMutationEnvelope),
    movePosition: (modelConfigId: number, targetId: number, toIndex: number) =>
      request<AccessTargetMutationEnvelope>(
        `/api/models/${modelConfigId}/targets/${targetId}/position`,
        {
          method: "PATCH",
          body: JSON.stringify({ to_index: toIndex }),
        },
      ).then(normalizeTargetMutationEnvelope),
    delete: (modelConfigId: number, targetId: number) =>
      request<AccessTargetMutationEnvelope>(
        `/api/models/${modelConfigId}/targets/${targetId}`,
        {
          method: "DELETE",
        },
      ).then(normalizeTargetMutationEnvelope),
  },
  catalog: {
    get: (modelConfigId: number) =>
      request<ModelCatalogResponse>(`/api/models/${modelConfigId}/catalog`),
    matchPreview: (modelConfigId: number) =>
      request<ModelCatalogMatchPreviewResponse>(
        `/api/models/${modelConfigId}/catalog/match-preview`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      ),
    bind: (modelConfigId: number, data: CatalogBindingRequest) =>
      request<ModelCatalogResponse>(
        `/api/models/${modelConfigId}/catalog/bind`,
        {
          method: "POST",
          body: JSON.stringify(data),
        },
      ),
    refreshPreview: (modelConfigId: number) =>
      request<ModelCatalogRefreshPreviewResponse>(
        `/api/models/${modelConfigId}/catalog/refresh/preview`,
        {
          method: "POST",
          body: JSON.stringify({}),
        },
      ),
    refreshCommit: (modelConfigId: number, expectedCatalogRevision: string) =>
      request<ModelCatalogResponse>(
        `/api/models/${modelConfigId}/catalog/refresh/commit`,
        {
          method: "POST",
          body: JSON.stringify({
            expected_catalog_revision: expectedCatalogRevision,
          }),
        },
      ),
    putOverride: (modelConfigId: number, patch: CatalogOverridePatch) =>
      request<ModelCatalogResponse>(
        `/api/models/${modelConfigId}/catalog/override`,
        {
          method: "PUT",
          body: JSON.stringify(patch),
        },
      ),
    clearOverride: (modelConfigId: number) =>
      request<ModelCatalogResponse>(
        `/api/models/${modelConfigId}/catalog/override`,
        {
          method: "DELETE",
        },
      ),
    unbind: (modelConfigId: number) =>
      request<ModelCatalogResponse>(`/api/models/${modelConfigId}/catalog`, {
        method: "DELETE",
      }),
    candidates: (
      modelConfigId: number,
      params?: {
        q?: string;
        scope?: "family" | "all";
        limit?: number;
        offset?: number;
      },
    ) => {
      const query = buildQuery(
        params as Record<string, string | number | undefined> | undefined,
      );
      return request<ModelCatalogCandidatesResponse>(
        `/api/models/${modelConfigId}/catalog/candidates${query ? `?${query}` : ""}`,
      );
    },
  },
  connections: {
    list: (modelConfigId: number) =>
      request<Connection[]>(`/api/models/${modelConfigId}/connections`),
    create: (modelConfigId: number, data: ModelConnectionCreate) =>
      request<ConnectionMutationEnvelope>(
        `/api/models/${modelConfigId}/connections`,
        {
          method: "POST",
          body: JSON.stringify(data),
        },
      ).then(normalizeConnectionMutationEnvelope),
    update: (
      modelConfigId: number,
      connectionId: number,
      data: ModelConnectionUpdate,
    ) =>
      request<ConnectionMutationEnvelope>(
        `/api/models/${modelConfigId}/connections/${connectionId}`,
        {
          method: "PATCH",
          body: JSON.stringify(data),
        },
      ).then(normalizeConnectionMutationEnvelope),
    delete: (modelConfigId: number, connectionId: number) =>
      request<DeletedConnectionMutationEnvelope>(
        `/api/models/${modelConfigId}/connections/${connectionId}`,
        {
          method: "DELETE",
        },
      ).then((response) => ({
        deleted: response.deleted,
        access_targets: response.access_targets ?? [],
        configuration_warnings: response.configuration_warnings ?? [],
      })),
    copies: (
      modelConfigId: number,
      connectionId: number,
      data: TerminalTargetCopyRequest,
    ) =>
      request<TerminalTargetCopyResponse>(
        `/api/models/${modelConfigId}/connections/${connectionId}/copies`,
        {
          method: "POST",
          body: JSON.stringify(data),
        },
      ),
  },
};

export const loadbalanceStrategies = {
  list: () =>
    request<RawLoadbalanceStrategy[]>("/api/loadbalance/strategies").then(
      (strategies) => strategies.map(normalizeLoadbalanceStrategy),
    ),
  createDefaults: () =>
    request<LoadbalanceStrategyDefaultsResponse>(
      "/api/loadbalance/strategies/defaults",
      {
        method: "POST",
      },
    ),
  setDefault: (strategyId: number, expectedDefaultStrategyId: number | null) =>
    request<StrategySetDefaultResponse>(
      `/api/loadbalance/strategies/${strategyId}/default`,
      {
        method: "PUT",
        body: JSON.stringify({
          expected_default_strategy_id: expectedDefaultStrategyId,
        } satisfies StrategySetDefaultRequest),
      },
    ),
  impact: (strategyId: number, params: { limit?: number; cursor?: string }) => {
    const query = buildQuery(params);
    return request<StrategyImpactListResponse>(
      `/api/loadbalance/strategies/${strategyId}/models${query ? `?${query}` : ""}`,
    );
  },
  preview: (data: StrategyPreviewDraft) =>
    request<StrategyPreviewResponse>("/api/loadbalance/strategies/preview", {
      method: "POST",
      body: JSON.stringify(data),
    }),
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

export {
  connections,
  endpoints,
  pricingTemplates,
} from "./management_resources";
