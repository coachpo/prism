import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  SidecarActionHistoryItem,
  SidecarAuthSnapshot,
  SidecarInstance,
  SidecarProviderSnapshot,
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
  const [sidecarDetailLoading, setSidecarDetailLoading] = useState(false);
  const [watchdogPolicySaving, setWatchdogPolicySaving] = useState(false);
  const [mutatingAuthKey, setMutatingAuthKey] = useState<string | null>(null);
  const detailRequestIdRef = useRef(0);

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
  }, []);

  const fetchSidecarDetail = useCallback(async (sidecarId: number) => {
    const messages = getStaticMessages();
    const requestId = detailRequestIdRef.current + 1;
    detailRequestIdRef.current = requestId;
    clearSidecarDetail();
    setSidecarDetailLoading(true);
    try {
      const [authResponse, providerResponse, policyResponse, actionsResponse] = await Promise.all([
        api.sidecars.authSnapshots(sidecarId),
        api.sidecars.providerSnapshots(sidecarId),
        api.sidecars.watchdogPolicy(sidecarId),
        api.sidecars.actionHistory(sidecarId),
      ]);
      if (detailRequestIdRef.current !== requestId) {
        return;
      }
      setAuthSnapshots(authResponse.items);
      setProviderSnapshots(providerResponse.items);
      setWatchdogPolicy(policyResponse);
      setActionHistory(actionsResponse.items);
    } catch (error) {
      if (detailRequestIdRef.current !== requestId) {
        return;
      }
      clearSidecarDetail();
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.loadSingleFailed);
    } finally {
      if (detailRequestIdRef.current === requestId) {
        setSidecarDetailLoading(false);
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
      }
    };
    const intervalId = window.setInterval(poll, POLL_INTERVAL_MS);
    const handleVisibilityChange = () => {
      if (!document.hidden) {
        void fetchSidecars({ silent: true });
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      window.clearInterval(intervalId);
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [fetchSidecars]);

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

  const handleSaveWatchdogPolicy = async (payload: SidecarWatchdogPolicyUpdate) => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    setWatchdogPolicySaving(true);
    try {
      const updated = await api.sidecars.updateWatchdogPolicy(selectedSidecarId, payload);
      setWatchdogPolicy(updated);
      toast.success(messages.sidecarsPage.watchdogSaveSucceeded);
      await fetchSidecarDetail(selectedSidecarId);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.saveFailed);
    } finally {
      setWatchdogPolicySaving(false);
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
    sidecarDetailLoading,
    watchdogPolicySaving,
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
    handlePatchAuthStatus,
    handlePatchAuthPriority,
    refreshSidecars: fetchSidecars,
    refreshSidecarDetail: fetchSidecarDetail,
  };
}
