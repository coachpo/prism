import type { ReactNode } from "react";
import { ChevronRight } from "lucide-react";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { cn } from "@/lib/utils";
import type { HeaderBlocklistRule } from "@/lib/types";
import { AuditConfigurationRuleTable } from "./AuditConfigurationRuleTable";

interface AuditConfigurationRuleSectionProps {
  emptyState: string;
  icon: ReactNode;
  locked: boolean;
  open: boolean;
  rules: HeaderBlocklistRule[];
  title: string;
  onOpenChange: (open: boolean) => void;
  onToggleRule?: (rule: HeaderBlocklistRule, checked: boolean) => Promise<void>;
  onEditRule?: (rule: HeaderBlocklistRule) => void;
  onDeleteRule?: (rule: HeaderBlocklistRule) => void;
}

export function AuditConfigurationRuleSection({
  emptyState,
  icon,
  locked,
  open,
  rules,
  title,
  onOpenChange,
  onToggleRule,
  onEditRule,
  onDeleteRule,
}: AuditConfigurationRuleSectionProps) {
  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm font-medium transition-colors hover:bg-muted/50">
        <ChevronRight className={cn("h-4 w-4 transition-transform", open && "rotate-90")} />
        {icon}
        {title}
        <span className="text-xs text-muted-foreground">({rules.length})</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        {rules.length === 0 ? (
          <div className="mt-1.5 rounded-md border px-3 py-3 text-sm text-muted-foreground">
            {emptyState}
          </div>
        ) : (
          <AuditConfigurationRuleTable
            locked={locked}
            rules={rules}
            onToggleRule={onToggleRule}
            onEditRule={onEditRule}
            onDeleteRule={onDeleteRule}
          />
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}
