import { useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { getStaticMessages } from "@/i18n/staticMessages";
import { api } from "@/lib/api";
import type { ProxyApiKey, ProxyApiKeyRotateResponse } from "@/lib/types";
import { reconcileProxyKeyLedgerAfterCreateOrRotate } from "./proxyKeyMutationReconciliation";

interface UseProxyKeyRotateMutationInput {
  showRotatedSecret: (rotated: ProxyApiKeyRotateResponse) => void;
}

export function useProxyKeyRotateMutation({
  showRotatedSecret,
}: UseProxyKeyRotateMutationInput) {
  const queryClient = useQueryClient();
  const messages = getStaticMessages();
  const [rotateError, setRotateError] = useState<string | null>(null);
  const [rotateConfirm, setRotateConfirm] = useState<ProxyApiKey | null>(null);
  const [rotateProxyKeyAlertOpen, setRotateProxyKeyAlertOpen] = useState(false);
  const [displayedRotateConfirm, setDisplayedRotateConfirm] =
    useState<ProxyApiKey | null>(null);
  const rotateMutation = useMutation({
    mutationFn: (keyId: number) => api.settings.auth.proxyKeys.rotate(keyId),
  });

  async function handleRotateProxyKey() {
    setRotateError(null);
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
    } catch {
      setRotateError(messages.proxyApiKeysData.rotateFailed);
    }
  }

  const setRotateConfirmState = (item: ProxyApiKey | null) => {
    setRotateError(null);
    setRotateConfirm(item);
    if (item) {
      setDisplayedRotateConfirm(item);
      setRotateProxyKeyAlertOpen(true);
      return;
    }
    setRotateProxyKeyAlertOpen(false);
  };

  const handleRotateDialogOpenChange = (open: boolean) => {
    if (rotateMutation.isPending) return;
    if (!open) {
      setRotateProxyKeyAlertOpen(false);
      setRotateConfirm(null);
      return;
    }
    setRotateProxyKeyAlertOpen(open);
  };

  return {
    rotateError,
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
