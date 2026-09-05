import { useMemo, useState } from "react";
import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  Endpoint,
  EndpointCreate,
  OpenAIAcceptedFormat,
} from "@/lib/types";
import { createDefaultEndpointForm } from "./connectionDataSupport";
import { getSelectedEndpoint } from "./connectionCollectionState";
import {
  emptyRoutingScheduleDraft,
  routingScheduleDraftFromSchedule,
  type RoutingScheduleDraft,
  type RoutingScheduleDraftError,
} from "./routingScheduleDraft";
import {
  customRequestParametersDraftFromValue,
  type CustomRequestParametersParseError,
} from "./customRequestParameters";

export interface HeaderRow {
  id: string;
  key: string;
  value: string;
  /** True when the value is the redaction sentinel from the backend and must
   *  be rendered as a write-only field; cleared once the operator types a
   *  replacement value. */
  redacted?: boolean;
}

export type ConnectionDialogForm = ConnectionCreate;

let headerRowIdCounter = 0;

export function createHeaderRow(overrides?: Partial<Pick<HeaderRow, "key" | "value" | "redacted">>): HeaderRow {
  headerRowIdCounter += 1;

  return {
    id: `header-row-${headerRowIdCounter}`,
    key: overrides?.key ?? "",
    value: overrides?.value ?? "",
    redacted: overrides?.redacted ?? false,
  };
}

export function createDefaultConnectionForm(
  apiFamily: ApiFamily | null = null,
  openAIMode: OpenAIAcceptedFormat | null = null,
  ownerModelID?: string | null,
): ConnectionDialogForm {
  const resolvedApiFamily = apiFamily ?? "openai";

  return {
    api_family: resolvedApiFamily,
    name: "",
    is_active: true,
    upstream_model_id: ownerModelID?.trim() ? ownerModelID.trim() : undefined,
    custom_headers: null,
    openai_text_capability: resolvedApiFamily === "openai" ? openAIMode : null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
  };
}

export function createEditConnectionForm(
  connection: Connection,
  options?: {
    apiFamily?: ApiFamily | null;
  },
): ConnectionDialogForm {
  const resolvedApiFamily = options?.apiFamily ?? connection.api_family;

  return {
    api_family: resolvedApiFamily,
    endpoint_id: connection.endpoint_id,
    name: connection.name ?? "",
    is_active: connection.is_active,
    upstream_model_id: connection.upstream_model_id ?? undefined,
    custom_headers: connection.custom_headers,
    openai_text_capability:
      resolvedApiFamily === "openai"
        ? (connection.openai_text_capability ?? null)
        : null,
    pricing_template_id: connection.pricing_template_id,
    qps_limit: connection.qps_limit,
    max_in_flight_non_stream: connection.max_in_flight_non_stream,
    max_in_flight_stream: connection.max_in_flight_stream,
  };
}

interface UseModelDetailDialogStateInput {
  apiFamily: ApiFamily | null;
  openAIMode?: OpenAIAcceptedFormat | null;
  globalEndpoints: Endpoint[];
  initialLockedEndpointId?: number | null;
  ownerModelID?: string | null;
}

