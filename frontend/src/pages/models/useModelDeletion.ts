import { useCallback, useState } from "react";

import { api } from "@/lib/api";
import type { ManagedModelConfigListItem } from "@/lib/api/models";
import { getStaticMessages } from "@/i18n/staticMessages";
import { toast } from "sonner";

interface UseModelDeletionInput {
  commitModels: (
    updater: (
      current: ManagedModelConfigListItem[],
    ) => ManagedModelConfigListItem[],
  ) => void;
}

export function useModelDeletion({ commitModels }: UseModelDeletionInput) {
  const [deleteTarget, setDeleteTarget] =
    useState<ManagedModelConfigListItem | null>(null);

  const handleDelete = useCallback(async () => {
    const messages = getStaticMessages();
    if (!deleteTarget) return;
    try {
      await api.models.delete(deleteTarget.id);
      commitModels((current) =>
        current.filter((model) => model.id !== deleteTarget.id),
      );
      toast.success(messages.modelsData.deleted);
      setDeleteTarget(null);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.modelsData.deleteFailed,
      );
    }
  }, [commitModels, deleteTarget]);

  return { deleteTarget, handleDelete, setDeleteTarget };
}
