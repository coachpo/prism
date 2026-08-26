import { useCallback } from "react";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type {
  Connection,
  ModelConfig,
  OpenAITextCapability,
} from "@/lib/types";
import {
  isOwnedConnectionTarget,
} from "./modelAccessTargetProjection";
import { removeConnectionFromList } from "./connectionCollectionState";
import type { CommitModelDetailConnection } from "./useModelDetailConnectionReconciliation";

interface UseModelDetailConnectionLifecycleInput {
  modelConfigId: number;
  model: ModelConfig | null;
  revision: number;
  refreshCurrentState: () => void | Promise<void>;
  refreshDiagnostics?: () => void | Promise<void>;
  setConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setAllConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  applyTargets: (targets: import("@/lib/types").ModelAccessTarget[]) => void;
  commitConnection: CommitModelDetailConnection;
}

const TERMINAL_TARGET_OWNER_MISMATCH =
  "Terminal Target owner does not match the current model";

export function useModelDetailConnectionLifecycle({
  modelConfigId,
  model,
  revision,
  refreshCurrentState,
  refreshDiagnostics,
  setConnections,
  setAllConnections,
  applyTargets,
  commitConnection,
}: UseModelDetailConnectionLifecycleInput) {
  const handleQuickCapabilityChange = useCallback(
    async (connection: Connection, capability: OpenAITextCapability) => {
      if (!Number.isFinite(modelConfigId)) return;
      if (!isOwnedConnectionTarget(model, modelConfigId, connection.id)) return;
      try {
        const response = await api.models.connections.update(
          modelConfigId,
          connection.id,
          { openai_text_capability: capability },
        );
        if (!commitConnection(response.connection)) return;
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
        void refreshDiagnostics?.();
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : "Failed to update capability",
        );
      }
    },
    [
      commitConnection,
      model,
      modelConfigId,
      refreshCurrentState,
      refreshDiagnostics,
      revision,
    ],
  );

  const handleQuickPricingChange = useCallback(
    async (connection: Connection, pricingTemplateId: number | null) => {
      if (!Number.isFinite(modelConfigId)) return;
      if (!isOwnedConnectionTarget(model, modelConfigId, connection.id)) return;
      try {
        const response = await api.models.connections.update(
          modelConfigId,
          connection.id,
          {
            pricing_template_id: pricingTemplateId,
            expected_connection_updated_at: connection.updated_at,
            expected_pricing_template_id: connection.pricing_template_id,
          },
        );
        if (!commitConnection(response.connection)) return;
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
        void refreshDiagnostics?.();
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : "Failed to update pricing template",
        );
      }
    },
    [
      commitConnection,
      model,
      modelConfigId,
      refreshCurrentState,
      refreshDiagnostics,
      revision,
    ],
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
        setAllConnections((current) =>
          removeConnectionFromList(current, connectionId),
        );
        setConnections((current) =>
          removeConnectionFromList(current, connectionId),
        );
        const targets = await api.models.targets.list(modelConfigId);
        applyTargets(targets);
        void refreshCurrentState();
        void refreshDiagnostics?.();
        toast.success(getStaticMessages().modelDetailData.connectionDeleted);
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : getStaticMessages().modelDetailData.deleteConnectionFailed,
        );
      }
    },
    [
      applyTargets,
      model,
      modelConfigId,
      refreshCurrentState,
      refreshDiagnostics,
      revision,
      setAllConnections,
      setConnections,
    ],
  );

  const handleToggleActive = useCallback(
    async (connection: Connection) => {
      try {
        if (!Number.isFinite(modelConfigId)) return;
        if (!isOwnedConnectionTarget(model, modelConfigId, connection.id)) {
          toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
          return;
        }
        const updatedResponse = await api.models.connections.update(
          modelConfigId,
          connection.id,
          { is_active: !connection.is_active },
        );
        if (!commitConnection(updatedResponse.connection)) return;
        clearSharedReferenceData(undefined, revision);
        void refreshCurrentState();
        void refreshDiagnostics?.();
      } catch {
        toast.error(getStaticMessages().modelDetailData.toggleConnectionFailed);
      }
    },
    [
      commitConnection,
      model,
      modelConfigId,
      refreshCurrentState,
      refreshDiagnostics,
      revision,
    ],
  );

  return {
    handleDeleteConnection,
    handleQuickCapabilityChange,
    handleQuickPricingChange,
    handleToggleActive,
  };
}
