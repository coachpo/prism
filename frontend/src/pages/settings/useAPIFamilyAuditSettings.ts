import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  ApiFamily,
  AuditAPIFamilySetting,
  AuditPolicyMode,
  AuditSettingsResponse,
  AuditStorageSummary,
} from "@/lib/types";
import { toast } from "sonner";
import type { SettingsSaveSection } from "./settingsSaveTypes";

interface UseAPIFamilyAuditSettingsInput {
  enabled: boolean;
  revision: number;
  setRecentlySavedSection: (section: SettingsSaveSection) => void;
}

export const AUDIT_API_FAMILIES = ["openai", "anthropic", "gemini"] as const satisfies readonly ApiFamily[];

const DEFAULT_API_FAMILY_AUDIT_SETTINGS: AuditAPIFamilySetting[] = AUDIT_API_FAMILIES.map((api_family) => ({
  api_family,
  audit_enabled: false,
  audit_capture_bodies: false,
}));

function getMessages() {
  return getStaticMessages();
}

function normalizeAPIFamilyAuditSettings(
  settings: AuditAPIFamilySetting[] | AuditSettingsResponse["policies"] | undefined,
): AuditAPIFamilySetting[] {
  const byFamily = new Map(
    settings?.map((setting) => {
      if ("family" in setting) {
        return [setting.family, setting.mode] as const;
      }
      return [setting.api_family, setting.audit_enabled
        ? setting.audit_capture_bodies ? "body_capture" : "metadata_only"
        : "disabled"] as const;
    }),
  );

  return AUDIT_API_FAMILIES.map((api_family) => {
    const mode = byFamily.get(api_family) as AuditPolicyMode | undefined;
    return {
      api_family,
      audit_enabled: mode === "metadata_only" || mode === "body_capture",
      audit_capture_bodies: mode === "body_capture",
    };
  });
}

function toAuditPolicies(settings: AuditAPIFamilySetting[]) {
  return settings.map((setting) => ({
    family: setting.api_family,
    mode: (setting.audit_enabled
      ? setting.audit_capture_bodies ? "body_capture" : "metadata_only"
      : "disabled") as AuditPolicyMode,
  }));
}

function areAPIFamilyAuditSettingsEqual(
  left: AuditAPIFamilySetting[],
  right: AuditAPIFamilySetting[],
): boolean {
  if (left.length !== right.length) {
    return false;
  }

  for (let index = 0; index < left.length; index += 1) {
    if (
      left[index].api_family !== right[index].api_family ||
      left[index].audit_enabled !== right[index].audit_enabled ||
      left[index].audit_capture_bodies !== right[index].audit_capture_bodies
    ) {
      return false;
    }
  }

  return true;
}

