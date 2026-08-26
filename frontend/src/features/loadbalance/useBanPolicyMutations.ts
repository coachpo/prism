import { useState } from "react";

import { ApiError, api } from "@/lib/api";
import type { LoadbalanceStrategy } from "@/lib/types";
import {
  banPolicyFormValuesFromStrategy,
  buildBanPolicyPayload,
  buildBanPolicyUpdatePayload,
  DEFAULT_BAN_POLICY_FORM_VALUES,
  getAttachedModelCountFromDeleteDetail,
  type BanPolicyFormValues,
} from "./banPolicySchemas";
import type { FragmentState } from "./strategyFragmentState";

export interface SetDefaultState {
  pending: boolean;
  error: string | null;
  conflictCurrentDefaultId: number | null;
}

export interface StrategyMutationError {
  message: string;
  attachedModelCount: number | null;
  defaultStrategyId: number | null;
}

interface UseBanPolicyMutationsInput {
  strategiesFragment: FragmentState<LoadbalanceStrategy[]>;
  commitStrategies: (
    updater: (current: LoadbalanceStrategy[]) => LoadbalanceStrategy[],
  ) => void;
  loadStrategy: (strategyId: number) => Promise<LoadbalanceStrategy>;
  markReadError: (error: unknown, fallback: string) => void;
  refreshStrategies: () => void | Promise<void>;
  refreshStrategiesAfterMutation: () => void | Promise<void>;
}

