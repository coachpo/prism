import type { ComponentProps } from "react";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { useLocale } from "@/i18n/useLocale";
import { OperatorSwitchField } from "@/shared/design-system";

type FormSubmitHandler = NonNullable<ComponentProps<"form">["onSubmit"]>;

interface ProxyKeyDetailSheetProps {
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
}

export function ProxyKeyDetailSheet({
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
}: ProxyKeyDetailSheetProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
        <SheetHeader>
          <SheetTitle>{copy.editProxyApiKey}</SheetTitle>
          <SheetDescription>{copy.editDescription}</SheetDescription>
        </SheetHeader>

        <form onSubmit={onSubmit} className="flex min-h-0 flex-1 flex-col gap-5 px-4">
          <FieldGroup className="gap-4">
            <Field data-disabled={saving || undefined}>
              <FieldLabel htmlFor="proxy-key-edit-name">{copy.name}</FieldLabel>
              <Input
                id="proxy-key-edit-name"
                name="proxy-key-name"
                autoComplete="off"
                value={proxyKeyName}
                onChange={(event) => setProxyKeyName(event.target.value)}
                placeholder={copy.namePlaceholder}
                disabled={saving}
              />
            </Field>

            <Field data-disabled={saving || undefined}>
              <FieldLabel htmlFor="proxy-key-edit-note">{copy.notes}</FieldLabel>
              <Textarea
                id="proxy-key-edit-note"
                name="proxy-key-notes"
                autoComplete="off"
                value={proxyKeyNotes}
                onChange={(event) => setProxyKeyNotes(event.target.value)}
                placeholder={copy.notesPlaceholder}
                disabled={saving}
              />
            </Field>

            <Field data-disabled={saving || undefined}>
              <div className="flex items-center justify-between gap-2">
                <FieldLabel htmlFor="proxy-key-edit-expires-at">{copy.expiresAt}</FieldLabel>
                {proxyKeyExpiresAt ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    className="h-auto px-0 text-muted-foreground"
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
              <FieldDescription>{copy.expiresAtDescription}</FieldDescription>
            </Field>
          </FieldGroup>

          <OperatorSwitchField
            label={copy.active}
            description={copy.retireDescription}
            checked={proxyKeyActive}
            onCheckedChange={setProxyKeyActive}
            disabled={saving}
            className="border-outline-variant bg-surface-container-low"
          />

          <SheetFooter className="px-0">
            <Button type="submit" disabled={saving || !proxyKeyName.trim()}>
              {saving ? <Spinner aria-hidden="true" data-icon="inline-start" /> : null}
              {saving ? messages.pricingTemplateDialog.saving : messages.modelsUi.save}
            </Button>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
              {messages.settingsDialogs.cancel}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
