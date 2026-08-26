import type { ComponentProps } from "react";
import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type {
  AuthSettings,
  ProxyApiKeyCreateResponse,
  ProxyKeyCapacity,
} from "@/lib/types";
import type { ResolvedExpiryInput } from "@/pages/proxy-api-keys/ProxyKeyExpiryField";
import { showProxyKeyMutationError } from "./proxyKeyMutationErrors";
import { reconcileProxyKeyLedgerAfterCreateOrRotate } from "./proxyKeyMutationReconciliation";

type FormSubmitEvent = Parameters<
  NonNullable<ComponentProps<"form">["onSubmit"]>
>[0];

interface UseProxyKeyCreateMutationInput {
  authSettings: AuthSettings | null;
  capacity: ProxyKeyCapacity | null;
  proxyKeyLimit: number;
  remainingKeys: number;
  showCreatedSecret: (created: ProxyApiKeyCreateResponse) => void;
}

export function useProxyKeyCreateMutation({
  authSettings,
  capacity,
  proxyKeyLimit,
  remainingKeys,
  showCreatedSecret,
}: UseProxyKeyCreateMutationInput) {
  const queryClient = useQueryClient();
  const messages = getStaticMessages();
  const [proxyKeyName, setProxyKeyName] = useState("");
  const [proxyKeyNotes, setProxyKeyNotes] = useState("");
  const [proxyKeyExpiresAt, setProxyKeyExpiresAt] = useState("");
  const [proxyKeyExpiresResolved, setProxyKeyExpiresResolved] =
    useState<ResolvedExpiryInput | null>(null);
  const [issueSheetOpen, setIssueSheetOpen] = useState(false);
  const createMutation = useMutation({
    mutationFn: api.settings.auth.proxyKeys.create,
  });

  const createDisabled =
    createMutation.isPending ||
    !authSettings ||
    !capacity ||
    remainingKeys === 0;

  async function handleCreateProxyKey() {
    if (!authSettings) {
      toast.error(messages.proxyApiKeysData.settingsUnavailable);
      return;
    }
    if (!proxyKeyName.trim()) {
      toast.error(messages.proxyApiKeysData.keyNameRequired);
      return;
    }
    if (remainingKeys <= 0) {
      toast.error(
        messages.proxyApiKeysData.maxKeysReached(String(proxyKeyLimit)),
      );
      return;
    }

    try {
      const created = await createMutation.mutateAsync({
        name: proxyKeyName.trim(),
        notes: proxyKeyNotes.trim() || null,
        expires_at:
          proxyKeyExpiresResolved &&
          !proxyKeyExpiresResolved.preserved &&
          !proxyKeyExpiresResolved.gapError
            ? proxyKeyExpiresResolved.instant
            : normalizeExpiresAtInput(proxyKeyExpiresAt),
      });
      showCreatedSecret(created);
      createMutation.reset();
      setProxyKeyName("");
      setProxyKeyNotes("");
      setProxyKeyExpiresAt("");
      setProxyKeyExpiresResolved(null);
      setIssueSheetOpen(false);
      reconcileProxyKeyLedgerAfterCreateOrRotate(
        queryClient,
        created.item,
        created.capacity,
      );
      toast.success(messages.proxyApiKeysData.created);
    } catch (error) {
      showProxyKeyMutationError(error, messages.proxyApiKeysData.createFailed);
    }
  }

  const handleCreateSubmit = (event: FormSubmitEvent) => {
    event.preventDefault();
    void handleCreateProxyKey();
  };

  return {
    createDisabled,
    creatingProxyKey: createMutation.isPending,
    handleCreateSubmit,
    issueSheetOpen,
    proxyKeyExpiresAt,
    proxyKeyExpiresResolved,
    proxyKeyName,
    proxyKeyNotes,
    setIssueSheetOpen,
    setProxyKeyExpiresAt,
    setProxyKeyExpiresResolved,
    setProxyKeyName,
    setProxyKeyNotes,
  };
}

function normalizeExpiresAtInput(value: string): string | null | undefined {
  const trimmed = value.trim();
  if (trimmed === "") return null;
  return trimmed;
}
