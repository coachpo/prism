import type { ComponentProps } from "react";
import { useEffect, useMemo, useState } from "react";
import { toast } from "sonner";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { AuthSettings, ProxyApiKey, ProxyApiKeyUpdate } from "@/lib/types";
import {
  getAuthStatusTone,
  normalizeExpiresAtInput,
  toDateTimeLocalValue,
} from "./proxyKeyFormatting";

type FormSubmitEvent = Parameters<NonNullable<ComponentProps<"form">["onSubmit"]>>[0];

export function useProxyApiKeysPageData() {
  const [authSettings, setAuthSettings] = useState<AuthSettings | null>(null);
  const [proxyKeys, setProxyKeys] = useState<ProxyApiKey[]>([]);
  const [proxyKeyName, setProxyKeyName] = useState("");
  const [proxyKeyNotes, setProxyKeyNotes] = useState("");
  const [proxyKeyExpiresAt, setProxyKeyExpiresAt] = useState("");
  const [creatingProxyKey, setCreatingProxyKey] = useState(false);
  const [pageLoading, setPageLoading] = useState(true);
  const [rotatingProxyKeyId, setRotatingProxyKeyId] = useState<number | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState<ProxyApiKey | null>(null);
  const [deleteProxyKeyAlertOpen, setDeleteProxyKeyAlertOpen] = useState(false);
  const [displayedDeleteConfirm, setDisplayedDeleteConfirm] = useState<ProxyApiKey | null>(null);
  const [deletingProxyKeyId, setDeletingProxyKeyId] = useState<number | null>(null);
  const [editingProxyKey, setEditingProxyKey] = useState<ProxyApiKey | null>(null);
  const [editProxyKeySheetOpen, setEditProxyKeySheetOpen] = useState(false);
  const [editingProxyKeyName, setEditingProxyKeyName] = useState("");
  const [editingProxyKeyNotes, setEditingProxyKeyNotes] = useState("");
  const [editingProxyKeyExpiresAt, setEditingProxyKeyExpiresAt] = useState("");
  const [editingProxyKeyActive, setEditingProxyKeyActive] = useState(false);
  const [savingEditedProxyKeyId, setSavingEditedProxyKeyId] = useState<number | null>(null);
  const [latestGeneratedKeyState, setLatestGeneratedKeyState] = useState<{
    keyId: number;
    value: string;
  } | null>(null);
  const latestGeneratedKey = latestGeneratedKeyState?.value ?? null;

  const displayedProxyKeys = useMemo(
    () => [...proxyKeys].sort((left, right) => right.id - left.id),
    [proxyKeys]
  );
  const proxyKeySuccessorByParentId = useMemo(() => {
    const map = new Map<number, number>();
    for (const key of proxyKeys) {
      if (key.rotated_from_id !== null) {
        map.set(key.rotated_from_id, key.id);
      }
    }
    return map;
  }, [proxyKeys]);
  const proxyKeyLimit = authSettings?.proxy_key_limit ?? 100;
  const remainingKeys = authSettings ? Math.max(proxyKeyLimit - proxyKeys.length, 0) : 0;
  const authStatusLabel = authSettings
    ? authSettings.auth_enabled
      ? getStaticMessages().proxyApiKeys.authenticationOn
      : getStaticMessages().proxyApiKeys.authenticationOff
    : getStaticMessages().proxyApiKeys.authenticationUnavailable;
  const authStatusTone = getAuthStatusTone(authSettings);
  const createDisabled = creatingProxyKey || !authSettings || remainingKeys === 0;

  useEffect(() => {
    const messages = getStaticMessages();
    let active = true;

    setPageLoading(true);
    void Promise.allSettled([api.settings.auth.get(), api.settings.auth.proxyKeys.list()])
      .then(([authResult, keysResult]) => {
        if (!active) {
          return;
        }

        if (authResult.status === "fulfilled") {
          setAuthSettings(authResult.value);
        } else {
          toast.error(
            authResult.reason instanceof Error
              ? authResult.reason.message
                : messages.proxyApiKeysData.loadAuthStatusFailed
          );
        }

        if (keysResult.status === "fulfilled") {
          setProxyKeys(keysResult.value);
        } else {
          toast.error(
            keysResult.reason instanceof Error
              ? keysResult.reason.message
                : messages.proxyApiKeysData.loadKeysFailed
          );
        }
      })
      .finally(() => {
        if (active) {
          setPageLoading(false);
        }
      });

    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!latestGeneratedKeyState) {
      return;
    }

    const keyStillExists = proxyKeys.some((key) => key.id === latestGeneratedKeyState.keyId);
    if (!keyStillExists) {
      setLatestGeneratedKeyState(null);
    }
  }, [latestGeneratedKeyState, proxyKeys]);

  async function handleCreateProxyKey() {
    const messages = getStaticMessages();
    if (!authSettings) {
      toast.error(messages.proxyApiKeysData.settingsUnavailable);
      return;
    }

    if (!proxyKeyName.trim()) {
      toast.error(messages.proxyApiKeysData.keyNameRequired);
      return;
    }

    if (remainingKeys <= 0) {
      toast.error(messages.proxyApiKeysData.maxKeysReached(String(proxyKeyLimit)));
      return;
    }

    setCreatingProxyKey(true);
    try {
      const created = await api.settings.auth.proxyKeys.create({
        name: proxyKeyName.trim(),
        notes: proxyKeyNotes.trim() || null,
        expires_at: normalizeExpiresAtInput(proxyKeyExpiresAt),
      });
      setLatestGeneratedKeyState({
        keyId: created.item.id,
        value: created.key,
      });
      setProxyKeyName("");
      setProxyKeyNotes("");
      setProxyKeyExpiresAt("");
      setProxyKeys((current) => [created.item, ...current]);
      toast.success(messages.proxyApiKeysData.created);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.createFailed);
    } finally {
      setCreatingProxyKey(false);
    }
  }

  async function handleRotateProxyKey(keyId: number) {
    const messages = getStaticMessages();
    setRotatingProxyKeyId(keyId);
    try {
      const rotated = await api.settings.auth.proxyKeys.rotate(keyId);
      setLatestGeneratedKeyState({
        keyId: rotated.item.id,
        value: rotated.key,
      });
      setProxyKeys((current) => {
        const rotationTimestamp = rotated.item.created_at;
        const withoutSuccessorDuplicates = current.filter((key) => key.id !== rotated.item.id);

        return [
          rotated.item,
          ...withoutSuccessorDuplicates.map((key) =>
            key.id === keyId
              ? {
                  ...key,
                  is_active: false,
                  expires_at: rotationTimestamp,
                  updated_at: rotationTimestamp,
                }
              : key
          ),
        ];
      });
      toast.success(messages.proxyApiKeysData.rotated);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.rotateFailed);
    } finally {
      setRotatingProxyKeyId(null);
    }
  }

  async function handleDeleteProxyKey() {
    const messages = getStaticMessages();
    if (!deleteConfirm) {
      return;
    }

    const deletingKey = deleteConfirm;

    setDeletingProxyKeyId(deletingKey.id);
    try {
      await api.settings.auth.proxyKeys.delete(deletingKey.id);
      setProxyKeys((current) => current.filter((key) => key.id !== deletingKey.id));
      if (latestGeneratedKeyState?.keyId === deletingKey.id) {
        setLatestGeneratedKeyState(null);
      }
      setDeleteProxyKeyAlertOpen(false);
      setDeleteConfirm(null);
      toast.success(messages.proxyApiKeysData.deleted);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.deleteFailed);
    } finally {
      setDeletingProxyKeyId(null);
    }
  }

  const startEditingProxyKey = (item: ProxyApiKey) => {
    setEditingProxyKey(item);
    setEditingProxyKeyName(item.name);
    setEditingProxyKeyNotes(item.notes ?? "");
    setEditingProxyKeyExpiresAt(toDateTimeLocalValue(item.expires_at));
    setEditingProxyKeyActive(item.is_active);
    setEditProxyKeySheetOpen(true);
  };

  async function handleSaveEditedProxyKey() {
    const messages = getStaticMessages();
    if (!editingProxyKey) {
      return;
    }

    const nextName = editingProxyKeyName.trim();
    if (!nextName) {
      toast.error(messages.proxyApiKeysData.keyNameRequired);
      return;
    }

    setSavingEditedProxyKeyId(editingProxyKey.id);
    try {
      const payload: ProxyApiKeyUpdate = {
        name: nextName,
        notes: editingProxyKeyNotes.trim() || null,
        is_active: editingProxyKeyActive,
        expires_at: normalizeExpiresAtInput(editingProxyKeyExpiresAt),
      };
      const updated = await api.settings.auth.proxyKeys.update(editingProxyKey.id, payload);
      setProxyKeys((current) =>
        current.map((key) => (key.id === updated.id ? updated : key))
      );
      setEditingProxyKey(updated);
      setEditingProxyKeyName(updated.name);
      setEditingProxyKeyNotes(updated.notes ?? "");
      setEditingProxyKeyExpiresAt(toDateTimeLocalValue(updated.expires_at));
      setEditingProxyKeyActive(updated.is_active);
      setEditProxyKeySheetOpen(false);
      toast.success(messages.proxyApiKeysData.updated);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : messages.proxyApiKeysData.updateFailed);
    } finally {
      setSavingEditedProxyKeyId(null);
    }
  }

  const handleCreateSubmit = (event: FormSubmitEvent) => {
    event.preventDefault();
    void handleCreateProxyKey();
  };

  const handleEditSubmit = (event: FormSubmitEvent) => {
    event.preventDefault();
    void handleSaveEditedProxyKey();
  };

  const handleDeleteDialogOpenChange = (open: boolean) => {
    if (!open && deletingProxyKeyId === null) {
      setDeleteProxyKeyAlertOpen(false);
      setDeleteConfirm(null);
      return;
    }

    setDeleteProxyKeyAlertOpen(open);
  };

  const handleEditDialogOpenChange = (open: boolean) => {
    if (!open && savingEditedProxyKeyId === null) {
      setEditProxyKeySheetOpen(false);
      return;
    }

    setEditProxyKeySheetOpen(open);
  };

  const setDeleteConfirmState = (item: ProxyApiKey | null) => {
    setDeleteConfirm(item);

    if (item) {
      setDisplayedDeleteConfirm(item);
      setDeleteProxyKeyAlertOpen(true);
      return;
    }

    setDeleteProxyKeyAlertOpen(false);
  };

  return {
    authSettings,
    authStatusLabel,
    authStatusTone,
    createDisabled,
    creatingProxyKey,
    deleteConfirm,
    deleteProxyKeyAlertOpen,
    deletingProxyKeyId,
    displayedDeleteConfirm,
    editProxyKeySheetOpen,
    editingProxyKey,
    editingProxyKeyActive,
    editingProxyKeyExpiresAt,
    editingProxyKeyName,
    editingProxyKeyNotes,
    displayedProxyKeys,
    handleCreateSubmit,
    handleDeleteDialogOpenChange,
    handleDeleteProxyKey,
    handleEditDialogOpenChange,
    handleEditSubmit,
    handleRotateProxyKey,
    latestGeneratedKey,
    pageLoading,
    proxyKeyExpiresAt,
    proxyKeyLimit,
    proxyKeyName,
    proxyKeyNotes,
    proxyKeySuccessorByParentId,
    proxyKeys,
    remainingKeys,
    rotatingProxyKeyId,
    savingEditedProxyKeyId,
    setDeleteConfirm: setDeleteConfirmState,
    setDeletingProxyKeyId,
    setEditingProxyKeyActive,
    setEditingProxyKeyExpiresAt,
    setEditingProxyKeyName,
    setEditingProxyKeyNotes,
    setProxyKeyExpiresAt,
    setProxyKeyName,
    setProxyKeyNotes,
    startEditingProxyKey,
  };
}
