import type { ComponentProps } from "react";
import { SwitchController } from "@/components/SwitchController";
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
  proxyKeyExpiresAt: string;
  proxyKeyName: string;
  proxyKeyNotes: string;
  saving: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: FormSubmitHandler;
  setProxyKeyActive: (value: boolean) => void;
  setProxyKeyExpiresAt: (value: string) => void;
  setProxyKeyName: (value: string) => void;
  setProxyKeyNotes: (value: string) => void;
};

export function EditProxyKeyDialog({
  open,
  proxyKeyActive,
  proxyKeyExpiresAt,
  proxyKeyName,
  proxyKeyNotes,
  saving,
  onOpenChange,
  onSubmit,
  setProxyKeyActive,
  setProxyKeyExpiresAt,
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

                <div className="flex flex-col gap-2">
                  <div className="flex items-center justify-between gap-2">
                    <Label htmlFor="proxy-key-edit-expires-at">{copy.expiresAt}</Label>
                    {proxyKeyExpiresAt ? (
                      <Button
                        type="button"
                        variant="ghost"
                        size="xs"
                        className="h-auto px-0 text-xs text-muted-foreground"
                        onClick={() => setProxyKeyExpiresAt("")}
                        disabled={saving}
                      >
                        {copy.clearExpiry}
                      </Button>
                    ) : null}
                  </div>
                  <Input
                    id="proxy-key-edit-expires-at"
                    name="proxy-key-edit-expires-at"
                    type="datetime-local"
                    value={proxyKeyExpiresAt}
                    onChange={(event) => setProxyKeyExpiresAt(event.target.value)}
                    disabled={saving}
                  />
                  <p className="text-xs text-muted-foreground">{copy.expiresAtDescription}</p>
                </div>
              </section>

              <section className="flex flex-col gap-4 rounded-lg border p-4">
                <SwitchController
                  label={copy.active}
                  description={copy.retireDescription}
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