export function useBanPolicyMutations({
  strategiesFragment,
  commitStrategies,
  loadStrategy,
  markReadError,
  refreshStrategies,
  refreshStrategiesAfterMutation,
}: UseBanPolicyMutationsInput) {
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingStrategy, setEditingStrategy] =
    useState<LoadbalanceStrategy | null>(null);
  const [formValues, setFormValues] = useState<BanPolicyFormValues>(
    DEFAULT_BAN_POLICY_FORM_VALUES,
  );
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [preparingEditId, setPreparingEditId] = useState<number | null>(null);
  const [deleteConfirm, setDeleteConfirm] =
    useState<LoadbalanceStrategy | null>(null);
  const [displayDelete, setDisplayDelete] =
    useState<LoadbalanceStrategy | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<StrategyMutationError | null>(
    null,
  );
  const [defaultsCreating, setDefaultsCreating] = useState(false);
  const [defaultsError, setDefaultsError] = useState<string | null>(null);
  const [setDefaultState, setSetDefaultState] = useState<
    Record<number, SetDefaultState>
  >({});

  const openCreate = () => {
    setEditingStrategy(null);
    setFormValues(DEFAULT_BAN_POLICY_FORM_VALUES);
    setSaveError(null);
    setDialogOpen(true);
  };

  const openEdit = async (strategy: LoadbalanceStrategy) => {
    setPreparingEditId(strategy.id);
    try {
      const loaded = await loadStrategy(strategy.id);
      setEditingStrategy(loaded);
      setFormValues(banPolicyFormValuesFromStrategy(loaded));
      setSaveError(null);
      setDialogOpen(true);
    } catch (error) {
      markReadError(error, "Failed to load routing strategy");
    } finally {
      setPreparingEditId(null);
    }
  };

  const save = async (values: BanPolicyFormValues) => {
    setSaving(true);
    setSaveError(null);
    try {
      if (editingStrategy) {
        const updated = await api.loadbalanceStrategies.update(
          editingStrategy.id,
          buildBanPolicyUpdatePayload(values),
        );
        commitStrategies((current) =>
          current.map((strategy) =>
            strategy.id === editingStrategy.id ? updated : strategy,
          ),
        );
      } else {
        const created = await api.loadbalanceStrategies.create(
          buildBanPolicyPayload(values),
        );
        commitStrategies((current) => [created, ...current]);
      }
      setDialogOpen(false);
    } catch (error) {
      setSaveError(
        error instanceof Error
          ? error.message
          : "Failed to save routing strategy",
      );
    } finally {
      setSaving(false);
    }
  };

  const createDefaults = async () => {
    setDefaultsCreating(true);
    setDefaultsError(null);
    try {
      const response = await api.loadbalanceStrategies.createDefaults();
      await refreshStrategiesAfterMutation();
      if (response.default_changed) setDefaultsError(null);
    } catch (error) {
      setDefaultsError(
        error instanceof Error
          ? error.message
          : "Failed to complete built-in strategies",
      );
    } finally {
      setDefaultsCreating(false);
    }
  };

  const setDefault = async (strategyId: number) => {
    const currentDefault =
      strategiesFragment.data?.find((strategy) => strategy.is_default)?.id ??
      null;
    setSetDefaultState((current) => ({
      ...current,
      [strategyId]: {
        pending: true,
        error: null,
        conflictCurrentDefaultId: null,
      },
    }));
    try {
      const response = await api.loadbalanceStrategies.setDefault(
        strategyId,
        currentDefault,
      );
      commitStrategies((current) =>
        current.map((strategy) => ({
          ...strategy,
          is_default: strategy.id === response.default_strategy_id,
        })),
      );
      setSetDefaultState((current) => ({
        ...current,
        [strategyId]: {
          pending: false,
          error: null,
          conflictCurrentDefaultId: null,
        },
      }));
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        const detail = error.detail as {
          current_default_strategy_id?: number | null;
        } | null;
        setSetDefaultState((current) => ({
          ...current,
          [strategyId]: {
            pending: false,
            error: error instanceof Error ? error.message : "Default changed",
            conflictCurrentDefaultId:
              detail?.current_default_strategy_id ?? null,
          },
        }));
        void refreshStrategies();
      } else {
        setSetDefaultState((current) => ({
          ...current,
          [strategyId]: {
            pending: false,
            error:
              error instanceof Error ? error.message : "Failed to set default",
            conflictCurrentDefaultId: null,
          },
        }));
      }
    }
  };

  const clearSetDefaultError = (strategyId: number) => {
    setSetDefaultState((current) => ({
      ...current,
      [strategyId]: {
        pending: false,
        error: null,
        conflictCurrentDefaultId: null,
      },
    }));
  };

  const openDelete = (strategy: LoadbalanceStrategy) => {
    setDeleteConfirm(strategy);
    setDisplayDelete(strategy);
    setDeleteError(null);
  };

  const closeDelete = () => {
    setDeleteConfirm(null);
    setDisplayDelete(null);
    setDeleteError(null);
  };

  const deleteStrategy = async () => {
    if (!deleteConfirm) return;
    setDeleting(true);
    setDeleteError(null);
    try {
      await api.loadbalanceStrategies.delete(deleteConfirm.id);
      commitStrategies((current) =>
        current.filter((strategy) => strategy.id !== deleteConfirm.id),
      );
      closeDelete();
    } catch (error) {
      const mutationError: StrategyMutationError = {
        message:
          error instanceof Error
            ? error.message
            : "Failed to delete routing strategy",
        attachedModelCount: null,
        defaultStrategyId: null,
      };
      if (error instanceof ApiError && error.status === 409) {
        const attachedModelCount = getAttachedModelCountFromDeleteDetail(
          error.detail,
        );
        mutationError.attachedModelCount = attachedModelCount;
        const detail = error.detail as {
          default_strategy_id?: number | null;
        } | null;
        mutationError.defaultStrategyId = detail?.default_strategy_id ?? null;
        if (attachedModelCount !== null) {
          const blocked = {
            ...deleteConfirm,
            attached_model_count: attachedModelCount,
          };
          setDeleteConfirm(blocked);
          setDisplayDelete(blocked);
        }
      }
      setDeleteError(mutationError);
    } finally {
      setDeleting(false);
    }
  };

  return {
    clearSetDefaultError,
    closeDelete,
    createDefaults,
    defaultsCreating,
    defaultsError,
    deleteConfirm,
    deleteError,
    deleteStrategy,
    deleting,
    dialogOpen,
    displayDelete,
    editingStrategy,
    formValues,
    openCreate,
    openDelete,
    openEdit,
    preparingEditId,
    save,
    saveError,
    saving,
    setDefault,
    setDefaultState,
    setDialogOpen,
  };
}
