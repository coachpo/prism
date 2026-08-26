import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "@/lib/api";
import { getStaticMessages } from "@/i18n/staticMessages";
import type {
  HeaderBlocklistRule,
  HeaderBlocklistRuleCreate,
} from "@/lib/types";
import { toast } from "sonner";

interface UseHeaderBlocklistRulesInput {
  enabled: boolean;
  revision: number;
}

const DEFAULT_RULE_FORM: HeaderBlocklistRuleCreate = {
  name: "",
  match_type: "exact",
  pattern: "",
  enabled: true,
};

function getMessages() {
  return getStaticMessages();
}

export function useHeaderBlocklistRules({ enabled, revision }: UseHeaderBlocklistRulesInput) {
  const [blocklistRules, setBlocklistRules] = useState<HeaderBlocklistRule[]>([]);
  const [loadingRules, setLoadingRules] = useState(false);
  const [ruleDialogOpen, setRuleDialogOpen] = useState(false);
  const [editingRule, setEditingRule] = useState<HeaderBlocklistRule | null>(null);
  const [ruleForm, setRuleForm] = useState<HeaderBlocklistRuleCreate>(DEFAULT_RULE_FORM);
  const [deleteRuleConfirm, setDeleteRuleConfirmState] = useState<HeaderBlocklistRule | null>(null);
  const [deleteRuleDialogOpen, setDeleteRuleDialogOpen] = useState(false);
  const [displayedDeleteRuleConfirm, setDisplayedDeleteRuleConfirm] = useState<HeaderBlocklistRule | null>(null);
  const [systemRulesOpen, setSystemRulesOpen] = useState(false);
  const [userRulesOpen, setUserRulesOpen] = useState(true);
  const rulesRequestIdRef = useRef(0);

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

  useEffect(() => {
    if (!enabled) {
      return;
    }

    void revision;
    void fetchRules();
  }, [enabled, fetchRules, revision]);

  const systemRules = useMemo(() => blocklistRules.filter((rule) => rule.is_system), [blocklistRules]);
  const customRules = useMemo(() => blocklistRules.filter((rule) => !rule.is_system), [blocklistRules]);

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

  return {
    customRules,
    deleteRuleConfirm,
    deleteRuleDialogOpen,
    displayedDeleteRuleConfirm,
    editingRule,
    handleDeleteRule,
    handleSaveRule,
    handleToggleRule,
    loadingRules,
    openAddRuleDialog,
    openEditRuleDialog,
    ruleDialogOpen,
    ruleForm,
    setDeleteRuleConfirm,
    setRuleDialogOpen,
    setRuleForm,
    setSystemRulesOpen,
    setUserRulesOpen,
    systemRules,
    systemRulesOpen,
    userRulesOpen,
  };
}
