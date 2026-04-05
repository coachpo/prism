import type { ComponentProps } from "react";
import { SwitchController } from "@/components/SwitchController";
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

type FormSubmitHandler = NonNullable<ComponentProps<"form">["onSubmit"]>;

type Props = {
  open: boolean;
  proxyKeyActive: boolean;
  proxyKeyName: string;
  proxyKeyNotes: string;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: FormSubmitHandler;
  setProxyKeyActive: (value: boolean) => void;
  setProxyKeyName: (value: string) => void;
  setProxyKeyNotes: (value: string) => void;
};

export function EditProxyKeyDialog({
  open,
  proxyKeyActive,
  proxyKeyName,
  proxyKeyNotes,
  saving,
  onOpenChange,
  onSubmit,
  setProxyKeyActive,
  setProxyKeyName,
  setProxyKeyNotes,
}: Props) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{copy.editProxyApiKey}</DialogTitle>
          <DialogDescription>{copy.editDescription}</DialogDescription>
        </DialogHeader>

        <form onSubmit={onSubmit} className="space-y-4">
          <div className="space-y-2">
            <Label htmlFor="proxy-key-edit-name">{copy.name}</Label>
            <Input
              id="proxy-key-edit-name"
              name="proxy-key-name"
              autoComplete="off"
              value={proxyKeyName}
              onChange={(event) => setProxyKeyName(event.target.value)}
              placeholder={copy.namePlaceholder}
              disabled={saving}
            />
          </div>

          <div className="space-y-2">
            <Label htmlFor="proxy-key-edit-note">{copy.notes}</Label>
            <Input
              id="proxy-key-edit-note"
              name="proxy-key-notes"
              autoComplete="off"
              value={proxyKeyNotes}
              onChange={(event) => setProxyKeyNotes(event.target.value)}
              placeholder={copy.notesPlaceholder}
              disabled={saving}
            />
          </div>

          <SwitchController
            label={copy.active}
            checked={proxyKeyActive}
            onCheckedChange={setProxyKeyActive}
            disabled={saving}
            className="border-border bg-muted/20"
          />

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
              {messages.settingsDialogs.cancel}
            </Button>
            <Button type="submit" disabled={saving || !proxyKeyName.trim()}>
              {saving ? messages.pricingTemplateDialog.saving : messages.modelsUi.save}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
