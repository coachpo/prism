import type { RoutingSchedule } from "@/lib/types/routing"
import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  EndpointCreate,
  JsonObject,
  ModelConnectionUpdate,
} from "@/lib/types"
import { getStaticMessages } from "@/i18n/staticMessages"

export const createDefaultEndpointForm = (): EndpointCreate => ({
  name: "",
  base_url: "",
  api_key: "",
});

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

  // Interactive forms validate this required value before projection. Keep an
  // explicit blank present as a fail-closed backend 422 instead of silently
  // converting operator input into create-default or PATCH-preserve semantics.
  const upstreamModelId = connectionForm.upstream_model_id?.trim();

  const resolvedApiFamily = apiFamily ?? connectionForm.api_family;
  const payload: ConnectionCreate = {
    api_family: resolvedApiFamily,
    name: resolvedConnectionName,
    is_active: connectionForm.is_active,
    custom_headers: customHeaders,
    custom_request_parameters: customRequestParametersValue,
    routing_schedule: routingScheduleValue,
    // The backend requires strict equality with the owner model's accepted
    // format, absent modes included. An image-only owner declares none, so a
    // substituted default would turn every save into a 422.
    openai_text_capability:
      resolvedApiFamily === "openai"
        ? (connectionForm.openai_text_capability ?? null)
        : undefined,
    pricing_template_id: connectionForm.pricing_template_id,
    qps_limit: normalizeLimiterField(connectionForm.qps_limit),
    max_in_flight_non_stream: normalizeLimiterField(connectionForm.max_in_flight_non_stream),
    max_in_flight_stream: normalizeLimiterField(connectionForm.max_in_flight_stream),
  };

  if (upstreamModelId !== undefined) {
    payload.upstream_model_id = upstreamModelId;
  }

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
 * backend then requires both CAS fields alongside it. An unchanged upstream
 * model ID is omitted; an explicit blank stays present for backend rejection.
 */
export function buildConnectionUpdatePayload(
  draftPayload: ConnectionCreate,
  editingConnection: Connection,
): ModelConnectionUpdate {
  const payload: ModelConnectionUpdate = { ...draftPayload };
  const nextUpstreamModelId = draftPayload.upstream_model_id?.trim();
  const currentUpstreamModelId = editingConnection.upstream_model_id ?? "";
  if (
    nextUpstreamModelId === undefined ||
    nextUpstreamModelId === currentUpstreamModelId
  ) {
    delete payload.upstream_model_id;
  }
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
