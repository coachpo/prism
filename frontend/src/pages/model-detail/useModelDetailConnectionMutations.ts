import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type {
  Connection,
  ConnectionCreate,
  Endpoint,
  EndpointCreate,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
} from "@/lib/types";
import type { HeaderRow } from "./useModelDetailDialogState";
import {
  buildConnectionDraftPayload,
  patchModelListConnectionCounts,
  hydrateConnectionPricingTemplate,
  removeConnectionFromList,
  upsertConnectionInList,
  upsertEndpointInList,
} from "./useModelDetailDataSupport";

interface UseModelDetailConnectionMutationsInput {
  id: string | undefined;
  revision: number;
  createMode: "select" | "new";
  selectedEndpointId: string;
  newEndpointForm: EndpointCreate;
  connectionForm: ConnectionCreate;
  headerRows: HeaderRow[];
  editingConnection: Connection | null;
  pricingTemplates: PricingTemplate[];
  endpointSourceDefaultName: string | null;
  refreshCurrentState: () => void | Promise<void>;
  setIsConnectionDialogOpen: (open: boolean) => void;
  setAllModels: React.Dispatch<React.SetStateAction<ModelConfigListItem[]>>;
  setConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setGlobalEndpoints: React.Dispatch<React.SetStateAction<Endpoint[]>>;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
}

export function useModelDetailConnectionMutations({
  id,
  revision,
  createMode,
  selectedEndpointId,
  newEndpointForm,
  connectionForm,
  headerRows,
  editingConnection,
  pricingTemplates,
  endpointSourceDefaultName,
  refreshCurrentState,
  setIsConnectionDialogOpen,
  setAllModels,
  setConnections,
  setGlobalEndpoints,
  setModel,
}: UseModelDetailConnectionMutationsInput) {
  const modelConfigId = id ? Number.parseInt(id, 10) : NaN;

  const commitConnections = useCallback(
    (updater: (current: Connection[]) => Connection[]) => {
      setConnections((current) => {
        const next = updater(current);
        setModel((previousModel) => (
          previousModel ? { ...previousModel, connections: next } : previousModel
        ));
        if (Number.isFinite(modelConfigId)) {
          setAllModels((currentModels) => patchModelListConnectionCounts(currentModels, modelConfigId, next));
        }
        return next;
      });
    },
    [modelConfigId, setAllModels, setConnections, setModel],
  );

  const handleConnectionSubmit = useCallback(
    async (event: React.FormEvent) => {
      event.preventDefault();

      if (!id) {
        return;
      }

      const { errorMessage, payload } = buildConnectionDraftPayload({
        createMode,
        selectedEndpointId,
        newEndpointForm,
        connectionForm,
        headerRows,
        editingConnection,
        endpointSourceDefaultName,
      });

      if (!payload) {
        if (errorMessage) {
          toast.error(errorMessage);
        }
        return;
      }

      try {
        const savedConnection = editingConnection
          ? await api.connections.update(editingConnection.id, { ...payload })
          : await api.connections.create(Number.parseInt(id, 10), payload);
        const committedConnection = hydrateConnectionPricingTemplate(savedConnection, pricingTemplates);

        if (editingConnection) {
          toast.success(getStaticMessages().modelDetailData.connectionUpdated);
        } else {
          toast.success(getStaticMessages().modelDetailData.connectionCreated);
        }

        clearSharedReferenceData(undefined, revision);
        setGlobalEndpoints((current) => upsertEndpointInList(current, committedConnection.endpoint));
        commitConnections((current) => upsertConnectionInList(current, committedConnection));
        setIsConnectionDialogOpen(false);
        void refreshCurrentState();
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.saveConnectionFailed);
      }
    },
    [
      connectionForm,
      createMode,
      editingConnection,
      endpointSourceDefaultName,
      headerRows,
      id,
      newEndpointForm,
      pricingTemplates,
      refreshCurrentState,
      revision,
      selectedEndpointId,
      commitConnections,
      setGlobalEndpoints,
      setIsConnectionDialogOpen,
    ],
  );

  const handleDeleteConnection = useCallback(
    async (connectionId: number) => {
      try {
        await api.connections.delete(connectionId);
        clearSharedReferenceData(undefined, revision);
        commitConnections((current) => removeConnectionFromList(current, connectionId));
        void refreshCurrentState();
        toast.success(getStaticMessages().modelDetailData.connectionDeleted);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.deleteConnectionFailed);
      }
    },
    [commitConnections, refreshCurrentState, revision],
  );

  const handleToggleActive = useCallback(
    async (connection: Connection) => {
      try {
        const updatedConnection = await api.connections.update(connection.id, {
          is_active: !connection.is_active,
        });
        const committedConnection = hydrateConnectionPricingTemplate(updatedConnection, pricingTemplates);
        clearSharedReferenceData(undefined, revision);
        setGlobalEndpoints((current) => upsertEndpointInList(current, committedConnection.endpoint));
        commitConnections((current) => upsertConnectionInList(current, committedConnection));
        void refreshCurrentState();
      } catch {
        toast.error(getStaticMessages().modelDetailData.toggleConnectionFailed);
      }
    },
    [commitConnections, pricingTemplates, refreshCurrentState, revision, setGlobalEndpoints],
  );

  return {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
  };
}
