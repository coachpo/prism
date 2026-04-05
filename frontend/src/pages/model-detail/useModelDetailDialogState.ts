import { useMemo, useState } from "react";
import type { Connection, ConnectionCreate, Endpoint, EndpointCreate } from "@/lib/types";
import { createDefaultConnectionForm, createDefaultEndpointForm, getSelectedEndpoint } from "./useModelDetailDataSupport";

export interface HeaderRow {
  id: string;
  key: string;
  value: string;
}

let headerRowIdCounter = 0;

export function createHeaderRow(overrides?: Partial<Pick<HeaderRow, "key" | "value">>): HeaderRow {
  headerRowIdCounter += 1;

  return {
    id: `header-row-${headerRowIdCounter}`,
    key: overrides?.key ?? "",
    value: overrides?.value ?? "",
  };
}

interface UseModelDetailDialogStateInput {
  globalEndpoints: Endpoint[];
}

export function useModelDetailDialogState({ globalEndpoints }: UseModelDetailDialogStateInput) {
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
  const [connectionForm, setConnectionForm] = useState<ConnectionCreate>(() => ({
    ...createDefaultConnectionForm(),
  }));
  const [headerRows, setHeaderRows] = useState<HeaderRow[]>([]);

  const selectedEndpoint = useMemo(
    () => getSelectedEndpoint(globalEndpoints, selectedEndpointId),
    [globalEndpoints, selectedEndpointId]
  );

  const endpointSourceDefaultName = useMemo(() => {
    if (createMode === "select") {
      const selectedName = selectedEndpoint?.name?.trim();
      return selectedName && selectedName.length > 0 ? selectedName : null;
    }

    const inlineEndpointName = newEndpointForm.name.trim();
    return inlineEndpointName.length > 0 ? inlineEndpointName : null;
  }, [createMode, newEndpointForm.name, selectedEndpoint]);

  const openConnectionDialog = (connection?: Connection) => {
    if (connection) {
      setEditingConnection(connection);
      const headers = connection.custom_headers
        ? Object.entries(connection.custom_headers).map(([key, value]) => createHeaderRow({ key, value }))
        : [];
      setHeaderRows(headers);
      setConnectionForm({
        endpoint_id: connection.endpoint_id,
        name: connection.name ?? "",
        is_active: connection.is_active,
        custom_headers: connection.custom_headers,
        pricing_template_id: connection.pricing_template_id,
        qps_limit: connection.qps_limit,
        max_in_flight_non_stream: connection.max_in_flight_non_stream,
        max_in_flight_stream: connection.max_in_flight_stream,
      });
      setNewEndpointForm({ ...createDefaultEndpointForm() });
      setCreateMode("select");
      setSelectedEndpointId(String(connection.endpoint_id));
    } else {
      setEditingConnection(null);
      setHeaderRows([]);
      setConnectionForm({ ...createDefaultConnectionForm() });
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
    connectionForm,
    setConnectionForm,
    headerRows,
    setHeaderRows,
    selectedEndpoint,
    endpointSourceDefaultName,
    openConnectionDialog,
  };
}
