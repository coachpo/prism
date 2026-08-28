import { useCallback } from "react";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type {
  Connection,
  ModelAccessTarget,
  ModelConfig,
} from "@/lib/types";
import {
  getOwnedModelConnections,
} from "./modelAccessTargetProjection";

interface UseModelDetailTargetReconciliationInput {
  modelConfigId: number;
  revision: number;
  refreshCurrentState: () => void | Promise<void>;
  refreshDiagnostics?: () => void | Promise<void>;
  refreshModels?: () => Promise<void>;
  setConnections: React.Dispatch<React.SetStateAction<Connection[]>>;
  setModel: React.Dispatch<React.SetStateAction<ModelConfig | null>>;
}

/**
 * Reconcile a server-returned mixed access-target list into the detail model,
 * owned connection projection, and models cache. All target mutation owners
 * share this response boundary.
 */
export function useModelDetailTargetReconciliation({
  modelConfigId,
  revision,
  refreshCurrentState,
  refreshDiagnostics,
  refreshModels,
  setConnections,
  setModel,
}: UseModelDetailTargetReconciliationInput) {
  const applyTargets = useCallback(
    (targets: ModelAccessTarget[]) => {
      setModel((currentModel) => {
        if (!currentModel) return currentModel;
        const nextModel = { ...currentModel, access_targets: targets };
        setConnections(getOwnedModelConnections(nextModel, modelConfigId));
        return nextModel;
      });
      clearSharedReferenceData(undefined, revision);
      void refreshCurrentState();
      void refreshDiagnostics?.();
      void refreshModels?.().catch((error) => {
        console.error("Failed to refresh authoritative model list", error);
      });
    },
    [
      modelConfigId,
      refreshCurrentState,
      refreshDiagnostics,
      refreshModels,
      revision,
      setConnections,
      setModel,
    ],
  );

  return { applyTargets };
}
