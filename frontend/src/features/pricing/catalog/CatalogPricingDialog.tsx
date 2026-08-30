import { useState, type ReactNode } from "react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useLocale } from "@/i18n/useLocale";
import {
  OperatorCallout,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorRetryButton,
  OperatorStatusBadge,
} from "@/shared/design-system";

import { CatalogPricingPreviewPanel } from "./CatalogPricingPreviewPanel";
import { catalogCommitBlockers } from "./catalogPricingPresentation";
import {
  useCatalogPricingImport,
  type CatalogPricingSource,
} from "./useCatalogPricingImport";

export type { CatalogPricingSource } from "./useCatalogPricingImport";

/**
 * A Terminal Target the operator may assign prices to. Both surfaces can build
 * one: model detail maps its full Connection rows, the pricing page maps the
 * shared connection-option dropdown list.
 */
export type CatalogPricingTargetOption = { id: number; name: string | null };

/**
 * The shared models.dev source-linked pricing dialog.
 *
 * Model detail mounts it with a resolved `bound_model` source and the current
 * Terminal Target preselected and locked. The /route/pricing catalog import
 * mounts it with a `discovery` node and no source yet: the preview and commit
 * only appear once the operator has resolved an offering, and the target set
 * starts empty.
 *
 * Nothing here auto-selects or auto-commits. The preview is re-read whenever the
 * target set changes, and a rejected commit forces a fresh preview.
 */
