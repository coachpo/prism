import { Info } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { SwitchController } from "@/components/SwitchController";
import type { HeaderBlocklistRule, HeaderBlocklistRuleCreate } from "@/lib/types";
import type { FormEvent } from "react";

interface RuleDialogProps {
  ruleDialogOpen: boolean;
  setRuleDialogOpen: (open: boolean) => void;
  editingRule: HeaderBlocklistRule | null;
  ruleForm: HeaderBlocklistRuleCreate;
  setRuleForm: React.Dispatch<React.SetStateAction<HeaderBlocklistRuleCreate>>;
  handleSaveRule: () => Promise<void>;
}

export function RuleDialog({
  ruleDialogOpen,
  setRuleDialogOpen,
  editingRule,
  ruleForm,
  setRuleForm,
  handleSaveRule,
}: RuleDialogProps) {
  const { messages } = useLocale();
  const copy = messages.settingsDialogs;
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    void handleSaveRule();
  };

  return (
    <Dialog open={ruleDialogOpen} onOpenChange={setRuleDialogOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{editingRule ? copy.ruleDialogEditTitle : copy.ruleDialogAddTitle}</DialogTitle>
          <DialogDescription>{editingRule ? copy.ruleDialogEditDescription : copy.ruleDialogAddDescription}</DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-1">
          <input type="hidden" name="match_type" value={ruleForm.match_type} />
          <input type="hidden" name="enabled" value={String(ruleForm.enabled)} />
          <div className="rounded-md border bg-muted/40 p-3 text-xs text-muted-foreground">
            <div className="flex items-start gap-2">
              <Tooltip>
                <TooltipTrigger asChild>
                  <button type="button" aria-label={copy.whyBlockHeaders} className="mt-0.5">
                    <Info className="h-3.5 w-3.5" />
                  </button>
                </TooltipTrigger>
                <TooltipContent side="right" className="max-w-xs">
                  {copy.blockHeadersTooltip}
                </TooltipContent>
              </Tooltip>

              <div className="space-y-1">
                <p>{copy.blockHeadersExamples}</p>
                <p>{copy.stripSensitiveHeaders}</p>
              </div>
            </div>
          </div>

          <div className="grid gap-4">
            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="rule-name" className="text-right">
                {copy.name}
              </Label>
              <Input
                id="rule-name"
                name="name"
                autoComplete="off"
                value={ruleForm.name}
                onChange={(event) =>
                  setRuleForm((prev) => ({ ...prev, name: event.target.value }))
                }
                className="col-span-3"
                placeholder={copy.namePlaceholder}
              />
            </div>

            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="rule-type" className="text-right">
                {copy.type}
              </Label>
              <Select
                value={ruleForm.match_type}
                onValueChange={(value) =>
                  setRuleForm((prev) => ({
                    ...prev,
                    match_type: value as "exact" | "prefix",
                  }))
                }
              >
                <SelectTrigger className="col-span-3" id="rule-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="exact">{copy.exactMatch}</SelectItem>
                  <SelectItem value="prefix">{copy.prefixMatch}</SelectItem>
                </SelectContent>
              </Select>
            </div>

            <div className="grid grid-cols-4 items-center gap-4">
              <Label htmlFor="rule-pattern" className="text-right">
                {copy.pattern}
              </Label>
              <div className="col-span-3 space-y-1">
                <Input
                  id="rule-pattern"
                  name="pattern"
                  autoComplete="off"
                  value={ruleForm.pattern}
                  onChange={(event) =>
                    setRuleForm((prev) => ({ ...prev, pattern: event.target.value }))
                  }
                  className="font-mono"
                  placeholder={
                    ruleForm.match_type === "prefix"
                      ? copy.patternPlaceholderPrefix
                      : copy.patternPlaceholderExact
                  }
                />
                {ruleForm.match_type === "prefix" && (
                  <p className="text-[0.8rem] text-muted-foreground">
                    {copy.prefixMatchMustEndHyphen}
                  </p>
                )}
              </div>
            </div>

            <SwitchController
              label={copy.enabled}
              description={copy.activateRuleImmediately}
              checked={ruleForm.enabled}
              onCheckedChange={(checked) =>
                setRuleForm((prev) => ({ ...prev, enabled: checked }))
              }
            />
          </div>
          <DialogFooter>
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
