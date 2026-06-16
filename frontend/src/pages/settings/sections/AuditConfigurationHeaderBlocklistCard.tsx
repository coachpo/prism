import type { RefObject } from "react";
import { Ban } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import type { HeaderBlocklistRule } from "@/lib/types";
import { OperatorSectionCard } from "@/shared/design-system";
import { AuditConfigurationRulesPanel } from "./AuditConfigurationRulesPanel";

interface AuditConfigurationHeaderBlocklistCardProps {
  cardRef?: RefObject<HTMLDivElement | null>;
  className?: string;
  loadingRules: boolean;
  systemRulesOpen: boolean;
  setSystemRulesOpen: (open: boolean) => void;
  systemRules: HeaderBlocklistRule[];
  userRulesOpen: boolean;
  setUserRulesOpen: (open: boolean) => void;
  customRules: HeaderBlocklistRule[];
  handleToggleRule: (rule: HeaderBlocklistRule, checked: boolean) => Promise<void>;
  openAddRuleDialog: () => void;
  openEditRuleDialog: (rule: HeaderBlocklistRule) => void;
  setDeleteRuleConfirm: (rule: HeaderBlocklistRule | null) => void;
}

export function AuditConfigurationHeaderBlocklistCard({
  cardRef,
  className,
  loadingRules,
  systemRulesOpen,
  setSystemRulesOpen,
  systemRules,
  userRulesOpen,
  setUserRulesOpen,
  customRules,
  handleToggleRule,
  openAddRuleDialog,
  openEditRuleDialog,
  setDeleteRuleConfirm,
}: AuditConfigurationHeaderBlocklistCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAudit;

  return (
    <OperatorSectionCard
      ref={cardRef}
      className={className}
      title={(
        <span className="flex items-center gap-2">
            <Ban data-icon="inline-start" />
            {copy.headerBlocklist}
        </span>
      )}
      description={copy.stripsHeadersBeforeSendingUpstream}
      contentClassName="flex flex-col gap-3"
    >
        <AuditConfigurationRulesPanel
          customRules={customRules}
          loadingRules={loadingRules}
          onDeleteRule={setDeleteRuleConfirm}
          onEditRule={openEditRuleDialog}
          onOpenAddRuleDialog={openAddRuleDialog}
          onOpenChangeSystemRules={setSystemRulesOpen}
          onOpenChangeUserRules={setUserRulesOpen}
          onToggleRule={handleToggleRule}
          systemRules={systemRules}
          systemRulesOpen={systemRulesOpen}
          userRulesOpen={userRulesOpen}
        />
    </OperatorSectionCard>
  );
}
