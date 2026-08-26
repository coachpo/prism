import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  RetentionPreflightResponse,
  RetentionSettingsResponsePolicies,
  RetentionSettingsPolicies,
  RetentionSettingsResponse,
} from "@/lib/types";
import { toast } from "sonner";
import {
  isStaleRetentionPreflightError,
  newRetentionOperationId,
  retentionPreflightFactsComplete,
} from "./retentionProtocol";
import type { SettingsSaveSection } from "./settingsSaveTypes";

interface UseRetentionPolicyInput {
  enabled: boolean;
  onJobsMutation: () => void | Promise<void>;
  setRecentlySavedSection?: (section: SettingsSaveSection | null) => void;
}

type RetentionSettingKey = keyof RetentionSettingsPolicies;

function policiesOf(
  settings: RetentionSettingsResponse,
): RetentionSettingsPolicies {
  const policies = settings.policies as
    | RetentionSettingsResponsePolicies
    | undefined;
  if (
    policies &&
    Object.values(policies).every(
      (value) => typeof value === "number" || value === null,
    )
  ) {
    return policies as RetentionSettingsPolicies;
  }
  const tagged = policies as
    | Record<
        keyof RetentionSettingsPolicies,
        { value?: number | null; raw_integer?: string }
      >
    | undefined;
  const editableValue = (
    value: { value?: number | null; raw_integer?: string } | undefined,
  ) => {
    if (!value) return null;
    if (value.value !== undefined && value.value !== null) return value.value;
    // Preserve an exact in-range integer for a repair form. Out-of-range
    // legacy values stay visibly invalid and must be replaced before PUT.
    const parsed =
      value.raw_integer === undefined ? null : Number(value.raw_integer);
    return Number.isSafeInteger(parsed) ? parsed : null;
  };
  if (tagged) {
    return {
      request_logs_retention_days: editableValue(
        tagged.request_logs_retention_days,
      ),
      statistics_retention_days: editableValue(
        tagged.statistics_retention_days,
      ),
      audit_logs_retention_days: editableValue(
        tagged.audit_logs_retention_days,
      ),
      loadbalance_events_retention_days: editableValue(
        tagged.loadbalance_events_retention_days,
      ),
    };
  }
  return {
    request_logs_retention_days: settings.request_logs_retention_days ?? null,
    statistics_retention_days: settings.statistics_retention_days ?? null,
    audit_logs_retention_days: settings.audit_logs_retention_days ?? null,
    loadbalance_events_retention_days:
      settings.loadbalance_events_retention_days ?? null,
  };
}

function policiesEqual(
  left: RetentionSettingsPolicies,
  right: RetentionSettingsPolicies,
) {
  return (
    left.request_logs_retention_days === right.request_logs_retention_days &&
    left.statistics_retention_days === right.statistics_retention_days &&
    left.audit_logs_retention_days === right.audit_logs_retention_days &&
    left.loadbalance_events_retention_days ===
      right.loadbalance_events_retention_days
  );
}

function hasDestructiveChange(
  before: RetentionSettingsPolicies,
  after: RetentionSettingsPolicies,
) {
  return (Object.keys(before) as RetentionSettingKey[]).some((key) => {
    const oldValue = before[key];
    const nextValue = after[key];
    return (
      (oldValue === null && nextValue !== null) ||
      (oldValue !== null && nextValue !== null && nextValue < oldValue)
    );
  });
}

