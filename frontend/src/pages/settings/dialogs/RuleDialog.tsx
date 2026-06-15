import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { HeaderBlocklistRule, HeaderBlocklistRuleCreate } from "@/lib/types";
import type { Dispatch, FormEvent, SetStateAction } from "react";
import { OperatorSwitchField } from "@/shared/design-system";

interface RuleDialogProps {
  ruleDialogOpen: boolean;
  setRuleDialogOpen: (open: boolean) => void;
  editingRule: HeaderBlocklistRule | null;
  ruleForm: HeaderBlocklistRuleCreate;
  setRuleForm: Dispatch<SetStateAction<HeaderBlocklistRuleCreate>>;
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
      <DialogContent aria-describedby={undefined} className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editingRule ? copy.ruleDialogEditTitle : copy.ruleDialogAddTitle}</DialogTitle>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="flex flex-col gap-5">
          <input type="hidden" name="match_type" value={ruleForm.match_type} />
          <input type="hidden" name="enabled" value={String(ruleForm.enabled)} />
          <DialogBody>
            <div className="flex flex-col gap-4 rounded-lg border p-4">
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="flex flex-col gap-2">
                  <Label htmlFor="rule-name">{copy.name}</Label>
                  <Input
                    id="rule-name"
                    name="name"
                    autoComplete="off"
                    value={ruleForm.name}
                    onChange={(event) =>
                      setRuleForm((prev) => ({ ...prev, name: event.target.value }))
                    }
                    placeholder={copy.namePlaceholder}
                  />
                </div>

                <div className="flex flex-col gap-2">
                  <Label htmlFor="rule-type">{copy.type}</Label>
                  <Select
                    value={ruleForm.match_type}
                    onValueChange={(value) =>
                      setRuleForm((prev) => ({
                        ...prev,
                        match_type: value as "exact" | "prefix",
                      }))
                    }
                  >
                    <SelectTrigger id="rule-type">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="exact">{copy.exactMatch}</SelectItem>
                      <SelectItem value="prefix">{copy.prefixMatch}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>

              <div className="flex flex-col gap-2">
                <Label htmlFor="rule-pattern">{copy.pattern}</Label>
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
                  <p className="text-xs leading-5 text-muted-foreground">
                    {copy.prefixMatchMustEndHyphen}
                  </p>
                )}
              </div>
            </div>

            <OperatorSwitchField
              label={copy.enabled}
              description={copy.activateRuleImmediately}
              checked={ruleForm.enabled}
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
