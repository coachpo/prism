import { useCallback, useState } from "react";
import { MoreHorizontal, RefreshCw } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import { models as modelsApi } from "@/lib/api/models";
import {
  OperatorCallout,
  OperatorDestructiveDialog,
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorRetryButton,
  OperatorStalenessBadge,
  OperatorStatusBadge,
} from "@/shared/design-system";
import { CatalogBindDialog } from "@/pages/model-detail/CatalogBindDialog";
import { CatalogOverrideDialog } from "@/pages/model-detail/CatalogOverrideDialog";
import { CatalogRefreshDialog } from "@/pages/model-detail/CatalogRefreshDialog";
import type { ModelCatalogView } from "@/pages/model-detail/useModelCatalog";
import {
  CATALOG_FIELD_ORDER,
  catalogFieldLabel,
  renderCatalogFieldValue,
} from "@/pages/model-detail/catalogMetadataPresentation";

/**
 * models.dev metadata panel body. The card owns only the honest read state,
 * effective projection, action lock, and dialog composition; bind,
 * refresh-diff, and override workflows live in their matching dialogs.
 *
 * The three read states stay distinguishable: a first read in flight is a
 * loading surface, a failed first read is an error surface (never a
 * fabricated "unbound"), and a failed re-read keeps the last good metadata
 * with a single staleness badge. Only a successful `bound:false` response may
 * render the unbound conclusion.
 */
