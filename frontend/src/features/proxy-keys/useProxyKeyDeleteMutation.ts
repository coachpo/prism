import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { ProxyApiKey } from "@/lib/types";
import { showProxyKeyMutationError } from "./proxyKeyMutationErrors";
import { reconcileProxyKeyLedgerAfterDelete } from "./proxyKeyMutationReconciliation";

export function useProxyKeyDeleteMutation() {
  const queryClient = useQueryClient();
  const messages = getStaticMessages();
  const [deleteConfirm, setDeleteConfirm] = useState<ProxyApiKey | null>(null);
  const [deleteProxyKeyAlertOpen, setDeleteProxyKeyAlertOpen] = useState(false);
  const [displayedDeleteConfirm, setDisplayedDeleteConfirm] =
    useState<ProxyApiKey | null>(null);
  const deleteMutation = useMutation({
    mutationFn: (keyId: number) => api.settings.auth.proxyKeys.delete(keyId),
  });

  async function handleDeleteProxyKey() {
    if (!deleteConfirm) return;
    const deletingKey = deleteConfirm;
    try {
      const deleted = await deleteMutation.mutateAsync(deletingKey.id);
      reconcileProxyKeyLedgerAfterDelete(
        queryClient,
        deletingKey.id,
        deleted.capacity,
      );
      setDeleteProxyKeyAlertOpen(false);
      setDeleteConfirm(null);
      toast.success(messages.proxyApiKeysData.deleted);
    } catch (error) {
      showProxyKeyMutationError(error, messages.proxyApiKeysData.deleteFailed);
    }
  }

  const setDeleteConfirmState = (item: ProxyApiKey | null) => {
    setDeleteConfirm(item);
    if (item) {
      setDisplayedDeleteConfirm(item);
      setDeleteProxyKeyAlertOpen(true);
      return;
    }
    setDeleteProxyKeyAlertOpen(false);
  };

  const handleDeleteDialogOpenChange = (open: boolean) => {
    if (!open && !deleteMutation.isPending) {
      setDeleteProxyKeyAlertOpen(false);
      setDeleteConfirm(null);
      return;
    }
    setDeleteProxyKeyAlertOpen(open);
  };

  return {
    deleteConfirm,
    deleteProxyKeyAlertOpen,
    deletingProxyKeyId: deleteMutation.isPending
      ? deleteMutation.variables ?? null
      : null,
    displayedDeleteConfirm,
    handleDeleteDialogOpenChange,
    handleDeleteProxyKey,
    setDeleteConfirm: setDeleteConfirmState,
  };
}
