import { useLayoutEffect, useMemo, useState } from "react";

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
import { Spinner } from "@/components/ui/spinner";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorMissingValue,
} from "@/shared/design-system";
import type { PiCandidateWire } from "@/lib/types";
import { piBindingCoordinateKey } from "./piBindingCoordinate";
import { PiCandidateEvidence } from "./PiCandidateEvidence";
import { PiDirectorySearchPanel } from "./PiDirectorySearchPanel";
import type {
  PiBindingController,
  PiCatalogModelView,
} from "./usePiBindingController";

type Copy = Record<string, string>;
type DiscoveryLayer = "exact" | "search";

interface PendingChoice {
  key: string;
  layer: DiscoveryLayer;
  evidenceKey: string;
}

/**
 * Pi binding dialog: the single binding-authoring entry for every model whose
 * final Pi API is determinable, on top of the shared
 * {@link PiBindingController}. The two discovery layers stay visibly separate
 * — the default exact candidates (complete Prism model_id, case-sensitive,
 * same final Pi API) and the explicit paged directory search — and nothing is
 * preselected in either layer. A one-hit search is still only evidence.
 *
 * The selected candidate's catalog revision comes from the layer it belongs
 * to; when the source revision or Prism identity changes after selection,
 * the choice is invalidated and confirm stays inert. Stale catalog evidence
 * is readable but never confirmable.
 */
