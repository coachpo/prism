import { useCallback, useEffect, useMemo, useState } from "react";
import type { NavigateFunction } from "react-router-dom";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { AuthSettings } from "@/lib/types";
import { toast } from "sonner";
import { validateAuthPassword } from "./settingsPageHelpers";

interface UseAuthenticationSettingsDataInput {
  navigate: NavigateFunction;
  refreshAuth: () => Promise<void>;
  revision: number;
}

function getAuthenticationMessages() {
  return getStaticMessages().settingsAuthentication;
}

function getMessages() {
  return getStaticMessages();
}

export function useAuthenticationSettingsData({
  navigate,
  refreshAuth,
  revision,
}: UseAuthenticationSettingsDataInput) {
  const [authSettings, setAuthSettings] = useState<AuthSettings | null>(null);
  const [authEnabledInput, setAuthEnabledInput] = useState(false);
  const [authUsername, setAuthUsername] = useState("");
  const [authEmail, setAuthEmail] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [authPasswordConfirm, setAuthPasswordConfirm] = useState("");
  const [authSaving, setAuthSaving] = useState(false);
  const [emailVerificationOtp, setEmailVerificationOtp] = useState("");
  const [sendingEmailVerification, setSendingEmailVerification] = useState(false);
  const [confirmingEmailVerification, setConfirmingEmailVerification] = useState(false);

  const fetchAuthSettings = useCallback(async () => {
    const messages = getMessages();
    try {
      const data = await api.settings.auth.get();
      setAuthSettings(data);
      setAuthEnabledInput(data.auth_enabled);
      setAuthUsername(data.username ?? "");
      setAuthEmail(data.pending_email ?? data.email ?? "");
      setAuthPassword("");
      setAuthPasswordConfirm("");
      setEmailVerificationOtp("");
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.proxyApiKeysData.loadAuthStatusFailed,
      );
    }
  }, []);

  useEffect(() => {
    void revision;
    void fetchAuthSettings();
  }, [fetchAuthSettings, revision]);

  const authPasswordError = useMemo(() => validateAuthPassword(authPassword), [authPassword]);
  const authPasswordMismatch = useMemo(
    () => Boolean(authPassword) && authPassword !== authPasswordConfirm,
    [authPassword, authPasswordConfirm]
  );

  const handleSaveAuthSettings = useCallback(
    async (nextEnabled?: boolean) => {
      const messages = getMessages();
      const wasEnabled = authSettings?.auth_enabled ?? false;
      const isDisablingAuth = nextEnabled === false && wasEnabled;

      if (!isDisablingAuth && authPasswordError) {
        toast.error(authPasswordError);
        return;
      }
      if (!isDisablingAuth && authPasswordMismatch) {
        toast.error(messages.settingsAuthentication.passwordsMustMatch);
        return;
      }

      const targetEnabled = nextEnabled ?? authEnabledInput;
      setAuthEnabledInput(targetEnabled);
      setAuthSaving(true);

      try {
        const saved = await api.settings.auth.update({
          auth_enabled: targetEnabled,
          username: authUsername.trim() || null,
          password: isDisablingAuth ? null : authPassword || null,
        });

        setAuthSettings(saved);
        setAuthEnabledInput(saved.auth_enabled);
        setAuthUsername(saved.username ?? "");
        setAuthEmail(saved.pending_email ?? saved.email ?? "");
        setAuthPassword("");
        setAuthPasswordConfirm("");

        try {
          await refreshAuth();
        } catch {
          void 0;
        }

        if (!wasEnabled && saved.auth_enabled) {
          toast.success(messages.auth.signInToContinue);
          navigate("/login", { replace: true });
          return;
        }

        toast.success(messages.settingsAuthentication.authenticationStatus);
      } catch (error) {
        setAuthEnabledInput(authSettings?.auth_enabled ?? false);
        toast.error(
          error instanceof Error
            ? error.message
            : messages.proxyApiKeysData.updateFailed,
        );
      } finally {
        setAuthSaving(false);
      }
    },
    [authEnabledInput, authPassword, authPasswordError, authPasswordMismatch, authSettings, authUsername, navigate, refreshAuth]
  );

  const handleRequestEmailVerification = useCallback(async () => {
    const messages = getMessages();
    const authenticationMessages = getAuthenticationMessages();
    if (!authEmail.trim()) {
      toast.error(authenticationMessages.emailRequired);
      return;
    }

    setSendingEmailVerification(true);
    try {
      const result = await api.settings.auth.requestEmailVerification({
        email: authEmail.trim(),
      });
      setAuthSettings((prev) =>
        prev
          ? {
              ...prev,
              email: result.email,
              email_bound_at: result.email_bound_at,
              pending_email: result.pending_email,
              email_verification_required: Boolean(result.pending_email),
            }
          : prev
      );
      toast.success(authenticationMessages.verificationCodeSent);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.settingsAuthentication.emailVerificationFailed,
      );
    } finally {
      setSendingEmailVerification(false);
    }
  }, [authEmail]);

  const handleConfirmEmailVerification = useCallback(async () => {
    const authenticationMessages = getAuthenticationMessages();
    if (!emailVerificationOtp.trim()) {
      toast.error(authenticationMessages.verificationCodeRequired);
      return;
    }

    setConfirmingEmailVerification(true);
    try {
      const result = await api.settings.auth.confirmEmailVerification({
        otp_code: emailVerificationOtp.trim(),
      });
      setAuthSettings((prev) =>
        prev
          ? {
              ...prev,
              email: result.email,
              email_bound_at: result.email_bound_at,
              pending_email: result.pending_email,
              email_verification_required: Boolean(result.pending_email),
            }
          : prev
      );
      setAuthEmail(result.email ?? authEmail);
      setEmailVerificationOtp("");
      toast.success(authenticationMessages.emailVerificationSucceeded);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : authenticationMessages.emailVerificationFailed,
      );
    } finally {
      setConfirmingEmailVerification(false);
    }
  }, [authEmail, emailVerificationOtp]);

  return {
    authEmail,
    authEnabledInput,
    authPassword,
    authPasswordConfirm,
    authPasswordError,
    authPasswordMismatch,
    authSaving,
    authSettings,
    authUsername,
    confirmingEmailVerification,
    emailVerificationOtp,
    handleConfirmEmailVerification,
    handleRequestEmailVerification,
    handleSaveAuthSettings,
    sendingEmailVerification,
    setAuthEmail,
    setAuthPassword,
    setAuthPasswordConfirm,
    setAuthUsername,
    setEmailVerificationOtp,
  };
}
