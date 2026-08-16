import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  ApiFamily,
  AuditAPIFamilySetting,
  AuditPolicyMode,
  AuditSettingsResponse,
  AuditStorageSummary,
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
  UserAgentClientRule,
  UserAgentClientRuleCreate,
} from "@/lib/types";
import { toast } from "sonner";
import type { SettingsSaveSection } from "./settingsSaveTypes";

interface UseAuditConfigurationDataInput {
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

const DEFAULT_RULE_FORM: HeaderBlocklistRuleCreate = {
  name: "",
  match_type: "exact",
  pattern: "",
  enabled: true,
};

const DEFAULT_USER_AGENT_CLIENT_RULE_FORM: UserAgentClientRuleCreate = {
  name: "",
  pattern: "",
  enabled: true,
};

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

export function useAuditConfigurationData({
  enabled,
  revision,
  setRecentlySavedSection,
}: UseAuditConfigurationDataInput) {
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
  const [blocklistRules, setBlocklistRules] = useState<HeaderBlocklistRule[]>([]);
  const [userAgentClientRules, setUserAgentClientRules] = useState<UserAgentClientRule[]>([]);
  const [loadingRules, setLoadingRules] = useState(false);
  const [loadingUserAgentClientRules, setLoadingUserAgentClientRules] = useState(false);
  const [ruleDialogOpen, setRuleDialogOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<HeaderBlocklistRule | null>(null);
  const [ruleForm, setRuleForm] = useState<HeaderBlocklistRuleCreate>(DEFAULT_RULE_FORM);
  const [deleteRuleConfirm, setDeleteRuleConfirmState] = useState<HeaderBlocklistRule | null>(null);
  const [deleteRuleDialogOpen, setDeleteRuleDialogOpen] = useState(false);
  const [displayedDeleteRuleConfirm, setDisplayedDeleteRuleConfirm] = useState<HeaderBlocklistRule | null>(null);
  const [systemRulesOpen, setSystemRulesOpen] = useState(false);
  const [userRulesOpen, setUserRulesOpen] = useState(true);
  const [userAgentClientRuleDialogOpen, setUserAgentClientRuleDialogOpen] = useState(false);
  const [editingUserAgentClientRule, setEditingUserAgentClientRule] =
    useState<UserAgentClientRule | null>(null);
  const [userAgentClientRuleForm, setUserAgentClientRuleForm] = useState<UserAgentClientRuleCreate>(
    DEFAULT_USER_AGENT_CLIENT_RULE_FORM,
  );
  const [deleteUserAgentClientRuleConfirm, setDeleteUserAgentClientRuleConfirmState] =
    useState<UserAgentClientRule | null>(null);
  const [deleteUserAgentClientRuleDialogOpen, setDeleteUserAgentClientRuleDialogOpen] =
    useState(false);
  const [displayedDeleteUserAgentClientRuleConfirm, setDisplayedDeleteUserAgentClientRuleConfirm] =
    useState<UserAgentClientRule | null>(null);
  const [userAgentClientSystemRulesOpen, setUserAgentClientSystemRulesOpen] = useState(false);
  const [userAgentClientUserRulesOpen, setUserAgentClientUserRulesOpen] = useState(true);
  const apiFamilyAuditSettingsRequestIdRef = useRef(0);
  const rulesRequestIdRef = useRef(0);
  const userAgentClientRulesRequestIdRef = useRef(0);

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

  const fetchRules = useCallback(async () => {
    const requestId = ++rulesRequestIdRef.current;
    setLoadingRules(true);
    try {
      const rules = await api.config.headerBlocklistRules.list(true);
      if (requestId !== rulesRequestIdRef.current) {
        return;
      }
      setBlocklistRules(rules);
    } catch {
      if (requestId !== rulesRequestIdRef.current) {
        return;
      }
      toast.error(getMessages().settingsAuditData.loadHeaderRulesFailed);
    } finally {
      if (requestId === rulesRequestIdRef.current) {
        setLoadingRules(false);
      }
    }
  }, []);

  const fetchUserAgentClientRules = useCallback(async () => {
    const requestId = ++userAgentClientRulesRequestIdRef.current;
    setLoadingUserAgentClientRules(true);
    try {
      const rules = await api.config.userAgentClientRules.list(true);
      if (requestId !== userAgentClientRulesRequestIdRef.current) {
        return;
      }
      setUserAgentClientRules(rules);
    } catch {
      if (requestId !== userAgentClientRulesRequestIdRef.current) {
        return;
      }
      toast.error(getMessages().settingsAuditData.loadUserAgentClientRulesFailed);
    } finally {
      if (requestId === userAgentClientRulesRequestIdRef.current) {
        setLoadingUserAgentClientRules(false);
      }
    }
  }, []);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    void revision;
    void fetchAPIFamilyAuditSettings();
    void fetchStorageSummary();
    void fetchRules();
    void fetchUserAgentClientRules();
  }, [enabled, fetchAPIFamilyAuditSettings, fetchRules, fetchStorageSummary, fetchUserAgentClientRules, revision]);

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

