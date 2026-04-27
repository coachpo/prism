import { ChevronRight, Fingerprint, Lock, Pencil, Plus, Trash2 } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
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
      <CollapsibleTrigger className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm font-medium transition-colors hover:bg-muted/50">
        <ChevronRight className={cn("h-4 w-4 transition-transform", open && "rotate-90")} />
        {locked ? <Lock className="h-3.5 w-3.5 text-muted-foreground" /> : <Pencil className="h-3.5 w-3.5 text-muted-foreground" />}
        {title}
        <span className="text-xs text-muted-foreground">({rules.length})</span>
      </CollapsibleTrigger>
      <CollapsibleContent>
        {rules.length === 0 ? (
          <div className="mt-1.5 rounded-md border px-3 py-3 text-sm text-muted-foreground">
            {emptyState}
          </div>
        ) : (
          <div className="mt-1.5 rounded-md border">
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
                        className={toggleLocked ? undefined : "data-[state=checked]:bg-emerald-500"}
                      />
                    </TableCell>
                    <TableCell className="font-medium">
                      <div className="inline-flex items-center gap-2">
                        {rule.name}
                        {locked ? <Lock className="h-3 w-3 text-muted-foreground" /> : null}
                      </div>
                    </TableCell>
                    <TableCell>
                      <code className="rounded bg-muted px-[0.3rem] py-[0.2rem] font-mono text-sm">
                        {rule.pattern}
                      </code>
                    </TableCell>
                    <TableCell className="text-right">
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
    <Card data-testid="audit-user-agent-client-rules-card">
      <CardHeader className="pb-3">
        <div className="space-y-1">
          <CardTitle className="flex items-center gap-2 text-sm">
            <Fingerprint className="h-4 w-4" />
            {copy.userAgentClientRules}
          </CardTitle>
          <CardDescription className="text-xs">
            {copy.classifyClientsFromUserAgent}
          </CardDescription>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
          <div className="space-y-2 rounded-md border bg-muted/30 px-3 py-2 text-xs text-muted-foreground sm:flex-1">
            <p>{rulesCopy.description}</p>
            <p>
              <span className="font-medium text-foreground">{rulesCopy.systemRulesLocked}:</span>{" "}
              {rulesCopy.systemRulesExplanation}
            </p>
            <p>
              <span className="font-medium text-foreground">{rulesCopy.customRules}:</span>{" "}
              {rulesCopy.customRulesExplanation}
            </p>
            <p>{rulesCopy.precedenceExplanation}</p>
          </div>
          <Button size="sm" variant="outline" onClick={openAddRuleDialog} className="sm:self-start">
            <Plus className="mr-2 h-3.5 w-3.5" />
            {rulesCopy.addRule}
          </Button>
        </div>

        {loadingRules ? (
          <div className="flex h-24 items-center justify-center text-sm text-muted-foreground">
            {rulesCopy.loadingRules}
          </div>
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
      </CardContent>
    </Card>
  );
}
