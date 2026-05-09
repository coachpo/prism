import { useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { LogRetentionTable, RetentionSettingsResponse } from "@/lib/types";
import { toast } from "sonner";
import {
  type CleanupType,
  type DeleteCleanupType,
  type RetentionPreset,
  getCleanupTypeLabel,
} from "./settingsPageHelpers";
import type { SettingsSaveSection } from "./settingsSaveTypes";

interface UseRetentionDeletionDataInput {
  setRecentlySavedSection?: (section: SettingsSaveSection | null) => void;
}

type RetentionSettingKey = keyof Pick<
  RetentionSettingsResponse,
  "request_logs_retention_days" | "statistics_retention_days" | "audit_logs_retention_days" | "loadbalance_events_retention_days"
>;

const CLEANUP_TABLES: Record<DeleteCleanupType, LogRetentionTable> = {
  requests: "request_logs",
  statistics: "usage_request_events",
  audits: "audit_logs",
  loadbalance_events: "loadbalance_events",
};

const DAY_IN_MILLISECONDS = 24 * 60 * 60 * 1000;

export function useRetentionDeletionData({
  setRecentlySavedSection,
}: UseRetentionDeletionDataInput = {}) {
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
  const [retentionSettingsLoading, setRetentionSettingsLoading] = useState(true);
  const [retentionSettingsSaving, setRetentionSettingsSaving] = useState(false);
  const [savedRetentionSettings, setSavedRetentionSettings] = useState<RetentionSettingsResponse | null>(null);
  const [retentionSettings, setRetentionSettings] = useState<RetentionSettingsResponse | null>(null);

  const isDeletePhraseValid = useMemo(
    () => deleteConfirmPhrase.trim().toLowerCase() === deleteKeyword.toLowerCase(),
    [deleteConfirmPhrase, deleteKeyword]
  );
  const retentionSettingsDirty = useMemo(() => {
    if (!savedRetentionSettings || !retentionSettings) {
      return false;
    }

    return (
      savedRetentionSettings.request_logs_retention_days !== retentionSettings.request_logs_retention_days
      || savedRetentionSettings.statistics_retention_days !== retentionSettings.statistics_retention_days
      || savedRetentionSettings.audit_logs_retention_days !== retentionSettings.audit_logs_retention_days
      || savedRetentionSettings.loadbalance_events_retention_days !== retentionSettings.loadbalance_events_retention_days
    );
  }, [retentionSettings, savedRetentionSettings]);

  useEffect(() => {
    let active = true;

    setRetentionSettingsLoading(true);
    void api.settings.retention.get()
      .then((nextSettings) => {
        if (!active) {
          return;
        }
        setSavedRetentionSettings(nextSettings);
        setRetentionSettings(nextSettings);
      })
      .catch((error) => {
        if (!active) {
          return;
        }
        const messages = getStaticMessages();
        toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.retentionLoadedFailed);
      })
      .finally(() => {
        if (active) {
          setRetentionSettingsLoading(false);
        }
      });

    return () => {
      active = false;
    };
  }, []);

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
    const cutoff = deleteAll ? null : new Date(Date.now() - days! * DAY_IN_MILLISECONDS).toISOString();
    setDeleting(true);
    try {
      const job = await api.settings.retention.createJob({
        table: CLEANUP_TABLES[type],
        cutoff,
        delete_all: deleteAll,
        reason: "manual_ui_cleanup",
      });

      toast.success(
        messages.settingsRetentionDeletion.deletionRequested(getCleanupTypeLabel(type), job.job_id, job.status_url),
      );

      setDeleteConfirmDialogOpen(false);
      setDeleteConfirmState(null);
    } catch {
      toast.error(messages.settingsRetentionDeletion.deletionFailed);
    } finally {
      setDeleting(false);
    }
  };

  const setRetentionDays = (
    key: RetentionSettingKey,
    value: number | null,
  ) => {
    setRetentionSettings((current) => {
      if (!current) {
        return current;
      }

      return {
        ...current,
        [key]: value,
      };
    });
  };

  const handleSaveRetentionSettings = async () => {
    const messages = getStaticMessages();
    if (!retentionSettings || !retentionSettingsDirty) {
      return;
    }

    setRetentionSettingsSaving(true);
    try {
      const updated = await api.settings.retention.update({
        request_logs_retention_days: retentionSettings.request_logs_retention_days,
        statistics_retention_days: retentionSettings.statistics_retention_days,
        audit_logs_retention_days: retentionSettings.audit_logs_retention_days,
        loadbalance_events_retention_days: retentionSettings.loadbalance_events_retention_days,
      });
      setSavedRetentionSettings(updated);
      setRetentionSettings(updated);
      setRecentlySavedSection?.("retention");
      toast.success(messages.settingsRetentionDeletion.retentionUpdated);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.retentionUpdateFailed);
    } finally {
      setRetentionSettingsSaving(false);
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
    handleSaveRetentionSettings,
    isDeletePhraseValid,
    retentionPreset,
    retentionSettings,
    retentionSettingsDirty,
    retentionSettingsLoading,
    retentionSettingsSaving,
    setCleanupType,
    setDeleteConfirm,
    setDeleteConfirmPhrase,
    setRetentionDays,
    setRetentionPreset,
  };
}