export function PiBindingSourceDialog({
  copy,
  view,
  onClose,
  controller,
}: {
  copy: Copy;
  view: PiCatalogModelView;
  onClose: () => void;
  controller: PiBindingController;
}) {
  const [pendingChoice, setPendingChoice] = useState<PendingChoice | null>(
    null,
  );
  const [actionError, setActionError] = useState<string | null>(null);
  const search = controller.directorySearch;
  const resetSearch = search.reset;

  // A page-level export controller may outlive one row dialog. Reset before
  // paint on every model identity so closing A and opening B can never expose
  // A's query, rows, or freshness evidence.
  useLayoutEffect(() => {
    resetSearch();
    return resetSearch;
  }, [resetSearch, view.modelConfigId, view.modelId, view.piApi]);

  const exactEvidenceKey = `${view.modelConfigId}\u0000${view.modelId}\u0000${view.piApi}\u0000${view.catalogRevision}`;
  const searchEvidence = search.evidence;
  const searchEvidenceKey = searchEvidence
    ? `${searchEvidence.modelConfigId}\u0000${searchEvidence.modelId}\u0000${searchEvidence.piApi}\u0000${searchEvidence.query}\u0000${searchEvidence.nonce}\u0000${searchEvidence.catalogRevision}`
    : "";

  // A choice belongs to the exact model/revision/query evidence the operator
  // selected. Derive invalidation instead of synchronizing state in an effect:
  // a same-coordinate result from a later generation is not the same choice.
  const effectivePendingChoice = pendingChoice
    ? pendingChoice.evidenceKey ===
      (pendingChoice.layer === "search"
        ? searchEvidenceKey
        : exactEvidenceKey)
      ? pendingChoice
      : null
    : null;

  const exactCandidates = view.liveCandidates;
  const searchResults = search.pager.items;
  const chosen = useMemo(() => {
    if (!effectivePendingChoice) return null;
    const currentEvidenceKey =
      effectivePendingChoice.layer === "search"
        ? searchEvidenceKey
        : exactEvidenceKey;
    if (effectivePendingChoice.evidenceKey !== currentEvidenceKey) return null;
    const candidates =
      effectivePendingChoice.layer === "search"
        ? searchResults
        : exactCandidates;
    return (
      candidates.find(
        (candidate) =>
          piBindingCoordinateKey(candidate) === effectivePendingChoice.key,
      ) ?? null
    );
  }, [effectivePendingChoice, exactCandidates, exactEvidenceKey, searchEvidenceKey, searchResults]);
  const soleExactCandidate =
    exactCandidates.length === 1 ? exactCandidates[0] : null;
  // The preserved single-candidate shortcut: with nothing chosen and exactly
  // one default exact candidate, confirm applies that candidate implicitly and
  // the backend records bind_source=single_candidate.
  const shortcutSole = !effectivePendingChoice && soleExactCandidate !== null;
  const effective = chosen ?? (shortcutSole ? soleExactCandidate : null);
  const effectiveLayer: DiscoveryLayer | null = chosen
    ? (effectivePendingChoice?.layer ?? null)
    : shortcutSole
      ? "exact"
      : null;

  const evidenceRevision =
    effectiveLayer === "search"
      ? (searchEvidence?.catalogRevision ?? "")
      : view.catalogRevision;
  const identityMatches =
    effectiveLayer === "search"
      ? searchEvidence?.modelConfigId === view.modelConfigId &&
        searchEvidence.modelId === view.modelId &&
        searchEvidence.piApi === view.piApi &&
        search.activeModelConfigId === view.modelConfigId
      : true;
  const evidenceFresh =
    effectiveLayer === "search"
      ? searchEvidence?.catalogStatus === "fresh" &&
        Boolean(evidenceRevision) &&
        identityMatches
      : view.catalogFresh && Boolean(view.catalogRevision);
  const staleLayer =
    effectiveLayer === "search"
      ? searchEvidence !== null && searchEvidence.catalogStatus !== "fresh"
      : !view.catalogFresh;

  const candidateMatchesIdentity =
    Boolean(effective) && effective?.api === view.piApi;

  const selected = view.selected;
  const coordinateChanges = Boolean(
    effective &&
      selected &&
      (effective.provider_id !== selected.provider_id ||
        effective.model_id !== selected.model_id ||
        effective.api !== selected.api),
  );
  const isCrossDirectory = Boolean(
    effective && effective.model_id !== view.modelId,
  );
  const clearsOverrides = coordinateChanges && Boolean(view.bindingOverride);

  // Stale last-known-good evidence is readable but never confirmable: bind
  // always requires a fresh fetch the backend re-verified.
  const canConfirm =
    evidenceFresh &&
    identityMatches &&
    candidateMatchesIdentity &&
    Boolean(effective) &&
    Boolean(view.piApi) &&
    !controller.actionsBlocked &&
    !search.pending;

  function handleSelect(key: string | null, layer: DiscoveryLayer) {
    const evidenceKey = layer === "search" ? searchEvidenceKey : exactEvidenceKey;
    setPendingChoice(key && evidenceKey ? { key, layer, evidenceKey } : null);
  }

  async function handleConfirm() {
    if (!effective || !view.piApi || !view.modelId) return;
    setActionError(null);
    try {
      await controller.bind({
        modelConfigId: view.modelConfigId,
        providerId: shortcutSole ? undefined : effective.provider_id,
        catalogModelId: shortcutSole ? undefined : effective.model_id,
        expectedCatalogRevision: evidenceRevision,
        expectedPrismModelId: view.modelId,
        expectedPiApi: view.piApi,
      });
      onClose();
    } catch (cause) {
      setActionError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !controller.bindPending) onClose();
      }}
    >
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>{copy.sourceDialogTitle}</DialogTitle>
          <DialogDescription>{copy.sourceDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          <OperatorInsetPanel title={copy.exportIdentityTitle}>
            <dl className="grid gap-x-3 gap-y-1 text-xs sm:grid-cols-[max-content_minmax(0,1fr)]">
              <dt className="text-muted-foreground">
                {copy.exportIdentityModelLabel}
              </dt>
              <dd className="break-all font-mono">{view.modelId}</dd>
              <dt className="text-muted-foreground">
                {copy.exportIdentityApiLabel}
              </dt>
              <dd className="break-all font-mono">
                {view.piApi || copy.exportIdentityApiUnknown}
              </dd>
              <dt className="text-muted-foreground">
                {copy.exportIdentityProviderLabel}
              </dt>
              <dd>{copy.exportIdentityProviderHint}</dd>
            </dl>
            <p className="text-xs text-muted-foreground">
              {copy.exportIdentityNote}
            </p>
          </OperatorInsetPanel>

          {selected ? (
            <p className="text-xs text-muted-foreground">
              {copy.rebindCurrentLabel}:{" "}
              <span className="font-mono">
                {selected.provider_id}/{selected.model_id} ({selected.api})
              </span>
              {view.bindingPrismModelId ? (
                <>
                  {" · "}
                  {copy.currentBindingIdentityLabel}:{" "}
                  <span className="font-mono">{view.bindingPrismModelId}</span>
                </>
              ) : null}
            </p>
          ) : null}

          {!view.catalogFresh && effectiveLayer !== "search" ? (
            <OperatorCallout
              intent="warning"
              description={copy.catalogNotFreshBlocked}
            />
          ) : null}
          {effectiveLayer === "search" && !evidenceFresh ? (
            <OperatorCallout
              intent="warning"
              description={copy.catalogNotFreshBlocked}
            />
          ) : null}
          {staleLayer ? (
            <OperatorCallout
              intent="warning"
              description={copy.directorySearchStaleReadOnly}
            />
          ) : null}

          <section className="flex flex-col gap-2">
            <h3 className="text-xs font-medium">{copy.exactCandidatesTitle}</h3>
            {exactCandidates.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                {copy.exactCandidatesEmpty}
              </p>
            ) : (
              <select
                aria-label={copy.candidateSelectLabel}
                className="h-8 w-full rounded-md border bg-background px-2 text-sm"
                value={
                  effectivePendingChoice?.layer === "exact"
                    ? effectivePendingChoice.key
                    : ""
                }
                disabled={controller.actionsBlocked}
                onChange={(event) =>
                  handleSelect(event.target.value || null, "exact")
                }
              >
                <option value="">{copy.candidateSelectPlaceholder}</option>
                {exactCandidates.map((candidate) => (
                  <option
                    key={piBindingCoordinateKey(candidate)}
                    value={piBindingCoordinateKey(candidate)}
                  >
                    {candidate.provider_id}/{candidate.model_id} (
                    {candidate.api})
                  </option>
                ))}
              </select>
            )}
            {exactCandidates.length > 1 ? (
              <p className="text-xs text-destructive">
                {copy.candidateAmbiguousHint}
              </p>
            ) : null}
          </section>

          {view.piApi ? (
            <PiDirectorySearchPanel
              copy={copy}
              modelConfigId={view.modelConfigId}
              modelId={view.modelId}
              piApi={view.piApi}
              controller={controller}
              selectedKey={
                effectivePendingChoice?.layer === "search"
                  ? effectivePendingChoice.key
                  : null
              }
              onSelect={(key) => handleSelect(key, "search")}
              disabled={controller.actionsBlocked}
            />
          ) : null}

          {effective ? (
            <>
              <OperatorInsetPanel title={copy.chosenCoordinateTitle}>
                <dl className="grid gap-x-3 gap-y-1 text-xs sm:grid-cols-[max-content_minmax(0,1fr)]">
                  <dt className="text-muted-foreground">
                    {copy.catalogProviderLabel}
                  </dt>
                  <dd className="break-all font-mono">
                    {effective.provider_id}
                  </dd>
                  <dt className="text-muted-foreground">
                    {copy.directorySearchLabel}
                  </dt>
                  <dd className="break-all font-mono">{effective.model_id}</dd>
                  <dt className="text-muted-foreground">
                    {copy.catalogApiLabel}
                  </dt>
                  <dd className="break-all font-mono">{effective.api}</dd>
                </dl>
                <p className="text-xs text-muted-foreground">
                  {isCrossDirectory
                    ? copy.chosenCrossDirectory
                    : copy.chosenSameDirectory}
                </p>
              </OperatorInsetPanel>
              <PiCandidateEvidence candidate={effective} copy={copy} />
            </>
          ) : (
            <OperatorMissingValue reason={copy.noCoordinateChosen} />
          )}

          {clearsOverrides ? (
            <OperatorCallout
              intent="danger"
              description={copy.rebindClearsOverrides}
            />
          ) : null}
          {actionError ? (
            <OperatorCallout intent="danger" description={actionError} />
          ) : controller.actionsBlocked && !controller.mutationPending ? (
            <OperatorCallout
              intent="danger"
              description={copy.sourceActionsBlocked}
            />
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
            disabled={controller.bindPending}
          >
            {copy.cancel}
          </Button>
          <Button
            variant={clearsOverrides ? "destructive" : "default"}
            onClick={() => void handleConfirm()}
            disabled={!canConfirm}
          >
            {controller.bindPending ? (
              <Spinner data-icon="inline-start" />
            ) : null}
            {clearsOverrides
              ? copy.rebindConfirmDestructiveAction
              : copy.sourceDialogConfirmAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// Re-export for tests that assert on wire-level candidate identity.
export type { PiCandidateWire };
