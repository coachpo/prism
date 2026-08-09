import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { ApiError } from "@/lib/api/core";
import { getStaticMessages } from "@/i18n/staticMessages";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type {
  ApiFamily,
  Connection,
  OpenAITextCapability,
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
import { parseCustomRequestParametersDraft, type CustomRequestParametersParseError } from "./customRequestParameters";
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
  customRequestParametersDraft: string;
  setCustomRequestParametersError: (error: CustomRequestParametersParseError | null) => void;
  editingConnection: Connection | null;
  pricingTemplates: PricingTemplate[];
  endpointSourceDefaultName: string | null;
  refreshCurrentState: () => void | Promise<void>;
  refreshDiagnostics?: () => void | Promise<void>;
  setIsConnectionDialogOpen: (open: boolean) => void;
  setAllModels: React.Dispatch<React.SetStateAction<ModelConfigListItem[]>>;
  setConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setAllConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
  setGlobalEndpoints: React.Dispatch<React.SetStateAction<Endpoint[]>>;
}

function isCustomRequestParametersValidationError(error: unknown): error is ApiError {
  if (!(error instanceof ApiError) || error.status !== 422) return false;
  const detail = error.detail;
  return Boolean(detail && typeof detail === "object" && (detail as { detail?: unknown }).detail === "Invalid custom request parameters");
}

function customRequestParametersErrorFromServerBody(body: Record<string, unknown>): CustomRequestParametersParseError {
  const reason = typeof body.reason === "string" ? body.reason as CustomRequestParametersParseError["reason"] : "not_object";
  return {
    reason,
    path: typeof body.path === "string" ? body.path : "custom_request_parameters",
    limit: typeof body.limit === "number" ? body.limit : undefined,
  };
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
  customRequestParametersDraft,
  setCustomRequestParametersError,
  editingConnection,
  pricingTemplates,
  endpointSourceDefaultName,
  refreshCurrentState,
  refreshDiagnostics,
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
      void refreshDiagnostics?.();
    },
    [modelConfigId, refreshCurrentState, refreshDiagnostics, revision, setAllModels, setConnections, setModel],
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

      const parsedCustomRequestParameters = parseCustomRequestParametersDraft(customRequestParametersDraft);
      if (parsedCustomRequestParameters.error) {
        setCustomRequestParametersError(parsedCustomRequestParameters.error);
        return;
      }
      setCustomRequestParametersError(null);

      const { errorMessage, payload } = buildConnectionDraftPayload({
        apiFamily: modelApiFamily,
        createMode,
        selectedEndpointId,
        newEndpointForm,
        connectionForm,
        headerRows,
        customRequestParametersValue: parsedCustomRequestParameters.value,
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
          const updatedResponse = await api.models.connections.update(modelConfigId, editingConnection.id, { ...payload });
          if (!commitConnection(updatedResponse.connection)) return;
          toast.success(getStaticMessages().modelDetailData.connectionUpdated);
        } else {
          const createdResponse = await api.models.connections.create(modelConfigId, payload);
          if (!commitConnection(createdResponse.connection)) return;
          const targets = await api.models.targets.list(modelConfigId);
          applyTargets(targets);
          toast.success(getStaticMessages().modelDetailData.connectionCreated);
        }
        clearSharedReferenceData(undefined, revision);
        setIsConnectionDialogOpen(false);
      } catch (error) {
        if (isCustomRequestParametersValidationError(error)) {
          setCustomRequestParametersError(customRequestParametersErrorFromServerBody(error.detail as Record<string, unknown>));
          return;
        }
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
      customRequestParametersDraft,
      setCustomRequestParametersError,
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
        const response = await api.models.targets.create(modelConfigId, target);
        applyTargets(response.access_targets);
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
        const response = await api.models.targets.movePosition(modelConfigId, target.id, toIndex);
        applyTargets(response.access_targets);
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
        const response = await api.models.targets.update(modelConfigId, target.id, { is_enabled: enabled });
        applyTargets(response.access_targets);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to update access target");
        throw error;
      }
    },
    [applyTargets, model, modelConfigId],
  );

  const handleQuickCapabilityChange = useCallback(
    async (connection: Connection, capability: OpenAITextCapability) => {
      if (!Number.isFinite(modelConfigId)) return
      if (!isOwnedConnectionTarget(model, modelConfigId, connection.id)) return
      try {
        const response = await api.models.connections.update(modelConfigId, connection.id, { openai_text_capability: capability });
        if (!commitConnection(response.connection)) return;
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
        void refreshDiagnostics?.();
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to update capability");
      }
    },
    [commitConnection, model, modelConfigId, refreshCurrentState, refreshDiagnostics, revision],
  );

  const handleQuickPricingChange = useCallback(
    async (connection: Connection, pricingTemplateId: number | null) => {
      if (!Number.isFinite(modelConfigId)) return
      if (!isOwnedConnectionTarget(model, modelConfigId, connection.id)) return
      try {
        const response = await api.models.connections.update(modelConfigId, connection.id, { pricing_template_id: pricingTemplateId });
        if (!commitConnection(response.connection)) return;
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
        void refreshDiagnostics?.();
      } catch (error) {
        toast.error(error instanceof Error ? error.message : "Failed to update pricing template");
      }
    },
    [commitConnection, model, modelConfigId, refreshCurrentState, refreshDiagnostics, revision],
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
        const response = await api.models.targets.delete(modelConfigId, target.id);
        applyTargets(response.access_targets);
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
        void refreshDiagnostics?.();
        toast.success(getStaticMessages().modelDetailData.connectionDeleted);
      } catch (error) {
        toast.error(error instanceof Error ? error.message : getStaticMessages().modelDetailData.deleteConnectionFailed);
      }
    },
    [applyTargets, model, modelConfigId, refreshCurrentState, refreshDiagnostics, revision, setAllConnections, setConnections],
  );

  const handleToggleActive = useCallback(
    async (connection: Connection) => {
      try {
        if (!Number.isFinite(modelConfigId)) return;
        if (!isOwnedConnectionTarget(model, modelConfigId, connection.id)) {
          toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
          return;
        }
        const updatedResponse = await api.models.connections.update(modelConfigId, connection.id, { is_active: !connection.is_active });
        if (!commitConnection(updatedResponse.connection)) return;
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
        void refreshDiagnostics?.();
      } catch {
        toast.error(getStaticMessages().modelDetailData.toggleConnectionFailed);
      }
    },
    [commitConnection, model, modelConfigId, refreshCurrentState, refreshDiagnostics, revision],
  );

  return {
    handleConnectionSubmit,
    handleDeleteConnection,
    handleToggleActive,
    handleAddAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
    handleDeleteAccessTarget,
    handleQuickCapabilityChange,
    handleQuickPricingChange,
  };
}
