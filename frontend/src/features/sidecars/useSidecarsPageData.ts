import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { ApiError, api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  SidecarAuthModelsResponse,
  SidecarAuthFile,
  SidecarInstance,
  SidecarProviderSnapshot,
  SidecarSyncResponse,
} from "@/lib/types";
import { extractServerValidation, fieldErrorsFromServerValidation } from "@/shared/forms/serverValidation";
import {
  DEFAULT_SIDECAR_FORM,
  firstSidecarFormError,
  sidecarFormStateFromInstance,
  toSidecarCreatePayload,
  toSidecarUpdatePayload,
  validateSidecarForm,
  type SidecarFormErrors,
  type SidecarFormState,
} from "./sidecarFormState";
import type { AuthMutationNotice } from "./AuthFilesTable";

const POLL_INTERVAL_MS = 30_000;

type FetchOptions = {
  silent?: boolean;
};

function mutationErrorDetail(error: unknown, fallback: string) {
  if (error instanceof ApiError) {
    if (typeof error.detail === "object" && error.detail !== null) {
      const detail = (error.detail as { detail?: unknown }).detail;
      if (typeof detail === "string" && detail.trim().length > 0) {
        return detail;
      }
    }
    return error.message || fallback;
  }
  return error instanceof Error ? error.message : fallback;
}

function isSidecarSyncResponse(value: unknown): value is SidecarSyncResponse {
  return typeof value === "object" && value !== null && typeof (value as { state?: unknown }).state === "string";
}

function syncResponseFromError(error: unknown): SidecarSyncResponse | null {
  if (!(error instanceof ApiError)) {
    return null;
  }
  return isSidecarSyncResponse(error.detail) ? error.detail : null;
}

function syncFailureDetail(response: SidecarSyncResponse, fallback: string) {
  return response.error_detail ?? response.sync_status?.last_sync_error ?? fallback;
}

function authMutationFailureDetail(error: unknown, fallback: string) {
  const detail = mutationErrorDetail(error, fallback).trim();
  if (!(error instanceof ApiError)) {
    return detail || fallback;
  }

  if (error.status === 404 || /auth file not found in live sidecar state/i.test(detail)) {
    return "Live auth file was not found in the current sidecar state. Prism refreshed the live auth-file list; choose a current row before retrying.";
  }

  if (error.status === 409 && detail === "stale_auth_confirmation") {
    return "The live auth-file name changed before delete confirmation. Prism refreshed the live auth-file list; confirm the current name before retrying.";
  }

  if (error.status === 409 && detail.startsWith("unsafe_auth_identity")) {
    const reason = detail.split(":").slice(1).join(":").trim();
    return reason
      ? `Prism blocked this row because its live auth identity is unsafe: ${reason}.`
      : "Prism blocked this row because its live auth identity is unsafe.";
  }

  if ([424, 502, 503, 504].includes(error.status) || /sidecar mutation failed|sidecar management|upstream/i.test(detail)) {
    return `Sidecar upstream mutation failed: ${detail || fallback}`;
  }

  return detail || fallback;
}

function authMutationSyncWarning(
  response: { state?: string; sync_error?: string | null; sync_status?: { last_sync_error?: string } },
  fallback: string,
) {
  if (response.state !== "succeeded_sync_failed") {
    return null;
  }
  const detail = response.sync_error ?? response.sync_status?.last_sync_error ?? fallback;
  return getStaticMessages().sidecarsPage.syncFailedWithDetail(detail);
}

