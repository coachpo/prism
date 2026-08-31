import type {
  CatalogBindingRequest,
  ModelCatalogOverrideClearRequest,
  ModelCatalogOverrideWriteRequest,
  ModelCatalogRefreshCommitRequest,
  ModelCatalogUnbindRequest,
  Connection,
  ModelCatalogCandidatesResponse,
  ModelCatalogMatchPreviewResponse,
  ModelCatalogRefreshPreviewResponse,
  ModelCatalogResponse,
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
  OpenAIImageCapability,
  OpenAITextCapability,
} from "../types";
import {
  normalizeLoadbalanceStrategySummary,
  type RawLoadbalanceStrategySummary,
} from "./loadbalanceStrategies";
import { buildQuery, request } from "./request";

export type ManagedModelConfigListItem = ModelConfigListItem;
export type ManagedModelConfig = ModelConfig;
export type ManagedModelConfigCreate = ModelConfigCreate;
/** Composite create: optionally carries initial_terminal_target (model + first target in one transaction). */
export type ManagedModelConfigCompositeCreate = ModelConfigCompositeCreate;
export type ManagedModelConfigUpdate = ModelConfigUpdate;

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
  access_targets: ConnectionMutationAccessTarget[];
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
      openai_text_capability: OpenAITextCapability | null;
      openai_image_capability: OpenAIImageCapability | null;
      pricing_template: { id: number; name: string } | null;
      qps_limit: number | null;
      max_in_flight_non_stream: number | null;
      max_in_flight_stream: number | null;
      custom_header_count: number;
      custom_request_parameter_count: number;
    };
    access_target: {
      id: number;
      target_type: "connection";
      connection_id: number;
      terminal_target_id: number;
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
    routing_summary: model.routing_summary,
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
    request<{ deleted: true }>(`/api/models/${id}`, { method: "DELETE" }),
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
    refreshCommit: (
      modelConfigId: number,
      expected: ModelCatalogRefreshCommitRequest,
    ) =>
      request<ModelCatalogResponse>(
        `/api/models/${modelConfigId}/catalog/refresh/commit`,
        {
          method: "POST",
          body: JSON.stringify(expected),
        },
      ),
    putOverride: (
      modelConfigId: number,
      expected: ModelCatalogOverrideWriteRequest,
    ) =>
      request<ModelCatalogResponse>(
        `/api/models/${modelConfigId}/catalog/override`,
        {
          method: "PUT",
          body: JSON.stringify(expected),
        },
      ),
    clearOverride: (
      modelConfigId: number,
      expected: ModelCatalogOverrideClearRequest,
    ) =>
      request<ModelCatalogResponse>(
        `/api/models/${modelConfigId}/catalog/override`,
        {
          method: "DELETE",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(expected),
        },
      ),
    unbind: (modelConfigId: number, expected: ModelCatalogUnbindRequest) =>
      request<ModelCatalogResponse>(`/api/models/${modelConfigId}/catalog`, {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(expected),
      }),
    candidates: (
      modelConfigId: number,
      params?: {
        q?: string;
        scope?: "family" | "all";
        limit?: number;
        offset?: number;
        signal?: AbortSignal;
      },
    ) => {
      const { signal, ...queryParams } = params ?? {};
      const query = buildQuery(
        queryParams as Record<string, string | number | undefined> | undefined,
      );
      return request<ModelCatalogCandidatesResponse>(
        `/api/models/${modelConfigId}/catalog/candidates${query ? `?${query}` : ""}`,
        signal ? { signal } : undefined,
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
