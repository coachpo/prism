import { useState } from "react";

import { Button } from "@/components/ui/button";
import { RefreshCw } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import {
  OperatorCallout,
  OperatorDestructiveDialog,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorMissingValue,
  OperatorRetryButton,
  OperatorStalenessBadge,
  OperatorStatusBadge,
  OperatorTypeBadge,
  OperatorValueBadge,
} from "@/shared/design-system";
import type { PiModelReadResponse } from "@/lib/types";
import { PiBindingOverrideDialog } from "@/features/models/catalog/pi/PiBindingOverrideDialog";
import { PiBindingRefreshDialog } from "@/features/models/catalog/pi/PiBindingRefreshDialog";
import { PiBindingSourceDialog } from "@/features/models/catalog/pi/PiBindingSourceDialog";
import { PiDroppedFieldsEvidence } from "@/features/models/catalog/pi/PiDroppedFieldsEvidence";
import {
  formatPiBindingMetadataValue,
  PI_OVERRIDE_FIELD_ORDER,
  type PiOverrideField,
} from "@/features/models/catalog/pi/piOverrideDraft";
import type {
  PiBindingController,
  PiBindingMetadataView,
  PiCatalogModelView,
} from "@/features/models/catalog/pi/usePiBindingController";

type Copy = Record<string, string>;

const CANDIDATE_STATUS_LABEL_KEYS: Record<string, string> = {
  not_in_catalog: "candidateStatusNotInCatalog",
  api_mismatch: "candidateStatusApiMismatch",
  single: "candidateStatusSingle",
  multiple: "candidateStatusMultiple",
  catalog_unavailable: "candidateStatusCatalogUnavailable",
};

const BINDING_STATUS_LABEL_KEYS: Record<string, string> = {
  bound: "bindingStatusBound",
  bound_drifted: "bindingStatusDrifted",
  unbound: "bindingStatusUnbound",
};

// Same field-to-label mapping as the override editor.
const FIELD_LABEL_KEYS: Record<PiOverrideField, string> = {
  name: "overrideNameLabel",
  reasoning: "overrideReasoningLabel",
  input: "overrideInputLabel",
  context_window: "overrideContextWindowLabel",
  max_tokens: "overrideMaxTokensLabel",
  thinking_level_map: "overrideThinkingLevelMapLabel",
  compat: "overrideCompatLabel",
};

