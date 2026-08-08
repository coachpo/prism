import { useMemo, useState } from "react";
import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  Endpoint,
  EndpointCreate,
  OpenAIAcceptedFormat,
} from "@/lib/types";
import {
  createDefaultEndpointForm,
  getSelectedEndpoint,
  normalizeOpenAITextCapability,
} from "./useModelDetailDataSupport";

export interface HeaderRow {
  id: string;
  key: string;
  value: string;
}

export type ConnectionDialogForm = ConnectionCreate;

let headerRowIdCounter = 0;

export function createHeaderRow(overrides?: Partial<Pick<HeaderRow, "key" | "value">>): HeaderRow {
  headerRowIdCounter += 1;

  return {
    id: `header-row-${headerRowIdCounter}`,
    key: overrides?.key ?? "",
    value: overrides?.value ?? "",
  };
}

export function createDefaultConnectionForm(
  apiFamily: ApiFamily | null = null,
  openAIMode: OpenAIAcceptedFormat | null = null,
): ConnectionDialogForm {
  const resolvedApiFamily = apiFamily ?? "openai";

  return {
    api_family: resolvedApiFamily,
    name: "",
    is_active: true,
    custom_headers: null,
    openai_text_capability:
      resolvedApiFamily === "openai" ? normalizeOpenAITextCapability(openAIMode) : null,
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
    custom_headers: connection.custom_headers,
    openai_text_capability:
      resolvedApiFamily === "openai"
        ? normalizeOpenAITextCapability(connection.openai_text_capability)
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
}

export function useModelDetailDialogState({
  apiFamily,
  openAIMode = null,
  globalEndpoints,
}: UseModelDetailDialogStateInput) {
  const [isEditModelDialogOpen, setIsEditModelDialogOpen] = useState(false);
  const [editRedirectTo, setEditRedirectTo] = useState("");

  const [isConnectionDialogOpen, setIsConnectionDialogOpen] = useState(false);
  const [editingConnection, setEditingConnection] = useState<Connection | null>(null);

  const [createMode, setCreateMode] = useState<"select" | "new">("select");
  const [selectedEndpointId, setSelectedEndpointId] = useState("");
  const [newEndpointForm, setNewEndpointForm] = useState<EndpointCreate>(() => ({
    ...createDefaultEndpointForm(),
  }));
  const [connectionFormState, setConnectionFormState] = useState<ConnectionDialogForm>(() => ({
    ...createDefaultConnectionForm(apiFamily, openAIMode),
  }));
  const [headerRows, setHeaderRows] = useState<HeaderRow[]>([]);

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

  const setConnectionForm = (nextForm: ConnectionCreate | ConnectionDialogForm) => {
    setConnectionFormState((currentForm) => ({
      ...currentForm,
      ...nextForm,
    }));
  };

  const openConnectionDialog = (connection?: Connection) => {
    if (connection) {
      setEditingConnection(connection);
      const headers = connection.custom_headers
        ? Object.entries(connection.custom_headers).map(([key, value]) => createHeaderRow({ key, value }))
        : [];
      setHeaderRows(headers);
      setConnectionFormState(
        createEditConnectionForm(connection, {
          apiFamily: apiFamily ?? connection.api_family,
        }),
      );      setNewEndpointForm({ ...createDefaultEndpointForm() });
      setCreateMode("select");
      setSelectedEndpointId(String(connection.endpoint_id));
    } else {
      setEditingConnection(null);
      setHeaderRows([]);
      setConnectionFormState({ ...createDefaultConnectionForm(apiFamily, openAIMode) });
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
    createMode,
    setCreateMode,
    selectedEndpointId,
    setSelectedEndpointId,
    newEndpointForm,
    setNewEndpointForm,
    connectionForm: connectionFormState,
    setConnectionForm,
    headerRows,
    setHeaderRows,
    selectedEndpoint,
    endpointSourceDefaultName,
    openConnectionDialog,
  };
}
