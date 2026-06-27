import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  ConnectionPricingTemplateSummary,
  Endpoint,
  EndpointCreate,
  HealthCheckResponse,
  ModelAccessTarget,
  ModelConfig,
  ModelConfigListItem,
  OpenAITextCapability,
  PricingTemplate,
} from "@/lib/types";
import {
  getTerminalTarget,
  getTerminalTargetId,
  isTerminalTargetAccessTargetType,
} from "@/lib/types/target-compatibility";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getModelConnections, toModelListItem } from "../models/modelFormState";
import { normalizeOpenAIProbeEndpointVariant } from "./connectionProbeBehavior";

export const createDefaultEndpointForm = (): EndpointCreate => ({
  name: "",
  base_url: "",
  api_key: "",
});

export const DEFAULT_OPENAI_TEXT_CAPABILITY: OpenAITextCapability = "responses_only";

export function normalizeOpenAITextCapability(
  capability: OpenAITextCapability | null | undefined,
): OpenAITextCapability {
  return capability ?? DEFAULT_OPENAI_TEXT_CAPABILITY;
}

type HeaderRowLike = {
  id: string;
  key: string;
  value: string;
};

type ConnectionDialogFormLike = ConnectionCreate;

interface BuildConnectionDraftPayloadInput {
  apiFamily: ApiFamily | null;
  createMode: "select" | "new";
  selectedEndpointId: string;
  newEndpointForm: EndpointCreate;
  connectionForm: ConnectionDialogFormLike;
  headerRows: HeaderRowLike[];
  editingConnection: Connection | null;
  endpointSourceDefaultName: string | null;
}

export function normalizeConnectionHeaders(
  headerRows: HeaderRowLike[],
): Record<string, string> | null {
  const customHeaders = Object.fromEntries(
    headerRows.filter((row) => row.key.trim()).map((row) => [row.key.trim(), row.value]),
  );

  return Object.keys(customHeaders).length > 0 ? customHeaders : null;
}

export function buildConnectionDraftPayload({
  apiFamily,
  createMode,
  selectedEndpointId,
  newEndpointForm,
  connectionForm,
  headerRows,
  editingConnection,
  endpointSourceDefaultName,
}: BuildConnectionDraftPayloadInput): {
  errorMessage: string | null;
  payload: ConnectionCreate | null;
} {
  const messages = getStaticMessages();
  const customHeaders = normalizeConnectionHeaders(headerRows);

  const typedConnectionName = (connectionForm.name ?? "").trim();
  const resolvedConnectionName =
    typedConnectionName.length > 0
      ? typedConnectionName
      : !editingConnection
        ? endpointSourceDefaultName
        : null;

  const resolvedApiFamily = apiFamily ?? connectionForm.api_family;
  const payload: ConnectionCreate = {
    api_family: resolvedApiFamily,
    name: resolvedConnectionName,
    is_active: connectionForm.is_active,
    custom_headers: customHeaders,
    openai_text_capability:
      resolvedApiFamily === "openai"
        ? normalizeOpenAITextCapability(connectionForm.openai_text_capability)
        : undefined,
    openai_probe_endpoint_variant:
      resolvedApiFamily === "openai"
        ? normalizeOpenAIProbeEndpointVariant(connectionForm.openai_probe_endpoint_variant)
        : undefined,
    pricing_template_id: connectionForm.pricing_template_id,
    qps_limit: normalizeLimiterField(connectionForm.qps_limit),
    max_in_flight_non_stream: normalizeLimiterField(connectionForm.max_in_flight_non_stream),
    max_in_flight_stream: normalizeLimiterField(connectionForm.max_in_flight_stream),
  };

  if (resolvedApiFamily !== "openai") {
    delete payload.openai_text_capability;
    delete payload.openai_probe_endpoint_variant;
  }

  if (createMode === "select") {
    if (!selectedEndpointId) {
      return {
        errorMessage: messages.modelDetailData.selectEndpoint,
        payload: null,
      };
    }

    payload.endpoint_id = Number.parseInt(selectedEndpointId, 10);
    delete payload.endpoint_create;
    return { errorMessage: null, payload };
  }

  if (!newEndpointForm.name || !newEndpointForm.base_url || !newEndpointForm.api_key) {
    return {
      errorMessage: messages.modelDetailData.fillEndpointFields,
      payload: null,
    };
  }

  payload.endpoint_create = newEndpointForm;
  delete payload.endpoint_id;
  return { errorMessage: null, payload };
}

function normalizeLimiterField(value: number | null | undefined): number | null {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return null;
  }

  return value;
}

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

export function applyConnectionHealthChecks(
  connections: Connection[],
  checks: Map<number, HealthCheckResponse>
): Connection[] {
  return connections.map((connection) => {
    const check = checks.get(connection.id);
    if (!check) return connection;

    return {
      ...connection,
      health_status: check.health_status,
      health_detail: check.detail,
      last_health_check: check.checked_at,
    };
  });
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

  return [...nextEndpoints].sort((left, right) => left.position - right.position);
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
  enabledModelFallbackTargetCount: number;
  enabledTerminalTargetCount: number;
  firstEnabledModelFallbackTargetLabel: string | null;
  firstEnabledTerminalTargetLabel: string | null;
  routePolicyLabel: string;
}

function getAccessTargetLabel(target: ModelAccessTarget | null | undefined): string | null {
  if (!target) {
    return null;
  }

  if (target.target_type === "model") {
    return target.target_model?.display_name || target.target_model_id;
  }

  const terminalTarget = getTerminalTarget(target);
  const terminalTargetId = getTerminalTargetId(target);
  return terminalTarget?.name
    || terminalTarget?.endpoint?.name
    || (terminalTargetId != null
      ? getStaticMessages().modelDetail.connectionFallback(terminalTargetId)
      : null);
}

export function buildAccessTargetSummary(model: ModelConfig | null): AccessTargetSummary {
  const targets = model?.access_targets ?? [];
  const enabledTargets = targets.filter((target) => target.is_enabled);
  const enabledModelFallbackTargets = enabledTargets.filter((target) => target.target_type === "model");
  const enabledTerminalTargets = enabledTargets.filter((target) => isTerminalTargetAccessTargetType(target.target_type));

  return {
    totalTargetCount: targets.length,
    enabledTargetCount: enabledTargets.length,
    enabledModelFallbackTargetCount: enabledModelFallbackTargets.length,
    enabledTerminalTargetCount: enabledTerminalTargets.length,
    firstEnabledModelFallbackTargetLabel: getAccessTargetLabel(enabledModelFallbackTargets[0]),
    firstEnabledTerminalTargetLabel: getAccessTargetLabel(enabledTerminalTargets[0]),
    routePolicyLabel: getStaticMessages().modelDetail.orderedPriorityRouting,
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
