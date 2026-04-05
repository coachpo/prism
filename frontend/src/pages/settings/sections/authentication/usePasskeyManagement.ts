import { useCallback, useEffect, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { registerPasskey } from "@/lib/webauthn";
import { toast } from "sonner";
import type { PasskeyCredential } from "./types";

function getMessages() {
  return getStaticMessages();
}

export function usePasskeyManagement(authEnabled: boolean) {
  const [passkeys, setPasskeys] = useState<PasskeyCredential[]>([]);
  const [loadingPasskeys, setLoadingPasskeys] = useState(true);
  const [registering, setRegistering] = useState(false);
  const [isPasskeySupported, setIsPasskeySupported] = useState(true);
  const [registerDialogOpen, setRegisterDialogOpen] = useState(false);
  const [deviceName, setDeviceName] = useState("");
  const [removeDialogOpen, setRemoveDialogOpen] = useState(false);
  const [passkeyToRemove, setPasskeyToRemove] = useState<PasskeyCredential | null>(null);
  const [removing, setRemoving] = useState(false);

  const fetchPasskeys = useCallback(async () => {
    try {
      const response = await api.settings.auth.webauthn.listCredentials();
      setPasskeys(response.items);
    } catch {
      toast.error(getMessages().settingsPasskeysData.loadFailed);
    } finally {
      setLoadingPasskeys(false);
    }
  }, []);

  useEffect(() => {
    if (!authEnabled) {
      setLoadingPasskeys(false);
      return;
    }

    if (!window.PublicKeyCredential) {
      setIsPasskeySupported(false);
      setLoadingPasskeys(false);
      return;
    }

    void fetchPasskeys();
  }, [authEnabled, fetchPasskeys]);

  const handleRegisterClick = () => {
    setDeviceName("");
    setRegisterDialogOpen(true);
  };

  const handleRegisterSubmit = async () => {
    if (!deviceName.trim()) {
      toast.error(getMessages().settingsPasskeysData.deviceNameRequired);
      return;
    }

    setRegistering(true);
    try {
      await registerPasskey(deviceName.trim());
      toast.success(getMessages().settingsPasskeysData.registered);
      setRegisterDialogOpen(false);
      await fetchPasskeys();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : getMessages().settingsPasskeysData.registerFailed);
    } finally {
      setRegistering(false);
    }
  };

  const handleRemoveClick = (passkey: PasskeyCredential) => {
    setPasskeyToRemove(passkey);
    setRemoveDialogOpen(true);
  };

  const handleRemoveConfirm = async () => {
    if (!passkeyToRemove) {
      return;
    }

    setRemoving(true);
    try {
      await api.settings.auth.webauthn.revokeCredential(passkeyToRemove.id);
      toast.success(getMessages().settingsPasskeysData.removed);
      setRemoveDialogOpen(false);
      setPasskeyToRemove(null);
      await fetchPasskeys();
    } catch {
      toast.error(getMessages().settingsPasskeysData.removeFailed);
    } finally {
      setRemoving(false);
    }
  };

  return {
    deviceName,
    handleRegisterClick,
    handleRegisterSubmit,
    handleRemoveClick,
    handleRemoveConfirm,
    isPasskeySupported,
    loadingPasskeys,
    passkeyToRemove,
    passkeys,
    registerDialogOpen,
    registering,
    removeDialogOpen,
    removing,
    setDeviceName,
    setRegisterDialogOpen,
    setRemoveDialogOpen,
  };
}
