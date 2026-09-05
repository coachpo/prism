import { useCallback } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import type {
  ModelAccessTarget,
  ModelAccessTargetMutation,
  ModelConfig,
} from "@/lib/types";
import {
  getTerminalTargetId,
  isTerminalTargetAccessTargetType,
} from "@/lib/types/target-compatibility";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getOwnedConnectionTarget } from "./modelAccessTargetProjection";

const dataCopy = () => getStaticMessages().modelDetailData;

function findAccessTargetByRowId(
  model: ModelConfig | null,
  targetRowId: number,
): ModelAccessTarget | null {
  return (
    model?.access_targets.find((target) => target.id === targetRowId) ?? null
  );
}

interface UseModelDetailAccessTargetMutationsInput {
  modelConfigId: number;
  model: ModelConfig | null;
  applyTargets: (targets: ModelAccessTarget[]) => void;
}

export function useModelDetailAccessTargetMutations({
  modelConfigId,
  model,
  applyTargets,
}: UseModelDetailAccessTargetMutationsInput) {
  const handleAddAccessTarget = useCallback(
    async (target: ModelAccessTargetMutation) => {
      if (!Number.isFinite(modelConfigId)) return;
      if (target.target_type !== "model") {
        toast.error(dataCopy().terminalTargetsManagedFromDetail);
        return;
      }
      try {
        const response = await api.models.targets.create(modelConfigId, target);
        applyTargets(response.access_targets);
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : dataCopy().accessTargetAddFailed,
        );
        throw error;
      }
    },
    [applyTargets, modelConfigId],
  );

  const handleMoveAccessTarget = useCallback(
    async (targetRowId: number, toIndex: number) => {
      if (!Number.isFinite(modelConfigId)) return;
      const target = findAccessTargetByRowId(model, targetRowId);
      if (!target) {
        toast.error(dataCopy().accessTargetRowNotFound);
        return;
      }
      if (
        isTerminalTargetAccessTargetType(target.target_type) &&
        !getOwnedConnectionTarget(
          model,
          modelConfigId,
          getTerminalTargetId(target) ?? -1,
        )
      ) {
        toast.error(dataCopy().terminalTargetOwnerMismatch);
        return;
      }
      try {
        const response = await api.models.targets.movePosition(
          modelConfigId,
          target.id,
          toIndex,
        );
        applyTargets(response.access_targets);
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : dataCopy().accessTargetReorderFailed,
        );
        throw error;
      }
    },
    [applyTargets, model, modelConfigId],
  );

  const handleToggleAccessTarget = useCallback(
    async (targetRowId: number, enabled: boolean) => {
      if (!Number.isFinite(modelConfigId)) return;
      const target = findAccessTargetByRowId(model, targetRowId);
      if (!target) {
        toast.error(dataCopy().accessTargetRowNotFound);
        return;
      }
      if (
        isTerminalTargetAccessTargetType(target.target_type) &&
        !getOwnedConnectionTarget(
          model,
          modelConfigId,
          getTerminalTargetId(target) ?? -1,
        )
      ) {
        toast.error(dataCopy().terminalTargetOwnerMismatch);
        return;
      }
      try {
        const response = await api.models.targets.update(
          modelConfigId,
          target.id,
          { is_enabled: enabled },
        );
        applyTargets(response.access_targets);
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : dataCopy().accessTargetUpdateFailed,
        );
        throw error;
      }
    },
    [applyTargets, model, modelConfigId],
  );

  const handleDeleteAccessTarget = useCallback(
    async (targetRowId: number) => {
      if (!Number.isFinite(modelConfigId)) return;
      const target = findAccessTargetByRowId(model, targetRowId);
      if (!target) {
        toast.error(dataCopy().accessTargetRowNotFound);
        return;
      }
      if (
        isTerminalTargetAccessTargetType(target.target_type) &&
        !getOwnedConnectionTarget(
          model,
          modelConfigId,
          getTerminalTargetId(target) ?? -1,
        )
      ) {
        toast.error(dataCopy().terminalTargetOwnerMismatch);
        return;
      }
      try {
        const response = await api.models.targets.delete(
          modelConfigId,
          target.id,
        );
        applyTargets(response.access_targets);
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : dataCopy().accessTargetRemoveFailed,
        );
        throw error;
      }
    },
    [applyTargets, model, modelConfigId],
  );

  return {
    handleAddAccessTarget,
    handleDeleteAccessTarget,
    handleMoveAccessTarget,
    handleToggleAccessTarget,
  };
}
