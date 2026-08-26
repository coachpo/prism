import { useCallback } from "react";
import { clearSharedReferenceData } from "@/lib/referenceData";
import type {
  Connection,
  ModelAccessTarget,
  ModelConfig,
  ModelConfigListItem,
} from "@/lib/types";
import {
  getOwnedModelConnections,
} from "./modelAccessTargetProjection";
import { patchModelListItemFromDetail } from "../models/modelListProjection";

interface UseModelDetailTargetReconciliationInput {
  modelConfigId: number;
  revision: number;
  refreshCurrentState: () => void | Promise<void>;
  refreshDiagnostics?: () => void | Promise<void>;
  setAllModels: React.Dispatch<React.SetStateAction<ModelConfigListItem[]>>;
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
  setAllModels,
  setConnections,
  setModel,
}: UseModelDetailTargetReconciliationInput) {
  const applyTargets = useCallback(
    (targets: ModelAccessTarget[]) => {
      setModel((currentModel) => {
        if (!currentModel) return currentModel;
        const nextModel = { ...currentModel, access_targets: targets };
        setConnections(getOwnedModelConnections(nextModel, modelConfigId));
        setAllModels((currentModels) =>
          patchModelListItemFromDetail(currentModels, nextModel),
        );
        return nextModel;
      });
      clearSharedReferenceData(undefined, revision);
      void refreshCurrentState();
      void refreshDiagnostics?.();
    },
    [
      modelConfigId,
      refreshCurrentState,
      refreshDiagnostics,
      revision,
      setAllModels,
      setConnections,
      setModel,
    ],
  );

  return { applyTargets };
}
