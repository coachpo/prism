import { useMemo, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { toast } from "sonner";
import {
  type CleanupType,
  type DeleteCleanupType,
  type RetentionPreset,
  getCleanupTypeLabel,
} from "./settingsPageHelpers";

export function useRetentionDeletionData() {
  const deleteKeyword = getStaticMessages().settingsDialogs.deleteConfirmKeyword;
  const [cleanupType, setCleanupType] = useState<CleanupType>("");
  const [retentionPreset, setRetentionPreset] = useState<RetentionPreset>("");
  const [deleteConfirm, setDeleteConfirmState] = useState<{
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null>(null);
  const [deleteConfirmDialogOpen, setDeleteConfirmDialogOpen] = useState(false);
  const [displayedDeleteConfirm, setDisplayedDeleteConfirm] = useState<{
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null>(null);
  const [deleteConfirmPhrase, setDeleteConfirmPhrase] = useState("");
  const [deleting, setDeleting] = useState(false);

  const isDeletePhraseValid = useMemo(
    () => deleteConfirmPhrase.trim().toLowerCase() === deleteKeyword.toLowerCase(),
    [deleteConfirmPhrase, deleteKeyword]
  );

  const handleOpenDeleteConfirm = () => {
    const messages = getStaticMessages();
    if (!cleanupType || !retentionPreset) {
      return;
    }

    const deleteAll = retentionPreset === "all";
    const days = deleteAll ? null : Number.parseInt(retentionPreset, 10);
    if (!deleteAll && Number.isNaN(days)) {
      toast.error(messages.settingsRetentionDeletion.invalidRetentionOption);
      return;
    }

    const nextDeleteConfirm = { type: cleanupType, days, deleteAll };
    setDeleteConfirmState(nextDeleteConfirm);
    setDisplayedDeleteConfirm(nextDeleteConfirm);
    setDeleteConfirmDialogOpen(true);
    setDeleteConfirmPhrase("");
  };

  const handleBatchDelete = async () => {
    const messages = getStaticMessages();
    if (!deleteConfirm || !isDeletePhraseValid) {
      return;
    }

    const { type, days, deleteAll } = deleteConfirm;
    setDeleting(true);
    try {
      if (type === "requests") {
        if (deleteAll) {
          await api.stats.delete({ delete_all: true });
        } else {
          await api.stats.delete({ older_than_days: days! });
        }
      } else if (type === "statistics") {
        if (deleteAll) {
          await api.stats.deleteStatistics({ delete_all: true });
        } else {
          await api.stats.deleteStatistics({ older_than_days: days! });
        }
      } else if (type === "audits") {
        if (deleteAll) {
          await api.audit.delete({ delete_all: true });
        } else {
          await api.audit.delete({ older_than_days: days! });
        }
      } else {
        if (deleteAll) {
          await api.loadbalance.deleteEvents({ delete_all: true });
        } else {
          await api.loadbalance.deleteEvents({ older_than_days: days! });
        }
      }

      toast.success(
        messages.settingsRetentionDeletion.deletionRequested(getCleanupTypeLabel(type)),
      );

      setDeleteConfirmDialogOpen(false);
      setDeleteConfirmState(null);
    } catch {
      toast.error(messages.settingsRetentionDeletion.deletionFailed);
    } finally {
      setDeleting(false);
    }
  };

  const setDeleteConfirm = (confirm: {
    type: DeleteCleanupType;
    days: number | null;
    deleteAll: boolean;
  } | null) => {
    setDeleteConfirmState(confirm);

    if (confirm) {
      setDisplayedDeleteConfirm(confirm);
      setDeleteConfirmDialogOpen(true);
      return;
    }

    setDeleteConfirmDialogOpen(false);
  };

  return {
    cleanupType,
    deleteConfirm,
    deleteConfirmDialogOpen,
    deleteConfirmPhrase,
    deleting,
    displayedDeleteConfirm,
    handleBatchDelete,
    handleOpenDeleteConfirm,
    isDeletePhraseValid,
    retentionPreset,
    setCleanupType,
    setDeleteConfirm,
    setDeleteConfirmPhrase,
    setRetentionPreset,
  };
}
