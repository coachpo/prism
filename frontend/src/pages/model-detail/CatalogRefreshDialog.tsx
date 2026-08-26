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
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
import type { ModelCatalogRefreshPreviewResponse } from "@/lib/types";
import {
  catalogFieldLabel,
  type CatalogFieldKey,
} from "./catalogMetadataPresentation";

type CatalogActionRunner = (
  action: () => Promise<unknown>,
  done?: () => void,
) => Promise<void>;

export function CatalogRefreshDialog({
  isOpen,
  modelConfigId,
  onClose,
  runAction,
}: {
  isOpen: boolean;
  modelConfigId: number;
  onClose: () => void;
  runAction: CatalogActionRunner;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [settled, setSettled] = useState<{
    preview: ModelCatalogRefreshPreviewResponse | null;
    error: string | null;
  } | null>(null);
  const loading = settled === null;
  const preview = settled?.preview ?? null;
  const error = settled?.error ?? null;

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const response = await modelsApi.catalog.refreshPreview(modelConfigId);
        if (!cancelled) setSettled({ preview: response, error: null });
      } catch (cause) {
        if (!cancelled) {
          setSettled({
            preview: null,
            error: cause instanceof Error ? cause.message : String(cause),
          });
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [modelConfigId]);

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{copy.refreshDialogTitle}</DialogTitle>
          <DialogDescription>{copy.refreshDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-[var(--density-inline-gap)]">
          {loading && (
            <p className="text-sm text-muted-foreground">{copy.loadingText}</p>
          )}
          {error && (
            <p className="text-sm text-destructive" role="alert">
              {error}
            </p>
          )}
          {preview && (
            <>
              <p className="text-xs text-muted-foreground">
                {copy.refreshRevisionLabel}:{" "}
                <span className="font-mono">{preview.catalog_revision}</span>
              </p>
              {preview.changes.length === 0 ? (
                <p className="text-sm text-muted-foreground">
                  {copy.refreshNoChanges}
                </p>
              ) : (
                <ul className="flex flex-col gap-1">
                  {preview.changes.map((change) => (
                    <li
                      key={change.field}
                      className="rounded border px-2 py-1 text-sm"
                    >
                      <span className="font-mono text-xs text-muted-foreground">
                        {catalogFieldLabel(copy, change.field as CatalogFieldKey)}
                      </span>
                      <span className="mx-2 line-through opacity-60">
                        {change.current ?? copy.valueAbsent}
                      </span>
                      <span aria-hidden>→</span>
                      <span className="ml-2">
                        {change.next ?? copy.valueAbsent}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </>
          )}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            type="button"
            disabled={!preview || loading}
            onClick={() =>
              preview &&
              runAction(
                () =>
                  modelsApi.catalog.refreshCommit(
                    modelConfigId,
                    preview.catalog_revision,
                  ),
                onClose,
              )
            }
          >
            {copy.refreshApply}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
