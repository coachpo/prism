import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  SidecarActionHistoryItem,
  SidecarAuthQuotaState,
  SidecarAuthSnapshot,
  SidecarInstance,
  SidecarProviderSnapshot,
  SidecarQuotaScanRun,
  SidecarWatchdogPolicy,
  SidecarWatchdogPolicyUpdate,
} from "@/lib/types";
import {
  DEFAULT_SIDECAR_FORM,
  sidecarFormStateFromInstance,
  toSidecarCreatePayload,
  toSidecarUpdatePayload,
  type SidecarFormState,
} from "./sidecarFormState";

const POLL_INTERVAL_MS = 30_000;

type FetchOptions = {
  silent?: boolean;
};

type SidecarWatchdogPolicyFormUpdate = Omit<SidecarWatchdogPolicyUpdate, "expected_revision_id">;

function getWatchdogExpectedRevisionId(policy: SidecarWatchdogPolicy | null) {
  return policy?.pending_revision?.id ?? policy?.active_revision?.id ?? policy?.active_revision_id;
}

export function useSidecarsPageData() {
  const [sidecars, setSidecars] = useState<SidecarInstance[]>([]);
  const [sidecarsLoading, setSidecarsLoading] = useState(true);
  const [sidecarDialogOpen, setSidecarDialogOpen] = useState(false);
  const [editingSidecar, setEditingSidecar] = useState<SidecarInstance | null>(null);
  const [sidecarForm, setSidecarForm] = useState<SidecarFormState>(DEFAULT_SIDECAR_FORM);
  const [sidecarSaving, setSidecarSaving] = useState(false);
  const [preparingEditSidecarId, setPreparingEditSidecarId] = useState<number | null>(null);
  const [deleteSidecarConfirm, setDeleteSidecarConfirm] = useState<SidecarInstance | null>(null);
  const [deleteSidecarDialogOpen, setDeleteSidecarDialogOpen] = useState(false);
  const [displayedDeleteSidecarConfirm, setDisplayedDeleteSidecarConfirm] = useState<SidecarInstance | null>(null);
  const [sidecarDeleting, setSidecarDeleting] = useState(false);
  const [testingSidecarId, setTestingSidecarId] = useState<number | null>(null);
  const [syncingSidecarId, setSyncingSidecarId] = useState<number | null>(null);
  const [selectedSidecarId, setSelectedSidecarId] = useState<number | null>(null);
  const [authSnapshots, setAuthSnapshots] = useState<SidecarAuthSnapshot[]>([]);
  const [providerSnapshots, setProviderSnapshots] = useState<SidecarProviderSnapshot[]>([]);
  const [watchdogPolicy, setWatchdogPolicy] = useState<SidecarWatchdogPolicy | null>(null);
  const [actionHistory, setActionHistory] = useState<SidecarActionHistoryItem[]>([]);
  const [quotaStates, setQuotaStates] = useState<SidecarAuthQuotaState[]>([]);
  const [quotaScans, setQuotaScans] = useState<SidecarQuotaScanRun[]>([]);
  const [sidecarDetailLoading, setSidecarDetailLoading] = useState(false);
  const [watchdogPolicySaving, setWatchdogPolicySaving] = useState(false);
  const [watchdogPolicyApplying, setWatchdogPolicyApplying] = useState(false);
  const [quotaScanMutating, setQuotaScanMutating] = useState<"start" | "cancel" | null>(null);
  const [mutatingAuthKey, setMutatingAuthKey] = useState<string | null>(null);
  const detailRequestIdRef = useRef(0);
  const detailInFlightRef = useRef<Promise<void> | null>(null);

  const sortedSidecars = useMemo(
    () => [...sidecars].sort((left, right) => left.name.localeCompare(right.name)),
    [sidecars],
  );
  const selectedSidecar = useMemo(
    () => sortedSidecars.find((sidecar) => sidecar.id === selectedSidecarId) ?? null,
    [selectedSidecarId, sortedSidecars],
  );

  const clearSidecarDetail = useCallback(() => {
    setAuthSnapshots([]);
    setProviderSnapshots([]);
    setWatchdogPolicy(null);
    setActionHistory([]);
    setQuotaStates([]);
    setQuotaScans([]);
  }, []);

  const fetchSidecarDetail = useCallback(async (sidecarId: number, options: FetchOptions = {}) => {
    const messages = getStaticMessages();
    if (options.silent && detailInFlightRef.current) {
      return;
    }
    const requestId = detailRequestIdRef.current + 1;
    detailRequestIdRef.current = requestId;
    if (!options.silent) {
      clearSidecarDetail();
      setSidecarDetailLoading(true);
    }

    if (detailInFlightRef.current) {
      await detailInFlightRef.current;
      if (detailRequestIdRef.current !== requestId) {
        return;
      }
    }

    const loadPromise = (async () => {
      const isCurrentRequest = () => detailRequestIdRef.current === requestId;
      try {
        const authResponse = await api.sidecars.authSnapshots(sidecarId);
        if (!isCurrentRequest()) return;
        const providerResponse = await api.sidecars.providerSnapshots(sidecarId);
        if (!isCurrentRequest()) return;
        const policyResponse = await api.sidecars.watchdogPolicy(sidecarId);
        if (!isCurrentRequest()) return;
        const actionsResponse = await api.sidecars.actionHistory(sidecarId);
        if (!isCurrentRequest()) return;
        const quotaStateResponse = await api.sidecars.quotaStates(sidecarId);
        if (!isCurrentRequest()) return;
        const quotaScanResponse = await api.sidecars.quotaScans(sidecarId);
        if (!isCurrentRequest()) return;
        setAuthSnapshots(authResponse.items);
        setProviderSnapshots(providerResponse.items);
        setWatchdogPolicy(policyResponse);
        setActionHistory(actionsResponse.items);
        setQuotaStates(quotaStateResponse.items);
        setQuotaScans(quotaScanResponse.items);
      } catch (error) {
        if (!isCurrentRequest()) {
          return;
        }
        if (!options.silent) {
          clearSidecarDetail();
          toast.error(error instanceof Error ? error.message : messages.sidecarsPage.loadSingleFailed);
        }
      } finally {
        if (isCurrentRequest() && !options.silent) {
          setSidecarDetailLoading(false);
        }
      }
    })();

    detailInFlightRef.current = loadPromise;
    try {
      await loadPromise;
    } finally {
      if (detailInFlightRef.current === loadPromise) {
        detailInFlightRef.current = null;
      }
    }
  }, [clearSidecarDetail]);

  const fetchSidecars = useCallback(async (options: FetchOptions = {}) => {
    const messages = getStaticMessages();
    if (!options.silent) {
      setSidecarsLoading(true);
    }
    try {
      const response = await api.sidecars.list();
      setSidecars(response.items);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.loadFailed);
    } finally {
      if (!options.silent) {
        setSidecarsLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    void fetchSidecars();
  }, [fetchSidecars]);

  useEffect(() => {
    setSelectedSidecarId((current) => {
      if (sortedSidecars.length === 0) {
        return null;
      }
      if (current !== null && sortedSidecars.some((sidecar) => sidecar.id === current)) {
        return current;
      }
      return sortedSidecars[0].id;
    });
  }, [sortedSidecars]);

  useEffect(() => {
    if (selectedSidecarId === null) {
      detailRequestIdRef.current += 1;
      clearSidecarDetail();
      setSidecarDetailLoading(false);
      return;
    }
    void fetchSidecarDetail(selectedSidecarId);
  }, [clearSidecarDetail, fetchSidecarDetail, selectedSidecarId]);

  useEffect(() => {
    const poll = () => {
      if (typeof document === "undefined" || !document.hidden) {
        void fetchSidecars({ silent: true });
        if (selectedSidecarId !== null) {
          void fetchSidecarDetail(selectedSidecarId, { silent: true });
        }
      }
    };
    const intervalId = window.setInterval(poll, POLL_INTERVAL_MS);
    const handleVisibilityChange = () => {
      if (!document.hidden) {
        void fetchSidecars({ silent: true });
        if (selectedSidecarId !== null) {
          void fetchSidecarDetail(selectedSidecarId, { silent: true });
        }
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      window.clearInterval(intervalId);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [fetchSidecarDetail, fetchSidecars, selectedSidecarId]);

  const openCreateSidecarDialog = () => {
    setEditingSidecar(null);
    setSidecarForm(DEFAULT_SIDECAR_FORM);
    setSidecarDialogOpen(true);
  };

  const closeSidecarDialog = () => {
    setSidecarDialogOpen(false);
  };

  const handleEditSidecar = async (sidecarSummary: SidecarInstance) => {
    const messages = getStaticMessages();
    setPreparingEditSidecarId(sidecarSummary.id);
    try {
      const sidecar = await api.sidecars.get(sidecarSummary.id);
      setEditingSidecar(sidecar);
      setSidecarForm(sidecarFormStateFromInstance(sidecar));
      setSidecarDialogOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.loadSingleFailed);
    } finally {
      setPreparingEditSidecarId(null);
    }
  };

  const handleSaveSidecar = async () => {
    const messages = getStaticMessages();
    setSidecarSaving(true);
    try {
      if (editingSidecar) {
        const updated = await api.sidecars.update(editingSidecar.id, toSidecarUpdatePayload(sidecarForm, messages.sidecarsPage));
        setSidecars((current) => current.map((sidecar) => (sidecar.id === updated.id ? updated : sidecar)));
        toast.success(messages.sidecarsPage.updateSucceeded(updated.name));
      } else {
        const created = await api.sidecars.create(toSidecarCreatePayload(sidecarForm, messages.sidecarsPage));
        setSidecars((current) => [...current, created]);
        toast.success(messages.sidecarsPage.createSucceeded(created.name));
      }
      setSidecarDialogOpen(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.saveFailed);
    } finally {
      setSidecarSaving(false);
    }
  };

  const openDeleteSidecarDialog = (sidecar: SidecarInstance) => {
    setDeleteSidecarConfirm(sidecar);
    setDisplayedDeleteSidecarConfirm(sidecar);
    setDeleteSidecarDialogOpen(true);
  };

  const closeDeleteSidecarDialog = () => {
    setDeleteSidecarDialogOpen(false);
    setDeleteSidecarConfirm(null);
  };

  const handleDeleteSidecar = async () => {
    const messages = getStaticMessages();
    if (!deleteSidecarConfirm) {
      return;
    }
    setSidecarDeleting(true);
    try {
      await api.sidecars.delete(deleteSidecarConfirm.id);
      setSidecars((current) => current.filter((sidecar) => sidecar.id !== deleteSidecarConfirm.id));
      toast.success(messages.sidecarsPage.deleteSucceeded(deleteSidecarConfirm.name));
      setDeleteSidecarDialogOpen(false);
      setDeleteSidecarConfirm(null);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.deleteFailed);
    } finally {
      setSidecarDeleting(false);
    }
  };

  const handleTestConnection = async (sidecar: SidecarInstance) => {
    const messages = getStaticMessages();
    setTestingSidecarId(sidecar.id);
    try {
      const result = await api.sidecars.testConnection(sidecar.id);
      toast.success(messages.sidecarsPage.testSucceeded(sidecar.name, result.status_code));
      await fetchSidecars({ silent: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.testFailed);
    } finally {
      setTestingSidecarId(null);
    }
  };

  const handleManualSync = async (sidecar: SidecarInstance) => {
    const messages = getStaticMessages();
    setSyncingSidecarId(sidecar.id);
    try {
      await api.sidecars.sync(sidecar.id);
      toast.success(messages.sidecarsPage.syncAccepted(sidecar.name));
      await fetchSidecars({ silent: true });
      if (selectedSidecarId === sidecar.id) {
        await fetchSidecarDetail(sidecar.id);
      }
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.syncFailed);
    } finally {
      setSyncingSidecarId(null);
    }
  };

  const handleSelectSidecar = (sidecarId: number) => {
    setSelectedSidecarId(sidecarId);
  };

  const handleSaveWatchdogPolicy = async (payload: SidecarWatchdogPolicyFormUpdate) => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    const expectedRevisionId = getWatchdogExpectedRevisionId(watchdogPolicy);
    if (!expectedRevisionId) {
      toast.error(messages.sidecarsPage.saveFailed);
      return;
    }
    setWatchdogPolicySaving(true);
    try {
      const updated = await api.sidecars.updateWatchdogPolicy(selectedSidecarId, {
        ...payload,
        expected_revision_id: expectedRevisionId,
      });
      setWatchdogPolicy(updated);
      toast.success(messages.sidecarsPage.watchdogSaveSucceeded);
      await fetchSidecarDetail(selectedSidecarId);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.saveFailed);
    } finally {
      setWatchdogPolicySaving(false);
    }
  };

  const handleApplyWatchdogPolicy = async () => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null || !watchdogPolicy?.pending_revision || !watchdogPolicy.active_revision) {
      return;
    }
    setWatchdogPolicyApplying(true);
    try {
      const updated = await api.sidecars.applyWatchdogPolicy(selectedSidecarId, {
        target_revision_id: watchdogPolicy.pending_revision.id,
        expected_revision_id: watchdogPolicy.active_revision.id,
      });
      setWatchdogPolicy(updated);
      toast.success(messages.sidecarsPage.watchdogApplySucceeded);
      await fetchSidecarDetail(selectedSidecarId);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.watchdogApplyFailed);
    } finally {
      setWatchdogPolicyApplying(false);
    }
  };

  const handleStartQuotaScan = async () => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    setQuotaScanMutating("start");
    try {
      const scan = await api.sidecars.startQuotaScan(selectedSidecarId, { replace_active: false });
      setQuotaScans((current) => [scan, ...current.filter((item) => item.id !== scan.id)]);
      toast.success(messages.sidecarsPage.quotaScanStartSucceeded(selectedSidecar?.name ?? String(selectedSidecarId)));
      await fetchSidecarDetail(selectedSidecarId, { silent: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.quotaScanStartFailed);
    } finally {
      setQuotaScanMutating(null);
    }
  };

  const handleCancelQuotaScan = async (scan: SidecarQuotaScanRun) => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    setQuotaScanMutating("cancel");
    try {
      const cancelled = await api.sidecars.cancelQuotaScan(selectedSidecarId, scan.id);
      setQuotaScans((current) => current.map((item) => (item.id === cancelled.id ? cancelled : item)));
      toast.success(messages.sidecarsPage.quotaScanCancelSucceeded);
      await fetchSidecarDetail(selectedSidecarId, { silent: true });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.quotaScanCancelFailed);
    } finally {
      setQuotaScanMutating(null);
    }
  };

  const handlePatchAuthStatus = async (snapshot: SidecarAuthSnapshot, disabled: boolean, allowWatchdog: boolean) => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    setMutatingAuthKey(snapshot.auth_id);
    try {
      await api.sidecars.updateAuthFileStatus(selectedSidecarId, snapshot.auth_id, { disabled, allow_watchdog: allowWatchdog });
      toast.success(messages.sidecarsPage.authStatusUpdated(snapshot.name, disabled));
      await fetchSidecarDetail(selectedSidecarId);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.saveFailed);
    } finally {
      setMutatingAuthKey(null);
    }
  };

  const handlePatchAuthPriority = async (snapshot: SidecarAuthSnapshot, priority: number, allowWatchdog: boolean) => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    setMutatingAuthKey(snapshot.auth_id);
    try {
      await api.sidecars.updateAuthFileFields(selectedSidecarId, snapshot.auth_id, { priority, allow_watchdog: allowWatchdog });
      toast.success(messages.sidecarsPage.authPriorityUpdated(snapshot.name));
      await fetchSidecarDetail(selectedSidecarId);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.saveFailed);
    } finally {
      setMutatingAuthKey(null);
    }
  };

  return {
    sidecars: sortedSidecars,
    sidecarsLoading,
    sidecarDialogOpen,
    editingSidecar,
    sidecarForm,
    sidecarSaving,
    setSidecarForm,
    preparingEditSidecarId,
    deleteSidecarConfirm,
    deleteSidecarDialogOpen,
    displayedDeleteSidecarConfirm,
    sidecarDeleting,
    testingSidecarId,
    syncingSidecarId,
    selectedSidecarId,
    selectedSidecar,
    authSnapshots,
    providerSnapshots,
    watchdogPolicy,
    actionHistory,
    quotaStates,
    quotaScans,
    sidecarDetailLoading,
    watchdogPolicySaving,
    watchdogPolicyApplying,
    quotaScanMutating,
    mutatingAuthKey,
    setSelectedSidecarId: handleSelectSidecar,
    openCreateSidecarDialog,
    closeSidecarDialog,
    handleEditSidecar,
    handleSaveSidecar,
    openDeleteSidecarDialog,
    closeDeleteSidecarDialog,
    handleDeleteSidecar,
    handleTestConnection,
    handleManualSync,
    handleSaveWatchdogPolicy,
    handleApplyWatchdogPolicy,
    handleStartQuotaScan,
    handleCancelQuotaScan,
    handlePatchAuthStatus,
    handlePatchAuthPriority,
    refreshSidecars: fetchSidecars,
    refreshSidecarDetail: fetchSidecarDetail,
  };
}
