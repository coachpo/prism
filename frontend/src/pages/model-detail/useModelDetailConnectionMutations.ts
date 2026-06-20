import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type {
  ApiFamily,
  Connection,
  Endpoint,
  EndpointCreate,
  ModelAccessTarget,
  ModelAccessTargetMutation,
  ModelConfig,
  ModelConfigListItem,
  PricingTemplate,
} from "@/lib/types";
import {
  getTerminalTargetId,
  isTerminalTargetAccessTargetType,
} from "@/lib/types";
import type { ConnectionDialogForm, HeaderRow } from "./useModelDetailDialogState";
import {
  buildConnectionDraftPayload,
  connectionBelongsToModel,
  getOwnedConnectionTarget,
  getOwnedModelConnections,
  hydrateConnectionPricingTemplate,
  isOwnedConnectionTarget,
  patchModelListItemFromDetail,
  removeConnectionFromList,
  upsertConnectionInList,
  upsertEndpointInList,
} from "./useModelDetailDataSupport";

type ConnectionSubmitEvent = Pick<Event, "preventDefault">;

const TERMINAL_TARGET_OWNER_MISMATCH = "Terminal Target owner does not match the current model";
const TERMINAL_TARGETS_MANAGED_FROM_MODEL_DETAIL = "Manage Terminal Targets from the Model Detail Terminal Targets list.";

interface UseModelDetailConnectionMutationsInput {
  id: string | undefined;
  revision: number;
  model: ModelConfig | null;
  modelApiFamily: ApiFamily | null;
  createMode: "select" | "new";
  selectedEndpointId: string;
  newEndpointForm: EndpointCreate;
  connectionForm: ConnectionDialogForm;
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
        setConnections(getOwnedModelConnections(nextModel, modelConfigId));
        setAllModels((currentModels) => patchModelListItemFromDetail(currentModels, nextModel));
        return nextModel;
      });
      clearSharedReferenceData(undefined, revision);
      void refreshCurrentState();
    },
    [modelConfigId, refreshCurrentState, revision, setAllModels, setConnections, setModel],
  );

  const commitConnection = useCallback(
    (connection: Connection) => {
      if (!connectionBelongsToModel(connection, modelConfigId)) {
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
        return null;
      }

      const committedConnection = hydrateConnectionPricingTemplate(connection, pricingTemplates);
      setAllConnections((current) => upsertConnectionInList(current, committedConnection));
      setConnections((current) => upsertConnectionInList(current, committedConnection));
      setGlobalEndpoints((current) => upsertEndpointInList(current, committedConnection.endpoint));
      return committedConnection;
    },
    [modelConfigId, pricingTemplates, setAllConnections, setConnections, setGlobalEndpoints],
  );

  const handleConnectionSubmit = useCallback(
    async (event: ConnectionSubmitEvent) => {
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
          if (!isOwnedConnectionTarget(model, modelConfigId, editingConnection.id)) {
            toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
            return;
          }
          const updatedConnection = await api.models.connections.update(modelConfigId, editingConnection.id, { ...payload });
          if (!commitConnection(updatedConnection)) return;
          toast.success(getStaticMessages().modelDetailData.connectionUpdated);
        } else {
          const createdConnection = await api.models.connections.create(modelConfigId, payload);
          if (!commitConnection(createdConnection)) return;
          const targets = await api.models.targets.list(modelConfigId);
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
      model,
      commitConnection,
      applyTargets,
      revision,
      setIsConnectionDialogOpen,
    ],
  );

  const handleAddAccessTarget = useCallback(
    async (target: ModelAccessTargetMutation) => {
      if (!Number.isFinite(modelConfigId)) return;
      if (target.target_type !== "model") {
        toast.error(TERMINAL_TARGETS_MANAGED_FROM_MODEL_DETAIL);
        return;
      }
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
      const target = model?.access_targets[index] ?? null;
      if (!target) return;
      if (isTerminalTargetAccessTargetType(target.target_type)
        && !getOwnedConnectionTarget(model, modelConfigId, getTerminalTargetId(target) ?? -1)) {
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
        return;
      }
      try {
        const targets = await api.models.targets.movePosition(modelConfigId, target.id, toIndex);
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
      if (isTerminalTargetAccessTargetType(target.target_type)
        && !getOwnedConnectionTarget(model, modelConfigId, getTerminalTargetId(target) ?? -1)) {
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
        return;
      }
      try {
        const targets = await api.models.targets.update(modelConfigId, target.id, { is_enabled: enabled });
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
      const target = model?.access_targets[index] ?? null;
      if (!target) return;
      if (isTerminalTargetAccessTargetType(target.target_type)
        && !getOwnedConnectionTarget(model, modelConfigId, getTerminalTargetId(target) ?? -1)) {
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
        return;
      }
      try {
        const targets = await api.models.targets.delete(modelConfigId, target.id);
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
        if (!Number.isFinite(modelConfigId)) return;
        if (!isOwnedConnectionTarget(model, modelConfigId, connectionId)) {
          toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
          return;
        }
        await api.models.connections.delete(modelConfigId, connectionId);
        clearSharedReferenceData(undefined, revision);
        setAllConnections((current) => removeConnectionFromList(current, connectionId));
        setConnections((current) => removeConnectionFromList(current, connectionId));
        const targets = await api.models.targets.list(modelConfigId);
        applyTargets(targets);
        void refreshCurrentState();
        toast.success(getStaticMessages().modelDetailData.connectionDeleted);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.deleteConnectionFailed);
      }
    },
    [applyTargets, model, modelConfigId, refreshCurrentState, revision, setAllConnections, setConnections],
  );

  const handleToggleActive = useCallback(
    async (connection: Connection) => {
      try {
        if (!Number.isFinite(modelConfigId)) return;
        if (!isOwnedConnectionTarget(model, modelConfigId, connection.id)) {
          toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
          return;
        }
        const updatedConnection = await api.models.connections.update(modelConfigId, connection.id, { is_active: !connection.is_active });
        if (!commitConnection(updatedConnection)) return;
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
      } catch {
        toast.error(getStaticMessages().modelDetailData.toggleConnectionFailed);
      }
    },
    [commitConnection, model, modelConfigId, refreshCurrentState, revision],
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
