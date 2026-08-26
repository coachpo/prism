import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { ProxyApiKey, ProxyApiKeyRotateResponse } from "@/lib/types";
import { showProxyKeyMutationError } from "./proxyKeyMutationErrors";
import { reconcileProxyKeyLedgerAfterCreateOrRotate } from "./proxyKeyMutationReconciliation";

interface UseProxyKeyRotateMutationInput {
  showRotatedSecret: (rotated: ProxyApiKeyRotateResponse) => void;
}

export function useProxyKeyRotateMutation({
  showRotatedSecret,
}: UseProxyKeyRotateMutationInput) {
  const queryClient = useQueryClient();
  const messages = getStaticMessages();
  const [rotateConfirm, setRotateConfirm] = useState<ProxyApiKey | null>(null);
  const [rotateProxyKeyAlertOpen, setRotateProxyKeyAlertOpen] = useState(false);
  const [displayedRotateConfirm, setDisplayedRotateConfirm] =
    useState<ProxyApiKey | null>(null);
  const rotateMutation = useMutation({
    mutationFn: (keyId: number) => api.settings.auth.proxyKeys.rotate(keyId),
  });

  async function handleRotateProxyKey() {
    if (!rotateConfirm) return;
    const keyId = rotateConfirm.id;
    try {
      const rotated = await rotateMutation.mutateAsync(keyId);
      showRotatedSecret(rotated);
      rotateMutation.reset();
      setRotateProxyKeyAlertOpen(false);
      setRotateConfirm(null);
      reconcileProxyKeyLedgerAfterCreateOrRotate(
        queryClient,
        rotated.item,
        rotated.capacity,
      );
      toast.success(messages.proxyApiKeysData.rotated);
    } catch (error) {
      showProxyKeyMutationError(error, messages.proxyApiKeysData.rotateFailed);
    }
  }

  const setRotateConfirmState = (item: ProxyApiKey | null) => {
    setRotateConfirm(item);
    if (item) {
      setDisplayedRotateConfirm(item);
      setRotateProxyKeyAlertOpen(true);
      return;
    }
    setRotateProxyKeyAlertOpen(false);
  };

  const handleRotateDialogOpenChange = (open: boolean) => {
    if (!open && !rotateMutation.isPending) {
      setRotateProxyKeyAlertOpen(false);
      setRotateConfirm(null);
      return;
    }
    setRotateProxyKeyAlertOpen(open);
  };

  return {
    displayedRotateConfirm,
    handleRotateDialogOpenChange,
    handleRotateProxyKey,
    rotatingProxyKeyId: rotateMutation.isPending
      ? rotateMutation.variables ?? null
      : null,
    rotateConfirm,
    rotateProxyKeyAlertOpen,
    setRotateConfirm: setRotateConfirmState,
  };
}
