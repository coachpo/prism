import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api, ApiError } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  GlobalRetentionJobDetail,
  GlobalRetentionJobSummary,
  LogRetentionTable,
  RetentionPreflightResponse,
  RetentionSettingsResponsePolicies,
  RetentionSettingsPolicies,
  RetentionSettingsResponse,
} from "@/lib/types";
import { toast } from "sonner";
import {
  type CleanupType,
  type DeleteCleanupType,
  type RetentionPreset,
  getCleanupTypeLabel,
} from "./settingsPageHelpers";
import type { SettingsSaveSection } from "./settingsSaveTypes";

interface UseRetentionDeletionDataInput {
  enabled: boolean;
  setRecentlySavedSection?: (section: SettingsSaveSection | null) => void;
}

type RetentionSettingKey = keyof RetentionSettingsPolicies;

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

function newOperationId(prefix: string) {
  return typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function policiesOf(settings: RetentionSettingsResponse): RetentionSettingsPolicies {
  const policies = settings.policies as RetentionSettingsResponsePolicies | undefined;
  if (policies && Object.values(policies).every((value) => typeof value === "number" || value === null)) {
    return policies as RetentionSettingsPolicies;
  }
  const tagged = policies as Record<keyof RetentionSettingsPolicies, { value?: number | null; raw_integer?: string }> | undefined;
  const editableValue = (value: { value?: number | null; raw_integer?: string } | undefined) => {
    if (!value) return null;
    if (value.value !== undefined && value.value !== null) return value.value;
    // Preserve an exact in-range integer for a repair form. Out-of-range
    // legacy values stay visibly invalid and must be replaced before PUT.
    const parsed = value.raw_integer === undefined ? null : Number(value.raw_integer);
    return Number.isSafeInteger(parsed) ? parsed : null;
  };
  if (tagged) {
    return {
      request_logs_retention_days: editableValue(tagged.request_logs_retention_days),
      statistics_retention_days: editableValue(tagged.statistics_retention_days),
      audit_logs_retention_days: editableValue(tagged.audit_logs_retention_days),
      loadbalance_events_retention_days: editableValue(tagged.loadbalance_events_retention_days),
    };
  }
  return {
    request_logs_retention_days: settings.request_logs_retention_days ?? null,
    statistics_retention_days: settings.statistics_retention_days ?? null,
    audit_logs_retention_days: settings.audit_logs_retention_days ?? null,
    loadbalance_events_retention_days: settings.loadbalance_events_retention_days ?? null,
  };
}

function policiesEqual(left: RetentionSettingsPolicies, right: RetentionSettingsPolicies) {
  return left.request_logs_retention_days === right.request_logs_retention_days
    && left.statistics_retention_days === right.statistics_retention_days
    && left.audit_logs_retention_days === right.audit_logs_retention_days
    && left.loadbalance_events_retention_days === right.loadbalance_events_retention_days;
}

function hasDestructiveChange(before: RetentionSettingsPolicies, after: RetentionSettingsPolicies) {
  return (Object.keys(before) as RetentionSettingKey[]).some((key) => {
    const oldValue = before[key];
    const nextValue = after[key];
    return (oldValue === null && nextValue !== null)
      || (oldValue !== null && nextValue !== null && nextValue < oldValue);
  });
}

function semanticFactsComplete(preflight: RetentionPreflightResponse | null) {
  return Boolean(
    preflight
      && preflight.affected_domains.length > 0
      && preflight.affected_domains.every((domain) => domain.impact.semantic_facts_complete),
  );
}

function isStalePreflightError(error: unknown) {
  return error instanceof ApiError && error.code === "retention_preflight_stale";
}

export function useRetentionDeletionData({
  enabled,
  setRecentlySavedSection,
}: UseRetentionDeletionDataInput) {
  const messages = getStaticMessages();
  const jobTerminalNotice = messages.settingsRetentionDeletion.jobTerminalNotice;
  const [cleanupType, setCleanupType] = useState<CleanupType>("");
  const [retentionPreset, setRetentionPreset] = useState<RetentionPreset>("");
  const [deleteConfirm, setDeleteConfirmState] = useState<DeleteConfirmation | null>(null);
  const [deleteConfirmDialogOpen, setDeleteConfirmDialogOpen] = useState(false);
  const [displayedDeleteConfirm, setDisplayedDeleteConfirm] = useState<DeleteConfirmation | null>(null);
  const [deleteConfirmPhrase, setDeleteConfirmPhrase] = useState("");
  const [manualPreflight, setManualPreflight] = useState<RetentionPreflightResponse | null>(null);
  const [manualPreflightLoading, setManualPreflightLoading] = useState(false);
  const [manualOperationId, setManualOperationId] = useState<string | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [retentionSettingsLoading, setRetentionSettingsLoading] = useState(true);
  const [retentionSettingsSaving, setRetentionSettingsSaving] = useState(false);
  const [savedRetentionSettings, setSavedRetentionSettings] = useState<RetentionSettingsResponse | null>(null);
  const [retentionSettings, setRetentionSettings] = useState<RetentionSettingsResponse | null>(null);
  const [policyPreflight, setPolicyPreflight] = useState<RetentionPreflightResponse | null>(null);
  const [policyPreflightLoading, setPolicyPreflightLoading] = useState(false);
  const [policyConfirmOpen, setPolicyConfirmOpen] = useState(false);
  const [policyConfirmationPhrase, setPolicyConfirmationPhrase] = useState("");
  const [policyOperationId, setPolicyOperationId] = useState<string | null>(null);
  const [jobs, setJobs] = useState<GlobalRetentionJobSummary[]>([]);
  const [jobsHasMore, setJobsHasMore] = useState(false);
  const [jobsLoading, setJobsLoading] = useState(false);
  const [jobsStale, setJobsStale] = useState(false);
  // Last successful jobs read, so the freshness bar can answer "when is this
  // from" even after a failed refresh keeps the previous rows on screen.
  const [jobsLoadedAt, setJobsLoadedAt] = useState<string | null>(null);
  const [jobOriginFilter, setJobOriginFilter] = useState<"all" | "manual" | "automatic">("all");
  const [jobStateFilter, setJobStateFilter] = useState("all");
  const [selectedJob, setSelectedJob] = useState<GlobalRetentionJobSummary | null>(null);
  const [jobDetail, setJobDetail] = useState<GlobalRetentionJobDetail | null>(null);
  const [jobDetailLoading, setJobDetailLoading] = useState(false);
  const jobsRequestIdRef = useRef(0);
  const jobsCursorRef = useRef<string | null>(null);
  const checkpointCursorRef = useRef<string | null>(null);
  const partitionCursorRef = useRef<string | null>(null);
  const terminalJobStateRef = useRef(new Map<string, string>());
  const cancelOperationIdsRef = useRef(new Map<string, string>());
  const jobsSnapshotInitializedRef = useRef(false);

  // The confirmation keyword is a protocol constant issued by the preflight,
  // not UI copy. The server compares it byte for byte, so a localized or
  // case-folded match here would accept a phrase the commit then rejects.
  const manualConfirmationKeyword = manualPreflight?.confirmation_keyword ?? null;
  const policyConfirmationKeyword = policyPreflight?.confirmation_keyword ?? null;
  const isDeletePhraseValid = useMemo(
    () => manualConfirmationKeyword !== null && deleteConfirmPhrase.trim() === manualConfirmationKeyword,
    [deleteConfirmPhrase, manualConfirmationKeyword],
  );
  const isPolicyPhraseValid = useMemo(
    () => policyConfirmationKeyword !== null && policyConfirmationPhrase.trim() === policyConfirmationKeyword,
    [policyConfirmationPhrase, policyConfirmationKeyword],
  );
  const retentionSettingsDirty = useMemo(() => {
    if (!savedRetentionSettings || !retentionSettings) return false;
    return !policiesEqual(policiesOf(savedRetentionSettings), policiesOf(retentionSettings));
  }, [retentionSettings, savedRetentionSettings]);
  const policyIsDestructive = Boolean(
    savedRetentionSettings && retentionSettings
      && hasDestructiveChange(policiesOf(savedRetentionSettings), policiesOf(retentionSettings)),
  );
  const isPolicyPreflightSemanticallyComplete = semanticFactsComplete(policyPreflight);
  const isManualPreflightSemanticallyComplete = semanticFactsComplete(manualPreflight);
  const isPolicyPreflightValid = isPolicyPhraseValid && isPolicyPreflightSemanticallyComplete;
  const isManualPreflightValid = isDeletePhraseValid && isManualPreflightSemanticallyComplete;

  const fetchRetentionSettings = useCallback(async () => {
    setRetentionSettingsLoading(true);
    try {
      const next = await api.settings.retention.get();
      setSavedRetentionSettings(next);
      setRetentionSettings(next);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.retentionLoadedFailed);
    } finally {
      setRetentionSettingsLoading(false);
    }
  }, [messages.settingsRetentionDeletion.retentionLoadedFailed]);

  const fetchJobs = useCallback(async (append = false) => {
    const requestId = ++jobsRequestIdRef.current;
    setJobsLoading(true);
    try {
      const response = await api.settings.retention.jobs.list({
        origin: jobOriginFilter === "all" ? undefined : jobOriginFilter,
        state: jobStateFilter === "all" ? undefined : [jobStateFilter],
        cursor: append ? jobsCursorRef.current ?? undefined : undefined,
      });
      if (requestId !== jobsRequestIdRef.current) return;
      if (jobsSnapshotInitializedRef.current) {
        for (const job of response.items) {
          const previousState = terminalJobStateRef.current.get(job.id);
          if (previousState && previousState !== job.state && ["cancelled", "succeeded", "failed", "superseded"].includes(job.state)) {
            toast.info(jobTerminalNotice(job.dataset, job.state, job.id));
          }
        }
      }
      for (const job of response.items) terminalJobStateRef.current.set(job.id, job.state);
      jobsSnapshotInitializedRef.current = true;
      setJobs((current) => append ? [...current, ...response.items] : response.items);
      jobsCursorRef.current = response.next_cursor;
      setJobsHasMore(response.has_more);
      setJobsStale(false);
      setJobsLoadedAt(new Date().toISOString());
    } catch {
      if (requestId === jobsRequestIdRef.current) setJobsStale(true);
    } finally {
      if (requestId === jobsRequestIdRef.current) setJobsLoading(false);
    }
  }, [jobOriginFilter, jobStateFilter, jobTerminalNotice]);

  useEffect(() => {
    if (!enabled) return;
    void fetchRetentionSettings();
  }, [enabled, fetchRetentionSettings]);

  useEffect(() => {
    if (!enabled) return;
    jobsCursorRef.current = null;
    void fetchJobs(false);
  }, [enabled, fetchJobs, jobOriginFilter, jobStateFilter]);

  useEffect(() => {
    if (!enabled) return;
    const interval = window.setInterval(() => {
      void fetchJobs(false);
    }, 5000);
    return () => window.clearInterval(interval);
  }, [enabled, fetchJobs]);

  const setRetentionDays = (key: RetentionSettingKey, value: number | null) => {
    setRetentionSettings((current) => current ? { ...current, policies: { ...policiesOf(current), [key]: value } } : current);
  };

  const previewPolicyChange = useCallback(async () => {
    if (!retentionSettings || !savedRetentionSettings || !policyIsDestructive) return;
    const nextOperationId = policyOperationId ?? newOperationId("retention-policy");
    const attemptId = newOperationId("retention-preview");
    setPolicyOperationId(nextOperationId);
    setPolicyPreflightLoading(true);
    try {
      const preview = await api.settings.retention.preflight({
        kind: "policy_change",
        operation_id: nextOperationId,
        preflight_attempt_id: attemptId,
        expected_settings_revision: retentionSettings.revision,
        policies: policiesOf(retentionSettings),
      });
      setPolicyPreflight(preview);
      setPolicyConfirmationPhrase("");
      setPolicyConfirmOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.preflightFailed);
    } finally {
      setPolicyPreflightLoading(false);
    }
  }, [messages.settingsRetentionDeletion.preflightFailed, policyIsDestructive, policyOperationId, retentionSettings, savedRetentionSettings]);

  const submitRetentionSettings = useCallback(async () => {
    if (!retentionSettings || !savedRetentionSettings || !retentionSettingsDirty) return;
    if (policyIsDestructive && (!policyPreflight || !isPolicyPreflightValid || !policyOperationId)) return;
    setRetentionSettingsSaving(true);
    try {
      const response = await api.settings.retention.update({
        operation_id: policyOperationId ?? newOperationId("retention-policy"),
        expected_revision: savedRetentionSettings.revision,
        policies: policiesOf(retentionSettings),
        ...(policyIsDestructive && policyPreflight ? {
          preflight_token: policyPreflight.preflight_token,
          confirmation: { keyword: policyConfirmationPhrase.trim() },
        } : {}),
      });
      setSavedRetentionSettings(response.settings);
      setRetentionSettings(response.settings);
      setPolicyPreflight(null);
      setPolicyConfirmOpen(false);
      setPolicyConfirmationPhrase("");
      setPolicyOperationId(null);
      setRecentlySavedSection?.("retention");
      toast.success(messages.settingsRetentionDeletion.retentionUpdated);
      void fetchJobs(false);
    } catch (error) {
      setPolicyPreflight(null);
      setPolicyConfirmationPhrase("");
      if (isStalePreflightError(error)) {
        // Keep the confirmation surface open, discard the sealed capability
        // and refresh authoritative settings/coverage. A stale token is
        // never retried with the same body.
        setPolicyConfirmOpen(true);
        void fetchRetentionSettings();
      }
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.retentionUpdateFailed);
    } finally {
      setRetentionSettingsSaving(false);
    }
  }, [fetchJobs, fetchRetentionSettings, isPolicyPreflightValid, messages.settingsRetentionDeletion.retentionUpdateFailed, messages.settingsRetentionDeletion.retentionUpdated, policyConfirmationPhrase, policyIsDestructive, policyOperationId, policyPreflight, retentionSettings, retentionSettingsDirty, savedRetentionSettings, setRecentlySavedSection]);

  const handleSaveRetentionSettings = useCallback(async () => {
    if (!retentionSettings || !retentionSettingsDirty) return;
    if (policyIsDestructive && !policyPreflight) {
      await previewPolicyChange();
      return;
    }
    await submitRetentionSettings();
  }, [policyIsDestructive, policyPreflight, previewPolicyChange, retentionSettings, retentionSettingsDirty, submitRetentionSettings]);

  const handleOpenDeleteConfirm = useCallback(async () => {
    if (!cleanupType || !retentionPreset) return;
    const deleteAll = retentionPreset === "all";
    const days = deleteAll ? null : Number.parseInt(retentionPreset, 10);
    if (!deleteAll && ![1, 7, 30, 90].includes(days as number)) {
      toast.error(messages.settingsRetentionDeletion.invalidRetentionOption);
      return;
    }
    const next = { type: cleanupType, days, deleteAll } satisfies DeleteConfirmation;
    const nextOperationId = newOperationId("retention-manual");
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
        preflight_attempt_id: newOperationId("retention-preview"),
        dataset: CLEANUP_TABLES[cleanupType],
        selection: deleteAll ? { mode: "delete_all" } : { mode: "keep_days", days: days as 1 | 7 | 30 | 90 },
      });
      setManualPreflight(preview);
      setDeleteConfirmDialogOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.preflightFailed);
      setDeleteConfirmState(null);
    } finally {
      setManualPreflightLoading(false);
    }
  }, [cleanupType, messages.settingsRetentionDeletion.invalidRetentionOption, messages.settingsRetentionDeletion.preflightFailed, retentionPreset]);

  const handleBatchDelete = useCallback(async () => {
    if (!deleteConfirm || !manualPreflight || !manualOperationId || !isManualPreflightValid) return;
    setDeleting(true);
    try {
      const response = await api.settings.retention.createJob({
        operation_id: manualOperationId,
        preflight_token: manualPreflight.preflight_token,
        confirmation: { keyword: deleteConfirmPhrase.trim() },
      });
      toast.success(messages.settingsRetentionDeletion.deletionRequested(getCleanupTypeLabel(deleteConfirm.type), response.job.id));
      setDeleteConfirmDialogOpen(false);
      setDeleteConfirmState(null);
      setManualPreflight(null);
      setManualOperationId(null);
      setDeleteConfirmPhrase("");
      void fetchJobs(false);
    } catch (error) {
      if (isStalePreflightError(error)) {
        setManualPreflight(null);
        setDeleteConfirmPhrase("");
        setDeleteConfirmDialogOpen(true);
        void fetchRetentionSettings();
      }
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.deletionFailed);
    } finally {
      setDeleting(false);
    }
  }, [deleteConfirm, deleteConfirmPhrase, fetchJobs, fetchRetentionSettings, isManualPreflightValid, manualOperationId, manualPreflight, messages.settingsRetentionDeletion]);

  const setDeleteConfirm = (confirm: DeleteConfirmation | null) => {
    setDeleteConfirmState(confirm);
    setDeleteConfirmDialogOpen(Boolean(confirm));
    if (!confirm) {
      setManualPreflight(null);
      setManualOperationId(null);
      setDeleteConfirmPhrase("");
    }
  };

  const openJobDetail = useCallback(async (job: GlobalRetentionJobSummary) => {
    setSelectedJob(job);
    setJobDetailLoading(true);
    checkpointCursorRef.current = null;
    partitionCursorRef.current = null;
    try {
      const detail = await api.settings.retention.jobs.get(job.id);
      checkpointCursorRef.current = detail.checkpoints.next_cursor;
      partitionCursorRef.current = detail.partitions.next_cursor;
      setJobDetail(detail);
    } catch (error) {
      setJobDetail(null);
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.jobDetailFailed);
    } finally {
      setJobDetailLoading(false);
    }
  }, [messages.settingsRetentionDeletion.jobDetailFailed]);

  const loadJobEvidence = useCallback(async (kind: "checkpoints" | "partitions") => {
    if (!selectedJob) return;
    const cursor = kind === "checkpoints" ? checkpointCursorRef.current : partitionCursorRef.current;
    if (!cursor) return;
    setJobDetailLoading(true);
    try {
      if (kind === "checkpoints") {
        const page = await api.settings.retention.jobs.checkpoints(selectedJob.id, { cursor });
        checkpointCursorRef.current = page.next_cursor;
        setJobDetail((current) => current ? { ...current, checkpoints: { ...page, items: [...current.checkpoints.items, ...page.items] } } : current);
      } else {
        const page = await api.settings.retention.jobs.partitions(selectedJob.id, { cursor });
        partitionCursorRef.current = page.next_cursor;
        setJobDetail((current) => current ? { ...current, partitions: { ...page, items: [...current.partitions.items, ...page.items] } } : current);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.jobDetailFailed);
    } finally {
      setJobDetailLoading(false);
    }
  }, [messages.settingsRetentionDeletion.jobDetailFailed, selectedJob]);

  const handleCancelJob = useCallback(async (job: GlobalRetentionJobSummary) => {
    // Keep the same operation identity across a response-loss retry.  A new
    // cancellation intent is created only after the server has durably
    // returned the previous result.
    const operationId = cancelOperationIdsRef.current.get(job.id) ?? newOperationId("retention-cancel");
    cancelOperationIdsRef.current.set(job.id, operationId);
    try {
      const response = await api.settings.retention.jobs.cancel(job.id, operationId);
      cancelOperationIdsRef.current.delete(job.id);
      setJobs((current) => current.map((item) => item.id === response.job.id ? response.job : item));
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.settingsRetentionDeletion.jobCancelFailed);
    }
  }, [messages.settingsRetentionDeletion.jobCancelFailed]);

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
    handleSaveRetentionSettings,
    isDeletePhraseValid,
    isManualPreflightSemanticallyComplete,
    isManualPreflightValid,
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
    policyConfirmOpen,
    policyConfirmationPhrase,
    policyIsDestructive,
    policyPreflight,
    policyPreflightLoading,
    isPolicyPhraseValid,
    isPolicyPreflightSemanticallyComplete,
    isPolicyPreflightValid,
    setPolicyConfirmOpen,
    setPolicyConfirmationPhrase,
    submitRetentionSettings,
    applyRecommendation: () => {
      const recommendation = retentionSettings?.recommendations[0];
      if (!recommendation || !retentionSettings) return;
      setRetentionSettings({ ...retentionSettings, policies: { ...recommendation.policies } });
    },
    jobs,
    jobsHasMore,
    jobsLoading,
    jobsLoadedAt,
    jobsStale,
    refreshJobs: () => { void fetchJobs(false); },
    jobOriginFilter,
    jobStateFilter,
    selectedJob,
    jobDetail,
    jobDetailLoading,
    openJobDetail,
    setJobOriginFilter,
    setJobStateFilter,
    setSelectedJob,
    loadMoreJobs: () => { void fetchJobs(true); },
    loadMoreJobCheckpoints: () => { void loadJobEvidence("checkpoints"); },
    loadMoreJobPartitions: () => { void loadJobEvidence("partitions"); },
    handleCancelJob,
  };
}
