import type { RoutingSchedule } from "@/lib/types/routing";
import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  ConnectionPricingTemplateSummary,
  Endpoint,
  EndpointCreate,
  ModelAccessTarget,
  ModelConfig,
  ModelConfigListItem,
  ModelConnectionUpdate,
  OpenAITextCapability,
  PricingTemplate,
  JsonObject,
} from "@/lib/types";
import {
  getTerminalTarget,
  getTerminalTargetId,
  isTerminalTargetAccessTargetType,
} from "@/lib/types/target-compatibility";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getModelConnections, toModelListItem } from "../models/modelFormState";

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
  customRequestParametersValue: JsonObject | null;
  routingScheduleValue: RoutingSchedule | null;
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
  customRequestParametersValue,
  routingScheduleValue,
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

  // The backend accepts a positive integer or nothing at all; 0 is rejected
  // there and would otherwise reach the operator as an untranslated 422 detail.
  const limiterError = firstInvalidLimiterMessage(connectionForm);
  if (limiterError) {
    return { errorMessage: limiterError, payload: null };
  }

  const resolvedApiFamily = apiFamily ?? connectionForm.api_family;
  const payload: ConnectionCreate = {
    api_family: resolvedApiFamily,
    name: resolvedConnectionName,
    is_active: connectionForm.is_active,
    custom_headers: customHeaders,
    custom_request_parameters: customRequestParametersValue,
    routing_schedule: routingScheduleValue,
    openai_text_capability:
      resolvedApiFamily === "openai"
        ? normalizeOpenAITextCapability(connectionForm.openai_text_capability)
        : undefined,
    pricing_template_id: connectionForm.pricing_template_id,
    qps_limit: normalizeLimiterField(connectionForm.qps_limit),
    max_in_flight_non_stream: normalizeLimiterField(connectionForm.max_in_flight_non_stream),
    max_in_flight_stream: normalizeLimiterField(connectionForm.max_in_flight_stream),
  };

  if (resolvedApiFamily !== "openai") {
    delete payload.openai_text_capability;
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

/**
 * Shapes the PATCH body for an existing Terminal Target. `pricing_template_id`
 * is only sent when the draft actually moves the pricing reference, and the
 * backend then requires both CAS fields alongside it.
 */
export function buildConnectionUpdatePayload(
  draftPayload: ConnectionCreate,
  editingConnection: Connection,
): ModelConnectionUpdate {
  const payload: ModelConnectionUpdate = { ...draftPayload };
  const nextPricingTemplateId = draftPayload.pricing_template_id ?? null;
  const currentPricingTemplateId = editingConnection.pricing_template_id ?? null;

  if (nextPricingTemplateId === currentPricingTemplateId) {
    delete payload.pricing_template_id;
    return payload;
  }

  payload.pricing_template_id = nextPricingTemplateId;
  payload.expected_connection_updated_at = editingConnection.updated_at;
  payload.expected_pricing_template_id = currentPricingTemplateId;
  return payload;
}

function normalizeLimiterField(value: number | null | undefined): number | null {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return null;
  }

  return value;
}

/**
 * The three limiter columns are "leave empty for no limit, otherwise a positive
 * integer". Zero is not a third option: the runtime only enforces a limit that
 * is greater than zero, so a stored 0 would read as a throttle on screen while
 * imposing nothing. The dialog names the offending field rather than letting the
 * backend answer with an untranslated 422.
 */
function firstInvalidLimiterMessage(connectionForm: ConnectionDialogFormLike): string | null {
  const messages = getStaticMessages();
  const fields: Array<{ value: number | null | undefined; label: string }> = [
    { value: connectionForm.qps_limit, label: messages.modelDetail.qpsLimit },
    { value: connectionForm.max_in_flight_non_stream, label: messages.modelDetail.maxInFlightNonStream },
    { value: connectionForm.max_in_flight_stream, label: messages.modelDetail.maxInFlightStream },
  ];
  for (const field of fields) {
    const normalized = normalizeLimiterField(field.value);
    if (normalized !== null && normalized < 1) {
      return messages.modelDetailData.limiterMustBePositive(field.label);
    }
  }
  return null;
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
