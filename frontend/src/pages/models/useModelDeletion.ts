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
  /** 删除成功后的去向；详情页删完自己要回列表。 */
  onDeleted?: (model: ManagedModelConfigListItem) => void;
}

export function useModelDeletion({
  commitModels,
  onDeleted,
}: UseModelDeletionInput) {
  const [deleteTarget, setDeleteTargetState] =
    useState<ManagedModelConfigListItem | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const setDeleteTarget = useCallback(
    (model: ManagedModelConfigListItem | null) => {
      setDeleteError(null);
      setDeleteTargetState(model);
    },
    [],
  );

  const handleDelete = useCallback(async () => {
    const messages = getStaticMessages();
    if (!deleteTarget) return;
    setDeleteError(null);
    try {
      await api.models.delete(deleteTarget.id);
      commitModels((current) =>
        current.filter((model) => model.id !== deleteTarget.id),
      );
      toast.success(messages.modelsData.deleted);
      setDeleteTargetState(null);
      onDeleted?.(deleteTarget);
    } catch (error) {
      // 对话框保持打开，原因就写在里面：稍纵即逝的 toast 说不清下一步。
      setDeleteError(
        error instanceof Error
          ? error.message
          : messages.modelsData.deleteFailed,
      );
    }
  }, [commitModels, deleteTarget, onDeleted]);

  return { deleteError, deleteTarget, handleDelete, setDeleteTarget };
}