export function useSidecarsPageData() {
  const [sidecars, setSidecars] = useState<SidecarInstance[]>([]);
  const [sidecarsLoading, setSidecarsLoading] = useState(true);
  const [sidecarDialogOpen, setSidecarDialogOpen] = useState(false);
  const [editingSidecar, setEditingSidecar] = useState<SidecarInstance | null>(null);
  const [sidecarForm, setSidecarForm] = useState<SidecarFormState>(DEFAULT_SIDECAR_FORM);
  const [sidecarFormErrors, setSidecarFormErrors] = useState<SidecarFormErrors>({});
  const [sidecarServerError, setSidecarServerError] = useState<string | null>(null);
  const [sidecarSaving, setSidecarSaving] = useState(false);
  const [preparingEditSidecarId, setPreparingEditSidecarId] = useState<number | null>(null);
  const [deleteSidecarConfirm, setDeleteSidecarConfirm] = useState<SidecarInstance | null>(null);
  const [deleteSidecarDialogOpen, setDeleteSidecarDialogOpen] = useState(false);
  const [displayedDeleteSidecarConfirm, setDisplayedDeleteSidecarConfirm] = useState<SidecarInstance | null>(null);
  const [sidecarDeleting, setSidecarDeleting] = useState(false);
  const [testingSidecarId, setTestingSidecarId] = useState<number | null>(null);
  const [syncingSidecarId, setSyncingSidecarId] = useState<number | null>(null);
  const [selectedSidecarId, setSelectedSidecarId] = useState<number | null>(null);
  const [authFiles, setAuthFiles] = useState<SidecarAuthFile[]>([]);
  const [providerSnapshots, setProviderSnapshots] = useState<SidecarProviderSnapshot[]>([]);
  const [sidecarDetailLoading, setSidecarDetailLoading] = useState(false);
  const [sidecarDetailRefreshError, setSidecarDetailRefreshError] = useState<string | null>(null);
  const [mutatingAuthKey, setMutatingAuthKey] = useState<string | null>(null);
  const [authMutationNotices, setAuthMutationNotices] = useState<Record<string, AuthMutationNotice | undefined>>({});
  const detailRequestIdRef = useRef(0);
  const detailInFlightRef = useRef<Promise<void> | null>(null);
  const loadedDetailSidecarIdRef = useRef<number | null>(null);

  const sortedSidecars = useMemo(
    () => [...sidecars].sort((left, right) => left.name.localeCompare(right.name)),
    [sidecars],
  );
  const selectedSidecar = useMemo(
    () => sortedSidecars.find((sidecar) => sidecar.id === selectedSidecarId) ?? null,
    [selectedSidecarId, sortedSidecars],
  );

  const clearSidecarDetail = useCallback(() => {
    loadedDetailSidecarIdRef.current = null;
    setAuthFiles([]);
    setProviderSnapshots([]);
    setSidecarDetailRefreshError(null);
    setAuthMutationNotices({});
  }, []);

  const refreshLiveAuthFiles = useCallback(async (sidecarId: number) => {
    const authResponse = await api.sidecars.authFiles(sidecarId);
    setAuthFiles(authResponse.items);
    loadedDetailSidecarIdRef.current = sidecarId;
    setSidecarDetailRefreshError(null);
    return authResponse.items;
  }, []);

  const fetchSidecarDetail = useCallback(async (sidecarId: number, options: FetchOptions = {}) => {
    const messages = getStaticMessages();
    if (options.silent && detailInFlightRef.current) {
      return;
    }
    const requestId = detailRequestIdRef.current + 1;
    detailRequestIdRef.current = requestId;
    if (!options.silent) {
      if (loadedDetailSidecarIdRef.current !== sidecarId) {
        clearSidecarDetail();
      }
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
        const authResponse = await api.sidecars.authFiles(sidecarId);
        if (!isCurrentRequest()) return;
        const providerResponse = await api.sidecars.providerSnapshots(sidecarId);
        if (!isCurrentRequest()) return;
        setAuthFiles(authResponse.items);
        setProviderSnapshots(providerResponse.items);
        loadedDetailSidecarIdRef.current = sidecarId;
        setSidecarDetailRefreshError(null);
      } catch (error) {
        if (!isCurrentRequest()) {
          return;
        }
        if (!options.silent) {
          setSidecarDetailRefreshError(messages.sidecarsPage.loadSingleFailed);
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
    setSidecarFormErrors({});
    setSidecarServerError(null);
    setSidecarDialogOpen(true);
  };

  const closeSidecarDialog = () => {
    setSidecarDialogOpen(false);
    setSidecarFormErrors({});
    setSidecarServerError(null);
  };

  const handleEditSidecar = async (sidecarSummary: SidecarInstance) => {
    const messages = getStaticMessages();
    setPreparingEditSidecarId(sidecarSummary.id);
    try {
      const sidecar = await api.sidecars.get(sidecarSummary.id);
      setEditingSidecar(sidecar);
      setSidecarForm(sidecarFormStateFromInstance(sidecar));
      setSidecarFormErrors({});
      setSidecarServerError(null);
      setSidecarDialogOpen(true);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.sidecarsPage.loadSingleFailed);
    } finally {
      setPreparingEditSidecarId(null);
    }
  };

  const handleSaveSidecar = async () => {
    const messages = getStaticMessages();
    const validation = validateSidecarForm(sidecarForm, { editing: editingSidecar !== null });
    setSidecarFormErrors(validation.errors);
    setSidecarServerError(null);
    if (!validation.values) {
      toast.error(firstSidecarFormError(validation.errors, messages.sidecarsPage));
      return;
    }

    setSidecarSaving(true);
    try {
      if (editingSidecar) {
        const updated = await api.sidecars.update(editingSidecar.id, toSidecarUpdatePayload(validation.values, messages.sidecarsPage));
        setSidecars((current) => current.map((sidecar) => (sidecar.id === updated.id ? updated : sidecar)));
        toast.success(messages.sidecarsPage.updateSucceeded(updated.name));
      } else {
        const created = await api.sidecars.create(toSidecarCreatePayload(validation.values, messages.sidecarsPage));
        setSidecars((current) => [...current, created]);
        toast.success(messages.sidecarsPage.createSucceeded(created.name));
      }
      setSidecarDialogOpen(false);
    } catch (error) {
      const validationError = extractServerValidation(error, messages.sidecarsPage.saveFailed);
      const serverFieldErrors = fieldErrorsFromServerValidation(validationError, ["name", "base_url", "environment_label", "management_password", "sync_interval_seconds", "request_timeout_seconds"] as const);
      setSidecarFormErrors((current) => ({ ...current, ...serverFieldErrors }));
      setSidecarServerError(validationError.summary);
      toast.error(validationError.summary);
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
    const applySyncResponse = (response: SidecarSyncResponse) => {
      if (response.sync_status) {
        setSidecars((current) => current.map((item) => item.id === sidecar.id ? { ...item, ...response.sync_status } : item));
      }
    };
    setSyncingSidecarId(sidecar.id);
    try {
      const response = await api.sidecars.sync(sidecar.id);
      applySyncResponse(response);
      await fetchSidecars({ silent: true });
      if (response.state !== "succeeded") {
        const detail = syncFailureDetail(response, messages.sidecarsPage.syncFailed);
        const message = messages.sidecarsPage.syncFailedWithDetail(detail);
        setSidecarDetailRefreshError(message);
        toast.warning(message);
        return;
      }
      if (selectedSidecarId === sidecar.id) {
        await fetchSidecarDetail(sidecar.id);
      }
      toast.success(messages.sidecarsPage.syncAccepted(sidecar.name));
    } catch (error) {
      const response = syncResponseFromError(error);
      if (response) {
        applySyncResponse(response);
        const detail = syncFailureDetail(response, messages.sidecarsPage.syncFailed);
        const message = messages.sidecarsPage.syncFailedWithDetail(detail);
        setSidecarDetailRefreshError(message);
        toast.error(message);
      } else {
        toast.error(error instanceof Error ? error.message : messages.sidecarsPage.syncFailed);
      }
    } finally {
      setSyncingSidecarId(null);
    }
  };

  const handleSelectSidecar = (sidecarId: number) => {
    setSelectedSidecarId(sidecarId);
  };

  const setAuthMutationNotice = (authId: string, notice: AuthMutationNotice | undefined) => {
    setAuthMutationNotices((current) => ({ ...current, [authId]: notice }));
  };

  const refreshLiveAuthFilesAfterMutation = async (
    sidecarId: number,
    authId: string,
    refreshWarning: (detail: string) => string,
    options: { notice?: boolean } = {},
  ) => {
    const messages = getStaticMessages();
    try {
      await refreshLiveAuthFiles(sidecarId);
      return true;
    } catch (error) {
      const detail = mutationErrorDetail(error, messages.sidecarsPage.loadSingleFailed);
      const message = refreshWarning(detail);
      setSidecarDetailRefreshError(message);
      if (options.notice !== false) {
        setAuthMutationNotice(authId, { kind: "refresh_failed", message });
        toast.warning(message);
      }
      return false;
    }
  };

  const applyAuthMutationSyncStatus = (sidecarId: number, syncStatus: Awaited<ReturnType<typeof api.sidecars.updateAuthFileStatus>>["sync_status"], syncError: string | null | undefined) => {
    if (!syncStatus) {
      return;
    }
    setSidecars((current) => current.map((item) => item.id === sidecarId ? { ...item, ...syncStatus, last_sync_error: syncStatus.last_sync_error ?? syncError ?? item.last_sync_error } : item));
  };

  const handleLoadAuthModels = async (snapshot: SidecarAuthFile): Promise<SidecarAuthModelsResponse> => {
    if (selectedSidecarId === null) {
      return { models: [] };
    }
    return api.sidecars.authFileModels(selectedSidecarId, snapshot.name);
  };

  const handleDeleteAuthFile = async (snapshot: SidecarAuthFile, confirmName: string): Promise<string | null> => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return messages.sidecarsPage.loadSingleFailed;
    }
    setMutatingAuthKey(snapshot.auth_id);
    setAuthMutationNotice(snapshot.auth_id, undefined);
    try {
      const response = await api.sidecars.deleteAuthFile(selectedSidecarId, snapshot.auth_id, { confirm_name: confirmName });
      applyAuthMutationSyncStatus(selectedSidecarId, response.sync_status, response.sync_error);
      const syncWarning = authMutationSyncWarning(response, messages.sidecarsPage.syncFailed);
      const refreshed = await refreshLiveAuthFilesAfterMutation(
        selectedSidecarId,
        snapshot.auth_id,
        messages.sidecarsPage.authDeleteRefreshWarning,
      );
      if (!refreshed) {
        return null;
      }
      if (syncWarning) {
        setSidecarDetailRefreshError(syncWarning);
        setAuthMutationNotice(snapshot.auth_id, { kind: "refresh_failed", message: syncWarning });
        toast.warning(syncWarning);
      } else {
        setAuthMutationNotice(snapshot.auth_id, undefined);
        toast.success(messages.sidecarsPage.authDeleteSucceeded(snapshot.name));
      }
      return null;
    } catch (error) {
      const message = messages.sidecarsPage.authDeleteFailed(authMutationFailureDetail(error, messages.sidecarsPage.saveFailed));
      setAuthMutationNotice(snapshot.auth_id, { kind: "failed", message });
      toast.error(message);
      await refreshLiveAuthFilesAfterMutation(
        selectedSidecarId,
        snapshot.auth_id,
        messages.sidecarsPage.authDeleteRefreshWarning,
        { notice: false },
      );
      return message;
    } finally {
      setMutatingAuthKey(null);
    }
  };

  const handlePatchAuthPriority = async (snapshot: SidecarAuthFile, priority: number) => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    setMutatingAuthKey(snapshot.auth_id);
    setAuthMutationNotice(snapshot.auth_id, undefined);
    try {
      const response = await api.sidecars.updateAuthFilePriority(selectedSidecarId, snapshot.auth_id, {
        priority,
      });
      applyAuthMutationSyncStatus(selectedSidecarId, response.sync_status, response.sync_error);
      const syncWarning = authMutationSyncWarning(response, messages.sidecarsPage.syncFailed);
      const refreshed = await refreshLiveAuthFilesAfterMutation(
        selectedSidecarId,
        snapshot.auth_id,
        messages.sidecarsPage.authPriorityRefreshWarning,
      );
      if (!refreshed) {
        return;
      }
      if (syncWarning) {
        setSidecarDetailRefreshError(syncWarning);
        setAuthMutationNotice(snapshot.auth_id, { kind: "refresh_failed", message: syncWarning });
        toast.warning(syncWarning);
      } else {
        toast.success(messages.sidecarsPage.authPriorityUpdated(snapshot.name));
      }
    } catch (error) {
      const message = authMutationFailureDetail(error, messages.sidecarsPage.saveFailed);
      setAuthMutationNotice(snapshot.auth_id, { kind: "failed", message });
      toast.error(message);
      await refreshLiveAuthFilesAfterMutation(
        selectedSidecarId,
        snapshot.auth_id,
        messages.sidecarsPage.authPriorityRefreshWarning,
        { notice: false },
      );
    } finally {
      setMutatingAuthKey(null);
    }
  };

  const handlePatchAuthStatus = async (snapshot: SidecarAuthFile, disabled: boolean) => {
    const messages = getStaticMessages();
    if (selectedSidecarId === null) {
      return;
    }
    setMutatingAuthKey(snapshot.auth_id);
    setAuthMutationNotice(snapshot.auth_id, undefined);
    try {
      const response = await api.sidecars.updateAuthFileStatus(selectedSidecarId, snapshot.auth_id, {
        disabled,
      });
      applyAuthMutationSyncStatus(selectedSidecarId, response.sync_status, response.sync_error);
      const syncWarning = authMutationSyncWarning(response, messages.sidecarsPage.syncFailed);
      const refreshed = await refreshLiveAuthFilesAfterMutation(
        selectedSidecarId,
        snapshot.auth_id,
        messages.sidecarsPage.authStatusRefreshWarning,
      );
      if (!refreshed) {
        return;
      }
      if (syncWarning) {
        setSidecarDetailRefreshError(syncWarning);
        setAuthMutationNotice(snapshot.auth_id, { kind: "refresh_failed", message: syncWarning });
        toast.warning(syncWarning);
      } else {
        setAuthMutationNotice(snapshot.auth_id, { kind: "success", message: messages.sidecarsPage.authStatusUpdateApplied });
        toast.success(messages.sidecarsPage.authStatusUpdated(snapshot.name, disabled));
      }
    } catch (error) {
      const message = messages.sidecarsPage.authStatusUpdateFailed(authMutationFailureDetail(error, messages.sidecarsPage.saveFailed));
      setAuthMutationNotice(snapshot.auth_id, { kind: "failed", message });
      toast.error(message);
      await refreshLiveAuthFilesAfterMutation(
        selectedSidecarId,
        snapshot.auth_id,
        messages.sidecarsPage.authStatusRefreshWarning,
        { notice: false },
      );
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
    sidecarFormErrors,
    sidecarServerError,
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
    authFiles,
    providerSnapshots,
    sidecarDetailLoading,
    sidecarDetailRefreshError,
    mutatingAuthKey,
    authMutationNotices,
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
    handleDeleteAuthFile,
    handleLoadAuthModels,
    handlePatchAuthPriority,
    handlePatchAuthStatus,
    refreshSidecars: fetchSidecars,
    refreshSidecarDetail: fetchSidecarDetail,
  };
}
