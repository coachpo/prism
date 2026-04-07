import type { ComponentProps } from "react";
import { SwitchController } from "@/components/SwitchController";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";

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
      <DialogContent className="max-h-[90vh] sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{copy.editProxyApiKey}</DialogTitle>
          <DialogDescription>{copy.editDescription}</DialogDescription>
        </DialogHeader>

        <form onSubmit={onSubmit} className="flex min-h-0 flex-col gap-5">
          <DialogBody className="min-h-0 flex-1 overflow-y-auto pr-1">
            <div className="flex flex-col gap-5">
              <section className="flex flex-col gap-4 rounded-lg border bg-muted/20 p-4">
                <div className="flex flex-col gap-1">
                  <p className="text-sm font-medium text-foreground">{copy.nameNote}</p>
                  <p className="text-sm text-muted-foreground">{copy.editDescription}</p>
                </div>

                <div className="flex flex-col gap-2">
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

                <div className="flex flex-col gap-2">
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
              </section>

              <section className="flex flex-col gap-4 rounded-lg border p-4">
                <div className="flex items-center justify-between gap-3">
                  <p className="text-sm font-medium text-foreground">{copy.active}</p>
                  <Badge variant="outline">{proxyKeyActive ? copy.active : copy.disabled}</Badge>
                </div>

                <SwitchController
                  label={copy.active}
                  checked={proxyKeyActive}
                  onCheckedChange={setProxyKeyActive}
                  disabled={saving}
                  className="border-border bg-muted/10"
                />
              </section>
            </div>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
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
