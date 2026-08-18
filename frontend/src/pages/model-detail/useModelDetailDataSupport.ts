import type {
  ApiFamily,
  Connection,
  ConnectionPricingTemplateSummary,
  Endpoint,
  ModelAccessTarget,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
} from "@/lib/types";
import {
  getTerminalTarget,
  getTerminalTargetId,
  isTerminalTargetAccessTargetType,
} from "@/lib/types/target-compatibility";
import { getModelConnections, toModelListItem } from "../models/modelFormState";

export function resequenceConnections(connections: Connection[]): Connection[] {
  return connections.map((connection, index) => {
    if (connection.priority === index) {
      return connection;
    }

    return {
      ...connection,
      priority: index,
    };
  });
}

export function moveConnectionInList(
  connections: Connection[],
  fromIndex: number,
  toIndex: number
): Connection[] {
  if (
    fromIndex < 0 ||
    toIndex < 0 ||
    fromIndex >= connections.length ||
    toIndex >= connections.length ||
    fromIndex === toIndex
  ) {
    return connections;
  }

  const nextConnections = [...connections];
  const [movedConnection] = nextConnections.splice(fromIndex, 1);

  if (!movedConnection) {
    return connections;
  }

  nextConnections.splice(toIndex, 0, movedConnection);
  return resequenceConnections(nextConnections);
}

export function getSelectedEndpoint(
  globalEndpoints: Endpoint[],
  selectedEndpointId: string
): Endpoint | null {
  const parsedEndpointId = Number.parseInt(selectedEndpointId, 10);
  if (!Number.isFinite(parsedEndpointId)) {
    return null;
  }
  return globalEndpoints.find((endpoint) => endpoint.id === parsedEndpointId) ?? null;
}

export function upsertConnectionInList(
  connections: Connection[],
  nextConnection: Connection,
): Connection[] {
  const hasExistingConnection = connections.some((connection) => connection.id === nextConnection.id);
  const nextConnections = hasExistingConnection
    ? connections.map((connection) => (
      connection.id === nextConnection.id ? nextConnection : connection
    ))
    : [...connections, nextConnection];

  return resequenceConnections(
    [...nextConnections].sort((left, right) => left.priority - right.priority),
  );
}

export function hydrateConnectionPricingTemplate(
  connection: Connection,
  pricingTemplates: PricingTemplate[],
): Connection {
  if (connection.pricing_template_id == null) {
    return connection.pricing_template == null
      ? connection
      : { ...connection, pricing_template: null };
  }

  if (connection.pricing_template?.id === connection.pricing_template_id) {
    return connection;
  }

  const matchedTemplate = pricingTemplates.find((template) => template.id === connection.pricing_template_id);

  if (!matchedTemplate) {
    return connection;
  }

  return {
    ...connection,
    pricing_template: buildConnectionPricingTemplateSummary(matchedTemplate),
  };
}

export function removeConnectionFromList(
  connections: Connection[],
  connectionId: number,
): Connection[] {
  return resequenceConnections(
    connections.filter((connection) => connection.id !== connectionId),
  );
}

export function upsertEndpointInList(
  endpoints: Endpoint[],
  endpoint: Endpoint | undefined,
): Endpoint[] {
  if (!endpoint) {
    return endpoints;
  }

  const hasExistingEndpoint = endpoints.some((current) => current.id === endpoint.id);
  const nextEndpoints = hasExistingEndpoint
    ? endpoints.map((current) => (current.id === endpoint.id ? endpoint : current))
    : [...endpoints, endpoint];

  return [...nextEndpoints].sort((left, right) => left.name.localeCompare(right.name, "zh-CN") || left.id - right.id);
}

export function patchModelListConnectionCounts(
  models: ModelConfigListItem[],
  modelConfigId: number,
  connections: Connection[],
): ModelConfigListItem[] {
  return models.map((item) => {
    if (item.id !== modelConfigId) {
      return item;
    }

    return {
      ...item,
      connection_count: connections.length,
      active_connection_count: connections.filter((connection) => connection.is_active).length,
    };
  });
}

