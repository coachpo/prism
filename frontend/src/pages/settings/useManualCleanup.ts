import { useCallback, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  LogRetentionTable,
  RetentionPreflightResponse,
} from "@/lib/types";
import { toast } from "sonner";
import {
  type CleanupType,
  type DeleteCleanupType,
  type RetentionPreset,
  getCleanupTypeLabel,
} from "./manualCleanup";
import {
  isStaleRetentionPreflightError,
  newRetentionOperationId,
  retentionPreflightFactsComplete,
} from "./retentionProtocol";

interface UseManualCleanupInput {
  onJobsMutation: () => void | Promise<void>;
  refreshRetentionSettings: () => void | Promise<void>;
}

type DeleteConfirmation = {
  type: DeleteCleanupType;
  days: number | null;
  deleteAll: boolean;
};

const CLEANUP_TABLES: Record<DeleteCleanupType, LogRetentionTable> = {
  requests: "request_logs",
  statistics: "usage_request_events",
  audits: "audit_logs",
  loadbalance_events: "loadbalance_events",
};

export function useManualCleanup({
  onJobsMutation,
  refreshRetentionSettings,
}: UseManualCleanupInput) {
  const messages = getStaticMessages();
  const [cleanupType, setCleanupType] = useState<CleanupType>("");
  const [retentionPreset, setRetentionPreset] = useState<RetentionPreset>("");
  const [deleteConfirm, setDeleteConfirmState] =
    useState<DeleteConfirmation | null>(null);
  const [deleteConfirmDialogOpen, setDeleteConfirmDialogOpen] = useState(false);
  const [displayedDeleteConfirm, setDisplayedDeleteConfirm] =
    useState<DeleteConfirmation | null>(null);
  const [deleteConfirmPhrase, setDeleteConfirmPhrase] = useState("");
  const [manualPreflight, setManualPreflight] =
    useState<RetentionPreflightResponse | null>(null);
  const [manualPreflightLoading, setManualPreflightLoading] = useState(false);
  const [manualOperationId, setManualOperationId] = useState<string | null>(
    null,
  );
  const [deleting, setDeleting] = useState(false);

  const manualConfirmationKeyword =
    manualPreflight?.confirmation_keyword ?? null;
  const isDeletePhraseValid = useMemo(
    () =>
      manualConfirmationKeyword !== null &&
      deleteConfirmPhrase.trim() === manualConfirmationKeyword,
    [deleteConfirmPhrase, manualConfirmationKeyword],
  );
  const isManualPreflightSemanticallyComplete =
    retentionPreflightFactsComplete(manualPreflight);
  const isManualPreflightValid =
    isDeletePhraseValid && isManualPreflightSemanticallyComplete;

  const handleOpenDeleteConfirm = useCallback(async () => {
    if (!cleanupType || !retentionPreset) return;
    const deleteAll = retentionPreset === "all";
    const days = deleteAll ? null : Number.parseInt(retentionPreset, 10);
    if (!deleteAll && ![1, 7, 30, 90].includes(days as number)) {
      toast.error(messages.settingsRetentionDeletion.invalidRetentionOption);
      return;
    }
    const next = {
      type: cleanupType,
      days,
      deleteAll,
    } satisfies DeleteConfirmation;
    const nextOperationId = newRetentionOperationId("retention-manual");
    setDeleteConfirmState(next);
    setDisplayedDeleteConfirm(next);
    setManualOperationId(nextOperationId);
    setManualPreflightLoading(true);
    setManualPreflight(null);
    setDeleteConfirmPhrase("");
    try {
      const preview = await api.settings.retention.preflight({
        kind: "manual_cleanup",
        operation_id: nextOperationId,
        preflight_attempt_id: newRetentionOperationId("retention-preview"),
        dataset: CLEANUP_TABLES[cleanupType],
        selection: deleteAll
          ? { mode: "delete_all" }
          : { mode: "keep_days", days: days as 1 | 7 | 30 | 90 },
      });
      setManualPreflight(preview);
      setDeleteConfirmDialogOpen(true);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.settingsRetentionDeletion.preflightFailed,
      );
      setDeleteConfirmState(null);
    } finally {
      setManualPreflightLoading(false);
    }
  }, [
    cleanupType,
    messages.settingsRetentionDeletion.invalidRetentionOption,
    messages.settingsRetentionDeletion.preflightFailed,
    retentionPreset,
  ]);

  const handleBatchDelete = useCallback(async () => {
    if (
      !deleteConfirm ||
      !manualPreflight ||
      !manualOperationId ||
      !isManualPreflightValid
    )
      return;
    setDeleting(true);
    try {
      const response = await api.settings.retention.createJob({
        operation_id: manualOperationId,
        preflight_token: manualPreflight.preflight_token,
        confirmation: { keyword: deleteConfirmPhrase.trim() },
      });
      toast.success(
        messages.settingsRetentionDeletion.deletionRequested(
          getCleanupTypeLabel(deleteConfirm.type),
          response.job.id,
        ),
      );
      setDeleteConfirmDialogOpen(false);
      setDeleteConfirmState(null);
      setManualPreflight(null);
      setManualOperationId(null);
      setDeleteConfirmPhrase("");
      void onJobsMutation();
    } catch (error) {
      if (isStaleRetentionPreflightError(error)) {
        setManualPreflight(null);
        setDeleteConfirmPhrase("");
        setDeleteConfirmDialogOpen(true);
        void refreshRetentionSettings();
      }
      toast.error(
        error instanceof Error
          ? error.message
          : messages.settingsRetentionDeletion.deletionFailed,
      );
    } finally {
      setDeleting(false);
    }
  }, [
    deleteConfirm,
    deleteConfirmPhrase,
    isManualPreflightValid,
    manualOperationId,
    manualPreflight,
    messages.settingsRetentionDeletion,
    onJobsMutation,
    refreshRetentionSettings,
  ]);

  const setDeleteConfirm = (confirm: DeleteConfirmation | null) => {
    setDeleteConfirmState(confirm);
    setDeleteConfirmDialogOpen(Boolean(confirm));
    if (!confirm) {
      setManualPreflight(null);
      setManualOperationId(null);
      setDeleteConfirmPhrase("");
    }
  };

  return {
    cleanupType,
    deleteConfirm,
    deleteConfirmDialogOpen,
    deleteConfirmPhrase,
    deleting,
    displayedDeleteConfirm,
    manualPreflight,
    manualPreflightLoading,
    handleBatchDelete,
    handleOpenDeleteConfirm,
    isDeletePhraseValid,
    isManualPreflightSemanticallyComplete,
    isManualPreflightValid,
    retentionPreset,
    setCleanupType,
    setDeleteConfirm,
    setDeleteConfirmPhrase,
    setRetentionPreset,
  };
}
