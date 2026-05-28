import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type {
  ApiFamily,
  Connection,
  ConnectionCreate,
  Endpoint,
  EndpointCreate,
  ModelAccessTarget,
  ModelAccessTargetMutation,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
} from "@/lib/types";
import { accessTargetToMutation, getModelConnections } from "../models/modelFormState";
import type { HeaderRow } from "./useModelDetailDialogState";
import {
  buildConnectionDraftPayload,
  hydrateConnectionPricingTemplate,
  patchModelListItemFromDetail,
  removeConnectionFromList,
  upsertConnectionInList,
  upsertEndpointInList,
} from "./useModelDetailDataSupport";

interface UseModelDetailConnectionMutationsInput {
  id: string | undefined;
  revision: number;
  model: ModelConfig | null;
  modelApiFamily: ApiFamily | null;
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
  setAllConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
  setGlobalEndpoints: React.Dispatch<React.SetStateAction<Endpoint[]>>;
}

export function useModelDetailConnectionMutations({
  id,
  revision,
  model,
  modelApiFamily,
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
  setAllConnections,
  setModel,
  setGlobalEndpoints,
}: UseModelDetailConnectionMutationsInput) {
  const modelConfigId = id ? Number.parseInt(id, 10) : NaN;

  const applyTargets = useCallback(
    (targets: ModelAccessTarget[]) => {
      setModel((currentModel) => {
        if (!currentModel) return currentModel;
        const nextModel = { ...currentModel, access_targets: targets };
        setConnections(getModelConnections(nextModel));
        setAllModels((currentModels) => patchModelListItemFromDetail(currentModels, nextModel));
        return nextModel;
      });
      clearSharedReferenceData(undefined, revision);
      void refreshCurrentState();
    },
    [refreshCurrentState, revision, setAllModels, setConnections, setModel],
  );

  const commitConnection = useCallback(
    (connection: Connection) => {
      const committedConnection = hydrateConnectionPricingTemplate(connection, pricingTemplates);
      setAllConnections((current) => upsertConnectionInList(current, committedConnection));
      setConnections((current) => upsertConnectionInList(current, committedConnection));
      setGlobalEndpoints((current) => upsertEndpointInList(current, committedConnection.endpoint));
      return committedConnection;
    },
    [pricingTemplates, setAllConnections, setConnections, setGlobalEndpoints],
  );

  const handleConnectionSubmit = useCallback(
    async (event: React.FormEvent) => {
      event.preventDefault();
      if (!id || !Number.isFinite(modelConfigId)) return;

      const { errorMessage, payload } = buildConnectionDraftPayload({
        apiFamily: modelApiFamily,
        createMode,
        selectedEndpointId,
        newEndpointForm,
        connectionForm,
        headerRows,
        editingConnection,
        endpointSourceDefaultName,
      });

      if (!payload) {
        if (errorMessage) toast.error(errorMessage);
        return;
      }

      try {
        if (editingConnection) {
          const updatedConnection = await api.connections.update(editingConnection.id, { ...payload });
          commitConnection(updatedConnection);
          toast.success(getStaticMessages().modelDetailData.connectionUpdated);
        } else {
          const createdConnection = await api.connections.create(payload);
          commitConnection(createdConnection);
          const targets = await api.models.targets.create(modelConfigId, {
            target_type: "connection",
            connection_id: createdConnection.id,
            position: 0,
            is_enabled: true,
          });
          applyTargets(targets);
          toast.success(getStaticMessages().modelDetailData.connectionCreated);
        }
        clearSharedReferenceData(undefined, revision);
        setIsConnectionDialogOpen(false);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.saveConnectionFailed);
      }
    },
    [
      id,
      modelConfigId,
      modelApiFamily,
      createMode,
      selectedEndpointId,
      newEndpointForm,
      connectionForm,
      headerRows,
      editingConnection,
      endpointSourceDefaultName,
      commitConnection,
      applyTargets,
      revision,
      setIsConnectionDialogOpen,
    ],
  );

  const handleAddAccessTarget = useCallback(
    async (target: ModelAccessTargetMutation) => {
      if (!Number.isFinite(modelConfigId)) return;
      try {
        const targets = await api.models.targets.create(modelConfigId, target);
        applyTargets(targets);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to add access target");
        throw error;
      }
    },
    [applyTargets, modelConfigId],
  );

  const handleMoveAccessTarget = useCallback(
    async (index: number, toIndex: number) => {
      if (!Number.isFinite(modelConfigId)) return;
      const targetId = model?.access_targets[index]?.id ?? null;
      if (!targetId) return;
      try {
        const targets = await api.models.targets.movePosition(modelConfigId, targetId, toIndex);
        applyTargets(targets);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to reorder access target");
        throw error;
      }
    },
    [applyTargets, model, modelConfigId],
  );

  const handleToggleAccessTarget = useCallback(
    async (index: number, enabled: boolean) => {
      if (!Number.isFinite(modelConfigId)) return;
      const target = model?.access_targets[index] ?? null;
      if (!target) return;
      const mutation = accessTargetToMutation(target);
      if (!mutation) return;
      try {
        const targets = await api.models.targets.update(modelConfigId, target.id, { ...mutation, is_enabled: enabled });
        applyTargets(targets);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to update access target");
        throw error;
      }
    },
    [applyTargets, model, modelConfigId],
  );

  const handleDeleteAccessTarget = useCallback(
    async (index: number) => {
      if (!Number.isFinite(modelConfigId)) return;
      const targetId = model?.access_targets[index]?.id ?? null;
      if (!targetId) return;
      try {
        const targets = await api.models.targets.delete(modelConfigId, targetId);
        applyTargets(targets);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to remove access target");
        throw error;
      }
    },
    [applyTargets, model, modelConfigId],
  );

  const handleDeleteConnection = useCallback(
    async (connectionId: number) => {
      try {
        await api.connections.delete(connectionId);
        clearSharedReferenceData(undefined, revision);
        setAllConnections((current) => removeConnectionFromList(current, connectionId));
        setConnections((current) => removeConnectionFromList(current, connectionId));
        void refreshCurrentState();
        toast.success(getStaticMessages().modelDetailData.connectionDeleted);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.deleteConnectionFailed);
      }
    },
    [refreshCurrentState, revision, setAllConnections, setConnections],
  );

  const handleToggleActive = useCallback(
    async (connection: Connection) => {
      try {
        const updatedConnection = await api.connections.update(connection.id, { is_active: !connection.is_active });
        commitConnection(updatedConnection);
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
      } catch {
        toast.error(getStaticMessages().modelDetailData.toggleConnectionFailed);
      }
    },
    [commitConnection, refreshCurrentState, revision],
  );

  return {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleDeleteAccessTarget,
  };
}