function PiMetadataProjection({
  label,
  metadata,
  copy,
}: {
  label: string;
  metadata: PiBindingMetadataView | null;
  copy: Copy;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <p className="text-xs font-medium">{label}</p>
      <dl className="flex flex-col gap-1">
        {PI_OVERRIDE_FIELD_ORDER.map((field) => (
          <div
            key={field}
            className="grid min-w-0 grid-cols-[minmax(7rem,auto)_minmax(0,1fr)] gap-2 text-xs"
          >
            <dt className="text-muted-foreground">
              {copy[FIELD_LABEL_KEYS[field]] ?? copy.unknownFieldLabel}
            </dt>
            <dd className="truncate font-medium">
              {formatPiBindingMetadataValue(metadata, field) ??
                copy.overrideValueAbsent}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

/**
 * pi.dev Pi template panel of the model detail page. It shows the Prism
 * identity, the final Pi API, the persisted binding truth (coordinate, frozen
 * bind-time identity, status, renderability, revisions and stamps,
 * source/override/effective seven-field evidence), plus the live catalog
 * evidence — all from the single-model management read, never from the export
 * snapshot. Bind/rebind, refresh, override, and unbind go through the shared
 * controller and dialogs; every successful mutation reconciles through the
 * host's authoritative re-read.
 */
export function PiDevCatalogPanel({
  controller,
  read,
  readFailed,
  readStale,
  readError,
  lastSuccessfulAt,
  actionsBlocked,
  view,
  onRetry,
  readPending,
  readRefreshing,
}: {
  controller: PiBindingController;
  read: PiModelReadResponse | null;
  readFailed: boolean;
  readStale: boolean;
  readError: string | null;
  lastSuccessfulAt: string | null;
  actionsBlocked: boolean;
  view: PiCatalogModelView | null;
  /** Host-authoritative re-read for the single-model Pi query. */
  onRetry: () => void;
  readPending: boolean;
  readRefreshing: boolean;
}) {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
  const copy = messages.modelExportPage as Copy;
  const piCopy = messages.externalCatalog;
  const [sourceOpen, setSourceOpen] = useState(false);
  const [refreshOpen, setRefreshOpen] = useState(false);
  const [overrideOpen, setOverrideOpen] = useState(false);
  const [unbindOpen, setUnbindOpen] = useState(false);
  const [unbindError, setUnbindError] = useState<string | null>(null);

  if (!read || !view) {
    if (readPending) {
      return (
        <OperatorLoadingState
          testId="pi-detail-read-loading"
          title={piCopy.readLoadingTitle}
          className="py-3"
        />
      );
    }
    if (readFailed && !controller.mutationPending) {
      // The authoritative read failed: an error + retry surface, never a
      // fabricated unbound conclusion.
      return (
        <OperatorErrorState
          testId="pi-detail-read-error"
          title={piCopy.readFailedTitle}
          description={readError ?? undefined}
          action={
            <OperatorRetryButton onClick={onRetry}>
              {messages.common.retry}
            </OperatorRetryButton>
          }
        />
      );
    }
    return (
      <OperatorLoadingState
        testId="pi-detail-read-loading"
        title={piCopy.readLoadingTitle}
        className="py-3"
      />
    );
  }

  const catalog = read.catalog;
  const selected = view.selected;
  const candidateKey = CANDIDATE_STATUS_LABEL_KEYS[read.candidate_status];
  const bindingKey = BINDING_STATUS_LABEL_KEYS[read.binding_status];
  const boundPrismModelId = view.bindingPrismModelId;
  const isCrossDirectory = selected && boundPrismModelId
    ? selected.model_id !== boundPrismModelId
    : false;
  const canBind = Boolean(view.piApi);

  async function handleUnbind() {
    setUnbindError(null);
    try {
      await controller.unbind(view!.modelConfigId);
      setUnbindOpen(false);
    } catch (cause) {
      setUnbindError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  return (
    <div className="flex flex-col gap-2">
      {readRefreshing ? (
        <OperatorCallout
          data-testid="pi-detail-read-refreshing"
          intent="muted"
          description={piCopy.readRefreshing}
        />
      ) : null}
      {readStale ? (
        <div className="flex flex-wrap items-center gap-2">
          <OperatorStalenessBadge
            data-testid="pi-detail-read-stale"
            label={piCopy.readStaleBadgeLabel}
            reason={
              lastSuccessfulAt
                ? `${piCopy.readStaleLastSuccessLabel} ${formatTime(lastSuccessfulAt)}${
                    readError ? ` · ${readError}` : ""
                  }`
                : (readError ?? undefined)
            }
          />
          <OperatorRetryButton onClick={onRetry} disabled={readRefreshing}>
            {messages.common.retry}
          </OperatorRetryButton>
        </div>
      ) : null}
      <div className="flex flex-wrap items-center gap-2">
        <OperatorTypeBadge
          intent="muted"
          label={copy[candidateKey] ?? copy.candidateStatusUnknown}
          title={candidateKey ? undefined : read.candidate_status}
        />
        {read.binding.bound ? (
          <OperatorStatusBadge
            intent={read.binding_status === "bound" ? "healthy" : "degraded"}
            label={copy[bindingKey] ?? copy.bindingStatusUnknown}
            title={bindingKey ? undefined : read.binding_status}
          />
        ) : (
          <OperatorTypeBadge intent="muted" label={copy.bindingStatusUnbound} />
        )}
        {catalog.revision ? (
          <OperatorValueBadge
            label={`${piCopy.catalogRevisionLabel}: ${catalog.revision.slice(0, 18)}`}
            title={catalog.revision}
          />
        ) : (
          <OperatorValueBadge label={piCopy.catalogUnavailableBadge} />
        )}
      </div>

      <dl className="grid gap-x-3 gap-y-1 text-xs sm:grid-cols-[max-content_minmax(0,1fr)]">
        <dt className="text-muted-foreground">{piCopy.prismModelIdLabel}</dt>
        <dd className="break-all font-mono">{view.modelId}</dd>
        <dt className="text-muted-foreground">{piCopy.finalPiApiLabel}</dt>
        <dd className="break-all font-mono">
          {view.piApi || piCopy.finalPiApiAbsent}
        </dd>
        {selected ? (
          <>
            <dt className="text-muted-foreground">
              {piCopy.piCoordinateLabel}
            </dt>
            <dd className="break-all font-mono">
              {selected.provider_id}/{selected.model_id}
              {isCrossDirectory ? ` · ${copy.boundCrossDirectoryLabel}` : ""}
            </dd>
            <dt className="text-muted-foreground">{piCopy.piDirectoryApiLabel}</dt>
            <dd className="break-all font-mono">{selected.api}</dd>
            <dt className="text-muted-foreground">
              {piCopy.bindIdentityLabel}
            </dt>
            <dd className="break-all font-mono">
              {boundPrismModelId ?? piCopy.bindingIdentityAbsent}
            </dd>
            <dt className="text-muted-foreground">
              {piCopy.bindingRevisionLabel}
            </dt>
            <dd className="break-all font-mono">
              {read.binding.catalog_revision ?? "—"}
            </dd>
            <dt className="text-muted-foreground">{piCopy.fetchedAtLabel}</dt>
            <dd className="font-mono">
              {read.binding.fetched_at
                ? formatTime(read.binding.fetched_at)
                : "—"}
            </dd>
            <dt className="text-muted-foreground">{piCopy.updatedAtLabel}</dt>
            <dd className="font-mono">
              {read.binding.updated_at
                ? formatTime(read.binding.updated_at)
                : "—"}
            </dd>
          </>
        ) : null}
        <dt className="text-muted-foreground">{piCopy.catalogFetchedAtLabel}</dt>
        <dd className="font-mono">
          {catalog.fetched_at ? formatTime(catalog.fetched_at) : "—"}
        </dd>
        <dt className="text-muted-foreground">{piCopy.catalogCheckedAtLabel}</dt>
        <dd className="font-mono">
          {catalog.checked_at ? formatTime(catalog.checked_at) : "—"}
        </dd>
      </dl>

      {!read.binding_renderable && read.binding.bound ? (
        <p className="text-xs text-destructive">{copy.bindingNotRenderable}</p>
      ) : null}

      {read.binding.bound && view.bindingIntegrityError ? (
        <OperatorCallout
          intent="danger"
          description={piCopy.bindingIntegrityError}
        />
      ) : null}

      {read.binding.bound ? (
        <PiDroppedFieldsEvidence
          fields={read.binding.dropped_fields}
          label={copy.droppedFieldsLabel}
        />
      ) : null}

      {read.binding.bound ? (
        <div className="grid gap-3 lg:grid-cols-3">
          <PiMetadataProjection
            label={piCopy.bindingSourceEvidenceTitle}
            metadata={read.binding.source}
            copy={copy}
          />
          <PiMetadataProjection
            label={piCopy.bindingOverrideEvidenceTitle}
            metadata={read.binding.override}
            copy={copy}
          />
          <PiMetadataProjection
            label={piCopy.bindingEffectiveEvidenceTitle}
            metadata={read.binding.effective}
            copy={copy}
          />
        </div>
      ) : null}

      {!view.piApi ? (
        <OperatorMissingValue reason={copy.noPiApiCannotBind} />
      ) : null}

      <div className="flex flex-wrap gap-1">
        {canBind ? (
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={actionsBlocked}
            onClick={() => setSourceOpen(true)}
          >
            {read.binding.bound ? copy.changeSourceAction : copy.bindSourceAction}
          </Button>
        ) : null}
        {read.binding.bound ? (
          <>
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={actionsBlocked || !read.binding_renderable}
              onClick={() => setRefreshOpen(true)}
            >
              <RefreshCw data-icon="inline-start" />
              {copy.refreshAction}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={actionsBlocked || !read.binding_renderable}
              onClick={() => setOverrideOpen(true)}
            >
              {copy.overrideAction}
            </Button>
            <Button
              type="button"
              size="sm"
              variant="destructive"
              disabled={actionsBlocked}
              onClick={() => {
                setUnbindError(null);
                setUnbindOpen(true);
              }}
            >
              {copy.unbindAction}
            </Button>
          </>
        ) : null}
      </div>

      {catalog.status !== "fresh" ? (
        <OperatorCallout
          intent="warning"
          description={
            catalog.status === "stale"
              ? piCopy.catalogStaleNote
              : read.binding_renderable
                ? piCopy.catalogUnavailableBoundNote
                : piCopy.catalogUnavailableNote
          }
        />
      ) : null}

      {sourceOpen && view.piApi ? (
        <PiBindingSourceDialog
          copy={copy}
          view={view}
          onClose={() => setSourceOpen(false)}
          controller={controller}
        />
      ) : null}
      {refreshOpen && read.binding.bound && read.binding_renderable ? (
        <PiBindingRefreshDialog
          copy={copy}
          modelConfigId={view.modelConfigId}
          onClose={() => setRefreshOpen(false)}
          controller={controller}
        />
      ) : null}
      {overrideOpen && read.binding.bound && read.binding_renderable ? (
        <PiBindingOverrideDialog
          copy={copy}
          view={view}
          onClose={() => setOverrideOpen(false)}
          controller={controller}
        />
      ) : null}
      {unbindOpen && read.binding.bound ? (
        <OperatorDestructiveDialog
          open
          onOpenChange={(open) => {
            if (!controller.mutationPending) setUnbindOpen(open);
          }}
          title={copy.unbindConfirmTitle}
          description={copy.unbindConfirmDescription}
          cancelLabel={copy.cancel}
          confirmLabel={copy.unbindConfirm}
          confirmingLabel={copy.unbinding}
          confirming={controller.mutationPending}
          confirmDisabled={actionsBlocked}
          cancelDisabled={controller.mutationPending}
          confirmTestId="pi-detail-unbind-confirm"
          onCancel={() => setUnbindOpen(false)}
          onConfirm={handleUnbind}
        >
          {view.bindingOverride ? (
            <OperatorCallout
              intent="warning"
              description={copy.unbindOverridesWarning}
            />
          ) : null}
          {unbindError ? (
            <OperatorCallout intent="danger" description={unbindError} />
          ) : null}
        </OperatorDestructiveDialog>
      ) : null}
    </div>
  );
}
