import { Lock, Pencil, Plus } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import type { HeaderBlocklistRule } from "@/lib/types";
import { OperatorLoadingState } from "@/shared/design-system";
import { AuditConfigurationRuleSection } from "./AuditConfigurationRuleSection";

interface AuditConfigurationRulesPanelProps {
  customRules: HeaderBlocklistRule[];
  loadingRules: boolean;
  onDeleteRule: (rule: HeaderBlocklistRule | null) => void;
  onEditRule: (rule: HeaderBlocklistRule) => void;
  onOpenAddRuleDialog: () => void;
  onOpenChangeSystemRules: (open: boolean) => void;
  onOpenChangeUserRules: (open: boolean) => void;
  onToggleRule: (rule: HeaderBlocklistRule, checked: boolean) => Promise<void>;
  systemRules: HeaderBlocklistRule[];
  systemRulesOpen: boolean;
  userRulesOpen: boolean;
}

export function AuditConfigurationRulesPanel({
  customRules,
  loadingRules,
  onDeleteRule,
  onEditRule,
  onOpenAddRuleDialog,
  onOpenChangeSystemRules,
  onOpenChangeUserRules,
  onToggleRule,
  systemRules,
  systemRulesOpen,
  userRulesOpen,
}: AuditConfigurationRulesPanelProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuditRules;
  return (
    <>
      <div className="flex items-center justify-between gap-3">
        <p className="text-xs text-muted-foreground">
          {copy.description}
        </p>
        <Button size="sm" variant="outline" onClick={onOpenAddRuleDialog}>
          <Plus data-icon="inline-start" />
          {copy.addRule}
        </Button>
      </div>

      {loadingRules ? (
        <OperatorLoadingState title={copy.loadingRules} />
      ) : (
        <>
          <AuditConfigurationRuleSection
            emptyState={copy.noSystemRules}
            icon={<Lock className="h-3.5 w-3.5 text-muted-foreground" />}
            locked
            toggleLocked={false}
            open={systemRulesOpen}
            rules={systemRules}
            title={copy.systemRulesLocked}
            onOpenChange={onOpenChangeSystemRules}
            onToggleRule={onToggleRule}
          />

          <AuditConfigurationRuleSection
            emptyState={copy.noCustomRules}
            icon={<Pencil className="h-3.5 w-3.5 text-muted-foreground" />}
            locked={false}
            open={userRulesOpen}
            rules={customRules}
            title={copy.customRules}
            onOpenChange={onOpenChangeUserRules}
            onToggleRule={onToggleRule}
            onEditRule={onEditRule}
            onDeleteRule={onDeleteRule}
          />
        </>
      )}
    </>
  );
}