export function patchModelListItemFromDetail(
  models: ModelConfigListItem[],
  model: ModelConfig,
): ModelConfigListItem[] {
  return models.map((item) => (item.id === model.id ? toModelListItem(model, item) : item));
}

export function connectionBelongsToModel(
  connection: Pick<Connection, "model_config_id"> | null | undefined,
  modelConfigId: number | undefined,
): boolean {
  if (!connection || !Number.isFinite(modelConfigId)) {
    return false;
  }

  return connection.model_config_id == null || connection.model_config_id === modelConfigId;
}

export function getOwnedConnectionTarget(
  model: Pick<ModelConfig, "access_targets"> | null | undefined,
  modelConfigId: number | undefined,
  connectionId: number,
): ModelAccessTarget | null {
  if (!model || !Number.isFinite(modelConfigId)) {
    return null;
  }

  const target = model.access_targets.find(
    (candidate) => isTerminalTargetAccessTargetType(candidate.target_type)
      && getTerminalTargetId(candidate) === connectionId,
  );

  if (!target) {
    return null;
  }

  const terminalTarget = getTerminalTarget(target);
  return !terminalTarget || connectionBelongsToModel(terminalTarget, modelConfigId)
    ? target
    : null;
}

export function isOwnedConnectionTarget(
  model: Pick<ModelConfig, "access_targets"> | null | undefined,
  modelConfigId: number | undefined,
  connectionId: number,
): boolean {
  return getOwnedConnectionTarget(model, modelConfigId, connectionId) !== null;
}

export function getSameFamilyConnections(
  connections: Connection[],
  apiFamily: ApiFamily,
  modelConfigId?: number,
): Connection[] {
  return connections.filter(
    (connection) => connection.api_family === apiFamily && connectionBelongsToModel(connection, modelConfigId),
  );
}

export function getOwnedModelConnections(
  model: Pick<ModelConfig, "access_targets">,
  modelConfigId: number | undefined,
): Connection[] {
  return getModelConnections(model).filter((connection) => connectionBelongsToModel(connection, modelConfigId));
}

export interface AccessTargetSummary {
  totalTargetCount: number;
  enabledTargetCount: number;
  totalModelTargetCount: number;
  totalTerminalTargetCount: number;
  enabledModelFallbackTargetCount: number;
  enabledTerminalTargetCount: number;
}

export function buildAccessTargetSummary(model: ModelConfig | null): AccessTargetSummary {
  const targets = model?.access_targets ?? [];
  const enabledTargets = targets.filter((target) => target.is_enabled);
  const modelTargets = targets.filter((target) => target.target_type === "model");
  const terminalTargets = targets.filter((target) => isTerminalTargetAccessTargetType(target.target_type));
  const enabledModelFallbackTargets = enabledTargets.filter((target) => target.target_type === "model");
  const enabledTerminalTargets = enabledTargets.filter((target) => isTerminalTargetAccessTargetType(target.target_type));

  return {
    totalTargetCount: targets.length,
    enabledTargetCount: enabledTargets.length,
    totalModelTargetCount: modelTargets.length,
    totalTerminalTargetCount: terminalTargets.length,
    enabledModelFallbackTargetCount: enabledModelFallbackTargets.length,
    enabledTerminalTargetCount: enabledTerminalTargets.length,
  };
}

function buildConnectionPricingTemplateSummary(
  pricingTemplate: PricingTemplate,
): ConnectionPricingTemplateSummary {
  return {
    id: pricingTemplate.id,
    name: pricingTemplate.name,
    pricing_unit: pricingTemplate.pricing_unit,
    pricing_currency_code: pricingTemplate.pricing_currency_code,
    version: pricingTemplate.version,
  };
}

export { buildConnectionDraftPayload, buildConnectionUpdatePayload, createDefaultEndpointForm, DEFAULT_OPENAI_TEXT_CAPABILITY, normalizeConnectionHeaders, normalizeOpenAITextCapability } from "./connectionDataSupport";
