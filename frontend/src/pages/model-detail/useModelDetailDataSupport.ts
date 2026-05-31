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
  PricingTemplate,
} from "@/lib/types";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getModelConnections } from "../models/modelFormState";
import type { HeaderRow } from "./useModelDetailDialogState";
import { normalizeOpenAIProbeEndpointVariant } from "./connectionProbeBehavior";

function resolveApiFamily(
  model: Pick<ModelConfig, "api_family"> | Pick<ModelConfigListItem, "api_family">,
): ApiFamily {
  return model.api_family;
}

function resolveVendorId(
  model: Pick<ModelConfig, "vendor_id"> | Pick<ModelConfigListItem, "vendor_id">,
) {
  return model.vendor_id ?? null;
}

export const createDefaultEndpointForm = (): EndpointCreate => ({
  name: "",
  base_url: "",
  api_key: "",
});

export const createDefaultConnectionForm = (apiFamily: ApiFamily | null = null): ConnectionCreate => ({
  api_family: apiFamily ?? "openai",
  name: "",
  is_active: true,
  custom_headers: null,
  openai_probe_endpoint_variant:
    apiFamily === "openai" ? normalizeOpenAIProbeEndpointVariant(undefined) : null,
  pricing_template_id: null,
  qps_limit: null,
  max_in_flight_non_stream: null,
  max_in_flight_stream: null,
});

interface BuildConnectionDraftPayloadInput {
  apiFamily: ApiFamily | null;
  createMode: "select" | "new";
  selectedEndpointId: string;
  newEndpointForm: EndpointCreate;
  connectionForm: ConnectionCreate;
  headerRows: HeaderRow[];
  editingConnection: Connection | null;
  endpointSourceDefaultName: string | null;
}

export function normalizeConnectionHeaders(
  headerRows: HeaderRow[],
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
  const customHeaders = normalizeConnectionHeaders(headerRows);

  const typedConnectionName = (connectionForm.name ?? "").trim();
  const resolvedConnectionName =
    typedConnectionName.length > 0
      ? typedConnectionName
      : !editingConnection
        ? endpointSourceDefaultName
        : null;

  const payload: ConnectionCreate = {
    ...connectionForm,
    api_family: apiFamily ?? connectionForm.api_family,
    name: resolvedConnectionName,
    custom_headers: customHeaders,
    pricing_template_id: connectionForm.pricing_template_id,
    openai_probe_endpoint_variant:
      apiFamily === "openai"
        ? normalizeOpenAIProbeEndpointVariant(connectionForm.openai_probe_endpoint_variant)
        : undefined,
    qps_limit: normalizeLimiterField(connectionForm.qps_limit),
    max_in_flight_non_stream: normalizeLimiterField(connectionForm.max_in_flight_non_stream),
    max_in_flight_stream: normalizeLimiterField(connectionForm.max_in_flight_stream),
  };

  if (apiFamily !== "openai") {
    delete payload.openai_probe_endpoint_variant;
  }

  if (createMode === "select") {
    if (!selectedEndpointId) {
      return {
        errorMessage: getStaticMessages().modelDetailData.selectEndpoint,
        payload: null,
      };
    }

    payload.endpoint_id = Number.parseInt(selectedEndpointId, 10);
    delete payload.endpoint_create;
    return { errorMessage: null, payload };
  }

  if (!newEndpointForm.name || !newEndpointForm.base_url || !newEndpointForm.api_key) {
    return {
      errorMessage: getStaticMessages().modelDetailData.fillEndpointFields,
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
  const connections = getModelConnections(model);

  return models.map((item) => {
    if (item.id !== model.id) {
      return item;
    }

    return {
      ...item,
      profile_id: model.profile_id,
      vendor_id: resolveVendorId(model),
      vendor: model.vendor,
      api_family: resolveApiFamily(model),
      model_id: model.model_id,
      display_name: model.display_name,
      loadbalance_strategy_id: model.loadbalance_strategy_id,
      loadbalance_strategy: model.loadbalance_strategy,
      access_targets: model.access_targets,
      is_enabled: model.is_enabled,
      connection_count: connections.length,
      active_connection_count: connections.filter((connection) => connection.is_active).length,
      updated_at: model.updated_at,
    };
  });
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
    (candidate) => candidate.target_type === "connection" && candidate.connection_id === connectionId,
  );

  if (!target) {
    return null;
  }

  return !target.connection || connectionBelongsToModel(target.connection, modelConfigId) ? target : null;
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

export function buildAccessTargetSummary(model: ModelConfig | null) {
  const targets = model?.access_targets ?? [];
  const enabledTargets = targets.filter((target) => target.is_enabled);
  const firstTarget = targets[0] ?? null;
  const firstTargetLabel = firstTarget?.target_type === "model"
    ? firstTarget.target_model?.display_name || firstTarget.target_model_id
    : firstTarget?.connection?.name || firstTarget?.connection?.endpoint?.name || (firstTarget?.connection_id ? getStaticMessages().modelDetail.connectionFallback(firstTarget.connection_id) : null);

  return {
    targetCount: targets.length,
    enabledTargetCount: enabledTargets.length,
    firstTargetLabel,
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