export function ModelsDevCatalogPanel({
  modelConfigId,
  prismModelId,
  apiFamily,
  catalogView,
  onChanged,
}: {
  modelConfigId: number;
  prismModelId: string;
  apiFamily: string;
  catalogView: ModelCatalogView;
  onChanged: () => void;
}) {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.modelCatalog;
  const catalog = catalogView.catalog;
  const [busy, setBusy] = useState(false);
  const [bindOpen, setBindOpen] = useState(false);
  const [refreshOpen, setRefreshOpen] = useState(false);
  const [overrideOpen, setOverrideOpen] = useState(false);
  const [unbindOpen, setUnbindOpen] = useState(false);
  const [unbindError, setUnbindError] = useState<string | null>(null);

  const runAction = useCallback(
    async (
      action: () => Promise<unknown>,
      done?: () => void,
      onError?: (message: string) => void,
    ) => {
      setBusy(true);
      try {
        await action();
        onChanged();
        done?.();
      } catch (cause) {
        onChanged();
        onError?.(cause instanceof Error ? cause.message : String(cause));
      } finally {
        setBusy(false);
      }
    },
    [onChanged],
  );

  const bound = Boolean(catalog?.bound);
  const actionsBlocked =
    busy || catalogView.loading || catalogView.refreshing || catalogView.failed;
  const unbindSnapshotComplete = Boolean(
    bound &&
      catalog?.provider_id &&
      catalog.catalog_model_id &&
      catalog.updated_at,
  );
  const matchBadgeIntent = !bound
    ? ("idle" as const)
    : catalog?.match_source === "manual"
      ? ("accent" as const)
      : catalog?.match_source === "unique_match"
        ? ("healthy" as const)
        : ("degraded" as const);
  const matchLabel = !bound
    ? copy.stateUnbound
    : catalog?.match_source === "manual"
      ? copy.stateManual
      : catalog?.match_source === "unique_match"
        ? copy.stateUnique
        : copy.matchSourceUnknown;

  async function handleUnbind() {
    if (!catalog?.provider_id || !catalog.catalog_model_id || !catalog.updated_at)
      return;
    setUnbindError(null);
    await runAction(
      () =>
        modelsApi.catalog.unbind(modelConfigId, {
          expected_provider_id: catalog.provider_id!,
          expected_catalog_model_id: catalog.catalog_model_id!,
          expected_binding_updated_at: catalog.updated_at!,
        }),
      () => setUnbindOpen(false),
      setUnbindError,
    );
  }

  const metadataBody = (() => {
    if (catalogView.loading) {
      return (
        <OperatorLoadingState
          testId="catalog-read-loading"
          title={copy.readLoadingTitle}
          className="py-3"
        />
      );
    }
    if (catalogView.failed && !catalogView.hasLastGood) {
      // A failed first read is a failure surface; it never degrades into the
      // unbound conclusion.
      return (
        <OperatorErrorState
          testId="catalog-read-error"
          title={copy.readFailedTitle}
          description={catalogView.error ?? undefined}
          action={
            <OperatorRetryButton onClick={catalogView.refresh}>
              <RefreshCw data-icon="inline-start" />
              {copy.readRetry}
            </OperatorRetryButton>
          }
        />
      );
    }
    if (!bound) {
      return (
        <p className="text-sm text-muted-foreground">{copy.unboundHint}</p>
      );
    }
    return (
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
            renderCatalogFieldValue(catalog?.override ?? null, key) !== null;
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
    );
  })();

  return (
    <OperatorInsetPanel
      title={copy.cardTitle}
      description={copy.cardDescription}
      actions={
        <div className="flex items-center gap-1">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={actionsBlocked}
            onClick={() => setBindOpen(true)}
          >
            {bound ? copy.rebindAction : copy.bindAction}
          </Button>
          {bound ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button
                  type="button"
                  size="sm"
                  variant="ghost"
                  aria-label={copy.bindingActionsLabel}
                  disabled={actionsBlocked}
                >
                  <MoreHorizontal data-icon="inline-start" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuGroup>
                  <DropdownMenuItem onSelect={() => setRefreshOpen(true)}>
                    {copy.refreshAction}
                  </DropdownMenuItem>
                  <DropdownMenuItem onSelect={() => setOverrideOpen(true)}>
                    {copy.overrideAction}
                  </DropdownMenuItem>
                </DropdownMenuGroup>
                <DropdownMenuSeparator />
                <DropdownMenuGroup>
                  <DropdownMenuItem
                    variant="destructive"
                    disabled={!unbindSnapshotComplete}
                    onSelect={() => {
                      setUnbindError(null);
                      setUnbindOpen(true);
                    }}
                  >
                    {copy.unbindAction}
                  </DropdownMenuItem>
                </DropdownMenuGroup>
              </DropdownMenuContent>
            </DropdownMenu>
          ) : null}
        </div>
      }
    >
      <div className="flex flex-col gap-[var(--density-inline-gap)]">
        {catalogView.refreshing ? (
          <OperatorCallout intent="muted" description={copy.readRefreshing} />
        ) : null}
        {catalogView.failed && catalogView.hasLastGood ? (
          // Re-read failure with last-good data: keep the metadata on screen
          // and mark exactly one staleness badge with the last success stamp.
          <div className="flex flex-wrap items-center gap-2">
            <OperatorStalenessBadge
              data-testid="catalog-read-stale"
              label={copy.staleBadgeLabel}
              reason={
                catalogView.lastSuccessfulAt
                  ? `${copy.staleBadgeReason(
                      formatTime(catalogView.lastSuccessfulAt),
                    )}${catalogView.error ? ` · ${catalogView.error}` : ""}`
                  : (catalogView.error ?? undefined)
              }
            />
            <OperatorRetryButton
              onClick={catalogView.refresh}
              disabled={catalogView.refreshing}
            >
              <RefreshCw data-icon="inline-start" />
              {copy.readRetry}
            </OperatorRetryButton>
          </div>
        ) : null}
        <div className="flex flex-wrap items-center gap-2">
          {/* A failed first read has no binding conclusion to show; only a
              settled read (or last-good under staleness) may name a state. */}
          {!catalogView.loading &&
          !(catalogView.failed && !catalogView.hasLastGood) ? (
            <OperatorStatusBadge
              intent={matchBadgeIntent}
              preserveLabel
              label={matchLabel}
            />
          ) : null}
          {bound && (
            <>
              <span className="font-mono text-sm text-muted-foreground">
                {catalog?.provider_id} / {catalog?.catalog_model_id}
              </span>
              {catalog?.fetched_at && (
                <span className="text-xs text-muted-foreground">
                  {copy.fetchedAtLabel}: {formatTime(catalog.fetched_at)}
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

        {metadataBody}

        {bound && !unbindSnapshotComplete ? (
          <OperatorCallout
            intent="danger"
            description={copy.bindingSnapshotIncomplete}
          />
        ) : null}
      </div>

      {/* Dialogs mount only while open: local draft state dies with the
          unmount instead of being reset by an effect. */}
      {bindOpen && (
        <CatalogBindDialog
          isOpen
          modelConfigId={modelConfigId}
          prismModelId={prismModelId}
          apiFamily={apiFamily}
          busy={busy}
          onClose={() => setBindOpen(false)}
          runAction={runAction}
        />
      )}
      {refreshOpen && (
        <CatalogRefreshDialog
          isOpen
          modelConfigId={modelConfigId}
          busy={busy}
          onClose={() => setRefreshOpen(false)}
          runAction={runAction}
        />
      )}
      {overrideOpen && (
        <CatalogOverrideDialog
          key={modelConfigId}
          modelConfigId={modelConfigId}
          catalog={catalog}
          busy={busy}
          onClose={() => setOverrideOpen(false)}
          runAction={runAction}
        />
      )}
      {unbindOpen && unbindSnapshotComplete ? (
        <OperatorDestructiveDialog
          open
          onOpenChange={(open) => {
            if (!busy) setUnbindOpen(open);
          }}
          title={copy.unbindConfirmTitle}
          description={copy.unbindConfirmDescription}
          cancelLabel={messages.settingsDialogs.cancel}
          confirmLabel={copy.unbindConfirm}
          confirmingLabel={copy.unbinding}
          confirming={busy}
          confirmDisabled={actionsBlocked}
          cancelDisabled={busy}
          confirmTestId="models-dev-unbind-confirm"
          onCancel={() => setUnbindOpen(false)}
          onConfirm={handleUnbind}
        >
          {unbindError ? (
            <OperatorCallout intent="danger" description={unbindError} />
          ) : null}
        </OperatorDestructiveDialog>
      ) : null}
    </OperatorInsetPanel>
  );
}
