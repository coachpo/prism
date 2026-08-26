import { useCallback, useState } from "react";
import { RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import type { ModelCatalogResponse } from "@/lib/types";
import {
  OperatorInsetPanel,
  OperatorSectionCard,
  OperatorStatusBadge,
} from "@/shared/design-system";
import { CatalogBindDialog } from "./CatalogBindDialog";
import { CatalogOverrideDialog } from "./CatalogOverrideDialog";
import { CatalogRefreshDialog } from "./CatalogRefreshDialog";
import {
  CATALOG_FIELD_ORDER,
  catalogFieldLabel,
  renderCatalogFieldValue,
} from "./catalogMetadataPresentation";

/**
 * Metadata card shell. The card owns only the effective projection, action
 * lock, and dialog composition; bind, refresh-diff, and override workflows
 * live in their matching dialogs.
 */
export function CatalogMetadataCard({
  modelConfigId,
  catalog,
  onChanged,
}: {
  modelConfigId: number;
  catalog: ModelCatalogResponse | null;
  onChanged: () => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [bindOpen, setBindOpen] = useState(false);
  const [refreshOpen, setRefreshOpen] = useState(false);
  const [overrideOpen, setOverrideOpen] = useState(false);

  const runAction = useCallback(
    async (action: () => Promise<unknown>, done?: () => void) => {
      setBusy(true);
      setError(null);
      try {
        await action();
        onChanged();
        done?.();
      } catch (cause) {
        setError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        setBusy(false);
      }
    },
    [onChanged],
  );

  const bound = Boolean(catalog?.bound);
  const matchBadgeIntent = !bound
    ? ("idle" as const)
    : catalog?.match_source === "manual"
      ? ("accent" as const)
      : ("healthy" as const);
  const matchLabel = !bound
    ? copy.stateUnbound
    : catalog?.match_source === "manual"
      ? copy.stateManual
      : copy.stateUnique;

  return (
    <OperatorSectionCard
      title={copy.cardTitle}
      description={copy.cardDescription}
      actions={
        <>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy || !bound}
            onClick={() => setRefreshOpen(true)}
          >
            <RefreshCw data-icon="inline-start" />
            {copy.refreshAction}
          </Button>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={busy}
            onClick={() => setBindOpen(true)}
          >
            {bound ? copy.rebindAction : copy.bindAction}
          </Button>
          <Button
            type="button"
            size="sm"
            disabled={busy || !bound}
            onClick={() => setOverrideOpen(true)}
          >
            {copy.overrideAction}
          </Button>
        </>
      }
    >
      <div className="flex flex-col gap-[var(--density-inline-gap)]">
        <div className="flex flex-wrap items-center gap-2">
          <OperatorStatusBadge
            intent={matchBadgeIntent}
            preserveLabel
            label={matchLabel}
          />
          {bound && (
            <>
              <span className="font-mono text-sm text-muted-foreground">
                {catalog?.provider_id} / {catalog?.catalog_model_id}
              </span>
              {catalog?.fetched_at && (
                <span className="text-xs text-muted-foreground">
                  {copy.fetchedAtLabel}:{" "}
                  {new Date(catalog.fetched_at).toLocaleString()}
                </span>
              )}
              {catalog?.override &&
                Object.values(catalog.override).some(
                  (value) => value !== null,
                ) && (
                  <OperatorStatusBadge
                    intent="degraded"
                    preserveLabel
                    label={copy.hasOverridesBadge}
                  />
                )}
            </>
          )}
        </div>

        {!bound ? (
          <p className="text-sm text-muted-foreground">{copy.unboundHint}</p>
        ) : (
          <OperatorInsetPanel className="grid gap-x-6 gap-y-1 sm:grid-cols-2 lg:grid-cols-3">
            {CATALOG_FIELD_ORDER.map((key) => {
              const effective = renderCatalogFieldValue(
                catalog?.effective ?? null,
                key,
              );
              if (
                effective === null &&
                renderCatalogFieldValue(catalog?.source ?? null, key) === null
              ) {
                return null;
              }
              const overridden =
                renderCatalogFieldValue(catalog?.override ?? null, key) !==
                null;
              return (
                <div key={key} className="flex min-w-0 flex-col">
                  <span className="text-xs text-muted-foreground">
                    {catalogFieldLabel(copy, key)}
                    {overridden && (
                      <span className="ml-1 text-warning-foreground">
                        ·{copy.overrideMarker}
                      </span>
                    )}
                  </span>
                  <span
                    className="truncate text-sm font-medium"
                    title={effective ?? undefined}
                  >
                    {effective ?? copy.valueAbsent}
                  </span>
                </div>
              );
            })}
          </OperatorInsetPanel>
        )}

        {error && (
          <p className="text-sm text-destructive" role="alert">
            {error}
          </p>
        )}
      </div>

      {/* Dialogs mount only while open: local draft state dies with the
          unmount instead of being reset by an effect. */}
      {bindOpen && (
        <CatalogBindDialog
          isOpen
          modelConfigId={modelConfigId}
          busy={busy}
          onClose={() => setBindOpen(false)}
          runAction={runAction}
        />
      )}
      {refreshOpen && (
        <CatalogRefreshDialog
          isOpen
          modelConfigId={modelConfigId}
          onClose={() => setRefreshOpen(false)}
          runAction={runAction}
        />
      )}
      {overrideOpen && (
        <CatalogOverrideDialog
          modelConfigId={modelConfigId}
          catalog={catalog}
          busy={busy}
          onClose={() => setOverrideOpen(false)}
          runAction={runAction}
        />
      )}
    </OperatorSectionCard>
  );
}
