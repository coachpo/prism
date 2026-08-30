import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  UserAgentClientRule,
  UserAgentClientRuleCreate,
} from "@/lib/types";
import { toast } from "sonner";

interface UseUserAgentClientRulesInput {
  enabled: boolean;
  revision: number;
}

const DEFAULT_USER_AGENT_CLIENT_RULE_FORM: UserAgentClientRuleCreate = {
  name: "",
  pattern: "",
  enabled: true,
};

function getMessages() {
  return getStaticMessages();
}

export function useUserAgentClientRules({ enabled, revision }: UseUserAgentClientRulesInput) {
  const [userAgentClientRules, setUserAgentClientRules] = useState<UserAgentClientRule[]>([]);
  const [loadingUserAgentClientRules, setLoadingUserAgentClientRules] = useState(false);
  const [userAgentClientRuleDialogOpen, setUserAgentClientRuleDialogOpen] = useState(false);
  const [editingUserAgentClientRule, setEditingUserAgentClientRule] = useState<UserAgentClientRule | null>(null);
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
  const [userAgentClientUserRulesOpen, setUserAgentClientUserRulesOpen] = useState(false);
  const userAgentClientRulesRequestIdRef = useRef(0);

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
    void fetchUserAgentClientRules();
  }, [enabled, fetchUserAgentClientRules, revision]);

  const userAgentClientSystemRules = useMemo(
    () => userAgentClientRules.filter((rule) => rule.is_system),
    [userAgentClientRules],
  );
  const userAgentClientCustomRules = useMemo(
    () => userAgentClientRules.filter((rule) => !rule.is_system),
    [userAgentClientRules],
  );

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
    deleteUserAgentClientRuleConfirm,
    deleteUserAgentClientRuleDialogOpen,
    displayedDeleteUserAgentClientRuleConfirm,
    editingUserAgentClientRule,
    handleDeleteUserAgentClientRule,
    handleSaveUserAgentClientRule,
    handleToggleUserAgentClientRule,
    loadingUserAgentClientRules,
    openAddUserAgentClientRuleDialog,
    openEditUserAgentClientRuleDialog,
    setDeleteUserAgentClientRuleConfirm,
    setUserAgentClientRuleDialogOpen,
    setUserAgentClientRuleForm,
    userAgentClientCustomRules,
    userAgentClientRuleDialogOpen,
    userAgentClientRuleForm,
    userAgentClientSystemRules,
    userAgentClientSystemRulesOpen,
    userAgentClientUserRulesOpen,
    setUserAgentClientSystemRulesOpen,
    setUserAgentClientUserRulesOpen,
  };
}
