import { useEffect, useState } from "react";

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
import { OperatorCallout, OperatorInsetPanel } from "@/shared/design-system";
import type { PiRefreshPreviewResponse } from "./exportTypes";
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

export function PiBindingRefreshDialog({
  copy,
  modelConfigId,
  onClose,
  sourceState,
}: {
  copy: Copy;
  modelConfigId: number;
  onClose: () => void;
  sourceState: ModelExportSourceState;
}) {
  const { refreshPreviewMutation, refreshCommitMutation } = sourceState;
  const [preview, setPreview] = useState<PiRefreshPreviewResponse | null>(null);
  const [error, setError] = useState<string | null>(null);
  const previewRequest = refreshPreviewMutation.mutateAsync;

  useEffect(() => {
    let active = true;
    void previewRequest({ modelConfigId })
      .then((result) => {
        if (active) setPreview(result);
      })
      .catch((cause: unknown) => {
        if (active) setError(apiErrorDetail(cause, copy));
      });
    return () => {
      active = false;
    };
  }, [copy, modelConfigId, previewRequest]);

  async function handleCommit() {
    if (!preview) return;
    setError(null);
    try {
      await refreshCommitMutation.mutateAsync({
        modelConfigId,
        expected: {
          provider_id: preview.provider_id,
          catalog_model_id: preview.catalog_model_id,
          api: preview.api,
          binding_updated_at: preview.binding_updated_at,
          catalog_revision: preview.catalog_revision,
        },
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
        if (!open && !refreshCommitMutation.isPending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{copy.refreshDialogTitle}</DialogTitle>
          <DialogDescription>{copy.refreshDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody>
          {refreshPreviewMutation.isPending ? (
            <div
              role="status"
              className="flex items-center gap-2 text-sm text-muted-foreground"
            >
              <Spinner data-icon="inline-start" />
              {copy.refreshLoadingPreview}
            </div>
          ) : null}
          {error ? (
            <OperatorCallout intent="danger" description={error} />
          ) : sourceState.sourceQuery.isError ? (
            <OperatorCallout
              intent="danger"
              description={copy.sourceActionsBlocked}
            />
          ) : null}
          {preview && !preview.changed ? (
            <p className="text-sm text-muted-foreground">
              {copy.refreshNoChanges}
            </p>
          ) : null}
          {preview && preview.changed ? (
            <div className="flex flex-col gap-2">
              {preview.changes.map((change) => (
                <OperatorInsetPanel key={change.field}>
                  <div className="font-mono text-xs font-medium">
                    {change.field}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {change.current ?? copy.refreshFieldAbsent} →{" "}
                    {change.next ?? copy.refreshFieldAbsent}
                  </div>
                </OperatorInsetPanel>
              ))}
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
            disabled={refreshCommitMutation.isPending}
          >
            {copy.cancel}
          </Button>
          <Button
            onClick={() => void handleCommit()}
            disabled={
              !preview ||
              refreshCommitMutation.isPending ||
              sourceState.sourceActionsBlocked
            }
          >
            {refreshCommitMutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : null}
            {copy.refreshCommitAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
