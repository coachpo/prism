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
import { useTimezone } from "@/hooks/useTimezone";
import type { ProxyKeyCapacity } from "@/lib/types";
import { ProxyKeyExpiryField, type ResolvedExpiryInput } from "./ProxyKeyExpiryField";

type FormSubmitHandler = NonNullable<ComponentProps<"form">["onSubmit"]>;

interface ProxyKeyIssuePanelProps {
  authAvailable: boolean;
  capacity: ProxyKeyCapacity | null;
  createDisabled: boolean;
  creatingProxyKey: boolean;
  handleCreateSubmit: FormSubmitHandler;
  onOpenChange: (open: boolean) => void;
  open: boolean;
  proxyKeyExpiresAt: string;
  proxyKeyExpiresResolved: ResolvedExpiryInput | null;
  proxyKeyLimit: number;
  proxyKeyName: string;
  proxyKeyNotes: string;
  remainingKeys: number;
  setProxyKeyExpiresAt: (value: string) => void;
  setProxyKeyExpiresResolved: (value: ResolvedExpiryInput | null) => void;
  setProxyKeyName: (value: string) => void;
  setProxyKeyNotes: (value: string) => void;
}

export function ProxyKeyIssuePanel({
  authAvailable,
  capacity,
  createDisabled,
  creatingProxyKey,
  handleCreateSubmit,
  onOpenChange,
  open,
  proxyKeyExpiresAt,
  proxyKeyExpiresResolved,
  proxyKeyLimit,
  proxyKeyName,
  proxyKeyNotes,
  remainingKeys,
  setProxyKeyExpiresAt,
  setProxyKeyExpiresResolved,
  setProxyKeyName,
  setProxyKeyNotes,
}: ProxyKeyIssuePanelProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const timezone = useTimezone();
  const fieldsDisabled = creatingProxyKey || !authAvailable || !capacity;

  // The submit button keeps its verb. Why it is unavailable is stated under it
  // instead of being written into the button's own label.
  const blockedReason = !authAvailable
    ? copy.createBlockedAuthUnavailable
    : !capacity
      ? copy.createBlockedCapacityUnknown
      : remainingKeys === 0
        ? copy.createBlockedLimitReached(formatNumber(proxyKeyLimit))
        : null;

  const handleExpiryChange = (value: ResolvedExpiryInput) => {
    setProxyKeyExpiresResolved(value);
    if (value.preserved || value.instant === null) {
      setProxyKeyExpiresAt("");
      return;
    }
    setProxyKeyExpiresAt(value.instant);
  };

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent size="md" className="overflow-y-auto" data-testid="proxy-key-issue-sheet">
        <SheetHeader>
          <SheetTitle>{copy.issueKey}</SheetTitle>
          <SheetDescription>{copy.issueKeyDescription}</SheetDescription>
        </SheetHeader>

        <form onSubmit={handleCreateSubmit} className="flex min-h-0 flex-1 flex-col gap-5 px-4">
          <FieldGroup className="gap-4">
            <Field data-disabled={fieldsDisabled || undefined}>
              <FieldLabel htmlFor="proxy-key-name">{copy.name}</FieldLabel>
              <Input
                id="proxy-key-name"
                name="proxy-key-name"
                autoComplete="off"
                value={proxyKeyName}
                onChange={(event) => setProxyKeyName(event.target.value)}
                placeholder={copy.namePlaceholder}
                disabled={fieldsDisabled}
              />
            </Field>

            <Field data-disabled={fieldsDisabled || undefined}>
              <FieldLabel htmlFor="proxy-key-notes">{copy.notes}</FieldLabel>
              <Textarea
                id="proxy-key-notes"
                name="proxy-key-notes"
                autoComplete="off"
                value={proxyKeyNotes}
                onChange={(event) => setProxyKeyNotes(event.target.value)}
                placeholder={copy.notesPlaceholder}
                disabled={fieldsDisabled}
              />
            </Field>

            <Field data-disabled={fieldsDisabled || undefined}>
              <div className="flex items-center justify-between gap-2">
                <FieldLabel>{copy.expiresAt}</FieldLabel>
                {proxyKeyExpiresAt ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    className="h-auto px-0 text-muted-foreground"
                    onClick={() => {
                      setProxyKeyExpiresAt("");
                      setProxyKeyExpiresResolved({
                        instant: null,
                        preserved: false,
                        gapError: false,
                        overlapNotice: false,
                      });
                    }}
                    disabled={fieldsDisabled}
                  >
                    {copy.clearExpiry}
                  </Button>
                ) : null}
              </div>
              <ProxyKeyExpiryField
                mode="create"
                timezone={timezone.timezone}
                timezoneLoading={timezone.loading}
                currentInstant={proxyKeyExpiresResolved?.instant ?? null}
                disabled={fieldsDisabled}
                onChange={handleExpiryChange}
              />
              <FieldDescription>{copy.expiresAtDescription}</FieldDescription>
            </Field>
          </FieldGroup>

          <SheetFooter className="px-0">
            <Field>
              <Button
                type="submit"
                disabled={createDisabled}
                aria-describedby={blockedReason ? "proxy-key-create-blocked" : undefined}
              >
                {creatingProxyKey ? <Spinner aria-hidden="true" data-icon="inline-start" /> : null}
                {creatingProxyKey ? copy.creating : copy.createKey}
              </Button>
              {blockedReason ? (
                <FieldDescription id="proxy-key-create-blocked">{blockedReason}</FieldDescription>
              ) : null}
            </Field>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={creatingProxyKey}>
              {messages.settingsDialogs.cancel}
            </Button>
          </SheetFooter>
        </form>
      </SheetContent>
    </Sheet>
  );
}
