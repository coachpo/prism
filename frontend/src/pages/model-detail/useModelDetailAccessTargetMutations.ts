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
import { getOwnedConnectionTarget } from "./modelAccessTargetProjection";

const TERMINAL_TARGET_OWNER_MISMATCH =
  "Terminal Target owner does not match the current model";
const TERMINAL_TARGETS_MANAGED_FROM_MODEL_DETAIL =
  "Manage Terminal Targets from the Model Detail Terminal Targets list.";
const ACCESS_TARGET_ROW_NOT_FOUND =
  "Access target row is no longer present in this model; reload the page and retry.";

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
        toast.error(TERMINAL_TARGETS_MANAGED_FROM_MODEL_DETAIL);
        return;
      }
      try {
        const response = await api.models.targets.create(modelConfigId, target);
        applyTargets(response.access_targets);
      } catch (error) {
        toast.error(
          error instanceof Error
            ? error.message
            : "Failed to add access target",
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
        toast.error(ACCESS_TARGET_ROW_NOT_FOUND);
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
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
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
            : "Failed to reorder access target",
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
        toast.error(ACCESS_TARGET_ROW_NOT_FOUND);
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
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
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
            : "Failed to update access target",
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
        toast.error(ACCESS_TARGET_ROW_NOT_FOUND);
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
        toast.error(TERMINAL_TARGET_OWNER_MISMATCH);
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
            : "Failed to remove access target",
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
