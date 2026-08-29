import { useMemo, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import { OperatorCallout, OperatorTypeBadge } from "@/shared/design-system";
import type { ExportSourceModelRow, PiCandidateWire } from "./exportTypes";
import { piBindingCoordinateKey } from "./piBindingCoordinate";
import { PiCandidateEvidence } from "./PiCandidateEvidence";
import {
  isModelExportSourceReconciliationError,
  type ModelExportSourceState,
} from "./useModelExportSource";

type Copy = Record<string, string>;

const CANDIDATE_STATUS_LABEL_KEYS: Record<string, string> = {
  not_in_catalog: "candidateStatusNotInCatalog",
  api_mismatch: "candidateStatusApiMismatch",
  catalog_unavailable: "candidateStatusCatalogUnavailable",
};

function apiErrorDetail(error: unknown, copy: Copy): string {
  if (isModelExportSourceReconciliationError(error)) {
    return copy.sourceReconciliationFailed;
  }
  return error instanceof Error ? error.message : String(error);
}

export function PiUnboundBinding({
  copy,
  model,
  sourceState,
}: {
  copy: Copy;
  model: ExportSourceModelRow;
  sourceState: ModelExportSourceState;
}) {
  const [pendingCandidate, setPendingCandidate] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const candidates = model.pi_candidates;
  const selectedCandidate = useMemo(
    () =>
      candidates.find(
        (candidate) => piBindingCoordinateKey(candidate) === pendingCandidate,
      ) ?? null,
    [candidates, pendingCandidate],
  );
  const only = candidates.length === 1 ? candidates[0] : null;

  async function handleBind(candidate?: PiCandidateWire) {
    setActionError(null);
    try {
      await sourceState.bindMutation.mutateAsync({
        modelConfigId: model.model_config_id,
        providerId: candidate?.provider_id,
        catalogModelId: candidate?.model_id,
        expectedCatalogRevision:
          sourceState.sourceQuery.data?.catalog.revision ?? "",
      });
    } catch (error) {
      setActionError(apiErrorDetail(error, copy));
    }
  }

  if (candidates.length === 0) {
    const key = CANDIDATE_STATUS_LABEL_KEYS[model.candidate_status];
    return (
      <OperatorTypeBadge
        intent="muted"
        label={copy[key] ?? model.candidate_status}
      />
    );
  }

  return (
    <div className="flex flex-col gap-1">
      {only ? (
        <>
          <span className="font-mono text-xs">
            {only.provider_id}/{only.model_id} ({only.api})
          </span>
          <PiCandidateEvidence candidate={only} copy={copy} />
        </>
      ) : (
        <Select
          disabled={sourceState.sourceActionsBlocked}
          value={pendingCandidate ?? ""}
          onValueChange={(value) => setPendingCandidate(value || null)}
        >
          <SelectTrigger
            size="sm"
            aria-label={copy.candidateSelectLabel}
            className="min-w-40"
          >
            <SelectValue placeholder={copy.candidateSelectPlaceholder} />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {candidates.map((candidate) => (
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
      <Button
        size="sm"
        variant="outline"
        disabled={
          (!only && !selectedCandidate) ||
          sourceState.bindMutation.isPending ||
          sourceState.sourceActionsBlocked
        }
        onClick={() => {
          if (only) {
            void handleBind();
          } else if (selectedCandidate) {
            void handleBind(selectedCandidate);
          }
        }}
      >
        {sourceState.bindMutation.isPending ? (
          <Spinner data-icon="inline-start" />
        ) : null}
        {copy.bindAction}
      </Button>
      {!only ? (
        <>
          <p className="text-xs text-destructive">
            {copy.candidateAmbiguousHint}
          </p>
          <PiCandidateEvidence candidate={selectedCandidate} copy={copy} />
        </>
      ) : null}
      {actionError ? (
        <OperatorCallout
          className="max-w-80"
          intent="danger"
          description={actionError}
        />
      ) : null}
    </div>
  );
}
