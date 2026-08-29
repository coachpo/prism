import { useMemo, useState } from "react";

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
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { OperatorCallout } from "@/shared/design-system";
import type { ExportSourceModelRow } from "./exportTypes";
import { piBindingCoordinateKey } from "./piBindingCoordinate";
import { PiCandidateEvidence } from "./PiCandidateEvidence";
import {
  isModelExportSourceReconciliationError,
  type ModelExportSourceState,
} from "./useModelExportSource";

type Copy = Record<string, string>;

function apiErrorDetail(error: unknown, copy: Copy): string {
  if (isModelExportSourceReconciliationError(error)) {
    return copy.sourceReconciliationFailed;
  }
  return error instanceof Error ? error.message : String(error);
}

export function PiBindingRebindDialog({
  copy,
  model,
  onClose,
  sourceState,
}: {
  copy: Copy;
  model: ExportSourceModelRow;
  onClose: () => void;
  sourceState: ModelExportSourceState;
}) {
  const [pendingCandidate, setPendingCandidate] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const selectedCandidate = useMemo(
    () =>
      model.pi_candidates.find(
        (candidate) => piBindingCoordinateKey(candidate) === pendingCandidate,
      ) ?? null,
    [model.pi_candidates, pendingCandidate],
  );
  const selected = model.pi_selected;
  const coordinateChanges = Boolean(
    selectedCandidate &&
      selected &&
      (selectedCandidate.provider_id !== selected.provider_id ||
        selectedCandidate.model_id !== selected.model_id ||
        selectedCandidate.api !== selected.api),
  );
  const clearsOverrides = coordinateChanges && Boolean(model.pi_binding_override);

  async function handleRebind() {
    if (!selectedCandidate) return;
    setError(null);
    try {
      await sourceState.bindMutation.mutateAsync({
        modelConfigId: model.model_config_id,
        providerId: selectedCandidate.provider_id,
        catalogModelId: selectedCandidate.model_id,
        expectedCatalogRevision:
          sourceState.sourceQuery.data?.catalog.revision ?? "",
      });
      onClose();
    } catch (cause) {
      setError(apiErrorDetail(cause, copy));
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !sourceState.bindMutation.isPending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{copy.rebindDialogTitle}</DialogTitle>
          <DialogDescription>{copy.rebindDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody>
          {selected ? (
            <p className="text-xs text-muted-foreground">
              {copy.rebindCurrentLabel}: {" "}
              <span className="font-mono">
                {selected.provider_id}/{selected.model_id} ({selected.api})
              </span>
            </p>
          ) : null}
          {model.pi_candidates.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              {copy.rebindNoCandidates}
            </p>
          ) : (
            <Select
              disabled={sourceState.sourceActionsBlocked}
              value={pendingCandidate ?? ""}
              onValueChange={(value) => setPendingCandidate(value || null)}
            >
              <SelectTrigger aria-label={copy.candidateSelectLabel}>
                <SelectValue placeholder={copy.candidateSelectPlaceholder} />
              </SelectTrigger>
              <SelectContent>
                <SelectGroup>
                  {model.pi_candidates.map((candidate) => (
                    <SelectItem
                      key={piBindingCoordinateKey(candidate)}
                      value={piBindingCoordinateKey(candidate)}
                    >
                      {candidate.provider_id}/{candidate.model_id} ({candidate.api})
                    </SelectItem>
                  ))}
                </SelectGroup>
              </SelectContent>
            </Select>
          )}
          <PiCandidateEvidence candidate={selectedCandidate} copy={copy} />
          {model.pi_candidates.length > 1 ? (
            <p className="text-xs text-muted-foreground">
              {copy.candidateAmbiguousHint}
            </p>
          ) : null}
          {clearsOverrides ? (
            <OperatorCallout
              intent="danger"
              description={copy.rebindClearsOverrides}
            />
          ) : null}
          {error ? (
            <OperatorCallout intent="danger" description={error} />
          ) : sourceState.sourceQuery.isError ? (
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
            disabled={sourceState.bindMutation.isPending}
          >
            {copy.cancel}
          </Button>
          <Button
            variant={clearsOverrides ? "destructive" : "default"}
            onClick={() => void handleRebind()}
            disabled={
              !selectedCandidate ||
              sourceState.bindMutation.isPending ||
              sourceState.sourceActionsBlocked
            }
          >
            {sourceState.bindMutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : null}
            {clearsOverrides
              ? copy.rebindConfirmDestructiveAction
              : copy.rebindConfirmAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