export function useModelDetailDialogState({
  apiFamily,
  openAIMode = null,
  globalEndpoints,
  initialLockedEndpointId = null,
  ownerModelID = null,
}: UseModelDetailDialogStateInput) {
  const [isEditModelDialogOpen, setIsEditModelDialogOpen] = useState(false);
  const [editRedirectTo, setEditRedirectTo] = useState("");

  const [isConnectionDialogOpen, setIsConnectionDialogOpen] = useState(false);
  const [editingConnection, setEditingConnection] = useState<Connection | null>(null);

  const [createMode, setCreateMode] = useState<"select" | "new">("select");
  const [selectedEndpointId, setSelectedEndpointId] = useState(initialLockedEndpointId != null ? String(initialLockedEndpointId) : "");
  const [newEndpointForm, setNewEndpointForm] = useState<EndpointCreate>(() => ({
    ...createDefaultEndpointForm(),
  }));
  const [connectionFormState, setConnectionFormState] = useState<ConnectionDialogForm>(() => ({
    ...createDefaultConnectionForm(apiFamily, openAIMode),
  }));
  const [headerRows, setHeaderRows] = useState<HeaderRow[]>([]);
  const [customRequestParametersDraft, setCustomRequestParametersDraft] = useState("");
  const [customRequestParametersError, setCustomRequestParametersError] =
    useState<CustomRequestParametersParseError | null>(null);
  const [upstreamModelIdError, setUpstreamModelIdError] = useState<string | null>(null);
  // Kept out of ConnectionDialogForm on purpose: that type is ConnectionCreate,
  // and the shallow merge in setConnectionForm would blank the window array.
  const [routingScheduleDraft, setRoutingScheduleDraft] = useState<RoutingScheduleDraft>(emptyRoutingScheduleDraft);
  const [routingScheduleError, setRoutingScheduleError] = useState<RoutingScheduleDraftError | null>(null);

  const selectedEndpoint = useMemo(
    () => getSelectedEndpoint(globalEndpoints, selectedEndpointId),
    [globalEndpoints, selectedEndpointId],
  );

  const endpointSourceDefaultName = useMemo(() => {
    if (createMode === "select") {
      const selectedName = selectedEndpoint?.name?.trim();
      return selectedName && selectedName.length > 0 ? selectedName : null;
    }

    const inlineEndpointName = newEndpointForm.name.trim();
    return inlineEndpointName.length > 0 ? inlineEndpointName : null;
  }, [createMode, newEndpointForm.name, selectedEndpoint]);

  const resolvedConnectionForm =
    editingConnection === null &&
    connectionFormState.upstream_model_id === undefined &&
    ownerModelID?.trim()
      ? { ...connectionFormState, upstream_model_id: ownerModelID.trim() }
      : connectionFormState;

  const setConnectionForm = (nextForm: ConnectionCreate | ConnectionDialogForm) => {
    setConnectionFormState((currentForm) => ({
      ...currentForm,
      ...nextForm,
    }));
  };

  const openConnectionDialog = (connection?: Connection) => {
    if (connection) {
      setEditingConnection(connection);
      const redactedNames = new Set(connection.custom_headers_redacted ?? []);
      const headers = connection.custom_headers
        ? Object.entries(connection.custom_headers).map(([key, value]) => createHeaderRow({ key, value, redacted: redactedNames.has(key) }))
        : [];
      setHeaderRows(headers);
      setCustomRequestParametersDraft(customRequestParametersDraftFromValue(connection.custom_request_parameters));
      setCustomRequestParametersError(null);
      setUpstreamModelIdError(null);
      setRoutingScheduleDraft(routingScheduleDraftFromSchedule(connection.routing_schedule));
      setRoutingScheduleError(null);
      setConnectionFormState(
        createEditConnectionForm(connection, {
          apiFamily: apiFamily ?? connection.api_family,
        }),
      );
      setNewEndpointForm({ ...createDefaultEndpointForm() });
      setCreateMode("select");
      setSelectedEndpointId(String(connection.endpoint_id));
    } else {
      setEditingConnection(null);
      setHeaderRows([]);
      setCustomRequestParametersDraft("");
      setCustomRequestParametersError(null);
      setUpstreamModelIdError(null);
      setRoutingScheduleDraft(emptyRoutingScheduleDraft());
      setRoutingScheduleError(null);
      setConnectionFormState({ ...createDefaultConnectionForm(apiFamily, openAIMode, ownerModelID) });
      setNewEndpointForm({ ...createDefaultEndpointForm() });
      setCreateMode("select");
      setSelectedEndpointId("");
    }

    setIsConnectionDialogOpen(true);
  };

  return {
    isEditModelDialogOpen,
    setIsEditModelDialogOpen,
    editRedirectTo,
    setEditRedirectTo,
    isConnectionDialogOpen,
    setIsConnectionDialogOpen,
    editingConnection,
    lockedEndpointId: initialLockedEndpointId,
    createMode,
    setCreateMode,
    selectedEndpointId,
    setSelectedEndpointId,
    newEndpointForm,
    setNewEndpointForm,
    connectionForm: resolvedConnectionForm,
    setConnectionForm,
    headerRows,
    setHeaderRows,
    customRequestParametersDraft,
    setCustomRequestParametersDraft,
    customRequestParametersError,
    setCustomRequestParametersError,
    upstreamModelIdError,
    setUpstreamModelIdError,
    routingScheduleDraft,
    setRoutingScheduleDraft,
    routingScheduleError,
    setRoutingScheduleError,
    selectedEndpoint,
    endpointSourceDefaultName,
    openConnectionDialog,
  };
}
