import type { ComponentProps } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Field,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { OperatorInsetPanel, OperatorSectionCard } from "@/shared/design-system";
import { getProxyKeyUsagePercent } from "./proxyKeyFormatting";
import { ProxyKeyExpiryField, type ResolvedExpiryInput } from "./ProxyKeyExpiryField";

type FormSubmitHandler = NonNullable<ComponentProps<"form">["onSubmit"]>;

interface ProxyKeyIssuePanelProps {
  authAvailable: boolean;
  capacity: { limit: number; used: number; remaining: number; counted_at: string } | null;
  createDisabled: boolean;
  creatingProxyKey: boolean;
  handleCreateSubmit: FormSubmitHandler;
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
  const used = capacity?.used ?? proxyKeyLimit - remainingKeys;
  const quotaPercent = getProxyKeyUsagePercent(used, proxyKeyLimit);
  const fieldsDisabled = creatingProxyKey || !authAvailable;
  const handleExpiryChange = (value: ResolvedExpiryInput) => {
    setProxyKeyExpiresResolved(value)
    if (value.preserved) {
      setProxyKeyExpiresAt("")
      return
    }
    if (value.instant === null) {
      setProxyKeyExpiresAt("")
      return
    }
    setProxyKeyExpiresAt(value.instant)
  }

  return (
    <form onSubmit={handleCreateSubmit}>
      <OperatorSectionCard
        className="h-full overflow-hidden"
        title={copy.createProxyKey}
        description={copy.createDescription}
        actions={(
            <Badge variant={remainingKeys === 0 ? "destructive" : "secondary"}>
              {remainingKeys === 0 ? copy.keyLimitReached : copy.slotsRemaining(formatNumber(remainingKeys))}
            </Badge>
        )}
        contentClassName="flex flex-col gap-5"
      >
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
                      setProxyKeyExpiresAt("")
                      setProxyKeyExpiresResolved({ instant: null, preserved: false, gapError: false, overlapNotice: false })
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
            </Field>
          </FieldGroup>

          <OperatorInsetPanel className="bg-surface">
            <div className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
              <span>{copy.keysUsed(formatNumber(used), formatNumber(proxyKeyLimit))}</span>
              <span>{copy.slotsRemaining(formatNumber(remainingKeys))}</span>
            </div>
            <Progress
              value={quotaPercent}
              aria-label={copy.keysUsed(formatNumber(used), formatNumber(proxyKeyLimit))}
            />
          </OperatorInsetPanel>

          <Button type="submit" disabled={createDisabled} className="self-start">
            {creatingProxyKey ? <Spinner aria-hidden="true" data-icon="inline-start" /> : null}
            {creatingProxyKey
              ? copy.creating
              : remainingKeys === 0
                ? copy.keyLimitReached
              : copy.createKey}
          </Button>
      </OperatorSectionCard>
    </form>
  );
}
