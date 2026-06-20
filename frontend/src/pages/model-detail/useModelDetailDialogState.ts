import { useMemo, useState } from "react";
import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  ContextCapabilityFields,
  ContextCapabilityOverrides,
  Endpoint,
  EndpointCreate,
} from "@/lib/types";
import {
  createDefaultEndpointForm,
  getSelectedEndpoint,
  normalizeOpenAITextCapability,
} from "./useModelDetailDataSupport";
import { normalizeOpenAIProbeEndpointVariant } from "./connectionProbeBehavior";

export interface HeaderRow {
  id: string;
  key: string;
  value: string;
}

export type ConnectionCapabilityFieldName = keyof ContextCapabilityOverrides;

export interface ConnectionCapabilityDraft {
  mode: "default" | "override";
  value: string;
}

export interface ConnectionDialogForm
  extends Omit<ConnectionCreate, ConnectionCapabilityFieldName> {
  context_capability_drafts: Record<ConnectionCapabilityFieldName, ConnectionCapabilityDraft>;
}

export type TerminalTargetCapabilityDefaults = Pick<ContextCapabilityFields, ConnectionCapabilityFieldName>;

type ConnectionCapabilityDefaultValues = Partial<Record<ConnectionCapabilityFieldName, number | null>>;

const CONNECTION_CAPABILITY_FIELDS: ConnectionCapabilityFieldName[] = [
  "context_window_tokens",
  "default_output_token_reserve",
  "max_context_utilization",
  "preferred_context_utilization_threshold",
];

let headerRowIdCounter = 0;

export function createHeaderRow(overrides?: Partial<Pick<HeaderRow, "key" | "value">>): HeaderRow {
  headerRowIdCounter += 1;

  return {
    id: `header-row-${headerRowIdCounter}`,
    key: overrides?.key ?? "",
    value: overrides?.value ?? "",
  };
}

function stringifyCapabilityDraftValue(value: number | null | undefined): string {
  return value == null ? "" : String(value);
}

function createConnectionCapabilityDraft(
  overrideValue: number | null | undefined,
  defaultValue: number | null | undefined,
): ConnectionCapabilityDraft {
  if (typeof overrideValue === "number") {
    return {
      mode: "override",
      value: stringifyCapabilityDraftValue(overrideValue),
    };
  }

  return {
    mode: "default",
    value: stringifyCapabilityDraftValue(defaultValue),
  };
}

function createConnectionCapabilityDrafts(
  overrides?: ContextCapabilityOverrides,
  defaultValues?: ConnectionCapabilityDefaultValues,
): Record<ConnectionCapabilityFieldName, ConnectionCapabilityDraft> {
  return {
    context_window_tokens: createConnectionCapabilityDraft(
      overrides?.context_window_tokens,
      defaultValues?.context_window_tokens,
    ),
    default_output_token_reserve: createConnectionCapabilityDraft(
      overrides?.default_output_token_reserve,
      defaultValues?.default_output_token_reserve,
    ),
    max_context_utilization: createConnectionCapabilityDraft(
      overrides?.max_context_utilization,
      defaultValues?.max_context_utilization,
    ),
    preferred_context_utilization_threshold: createConnectionCapabilityDraft(
      overrides?.preferred_context_utilization_threshold,
      defaultValues?.preferred_context_utilization_threshold,
    ),
  };
}

export function createDefaultConnectionForm(
  apiFamily: ApiFamily | null = null,
  terminalTargetCapabilityDefaults?: Partial<TerminalTargetCapabilityDefaults>,
): ConnectionDialogForm {
  const resolvedApiFamily = apiFamily ?? "openai";

  return {
    api_family: resolvedApiFamily,
    name: "",
    is_active: true,
    custom_headers: null,
    openai_text_capability:
      resolvedApiFamily === "openai" ? normalizeOpenAITextCapability(undefined) : null,
    openai_probe_endpoint_variant:
      resolvedApiFamily === "openai" ? normalizeOpenAIProbeEndpointVariant(undefined) : null,
    pricing_template_id: null,
    qps_limit: null,
    max_in_flight_non_stream: null,
    max_in_flight_stream: null,
    context_capability_drafts: createConnectionCapabilityDrafts(undefined, terminalTargetCapabilityDefaults),
  };
}

