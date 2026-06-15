import { ChevronRight, Fingerprint, Lock, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { useLocale } from "@/i18n/useLocale";
import type { UserAgentClientRule } from "@/lib/types";
import { cn } from "@/lib/utils";
import { OperatorEmptyState, OperatorLoadingState, OperatorSectionCard } from "@/shared/design-system";

interface UserAgentClientRuleSectionProps {
  emptyState: string;
  locked: boolean;
  open: boolean;
  rules: UserAgentClientRule[];
  title: string;
  toggleLocked?: boolean;
  onOpenChange: (open: boolean) => void;
  onToggleRule?: (rule: UserAgentClientRule, checked: boolean) => Promise<void>;
  onEditRule?: (rule: UserAgentClientRule) => void;
  onDeleteRule?: (rule: UserAgentClientRule | null) => void;
}

interface AuditConfigurationUserAgentClientRulesCardProps {
  loadingRules: boolean;
  systemRulesOpen: boolean;
  setSystemRulesOpen: (open: boolean) => void;
  systemRules: UserAgentClientRule[];
  userRulesOpen: boolean;
  setUserRulesOpen: (open: boolean) => void;
  customRules: UserAgentClientRule[];
  handleToggleRule: (rule: UserAgentClientRule, checked: boolean) => Promise<void>;
  openAddRuleDialog: () => void;
  openEditRuleDialog: (rule: UserAgentClientRule) => void;
  setDeleteRuleConfirm: (rule: UserAgentClientRule | null) => void;
}

function UserAgentClientRuleSection({
  emptyState,
  locked,
  open,
  rules,
  title,
  toggleLocked = locked,
  onOpenChange,
  onToggleRule,
  onEditRule,
  onDeleteRule,
}: UserAgentClientRuleSectionProps) {
  const { messages } = useLocale();

  return (
    <Collapsible open={open} onOpenChange={onOpenChange}>
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm font-medium transition-colors hover:bg-surface-container-low">
        <ChevronRight className={cn("h-4 w-4 transition-transform", open && "rotate-90")} />
        {locked ? <Lock className="h-3.5 w-3.5 text-muted-foreground" /> : <Pencil className="h-3.5 w-3.5 text-muted-foreground" />}
        {title}
        <span className="text-xs text-muted-foreground">({rules.length})</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        {rules.length === 0 ? (
          <OperatorEmptyState className="mt-1.5 py-8" title={emptyState} />
        ) : (
          <div className="operator-table-shell mt-1.5 overflow-hidden rounded-md border border-outline-variant">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[90px]">{messages.settingsDialogs.enabled}</TableHead>
                  <TableHead>{messages.settingsDialogs.name}</TableHead>
                  <TableHead>{messages.settingsDialogs.regexPattern}</TableHead>
                  <TableHead className="w-[120px] text-right">
                    {messages.pricingTemplatesUi.actions}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rules.map((rule) => (
                  <TableRow key={rule.id}>
                    <TableCell>
                      <Switch
                        checked={rule.enabled}
                        disabled={toggleLocked}
                        onCheckedChange={
                          toggleLocked || !onToggleRule
                            ? undefined
                            : (checked) => void onToggleRule(rule, checked)
                        }
                      />
                    </TableCell>
                    <TableCell className="font-medium">
                      <div className="inline-flex items-center gap-2">
                        {rule.name}
                        {locked ? <Lock className="h-3 w-3 text-muted-foreground" /> : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <code className="rounded bg-surface-container-low px-[0.3rem] py-[0.2rem] font-mono text-sm">
                        {rule.pattern}
                      </code>
                    </TableCell>
                    <TableCell className="text-right">
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
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CollapsibleContent>
    </Collapsible>
  );
}

export function AuditConfigurationUserAgentClientRulesCard({
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
}: AuditConfigurationUserAgentClientRulesCardProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAudit;
  const rulesCopy = messages.settingsAuditUserAgentRules;

  return (
    <OperatorSectionCard
      data-testid="audit-user-agent-client-rules-card"
      title={(
        <span className="flex items-center gap-2">
            <Fingerprint data-icon="inline-start" />
            {copy.userAgentClientRules}
        </span>
      )}
      description={copy.classifyClientsFromUserAgent}
      contentClassName="flex flex-col gap-3"
    >
        <div className="flex justify-end">
          <Button size="sm" variant="outline" onClick={openAddRuleDialog}>
            <Plus data-icon="inline-start" />
            {rulesCopy.addRule}
          </Button>
        </div>

        {loadingRules ? (
          <OperatorLoadingState title={rulesCopy.loadingRules} />
        ) : (
          <>
            <UserAgentClientRuleSection
              emptyState={rulesCopy.noSystemRules}
              locked
              toggleLocked={false}
              open={systemRulesOpen}
              rules={systemRules}
              title={rulesCopy.systemRulesLocked}
              onOpenChange={setSystemRulesOpen}
              onToggleRule={handleToggleRule}
            />
            <UserAgentClientRuleSection
              emptyState={rulesCopy.noCustomRules}
              locked={false}
              open={userRulesOpen}
              rules={customRules}
              title={rulesCopy.customRules}
              onOpenChange={setUserRulesOpen}
              onToggleRule={handleToggleRule}
              onEditRule={openEditRuleDialog}
              onDeleteRule={setDeleteRuleConfirm}
            />
          </>
        )}
    </OperatorSectionCard>
  );
}
