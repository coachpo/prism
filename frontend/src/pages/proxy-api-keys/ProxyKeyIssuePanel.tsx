import type { ComponentProps } from "react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Progress } from "@/components/ui/progress";
import { Spinner } from "@/components/ui/spinner";
import { Textarea } from "@/components/ui/textarea";
import { useLocale } from "@/i18n/useLocale";
import { getProxyKeyUsagePercent } from "./proxyKeyFormatting";

type FormSubmitHandler = NonNullable<ComponentProps<"form">["onSubmit"]>;

interface ProxyKeyIssuePanelProps {
  authAvailable: boolean;
  createDisabled: boolean;
  creatingProxyKey: boolean;
  handleCreateSubmit: FormSubmitHandler;
  proxyKeyExpiresAt: string;
  proxyKeyLimit: number;
  proxyKeyName: string;
  proxyKeyNotes: string;
  proxyKeysUsed: number;
  remainingKeys: number;
  setProxyKeyExpiresAt: (value: string) => void;
  setProxyKeyName: (value: string) => void;
  setProxyKeyNotes: (value: string) => void;
}

export function ProxyKeyIssuePanel({
  authAvailable,
  createDisabled,
  creatingProxyKey,
  handleCreateSubmit,
  proxyKeyExpiresAt,
  proxyKeyLimit,
  proxyKeyName,
  proxyKeyNotes,
  proxyKeysUsed,
  remainingKeys,
  setProxyKeyExpiresAt,
  setProxyKeyName,
  setProxyKeyNotes,
}: ProxyKeyIssuePanelProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const quotaPercent = getProxyKeyUsagePercent(proxyKeysUsed, proxyKeyLimit);
  const fieldsDisabled = creatingProxyKey || !authAvailable;

  return (
    <form onSubmit={handleCreateSubmit}>
      <Card className="h-full overflow-hidden">
        <CardHeader className="border-b bg-muted/20">
          <CardTitle className="text-base">{copy.createProxyKey}</CardTitle>
          <CardDescription>{copy.createDescription}</CardDescription>
          <CardAction>
            <Badge variant={remainingKeys === 0 ? "destructive" : "secondary"}>
              {remainingKeys === 0 ? copy.keyLimitReached : copy.slotsRemaining(formatNumber(remainingKeys))}
            </Badge>
          </CardAction>
        </CardHeader>
        <CardContent className="flex flex-col gap-5">
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
                <FieldLabel htmlFor="proxy-key-expires-at">{copy.expiresAt}</FieldLabel>
                {proxyKeyExpiresAt ? (
                  <Button
                    type="button"
                    variant="ghost"
                    size="xs"
                    className="h-auto px-0 text-muted-foreground"
                    onClick={() => setProxyKeyExpiresAt("")}
                    disabled={fieldsDisabled}
                  >
                    {copy.clearExpiry}
                  </Button>
                ) : null}
              </div>
              <Input
                id="proxy-key-expires-at"
                name="proxy-key-expires-at"
                type="datetime-local"
                value={proxyKeyExpiresAt}
                onChange={(event) => setProxyKeyExpiresAt(event.target.value)}
                disabled={fieldsDisabled}
              />
              <FieldDescription>{copy.expiresAtDescription}</FieldDescription>
            </Field>
          </FieldGroup>

          <div className="flex flex-col gap-3 rounded-lg border bg-muted/20 p-3">
            <div className="flex items-center justify-between gap-3 text-sm text-muted-foreground">
              <span>{copy.keysUsed(formatNumber(proxyKeysUsed), formatNumber(proxyKeyLimit))}</span>
              <span>{copy.slotsRemaining(formatNumber(remainingKeys))}</span>
            </div>
            <Progress
              value={quotaPercent}
              aria-label={copy.keysUsed(formatNumber(proxyKeysUsed), formatNumber(proxyKeyLimit))}
            />
          </div>

          <Button type="submit" disabled={createDisabled} className="self-start">
            {creatingProxyKey ? <Spinner aria-hidden="true" data-icon="inline-start" /> : null}
            {creatingProxyKey
              ? copy.creating
              : remainingKeys === 0
                ? copy.keyLimitReached
                : copy.createKey}
          </Button>
        </CardContent>
      </Card>
    </form>
  );
}