export function CatalogPricingDialog({
  isOpen,
  source,
  title,
  description,
  targets,
  initialConnectionIds,
  lockedConnectionIds = [],
  discovery,
  onClose,
  onCommitted,
}: {
  isOpen: boolean;
  source: CatalogPricingSource | null;
  title: string;
  description?: string;
  targets: CatalogPricingTargetOption[];
  initialConnectionIds: number[];
  /** Targets that stay checked: the model-detail current target. */
  lockedConnectionIds?: number[];
  /** Offering-discovery step shown until a source is resolved. */
  discovery?: ReactNode;
  onClose: () => void;
  onCommitted: (templateName: string, assignedCount: number) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-2xl" data-testid="catalog-pricing-dialog">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {description ?? copy.pricingDialogDescription}
          </DialogDescription>
        </DialogHeader>
        {/* Discovery stays mounted beside the preview once an offering is
            resolved, so the operator can change the model or re-pick a
            candidate without closing the dialog and losing their place. */}
        {discovery ? (
          <DialogBody className="flex max-h-[65vh] min-w-0 flex-col gap-[var(--density-inline-gap)] overflow-y-auto">
            {discovery}
            {!source ? (
              <OperatorCallout
                intent="info"
                title={copy.catalogPreviewAwaitingTitle}
                description={copy.catalogPreviewAwaitingDescription}
              />
            ) : null}
          </DialogBody>
        ) : null}
        {source ? (
          <CatalogPricingFlow
            source={source}
            targets={targets}
            initialConnectionIds={initialConnectionIds}
            lockedConnectionIds={lockedConnectionIds}
            onClose={onClose}
            onCommitted={onCommitted}
          />
        ) : (
          <>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={onClose}>
                {messages.settingsDialogs.cancel}
              </Button>
              <Button
                type="button"
                disabled
                data-testid="catalog-pricing-submit"
              >
                {initialConnectionIds.length === 0
                  ? copy.pricingCommitTemplateOnlyAction
                  : copy.pricingCommitAction}
              </Button>
            </DialogFooter>
            <p
              className="px-6 pb-4 text-xs text-muted-foreground"
              data-testid="catalog-pricing-blockers"
            >
              {copy.pricingBlockedNoPreview}
            </p>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

/**
 * The preview/target-selection/commit flow for one resolved offering source.
 * Mounted only while the dialog is open, so drafts never outlive a close.
 */
function CatalogPricingFlow({
  source,
  targets,
  initialConnectionIds,
  lockedConnectionIds,
  onClose,
  onCommitted,
}: {
  source: CatalogPricingSource;
  targets: CatalogPricingTargetOption[];
  initialConnectionIds: number[];
  lockedConnectionIds: number[];
  onClose: () => void;
  onCommitted: (templateName: string, assignedCount: number) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [commitError, setCommitError] = useState<string | null>(null);

  const {
    commit,
    committing,
    confirmDrift,
    connectionIds,
    error: previewError,
    loading,
    preview,
    refresh,
    setConfirmDrift,
    toggleConnection,
  } = useCatalogPricingImport({ source, initialConnectionIds, enabled: true });

  const blockers = catalogCommitBlockers(copy, preview, { confirmDrift });
  const canCommit = blockers.length === 0 && !loading && !committing;

  const handleSubmit = async () => {
    setCommitError(null);
    try {
      const committed = await commit();
      if (!committed) return;
      onCommitted(
        committed.template?.name ??
          `${committed.offering.provider_id}/${committed.offering.catalog_model_id}`,
        connectionIds.length,
      );
      onClose();
    } catch (cause) {
      // The hook already dropped the stale preview and re-read; this message
      // explains why the operator has to look again.
      setCommitError(cause instanceof Error ? cause.message : String(cause));
    }
  };

  return (
    <>
      <DialogBody className="flex max-h-[65vh] min-w-0 flex-col gap-[var(--density-inline-gap)] overflow-y-auto">
        {loading ? <OperatorLoadingState title={copy.loadingText} /> : null}

        {commitError ? (
          <OperatorCallout
            intent="danger"
            title={copy.pricingLoadFailed}
            description={commitError}
          />
        ) : null}

        {previewError ? (
          <OperatorErrorState
            title={copy.pricingLoadFailed}
            description={previewError}
            action={
              <OperatorRetryButton
                onClick={() => {
                  setCommitError(null);
                  refresh();
                }}
              >
                {messages.common.retry}
              </OperatorRetryButton>
            }
          />
        ) : null}

        {preview && !loading ? (
          <CatalogPricingPreviewPanel preview={preview} />
        ) : null}

        {preview?.drift ? (
          <label className="flex items-start gap-2 rounded-md border border-warning-foreground/40 bg-warning-background/20 p-3 text-sm">
            <Checkbox
              checked={confirmDrift}
              onCheckedChange={(checked) => setConfirmDrift(checked === true)}
              data-testid="catalog-pricing-confirm-drift"
            />
            <span>{copy.pricingDriftConfirmLabel}</span>
          </label>
        ) : null}

        <div className="flex min-w-0 flex-col gap-2">
          <p className="text-sm font-medium">{copy.pricingTargetsLabel}</p>
          <p className="text-xs text-muted-foreground">
            {copy.pricingTargetsHint}
          </p>
          {targets.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {copy.pricingNoTargets}
            </p>
          ) : (
            <div className="flex max-h-48 flex-col gap-1 overflow-y-auto">
              {targets.map((target) => {
                const locked = lockedConnectionIds.includes(target.id);
                return (
                  <label
                    key={target.id}
                    className="flex min-w-0 items-center gap-2 text-sm"
                  >
                    <Checkbox
                      checked={connectionIds.includes(target.id)}
                      onCheckedChange={() => toggleConnection(target.id)}
                      disabled={locked || committing}
                      data-testid={`catalog-pricing-target-${target.id}`}
                    />
                    <span className="truncate">
                      {target.name ?? copy.pricingTargetNameFallback(target.id)}
                    </span>
                    {locked ? (
                      <OperatorStatusBadge
                        intent="accent"
                        preserveLabel
                        label={copy.pricingCurrentTargetBadge}
                      />
                    ) : null}
                  </label>
                );
              })}
            </div>
          )}
        </div>

        {!canCommit && !loading && blockers.length > 0 ? (
          <ul
            className="flex flex-col gap-1 text-xs text-muted-foreground"
            data-testid="catalog-pricing-blockers"
          >
            {blockers.map((blocker) => (
              <li key={blocker}>{blocker}</li>
            ))}
          </ul>
        ) : null}
      </DialogBody>
      <DialogFooter>
        <Button
          type="button"
          variant="outline"
          onClick={onClose}
          disabled={committing}
        >
          {messages.settingsDialogs.cancel}
        </Button>
        <Button
          type="button"
          disabled={!canCommit}
          onClick={() => void handleSubmit()}
          data-testid="catalog-pricing-submit"
        >
          {connectionIds.length === 0
            ? copy.pricingCommitTemplateOnlyAction
            : copy.pricingCommitAction}
        </Button>
      </DialogFooter>
    </>
  );
}

export default CatalogPricingDialog;
