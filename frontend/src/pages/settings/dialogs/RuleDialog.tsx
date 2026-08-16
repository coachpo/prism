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
import {
  Field,
  FieldDescription,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import type { Dispatch, FormEvent, SetStateAction } from "react";
import type { HeaderBlocklistRule, HeaderBlocklistRuleCreate } from "@/lib/types";
import { OperatorInsetPanel, OperatorSwitchField } from "@/shared/design-system";

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
            <OperatorInsetPanel>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field>
                  <FieldLabel htmlFor="rule-name">{copy.name}</FieldLabel>
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
                </Field>

                <Field>
                  <FieldLabel htmlFor="rule-type">{copy.type}</FieldLabel>
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
                      <SelectGroup>
                        <SelectItem value="exact">{copy.exactMatch}</SelectItem>
                        <SelectItem value="prefix">{copy.prefixMatch}</SelectItem>
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </Field>
              </div>

              <Field>
                <FieldLabel htmlFor="rule-pattern">{copy.pattern}</FieldLabel>
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
                  <FieldDescription className="text-xs">
                    {copy.prefixMatchMustEndHyphen}
                  </FieldDescription>
                )}
              </Field>
            </OperatorInsetPanel>

            <OperatorSwitchField
              label={copy.enabled}
              description={copy.activateRuleImmediately}
              checked={ruleForm.enabled}
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