export function createEditConnectionForm(
  connection: Connection,
  options?: {
    apiFamily?: ApiFamily | null;
    terminalTargetCapabilityDefaults?: Partial<TerminalTargetCapabilityDefaults>;
  },
): ConnectionDialogForm {
  const defaultValues = CONNECTION_CAPABILITY_FIELDS.reduce<ConnectionCapabilityDefaultValues>(
    (drafts, field) => {
      drafts[field] = options?.terminalTargetCapabilityDefaults?.[field] ?? connection[field];
      return drafts;
    },
    {},
  );

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
    openai_probe_endpoint_variant:
      resolvedApiFamily === "openai"
        ? normalizeOpenAIProbeEndpointVariant(connection.openai_probe_endpoint_variant)
        : null,
    pricing_template_id: connection.pricing_template_id,
    qps_limit: connection.qps_limit,
    max_in_flight_non_stream: connection.max_in_flight_non_stream,
    max_in_flight_stream: connection.max_in_flight_stream,
    context_capability_drafts: createConnectionCapabilityDrafts(
      connection.context_capability_overrides,
      defaultValues,
    ),
  };
}

interface UseModelDetailDialogStateInput {
  apiFamily: ApiFamily | null;
  globalEndpoints: Endpoint[];
  terminalTargetCapabilityDefaults?: Partial<TerminalTargetCapabilityDefaults>;
}

export function useModelDetailDialogState({
  apiFamily,
  globalEndpoints,
  terminalTargetCapabilityDefaults,
}: UseModelDetailDialogStateInput) {
  const [isEditModelDialogOpen, setIsEditModelDialogOpen] = useState(false);
  const [editRedirectTo, setEditRedirectTo] = useState("");

  const [isConnectionDialogOpen, setIsConnectionDialogOpen] = useState(false);
  const [editingConnection, setEditingConnection] = useState<Connection | null>(null);
  const [dialogTestingConnection, setDialogTestingConnection] = useState(false);
  const [dialogTestResult, setDialogTestResult] = useState<{
    status: string;
    detail: string;
  } | null>(null);

  const [createMode, setCreateMode] = useState<"select" | "new">("select");
  const [selectedEndpointId, setSelectedEndpointId] = useState("");
  const [newEndpointForm, setNewEndpointForm] = useState<EndpointCreate>(() => ({
    ...createDefaultEndpointForm(),
  }));
  const [connectionFormState, setConnectionFormState] = useState<ConnectionDialogForm>(() => ({
    ...createDefaultConnectionForm(apiFamily, terminalTargetCapabilityDefaults),
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
      context_capability_drafts:
        "context_capability_drafts" in nextForm && nextForm.context_capability_drafts
          ? nextForm.context_capability_drafts
          : currentForm.context_capability_drafts,
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
          terminalTargetCapabilityDefaults,
        }),
      );
      setNewEndpointForm({ ...createDefaultEndpointForm() });
      setCreateMode("select");
      setSelectedEndpointId(String(connection.endpoint_id));
    } else {
      setEditingConnection(null);
      setHeaderRows([]);
      setConnectionFormState({ ...createDefaultConnectionForm(apiFamily, terminalTargetCapabilityDefaults) });
      setNewEndpointForm({ ...createDefaultEndpointForm() });
      setCreateMode("select");
      setSelectedEndpointId("");
    }

    setDialogTestResult(null);
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
    dialogTestingConnection,
    setDialogTestingConnection,
    dialogTestResult,
    setDialogTestResult,
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