  const systemRules = useMemo(() => blocklistRules.filter((rule) => rule.is_system), [blocklistRules]);
  const customRules = useMemo(() => blocklistRules.filter((rule) => !rule.is_system), [blocklistRules]);
  const userAgentClientSystemRules = useMemo(
    () => userAgentClientRules.filter((rule) => rule.is_system),
    [userAgentClientRules],
  );
  const userAgentClientCustomRules = useMemo(
    () => userAgentClientRules.filter((rule) => !rule.is_system),
    [userAgentClientRules],
  );

  const handleToggleRule = async (rule: HeaderBlocklistRule, checked: boolean) => {
    setBlocklistRules((prev) => prev.map((row) => (row.id === rule.id ? { ...row, enabled: checked } : row)));

    try {
      await api.config.headerBlocklistRules.update(rule.id, { enabled: checked });
    } catch {
      setBlocklistRules((prev) => prev.map((row) => (row.id === rule.id ? { ...row, enabled: !checked } : row)));
      toast.error(getMessages().settingsAuditData.updateRuleFailed);
    }
  };

  const openAddRuleDialog = () => {
    setEditingRule(null);
    setRuleForm(DEFAULT_RULE_FORM);
    setRuleDialogOpen(true);
  };

  const openEditRuleDialog = (rule: HeaderBlocklistRule) => {
    setEditingRule(rule);
    setRuleForm({
      name: rule.name,
      match_type: rule.match_type,
      pattern: rule.pattern,
      enabled: rule.enabled,
    });
    setRuleDialogOpen(true);
  };