export function useAPIFamilyAuditSettings({
  enabled,
  revision,
  setRecentlySavedSection,
}: UseAPIFamilyAuditSettingsInput) {
  const [apiFamilyAuditSettings, setApiFamilyAuditSettings] = useState<AuditAPIFamilySetting[]>(
    DEFAULT_API_FAMILY_AUDIT_SETTINGS,
  );
  const [auditRevision, setAuditRevision] = useState("1");
  const [auditStorageSummary, setAuditStorageSummary] = useState<AuditStorageSummary | null>(null);
  const [auditStorageLoading, setAuditStorageLoading] = useState(false);
  const [auditStorageFailed, setAuditStorageFailed] = useState(false);
  const [savedApiFamilyAuditSettings, setSavedApiFamilyAuditSettings] = useState<AuditAPIFamilySetting[]>(
    DEFAULT_API_FAMILY_AUDIT_SETTINGS,
  );
  const [loadingAPIFamilyAuditSettings, setLoadingAPIFamilyAuditSettings] = useState(false);
  const [savingAPIFamilyAuditSettings, setSavingAPIFamilyAuditSettings] = useState(false);
  const apiFamilyAuditSettingsRequestIdRef = useRef(0);

  const fetchAPIFamilyAuditSettings = useCallback(async () => {
    const requestId = ++apiFamilyAuditSettingsRequestIdRef.current;
    setLoadingAPIFamilyAuditSettings(true);
    try {
      const response = await api.settings.audit.get();
      if (requestId !== apiFamilyAuditSettingsRequestIdRef.current) {
        return;
      }
      const normalized = normalizeAPIFamilyAuditSettings(response.policies);
      setAuditRevision(response.revision);
      setApiFamilyAuditSettings(normalized);
      setSavedApiFamilyAuditSettings(normalized);
    } catch {
      if (requestId !== apiFamilyAuditSettingsRequestIdRef.current) {
        return;
      }
      toast.error(getMessages().settingsAuditData.loadAPIFamilySettingsFailed);
    } finally {
      if (requestId === apiFamilyAuditSettingsRequestIdRef.current) {
        setLoadingAPIFamilyAuditSettings(false);
      }
    }
  }, []);

  const fetchStorageSummary = useCallback(async () => {
    setAuditStorageLoading(true);
    try {
      setAuditStorageSummary(await api.settings.audit.storageSummary());
      setAuditStorageFailed(false);
    } catch {
      // Keep the last successful snapshot on screen and mark it stale. A
      // failed refresh is not evidence that the storage facts are gone.
      setAuditStorageFailed(true);
    } finally {
      setAuditStorageLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    void revision;
    void fetchAPIFamilyAuditSettings();
    void fetchStorageSummary();
  }, [enabled, fetchAPIFamilyAuditSettings, fetchStorageSummary, revision]);

  const apiFamilyAuditSettingsDirty = useMemo(
    () => !areAPIFamilyAuditSettingsEqual(savedApiFamilyAuditSettings, apiFamilyAuditSettings),
    [apiFamilyAuditSettings, savedApiFamilyAuditSettings],
  );

  const setAPIFamilyAuditEnabled = (apiFamily: ApiFamily, checked: boolean) => {
    setApiFamilyAuditSettings((prev) =>
      normalizeAPIFamilyAuditSettings(
        prev.map((setting) =>
          setting.api_family === apiFamily
            ? {
                ...setting,
                audit_enabled: checked,
                audit_capture_bodies: checked ? setting.audit_capture_bodies : false,
              }
            : setting,
        ),
      ),
    );
  };

  const setAPIFamilyAuditCaptureBodies = (apiFamily: ApiFamily, checked: boolean) => {
    setApiFamilyAuditSettings((prev) =>
      normalizeAPIFamilyAuditSettings(
        prev.map((setting) =>
          setting.api_family === apiFamily
            ? {
                ...setting,
                audit_capture_bodies: setting.audit_enabled && checked,
              }
            : setting,
        ),
      ),
    );
  };

  const handleSaveAPIFamilyAuditSettings = async () => {
    const payload = {
      operation_id: typeof crypto !== "undefined" && typeof crypto.randomUUID === "function"
        ? crypto.randomUUID()
        : `settings-audit-${Date.now()}`,
      expected_revision: auditRevision,
      policies: toAuditPolicies(normalizeAPIFamilyAuditSettings(apiFamilyAuditSettings)),
    };

    setSavingAPIFamilyAuditSettings(true);
    try {
      const saved = await api.settings.audit.update(payload);
      const normalized = normalizeAPIFamilyAuditSettings(saved.settings.policies);
      setAuditRevision(saved.settings.revision);
      setApiFamilyAuditSettings(normalized);
      setSavedApiFamilyAuditSettings(normalized);
      void fetchStorageSummary();
      setRecentlySavedSection("audit");
      toast.success(getMessages().settingsAuditData.apiFamilySettingsSaved);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : getMessages().settingsAuditData.saveAPIFamilySettingsFailed,
      );
    } finally {
      setSavingAPIFamilyAuditSettings(false);
    }
  };

  return {
    apiFamilyAuditSettings,
    apiFamilyAuditSettingsDirty,
    auditStorageFailed,
    auditStorageLoading,
    auditStorageSummary,
    refreshAuditStorage: fetchStorageSummary,
    handleSaveAPIFamilyAuditSettings,
    loadingAPIFamilyAuditSettings,
    savingAPIFamilyAuditSettings,
    setAPIFamilyAuditCaptureBodies,
    setAPIFamilyAuditEnabled,
  };
}
