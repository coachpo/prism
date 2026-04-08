import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import { getSharedVendors, setSharedVendors } from "@/lib/referenceData";
import type {
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
  UserAgentClientRule,
  UserAgentClientRuleCreate,
  Vendor,
} from "@/lib/types";
import { toast } from "sonner";

interface UseAuditConfigurationDataInput {
  revision: number;
}

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

export function useAuditConfigurationData({ revision }: UseAuditConfigurationDataInput) {
  const [vendors, setVendors] = useState<Vendor[]>([]);
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
  const vendorsRequestIdRef = useRef(0);
  const rulesRequestIdRef = useRef(0);
  const userAgentClientRulesRequestIdRef = useRef(0);

  const fetchVendors = useCallback(async () => {
    const requestId = ++vendorsRequestIdRef.current;
    try {
      const data = await getSharedVendors(revision);
      if (requestId !== vendorsRequestIdRef.current) {
        return;
      }
      setVendors(data);
    } catch {
      if (requestId !== vendorsRequestIdRef.current) {
        return;
      }
      toast.error(getMessages().settingsAuditData.loadVendorsFailed);
    }
  }, [revision]);

  const commitVendors = useCallback(
    (updater: (current: Vendor[]) => Vendor[]) => {
      setVendors((current) => {
        const next = updater(current);
        setSharedVendors(revision, next);
        return next;
      });
    },
    [revision],
  );

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
    void fetchVendors();
    void fetchRules();
    void fetchUserAgentClientRules();
  }, [fetchUserAgentClientRules, fetchVendors, fetchRules]);

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

  const toggleAudit = async (vendorId: number, checked: boolean) => {
    commitVendors((prev) =>
      prev.map((vendor) =>
        vendor.id === vendorId ? { ...vendor, audit_enabled: checked } : vendor
      )
    );

    try {
      await api.vendors.update(vendorId, { audit_enabled: checked });
    } catch {
      commitVendors((prev) =>
        prev.map((vendor) =>
          vendor.id === vendorId ? { ...vendor, audit_enabled: !checked } : vendor
        )
      );
      toast.error(getMessages().settingsAuditData.updateVendorFailed);
    }
  };

  const toggleBodies = async (vendorId: number, checked: boolean) => {
    commitVendors((prev) =>
      prev.map((vendor) =>
        vendor.id === vendorId ? { ...vendor, audit_capture_bodies: checked } : vendor
      )
    );

    try {
      await api.vendors.update(vendorId, { audit_capture_bodies: checked });
    } catch {
      commitVendors((prev) =>
        prev.map((vendor) =>
          vendor.id === vendorId ? { ...vendor, audit_capture_bodies: !checked } : vendor
        )
      );
      toast.error(getMessages().settingsAuditData.updateVendorFailed);
    }
  };

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
    handleSaveRule,
    handleSaveUserAgentClientRule,
    handleToggleRule,
    handleToggleUserAgentClientRule,
    loadingRules,
    loadingUserAgentClientRules,
    openAddRuleDialog,
    openAddUserAgentClientRuleDialog,
    openEditRuleDialog,
    openEditUserAgentClientRuleDialog,
    vendors,
    ruleDialogOpen,
    ruleForm,
    setDeleteRuleConfirm,
    setDeleteUserAgentClientRuleConfirm,
    setRuleDialogOpen,
    setRuleForm,
    setSystemRulesOpen,
    setUserRulesOpen,
    setUserAgentClientRuleDialogOpen,
    setUserAgentClientRuleForm,
    setUserAgentClientSystemRulesOpen,
    setUserAgentClientUserRulesOpen,
    systemRules,
    systemRulesOpen,
    toggleAudit,
    toggleBodies,
    userAgentClientCustomRules,
    userAgentClientRuleDialogOpen,
    userAgentClientRuleForm,
    userAgentClientSystemRules,
    userAgentClientSystemRulesOpen,
    userAgentClientUserRulesOpen,
    userRulesOpen,
  };
}
