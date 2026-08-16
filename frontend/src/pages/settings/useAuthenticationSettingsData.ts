import { useCallback, useEffect, useMemo, useState } from "react";
import { authSessionCoordinator } from "@/context/auth/coordinatorInstance";
import { broadcastAuthStateChange, randomUUID } from "@/context/auth/crossTab";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type { AuthSettings, AuthSettingsMutationResponse } from "@/lib/types";
import { toast } from "sonner";
import { validateAuthPassword } from "./settingsPageHelpers";

interface UseAuthenticationSettingsDataInput {
  enabled: boolean;
  navigate: (to: string, options?: { replace?: boolean }) => void;
  refreshAuth: () => Promise<void>;
  revision: number;
}

function getMessages() {
  return getStaticMessages();
}

function operationId() {
  return typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `settings-auth-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function normalizeAuthSettings(value: AuthSettings): AuthSettings {
  const effective = value.auth_mode?.effective === "enabled";
  const account = value.operator_account?.effective;
  return {
    ...value,
    auth_enabled: value.auth_enabled ?? effective,
    username: value.username ?? account?.username ?? null,
    has_password: value.has_password ?? account?.has_password ?? false,
  };
}

function isAuthEnabled(value: AuthSettings | null) {
  return value?.auth_mode?.effective === "enabled" || value?.auth_enabled === true;
}

export type AuthSettingsConfirmation = "disable" | "zero_keys" | "account_update";

export function useAuthenticationSettingsData({
  enabled,
  navigate,
  refreshAuth,
  revision,
}: UseAuthenticationSettingsDataInput) {
  const [authSettings, setAuthSettings] = useState<AuthSettings | null>(null);
  const [authEnabledInput, setAuthEnabledInput] = useState(false);
  const [authUsername, setAuthUsername] = useState("");
  const [authPassword, setAuthPassword] = useState("");
  const [authPasswordConfirm, setAuthPasswordConfirm] = useState("");
  const [authSaving, setAuthSaving] = useState(false);
  const [pendingAuthConfirmation, setPendingAuthConfirmation] =
    useState<AuthSettingsConfirmation | null>(null);

  const fetchAuthSettings = useCallback(async () => {
    const messages = getMessages();
    try {
      const data = normalizeAuthSettings(await api.settings.auth.get());
      setAuthSettings(data);
      setAuthEnabledInput(isAuthEnabled(data));
      setAuthUsername(data.operator_account?.effective.username ?? data.username ?? "");
      setAuthPassword("");
      setAuthPasswordConfirm("");
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.proxyApiKeysData.loadAuthStatusFailed,
      );
    }
  }, []);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    void revision;
    void fetchAuthSettings();
  }, [enabled, fetchAuthSettings, revision]);

  const authPasswordError = useMemo(() => validateAuthPassword(authPassword), [authPassword]);
  const authPasswordMismatch = useMemo(
    () => Boolean(authPassword) && authPassword !== authPasswordConfirm,
    [authPassword, authPasswordConfirm],
  );

  const persistAuthSettings = useCallback(
    async (nextEnabled?: boolean) => {
      const messages = getMessages();
      const currentEnabled = isAuthEnabled(authSettings);
      const targetEnabled = nextEnabled ?? authEnabledInput;
	  const currentUsername = authSettings?.operator_account?.effective.username ?? authSettings?.username ?? "";
	  const accountChanged = authUsername.trim() !== currentUsername || Boolean(authPassword);
	  const isEnabling = targetEnabled && !currentEnabled;
	  const isDisabling = !targetEnabled && currentEnabled;
      const readiness = authSettings?.proxy_key_readiness;
      const safeActive = Number.parseInt(readiness?.activation_guard?.safe_active ?? "0", 10);
      const acknowledgeZeroKeys = isEnabling && safeActive === 0;

      setAuthEnabledInput(targetEnabled);
      setAuthSaving(true);
      const request: Parameters<typeof api.settings.auth.update>[0] = {
        operation_id: operationId(),
        expected_revision: authSettings?.revision ?? "",
        expected_proxy_key_readiness_generation: isEnabling
          ? readiness?.readiness_generation ?? null
          : null,
        desired_auth_enabled: targetEnabled,
        account_change: accountChanged
          ? {
              kind: "update",
              username: authUsername.trim(),
              new_password: authPassword || null,
            }
          : { kind: "preserve" },
        acknowledgements: {
          ...(acknowledgeZeroKeys ? { enable_without_active_proxy_keys: true as const } : {}),
          ...(isDisabling ? { disable_to_permissive_access: true as const } : {}),
          ...(accountChanged && currentEnabled
            ? { invalidate_operator_sessions: true as const }
            : {}),
        },
      };

      try {
        const response: AuthSettingsMutationResponse = await api.settings.auth.update(request);
        const saved = normalizeAuthSettings(response.settings);
        setAuthSettings(saved);
        setAuthEnabledInput(isAuthEnabled(saved));
        setAuthUsername(saved.operator_account?.effective.username ?? saved.username ?? "");
        setAuthPassword("");
        setAuthPasswordConfirm("");
        const targetGeneration = randomUUID();
        const originGeneration = authSessionCoordinator.getSessionGenerationId();
        authSessionCoordinator.beginCrossTabBootstrap(targetGeneration);
        broadcastAuthStateChange(originGeneration, "auth_changed", targetGeneration);

        try {
          await refreshAuth();
        } catch {
          // The authoritative settings response is still retained; the shell
          // will re-bootstrap on the next auth-state broadcast.
        }

        if (response.session_action === "clear_and_login") {
          toast.success(messages.auth.signInToContinue);
          navigate("/auth/login", { replace: true });
          return;
        }

        toast.success(messages.settingsAuthentication.authenticationStatus);
      } catch (error) {
        setAuthEnabledInput(isAuthEnabled(authSettings));
        toast.error(
          error instanceof Error
            ? error.message
            : messages.proxyApiKeysData.updateFailed,
        );
      } finally {
        setAuthSaving(false);
      }
    },
    [
      authEnabledInput,
      authPassword,
      authSettings,
      authUsername,
      navigate,
      refreshAuth,
    ],
  );

  const handleSaveAuthSettings = useCallback(
    async (nextEnabled?: boolean) => {
      const messages = getMessages();
      const currentEnabled = isAuthEnabled(authSettings);
      const targetEnabled = nextEnabled ?? authEnabledInput;
      const isEnabling = targetEnabled && !currentEnabled;
      const isDisabling = !targetEnabled && currentEnabled;
      const currentUsername = authSettings?.operator_account?.effective.username ?? authSettings?.username ?? "";
      const accountChanged = authUsername.trim() !== currentUsername || Boolean(authPassword);

      if (!isDisabling && authPasswordError) {
        toast.error(authPasswordError);
        return;
      }
      if (!isDisabling && authPasswordMismatch) {
        toast.error(messages.settingsAuthentication.passwordsMustMatch);
        return;
      }

      const safeActive = Number.parseInt(
        authSettings?.proxy_key_readiness?.activation_guard?.safe_active ?? "0",
        10,
      );
      if (isDisabling) {
        setPendingAuthConfirmation("disable");
        return;
      }
      if (accountChanged && currentEnabled) {
        setPendingAuthConfirmation("account_update");
        return;
      }
      if (isEnabling && safeActive === 0) {
        setPendingAuthConfirmation("zero_keys");
        return;
      }

      await persistAuthSettings(nextEnabled);
    },
    [
      authEnabledInput,
      authPassword,
      authPasswordError,
      authPasswordMismatch,
      authSettings,
      authUsername,
      persistAuthSettings,
    ],
  );

  const confirmPendingAuthSettings = useCallback(async () => {
    if (!pendingAuthConfirmation) {
      return;
    }
    setPendingAuthConfirmation(null);
    await persistAuthSettings(pendingAuthConfirmation === "disable" ? false : true);
  }, [pendingAuthConfirmation, persistAuthSettings]);

  return {
    authEnabledInput,
    authPassword,
    authPasswordConfirm,
    authPasswordError,
    authPasswordMismatch,
    authSaving,
    authSettings,
    authUsername,
    confirmPendingAuthSettings,
    handleSaveAuthSettings,
    pendingAuthConfirmation,
    cancelPendingAuthConfirmation: () => setPendingAuthConfirmation(null),
    setAuthPassword,
    setAuthPasswordConfirm,
    setAuthUsername,
  };
}
