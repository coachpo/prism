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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { useLocale } from "@/i18n/useLocale";
import type { UserAgentClientRule, UserAgentClientRuleCreate } from "@/lib/types";
import { SwitchController } from "@/components/SwitchController";

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
      <DialogContent aria-describedby={undefined} className="sm:max-w-lg">
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
            <div className="rounded-lg border bg-muted/30 p-4 text-sm text-muted-foreground">
              <div className="flex items-start gap-3">
                <Tooltip>
                  <TooltipTrigger asChild>
                    <button
                      type="button"
                      aria-label={copy.whyMatchUserAgentClients}
                      className="mt-0.5 shrink-0"
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
                  <p>{copy.userAgentClientRulesExplanation}</p>
                </div>
              </div>
            </div>

            <div className="flex flex-col gap-4 rounded-lg border p-4">
              <div className="flex flex-col gap-2">
                <Label htmlFor="user-agent-client-rule-name">{copy.name}</Label>
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
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="user-agent-client-rule-pattern">{copy.regexPattern}</Label>
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
                <p className="text-xs leading-5 text-muted-foreground">
                  {copy.regexPatternHelp}
                </p>
              </div>
            </div>

            <SwitchController
              label={copy.enabled}
              description={copy.activateRuleImmediately}
              checked={ruleForm.enabled ?? true}
              onCheckedChange={(checked) =>
                setRuleForm((prev) => ({ ...prev, enabled: checked }))
              }
              className="bg-muted/20"
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
