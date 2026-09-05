import type { Dispatch, FormEvent, SetStateAction } from "react";
import { Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useLocale } from "@/i18n/useLocale";
import type { UserAgentClientRule, UserAgentClientRuleCreate } from "@/lib/types";
import { OperatorInsetPanel, OperatorSwitchField } from "@/shared/design-system";

interface UserAgentClientRuleDialogProps {
  ruleDialogOpen: boolean;
  setRuleDialogOpen: (open: boolean) => void;
  editingRule: UserAgentClientRule | null;
  ruleForm: UserAgentClientRuleCreate;
  setRuleForm: Dispatch<SetStateAction<UserAgentClientRuleCreate>>;
  handleSaveRule: () => Promise<void>;
}

export function UserAgentClientRuleDialog({
  ruleDialogOpen,
  setRuleDialogOpen,
  editingRule,
  ruleForm,
  setRuleForm,
  handleSaveRule,
}: UserAgentClientRuleDialogProps) {
  const { messages } = useLocale();
  const copy = messages.settingsDialogs;

  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void handleSaveRule();
  };

  return (
    <Dialog open={ruleDialogOpen} onOpenChange={setRuleDialogOpen}>
      <DialogContent aria-describedby={undefined} size="md">
        <DialogHeader>
          <DialogTitle>
            {editingRule
              ? copy.userAgentClientRuleDialogEditTitle
              : copy.userAgentClientRuleDialogAddTitle}
          </DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="flex flex-col gap-5">
          <input type="hidden" name="enabled" value={String(ruleForm.enabled)} />
          <DialogBody>
            <OperatorInsetPanel className="text-sm text-muted-foreground">
              <div className="flex items-start gap-3">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      aria-label={copy.whyMatchUserAgentClients}
                      className="mt-0.5 shrink-0 rounded-md text-muted-foreground transition-colors hover:text-foreground"
                    >
                      <Info className="size-4" />
                    </button>
                  </TooltipTrigger>
                  <TooltipContent side="right" className="max-w-xs">
                    {copy.userAgentClientRulesTooltip}
                  </TooltipContent>
                </Tooltip>

                <div className="flex flex-col gap-1">
                  <p>{copy.userAgentClientRulesExamples}</p>
                </div>
              </div>
            </OperatorInsetPanel>

            <OperatorInsetPanel>
              <Field>
                <FieldLabel htmlFor="user-agent-client-rule-name">{copy.name}</FieldLabel>
                <Input
                  id="user-agent-client-rule-name"
                  name="name"
                  autoComplete="off"
                  value={ruleForm.name}
                  onChange={(event) => {
                    setRuleForm((prev) => ({ ...prev, name: event.target.value }));
                  }}
                  placeholder={copy.userAgentClientRuleNamePlaceholder}
                />
              </Field>

              <Field>
                <FieldLabel htmlFor="user-agent-client-rule-pattern">{copy.regexPattern}</FieldLabel>
                <Input
                  id="user-agent-client-rule-pattern"
                  name="pattern"
                  autoComplete="off"
                  value={ruleForm.pattern}
                  onChange={(event) => {
                    setRuleForm((prev) => ({ ...prev, pattern: event.target.value }));
                  }}
                  className="font-mono"
                  placeholder={copy.regexPatternPlaceholder}
                />
                <FieldDescription className="text-xs">
                  {copy.regexPatternHelp}
                </FieldDescription>
              </Field>
            </OperatorInsetPanel>

            <OperatorSwitchField
              label={copy.enabled}
              description={copy.activateRuleImmediately}
              checked={ruleForm.enabled ?? true}
              onCheckedChange={(checked) =>
                setRuleForm((prev) => ({ ...prev, enabled: checked }))
              }
              className="border-border bg-inset"
            />
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button type="button" variant="outline" onClick={() => setRuleDialogOpen(false)}>
              {copy.cancel}
            </Button>
            <Button type="submit">{copy.saveRule}</Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
