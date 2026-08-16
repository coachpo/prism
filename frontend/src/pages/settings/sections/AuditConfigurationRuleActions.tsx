import { Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import type { HeaderBlocklistRule } from "@/lib/types";

interface AuditConfigurationRuleActionsProps {
  locked: boolean;
  rule: HeaderBlocklistRule;
  onEditRule?: (rule: HeaderBlocklistRule) => void;
  onDeleteRule?: (rule: HeaderBlocklistRule) => void;
}

export function AuditConfigurationRuleActions({
  locked,
  rule,
  onEditRule,
  onDeleteRule,
}: AuditConfigurationRuleActionsProps) {
  const { messages } = useLocale();

  return (
    <div className="flex justify-end gap-2">
      <Button
        variant="ghost"
        size="icon-sm"
        disabled={locked}
        aria-label={messages.common.edit}
        onClick={locked || !onEditRule ? undefined : () => onEditRule(rule)}
      >
        <Pencil />
      </Button>
      <Button
        variant="ghost"
        size="icon-sm"
        className="text-destructive hover:text-destructive"
        disabled={locked}
        aria-label={messages.settingsDialogs.delete}
        onClick={locked || !onDeleteRule ? undefined : () => onDeleteRule(rule)}
      >
        <Trash2 />
      </Button>
    </div>
  );
}
