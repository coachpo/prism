import { useCallback, useEffect, useState } from "react";

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
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorRetryButton,
} from "@/shared/design-system";
import type { PiRefreshPreviewResponse } from "@/lib/types";
import type { PiBindingController } from "./usePiBindingController";

type Copy = Record<string, string>;

/**
 * Pi binding refresh dialog. The preview/commit CAS chain (coordinate, API,
 * binding_updated_at token, catalog revision) comes from the shared
 * controller; after a successful commit the controller reconciles the host's
 * authoritative read. Stale evidence stays read-only inside the dialog.
 */
export function PiBindingRefreshDialog({
  copy,
  modelConfigId,
  onClose,
  controller,
}: {
  copy: Copy;
  modelConfigId: number;
  onClose: () => void;
  controller: PiBindingController;
}) {
  const [preview, setPreview] = useState<PiRefreshPreviewResponse | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [commitError, setCommitError] = useState<string | null>(null);
  const [previewPending, setPreviewPending] = useState(true);

  const loadPreview = useCallback(async () => {
    setPreviewPending(true);
    setPreview(null);
    setPreviewError(null);
    setCommitError(null);
    try {
      setPreview(await controller.openRefreshPreview(modelConfigId));
    } catch (cause) {
      setPreviewError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setPreviewPending(false);
    }
  }, [controller, modelConfigId]);

  useEffect(() => {
    let active = true;
    void (async () => {
      setPreviewPending(true);
      setPreview(null);
      setPreviewError(null);
      setCommitError(null);
      try {
        const result = await controller.openRefreshPreview(modelConfigId);
        if (active) setPreview(result);
      } catch (cause) {
        if (active)
          setPreviewError(cause instanceof Error ? cause.message : String(cause));
      } finally {
        if (active) setPreviewPending(false);
      }
    })();
    return () => {
      active = false;
    };
    // The preview is a one-shot for this mounted dialog/model. Controller
    // mutation-state renders must not reissue it; explicit retry uses
    // loadPreview above.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [modelConfigId]);

  async function handleCommit() {
    if (!preview) return;
    setCommitError(null);
    try {
      await controller.refreshCommit({
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
      setCommitError(cause instanceof Error ? cause.message : String(cause));
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !controller.mutationPending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{copy.refreshDialogTitle}</DialogTitle>
          <DialogDescription>{copy.refreshDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody>
          {previewPending ? (
            <OperatorLoadingState
              title={copy.refreshLoadingPreview}
              className="py-3"
            />
          ) : null}
          {previewError && !previewPending ? (
            <OperatorErrorState
              title={copy.refreshPreviewFailed}
              description={previewError}
              action={
                <OperatorRetryButton onClick={() => void loadPreview()}>
                  {copy.refreshPreviewRetry}
                </OperatorRetryButton>
              }
            />
          ) : controller.actionsBlocked ? (
            <OperatorCallout
              intent="danger"
              description={copy.sourceActionsBlocked}
            />
          ) : null}
          {commitError ? (
            <OperatorCallout
              intent="danger"
              title={copy.refreshCommitFailed}
              description={commitError}
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
            disabled={controller.mutationPending}
          >
            {copy.cancel}
          </Button>
          <Button
            onClick={() => void handleCommit()}
            disabled={
              !preview ||
              controller.mutationPending ||
              controller.actionsBlocked
            }
          >
            {controller.mutationPending ? (
              <Spinner data-icon="inline-start" />
            ) : null}
            {copy.refreshCommitAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
