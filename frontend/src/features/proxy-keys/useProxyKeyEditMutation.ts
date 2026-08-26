import type { ComponentProps } from "react";
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { ProxyApiKey, ProxyApiKeyUpdate } from "@/lib/types";
import type { ResolvedExpiryInput } from "@/pages/proxy-api-keys/ProxyKeyExpiryField";
import { showProxyKeyMutationError } from "./proxyKeyMutationErrors";
import { reconcileProxyKeyLedgerAfterUpdate } from "./proxyKeyMutationReconciliation";

type FormSubmitEvent = Parameters<
  NonNullable<ComponentProps<"form">["onSubmit"]>
>[0];

export function useProxyKeyEditMutation() {
  const queryClient = useQueryClient();
  const messages = getStaticMessages();
  const [editingProxyKey, setEditingProxyKey] = useState<ProxyApiKey | null>(
    null,
  );
  const [editProxyKeySheetOpen, setEditProxyKeySheetOpen] = useState(false);
  const [editingProxyKeyName, setEditingProxyKeyName] = useState("");
  const [editingProxyKeyNotes, setEditingProxyKeyNotes] = useState("");
  const [editingProxyKeyExpiresAt, setEditingProxyKeyExpiresAt] = useState("");
  const [editingProxyKeyExpiresResolved, setEditingProxyKeyExpiresResolved] =
    useState<ResolvedExpiryInput | null>(null);
  const [editingProxyKeyActive, setEditingProxyKeyActive] = useState(false);
  const updateMutation = useMutation({
    mutationFn: ({
      keyId,
      payload,
    }: {
      keyId: number;
      payload: ProxyApiKeyUpdate;
    }) => api.settings.auth.proxyKeys.update(keyId, payload),
  });

  const startEditingProxyKey = (item: ProxyApiKey) => {
    setEditingProxyKey(item);
    setEditingProxyKeyName(item.name);
    setEditingProxyKeyNotes(item.notes ?? "");
    setEditingProxyKeyExpiresAt(item.expires_at ?? "");
    setEditingProxyKeyExpiresResolved(null);
    setEditingProxyKeyActive(item.is_active);
    setEditProxyKeySheetOpen(true);
  };

  async function handleSaveEditedProxyKey() {
    if (!editingProxyKey) return;
    const nextName = editingProxyKeyName.trim();
    if (!nextName) {
      toast.error(messages.proxyApiKeysData.keyNameRequired);
      return;
    }

    try {
      const payload: ProxyApiKeyUpdate = {
        name: nextName,
        notes: editingProxyKeyNotes.trim() || null,
        is_active: editingProxyKeyActive,
        ...editingExpiryPayload(
          editingProxyKeyExpiresResolved,
          editingProxyKeyExpiresAt,
        ),
      };
      const updated = await updateMutation.mutateAsync({
        keyId: editingProxyKey.id,
        payload,
      });
      reconcileProxyKeyLedgerAfterUpdate(
        queryClient,
        updated.item,
        updated.capacity,
      );
      setEditingProxyKey(updated.item);
      setEditingProxyKeyName(updated.item.name);
      setEditingProxyKeyNotes(updated.item.notes ?? "");
      setEditingProxyKeyExpiresAt(updated.item.expires_at ?? "");
      setEditingProxyKeyExpiresResolved(null);
      setEditingProxyKeyActive(updated.item.is_active);
      setEditProxyKeySheetOpen(false);
      toast.success(messages.proxyApiKeysData.updated);
    } catch (error) {
      showProxyKeyMutationError(error, messages.proxyApiKeysData.updateFailed);
    }
  }

  const handleEditSubmit = (event: FormSubmitEvent) => {
    event.preventDefault();
    void handleSaveEditedProxyKey();
  };

  const handleEditDialogOpenChange = (open: boolean) => {
    if (!open && !updateMutation.isPending) {
      setEditProxyKeySheetOpen(false);
      return;
    }
    setEditProxyKeySheetOpen(open);
  };

  return {
    editProxyKeySheetOpen,
    editingProxyKey,
    editingProxyKeyActive,
    editingProxyKeyExpiresAt,
    editingProxyKeyExpiresResolved,
    editingProxyKeyName,
    editingProxyKeyNotes,
    handleEditDialogOpenChange,
    handleEditSubmit,
    savingEditedProxyKeyId: updateMutation.isPending
      ? updateMutation.variables?.keyId ?? null
      : null,
    setEditingProxyKeyActive,
    setEditingProxyKeyExpiresAt,
    setEditingProxyKeyExpiresResolved,
    setEditingProxyKeyName,
    setEditingProxyKeyNotes,
    startEditingProxyKey,
  };
}

function editingExpiryPayload(
  resolved: ResolvedExpiryInput | null,
  wallClock: string,
): Pick<ProxyApiKeyUpdate, "expires_at"> {
  if (resolved && resolved.preserved) return {};
  if (resolved && resolved.gapError) return { expires_at: undefined };
  if (resolved && resolved.instant === null) return { expires_at: null };
  if (resolved && resolved.instant !== null) {
    return { expires_at: resolved.instant };
  }
  const trimmed = wallClock.trim();
  if (trimmed === "") return {};
  return { expires_at: trimmed };
}
