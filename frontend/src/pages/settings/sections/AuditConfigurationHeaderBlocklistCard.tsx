import { Ban } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import type { HeaderBlocklistRule } from "@/lib/types";
import { AuditConfigurationRulesPanel } from "./AuditConfigurationRulesPanel";

interface AuditConfigurationHeaderBlocklistCardProps {
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
    <Card>
      <CardHeader className="pb-3">
        <div className="space-y-1">
          <CardTitle className="flex items-center gap-2 text-sm">
            <Ban className="h-4 w-4" />
            {copy.headerBlocklist}
          </CardTitle>
          <CardDescription className="text-xs">
            {copy.stripsHeadersBeforeSendingUpstream}
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
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
      </CardContent>
    </Card>
  );
}