  const handleSaveRule = async () => {
    if (!ruleForm.name || !ruleForm.pattern) {
      toast.error(getMessages().settingsAuditData.nameAndPatternRequired);
      return;
    }

    if (ruleForm.match_type === "prefix" && !ruleForm.pattern.endsWith("-")) {
      toast.error(getMessages().settingsAuditData.prefixPatternsHyphen);
      return;
    }

    try {
      if (editingRule) {
        const updatedRule = await api.config.headerBlocklistRules.update(editingRule.id, ruleForm);
        setBlocklistRules((prev) => prev.map((rule) => (rule.id === updatedRule.id ? updatedRule : rule)));
        toast.success(getMessages().settingsAuditData.ruleUpdated);
      } else {
        const createdRule = await api.config.headerBlocklistRules.create(ruleForm);
        setBlocklistRules((prev) => [...prev, createdRule]);
        toast.success(getMessages().settingsAuditData.ruleCreated);
      }
      setRuleDialogOpen(false);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : getMessages().settingsAuditData.saveRuleFailed);
    }
  };

  const handleDeleteRule = async () => {
    if (!deleteRuleConfirm) {
      return;
    }

    try {
      await api.config.headerBlocklistRules.delete(deleteRuleConfirm.id);
      setBlocklistRules((prev) => prev.filter((rule) => rule.id !== deleteRuleConfirm.id));
      toast.success(getMessages().settingsAuditData.ruleDeleted);
      setDeleteRuleDialogOpen(false);
      setDeleteRuleConfirmState(null);
    } catch {
      toast.error(getMessages().settingsAuditData.deleteRuleFailed);
    }
  };

  const setDeleteRuleConfirm = (rule: HeaderBlocklistRule | null) => {
    setDeleteRuleConfirmState(rule);

    if (rule) {
      setDisplayedDeleteRuleConfirm(rule);
      setDeleteRuleDialogOpen(true);
      return;
    }

    setDeleteRuleDialogOpen(false);
  };

  const handleToggleUserAgentClientRule = async (
    rule: UserAgentClientRule,
    checked: boolean,
  ) => {
    setUserAgentClientRules((prev) =>
      prev.map((row) => (row.id === rule.id ? { ...row, enabled: checked } : row)),
    );

    try {
      await api.config.userAgentClientRules.update(rule.id, { enabled: checked });
    } catch {
      setUserAgentClientRules((prev) =>
        prev.map((row) => (row.id === rule.id ? { ...row, enabled: !checked } : row)),
      );
      toast.error(getMessages().settingsAuditData.updateUserAgentClientRuleFailed);
    }
  };

  const openAddUserAgentClientRuleDialog = () => {
    setEditingUserAgentClientRule(null);
    setUserAgentClientRuleForm(DEFAULT_USER_AGENT_CLIENT_RULE_FORM);
    setUserAgentClientRuleDialogOpen(true);
  };

  const openEditUserAgentClientRuleDialog = (rule: UserAgentClientRule) => {
    setEditingUserAgentClientRule(rule);
    setUserAgentClientRuleForm({
      name: rule.name,
      pattern: rule.pattern,
      enabled: rule.enabled,
    });
    setUserAgentClientRuleDialogOpen(true);
  };

  const handleSaveUserAgentClientRule = async () => {
    if (!userAgentClientRuleForm.name || !userAgentClientRuleForm.pattern) {
      toast.error(getMessages().settingsAuditData.nameAndRegexRequired);
      return;
    }

    try {
      if (editingUserAgentClientRule) {
        const updatedRule = await api.config.userAgentClientRules.update(
          editingUserAgentClientRule.id,
          userAgentClientRuleForm,
        );
        setUserAgentClientRules((prev) =>
          prev.map((rule) => (rule.id === updatedRule.id ? updatedRule : rule)),
        );
        toast.success(getMessages().settingsAuditData.userAgentClientRuleUpdated);
      } else {
        const createdRule = await api.config.userAgentClientRules.create(userAgentClientRuleForm);
        setUserAgentClientRules((prev) => [...prev, createdRule]);
        toast.success(getMessages().settingsAuditData.userAgentClientRuleCreated);
      }
      setUserAgentClientRuleDialogOpen(false);
    } catch (error) {
      toast.error(
        error instanceof Error
          ? error.message
          : getMessages().settingsAuditData.saveUserAgentClientRuleFailed,
      );
    }
  };

  const handleDeleteUserAgentClientRule = async () => {
    if (!deleteUserAgentClientRuleConfirm) {
      return;
    }

    try {
      await api.config.userAgentClientRules.delete(deleteUserAgentClientRuleConfirm.id);
      setUserAgentClientRules((prev) =>
        prev.filter((rule) => rule.id !== deleteUserAgentClientRuleConfirm.id),
      );
      toast.success(getMessages().settingsAuditData.userAgentClientRuleDeleted);
      setDeleteUserAgentClientRuleDialogOpen(false);
      setDeleteUserAgentClientRuleConfirmState(null);
    } catch {
      toast.error(getMessages().settingsAuditData.deleteUserAgentClientRuleFailed);
    }
  };

  const setDeleteUserAgentClientRuleConfirm = (rule: UserAgentClientRule | null) => {
    setDeleteUserAgentClientRuleConfirmState(rule);

    if (rule) {
      setDisplayedDeleteUserAgentClientRuleConfirm(rule);
      setDeleteUserAgentClientRuleDialogOpen(true);
      return;
    }

    setDeleteUserAgentClientRuleDialogOpen(false);
  };

  return {
    apiFamilyAuditSettings,
    apiFamilyAuditSettingsDirty,
    auditStorageFailed,
    auditStorageLoading,
    auditStorageSummary,
    refreshAuditStorage: fetchStorageSummary,
    customRules,
    deleteRuleConfirm,
    deleteRuleDialogOpen,
    deleteUserAgentClientRuleConfirm,
    deleteUserAgentClientRuleDialogOpen,
    displayedDeleteRuleConfirm,
    displayedDeleteUserAgentClientRuleConfirm,
    editingRule,
    editingUserAgentClientRule,
    handleDeleteRule,
    handleDeleteUserAgentClientRule,
    handleSaveAPIFamilyAuditSettings,
    handleSaveRule,
    handleSaveUserAgentClientRule,
    handleToggleRule,
    handleToggleUserAgentClientRule,
    loadingAPIFamilyAuditSettings,
    loadingRules,
    loadingUserAgentClientRules,
    openAddRuleDialog,
    openAddUserAgentClientRuleDialog,
    openEditRuleDialog,
    openEditUserAgentClientRuleDialog,
    ruleDialogOpen,
    ruleForm,
    setDeleteRuleConfirm,
    setDeleteUserAgentClientRuleConfirm,
    setAPIFamilyAuditCaptureBodies,
    setAPIFamilyAuditEnabled,
    setRuleDialogOpen,
    setRuleForm,
    setSystemRulesOpen,
    setUserRulesOpen,
    setUserAgentClientRuleDialogOpen,
    setUserAgentClientRuleForm,
    setUserAgentClientSystemRulesOpen,
    setUserAgentClientUserRulesOpen,
    savingAPIFamilyAuditSettings,
    systemRules,
    systemRulesOpen,
    userAgentClientCustomRules,
    userAgentClientRuleDialogOpen,
    userAgentClientRuleForm,
    userAgentClientSystemRules,
    userAgentClientSystemRulesOpen,
    userAgentClientUserRulesOpen,
    userRulesOpen,
  };
}
