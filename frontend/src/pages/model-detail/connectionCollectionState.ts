import type {
  Connection,
  ConnectionPricingTemplateSummary,
  Endpoint,
  PricingTemplate,
} from "@/lib/types";

export function resequenceConnections(connections: Connection[]): Connection[] {
  return connections.map((connection, index) => {
    if (connection.priority === index) return connection;
    return { ...connection, priority: index };
  });
}

export function moveConnectionInList(
  connections: Connection[],
  fromIndex: number,
  toIndex: number,
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
  if (!movedConnection) return connections;
  nextConnections.splice(toIndex, 0, movedConnection);
  return resequenceConnections(nextConnections);
}

export function getSelectedEndpoint(
  globalEndpoints: Endpoint[],
  selectedEndpointId: string,
): Endpoint | null {
  const parsedEndpointId = Number.parseInt(selectedEndpointId, 10);
  if (!Number.isFinite(parsedEndpointId)) return null;
  return (
    globalEndpoints.find((endpoint) => endpoint.id === parsedEndpointId) ?? null
  );
}

export function upsertConnectionInList(
  connections: Connection[],
  nextConnection: Connection,
): Connection[] {
  const hasExistingConnection = connections.some(
    (connection) => connection.id === nextConnection.id,
  );
  const nextConnections = hasExistingConnection
    ? connections.map((connection) =>
        connection.id === nextConnection.id ? nextConnection : connection,
      )
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

  const matchedTemplate = pricingTemplates.find(
    (template) => template.id === connection.pricing_template_id,
  );
  if (!matchedTemplate) return connection;
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
  if (!endpoint) return endpoints;
  const hasExistingEndpoint = endpoints.some(
    (current) => current.id === endpoint.id,
  );
  const nextEndpoints = hasExistingEndpoint
    ? endpoints.map((current) =>
        current.id === endpoint.id ? endpoint : current,
      )
    : [...endpoints, endpoint];
  return [...nextEndpoints].sort(
    (left, right) =>
      left.name.localeCompare(right.name, "zh-CN") || left.id - right.id,
  );
}

function buildConnectionPricingTemplateSummary(
  pricingTemplate: PricingTemplate,
): ConnectionPricingTemplateSummary {
  return {
    id: pricingTemplate.id,
    name: pricingTemplate.name,
    pricing_unit: pricingTemplate.pricing_unit,
    pricing_currency_code: pricingTemplate.pricing_currency_code,
    template_kind: pricingTemplate.template_kind,
    version: pricingTemplate.version,
  };
}
