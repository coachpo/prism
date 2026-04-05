import { Pencil, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
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
  return (
    <div className="flex justify-end gap-2">
      <Button
        variant="ghost"
        size="icon"
        className="h-8 w-8"
        disabled={locked}
        onClick={locked || !onEditRule ? undefined : () => onEditRule(rule)}
      >
        <Pencil className="h-4 w-4" />
      </Button>
      <Button
        variant="ghost"
        size="icon"
        className="h-8 w-8 text-destructive hover:text-destructive"
        disabled={locked}
        onClick={locked || !onDeleteRule ? undefined : () => onDeleteRule(rule)}
      >
        <Trash2 className="h-4 w-4" />
      </Button>
    </div>
  );
}
