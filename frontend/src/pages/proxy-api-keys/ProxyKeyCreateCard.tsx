import type { ComponentProps } from "react";
import { CopyButton } from "@/components/CopyButton";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useLocale } from "@/i18n/useLocale";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type FormSubmitHandler = NonNullable<ComponentProps<"form">["onSubmit"]>;

interface ProxyKeyCreateCardProps {
  authAvailable: boolean;
  createDisabled: boolean;
  creatingProxyKey: boolean;
  handleCreateSubmit: FormSubmitHandler;
  latestGeneratedKey: string | null;
  proxyKeyLimit: number;
  proxyKeyName: string;
  proxyKeyNotes: string;
  proxyKeysUsed: number;
  remainingKeys: number;
  setProxyKeyName: (value: string) => void;
  setProxyKeyNotes: (value: string) => void;
}

export function ProxyKeyCreateCard({
  authAvailable,
  createDisabled,
  creatingProxyKey,
  handleCreateSubmit,
  latestGeneratedKey,
  proxyKeyLimit,
  proxyKeyName,
  proxyKeyNotes,
  proxyKeysUsed,
  remainingKeys,
  setProxyKeyName,
  setProxyKeyNotes,
}: ProxyKeyCreateCardProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.proxyApiKeys;
  return (
    <form onSubmit={handleCreateSubmit}>
      <Card>
        <CardHeader className="pb-3">
          <div className="space-y-1">
            <CardTitle className="text-base">{copy.createProxyKey}</CardTitle>
            <p className="text-sm text-muted-foreground">
              {copy.createDescription}
            </p>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {latestGeneratedKey ? (
            <div className="rounded-lg border border-emerald-500/25 bg-emerald-500/10 p-4">
              <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div className="space-y-1">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.2em] text-emerald-700 dark:text-emerald-300">
                    {copy.newSecret}
                  </p>
                  <p className="text-sm text-muted-foreground">
                    {copy.newSecretDescription}
                  </p>
                </div>
                <CopyButton
                  value={latestGeneratedKey}
                  label={copy.copyKey}
                  targetLabel={copy.apiKey}
                  variant="outline"
                />
              </div>
              <div className="mt-4 rounded-md border bg-background px-3 py-3">
                <p className="break-all font-mono text-sm">{latestGeneratedKey}</p>
              </div>
            </div>
          ) : null}

          <div className="flex flex-wrap items-end gap-3">
            <div className="space-y-1">
              <Label htmlFor="proxy-key-name" className="text-xs text-muted-foreground">
                {copy.name}
              </Label>
              <Input
                id="proxy-key-name"
                name="proxy-key-name"
                autoComplete="off"
                value={proxyKeyName}
                onChange={(event) => setProxyKeyName(event.target.value)}
                placeholder={copy.namePlaceholder}
                disabled={creatingProxyKey || !authAvailable}
                className="w-56"
              />
            </div>

            <div className="space-y-1">
              <Label htmlFor="proxy-key-notes" className="text-xs text-muted-foreground">
                {copy.notes}
              </Label>
              <Input
                id="proxy-key-notes"
                name="proxy-key-notes"
                autoComplete="off"
                value={proxyKeyNotes}
                onChange={(event) => setProxyKeyNotes(event.target.value)}
                placeholder={copy.notesPlaceholder}
                disabled={creatingProxyKey || !authAvailable}
                className="w-72"
              />
            </div>

            <Button type="submit" disabled={createDisabled}>
              {creatingProxyKey
                ? copy.creating
                : remainingKeys === 0
                  ? copy.keyLimitReached
                  : copy.createKey}
            </Button>
          </div>

          <div className="flex items-center justify-between text-sm text-muted-foreground">
            <p>{copy.keysUsed(formatNumber(proxyKeysUsed), formatNumber(proxyKeyLimit))}</p>
            <p>{copy.slotsRemaining(formatNumber(remainingKeys))}</p>
          </div>
        </CardContent>
      </Card>
    </form>
  );
}