export function useRetentionPolicy({
  enabled,
  onJobsMutation,
  setRecentlySavedSection,
}: UseRetentionPolicyInput) {
  const messages = getStaticMessages();
  const [retentionSettingsLoading, setRetentionSettingsLoading] =
    useState(true);
  const [retentionSettingsSaving, setRetentionSettingsSaving] = useState(false);
  const [savedRetentionSettings, setSavedRetentionSettings] =
    useState<RetentionSettingsResponse | null>(null);
  const [retentionSettings, setRetentionSettings] =
    useState<RetentionSettingsResponse | null>(null);
  const [policyPreflight, setPolicyPreflight] =
    useState<RetentionPreflightResponse | null>(null);
  const [policyPreflightLoading, setPolicyPreflightLoading] = useState(false);
  const [policyConfirmOpen, setPolicyConfirmOpen] = useState(false);
  const [policyConfirmationPhrase, setPolicyConfirmationPhrase] = useState("");
  const [policyOperationId, setPolicyOperationId] = useState<string | null>(
    null,
  );

  const manualConfirmationKeyword = policyPreflight?.confirmation_keyword ?? null;
  const isPolicyPhraseValid = useMemo(
    () =>
      manualConfirmationKeyword !== null &&
      policyConfirmationPhrase.trim() === manualConfirmationKeyword,
    [manualConfirmationKeyword, policyConfirmationPhrase],
  );
  const retentionSettingsDirty = useMemo(() => {
    if (!savedRetentionSettings || !retentionSettings) return false;
    return !policiesEqual(
      policiesOf(savedRetentionSettings),
      policiesOf(retentionSettings),
    );
  }, [retentionSettings, savedRetentionSettings]);
  const policyIsDestructive = Boolean(
    savedRetentionSettings &&
      retentionSettings &&
      hasDestructiveChange(
        policiesOf(savedRetentionSettings),
        policiesOf(retentionSettings),
      ),
  );
  const isPolicyPreflightSemanticallyComplete =
    retentionPreflightFactsComplete(policyPreflight);
  const isPolicyPreflightValid =
    isPolicyPhraseValid && isPolicyPreflightSemanticallyComplete;

  const fetchRetentionSettings = useCallback(async () => {
    setRetentionSettingsLoading(true);
    try {
      const next = await api.settings.retention.get();
      setSavedRetentionSettings(next);
      setRetentionSettings(next);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.settingsRetentionDeletion.retentionLoadedFailed,
      );
    } finally {
      setRetentionSettingsLoading(false);
    }
  }, [messages.settingsRetentionDeletion.retentionLoadedFailed]);

  useEffect(() => {
    if (!enabled) return;
    void fetchRetentionSettings();
  }, [enabled, fetchRetentionSettings]);

  const setRetentionDays = (key: RetentionSettingKey, value: number | null) => {
    setRetentionSettings((current) =>
      current
        ? { ...current, policies: { ...policiesOf(current), [key]: value } }
        : current,
    );
  };

  const previewPolicyChange = useCallback(async () => {
    if (!retentionSettings || !savedRetentionSettings || !policyIsDestructive)
      return;
    const nextOperationId =
      policyOperationId ?? newRetentionOperationId("retention-policy");
    const attemptId = newRetentionOperationId("retention-preview");
    setPolicyOperationId(nextOperationId);
    setPolicyPreflightLoading(true);
    try {
      const preview = await api.settings.retention.preflight({
        kind: "policy_change",
        operation_id: nextOperationId,
        preflight_attempt_id: attemptId,
        expected_settings_revision: retentionSettings.revision,
        policies: policiesOf(retentionSettings),
      });
      setPolicyPreflight(preview);
      setPolicyConfirmationPhrase("");
      setPolicyConfirmOpen(true);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : messages.settingsRetentionDeletion.preflightFailed,
      );
    } finally {
      setPolicyPreflightLoading(false);
    }
  }, [
    messages.settingsRetentionDeletion.preflightFailed,
    policyIsDestructive,
    policyOperationId,
    retentionSettings,
    savedRetentionSettings,
  ]);

  const submitRetentionSettings = useCallback(async () => {
    if (
      !retentionSettings ||
      !savedRetentionSettings ||
      !retentionSettingsDirty
    )
      return;
    if (
      policyIsDestructive &&
      (!policyPreflight || !isPolicyPreflightValid || !policyOperationId)
    )
      return;
    setRetentionSettingsSaving(true);
    try {
      const response = await api.settings.retention.update({
        operation_id: policyOperationId ?? newRetentionOperationId("retention-policy"),
        expected_revision: savedRetentionSettings.revision,
        policies: policiesOf(retentionSettings),
        ...(policyIsDestructive && policyPreflight
          ? {
              preflight_token: policyPreflight.preflight_token,
              confirmation: { keyword: policyConfirmationPhrase.trim() },
            }
          : {}),
      });
      setSavedRetentionSettings(response.settings);
      setRetentionSettings(response.settings);
      setPolicyPreflight(null);
      setPolicyConfirmOpen(false);
      setPolicyConfirmationPhrase("");
      setPolicyOperationId(null);
      setRecentlySavedSection?.("retention");
      toast.success(messages.settingsRetentionDeletion.retentionUpdated);
      void onJobsMutation();
    } catch (error) {
      setPolicyPreflight(null);
      setPolicyConfirmationPhrase("");
      if (isStaleRetentionPreflightError(error)) {
        // Keep the confirmation surface open, discard the sealed capability
        // and refresh authoritative settings/coverage. A stale token is
        // never retried with the same body.
        setPolicyConfirmOpen(true);
        void fetchRetentionSettings();
      }
      toast.error(
        error instanceof Error
          ? error.message
          : messages.settingsRetentionDeletion.retentionUpdateFailed,
      );
    } finally {
      setRetentionSettingsSaving(false);
    }
  }, [
    fetchRetentionSettings,
    isPolicyPreflightValid,
    messages.settingsRetentionDeletion.retentionUpdateFailed,
    messages.settingsRetentionDeletion.retentionUpdated,
    onJobsMutation,
    policyConfirmationPhrase,
    policyIsDestructive,
    policyOperationId,
    policyPreflight,
    retentionSettings,
    retentionSettingsDirty,
    savedRetentionSettings,
    setRecentlySavedSection,
  ]);

  const handleSaveRetentionSettings = useCallback(async () => {
    if (!retentionSettings || !retentionSettingsDirty) return;
    if (policyIsDestructive && !policyPreflight) {
      await previewPolicyChange();
      return;
    }
    await submitRetentionSettings();
  }, [
    policyIsDestructive,
    policyPreflight,
    previewPolicyChange,
    retentionSettings,
    retentionSettingsDirty,
    submitRetentionSettings,
  ]);

  const applyRecommendation = () => {
    const recommendation = retentionSettings?.recommendations[0];
    if (!recommendation || !retentionSettings) return;
    setRetentionSettings({
      ...retentionSettings,
      policies: { ...recommendation.policies },
    });
  };

  return {
    handleSaveRetentionSettings,
    isPolicyPhraseValid,
    isPolicyPreflightSemanticallyComplete,
    isPolicyPreflightValid,
    policyConfirmOpen,
    policyConfirmationPhrase,
    policyIsDestructive,
    policyPreflight,
    policyPreflightLoading,
    retentionSettings,
    retentionSettingsDirty,
    retentionSettingsLoading,
    retentionSettingsSaving,
    setPolicyConfirmOpen,
    setPolicyConfirmationPhrase,
    setRetentionDays,
    submitRetentionSettings,
    applyRecommendation,
    refreshRetentionSettings: fetchRetentionSettings,
  };
}
