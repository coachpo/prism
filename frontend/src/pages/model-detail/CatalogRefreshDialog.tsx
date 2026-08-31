import { useEffect, useState } from "react";
import { RefreshCw } from "lucide-react";

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
  OperatorErrorState,
  OperatorCallout,
  OperatorLoadingState,
  OperatorRetryButton,
} from "@/shared/design-system";
import {
  catalogFieldLabel,
  type CatalogFieldKey,
} from "./catalogMetadataPresentation";

type CatalogActionRunner = (
  action: () => Promise<unknown>,
  done?: () => void,
  onError?: (message: string) => void,
) => Promise<void>;

/**
 * models.dev refresh dialog. The preview read gets a first-class error+retry
 * surface (dialog-open failures stay inline, never toasts). The commit
 * carries the full local CAS chain from the preview — coordinate,
 * binding_updated_at token, and catalog revision — so a rebind/override that
 * happened between preview and commit rejects with 409 instead of clobbering
 * newer local facts.
 */
export function CatalogRefreshDialog({
  isOpen,
  modelConfigId,
  busy,
  onClose,
  runAction,
}: {
  isOpen: boolean;
  modelConfigId: number;
  busy: boolean;
  onClose: () => void;
  runAction: CatalogActionRunner;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;
  const [settled, setSettled] = useState<{
    preview: ModelCatalogRefreshPreviewResponse | null;
    error: string | null;
  } | null>(null);
  const [reloadToken, setReloadToken] = useState(0);
  const [mutationError, setMutationError] = useState<string | null>(null);
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
  }, [modelConfigId, reloadToken]);

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent className="max-w-lg">
        <DialogHeader>
          <DialogTitle>{copy.refreshDialogTitle}</DialogTitle>
          <DialogDescription>{copy.refreshDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-[var(--density-inline-gap)]">
          {loading && (
            <OperatorLoadingState
              testId="catalog-refresh-preview-loading"
              title={copy.readLoadingTitle}
              className="py-3"
            />
          )}
          {error && !loading && (
            <OperatorErrorState
              testId="catalog-refresh-preview-error"
              title={copy.previewFailedTitle}
              description={error}
              action={
                <OperatorRetryButton
                  onClick={() => {
                    setSettled(null);
                    setReloadToken((token) => token + 1);
                  }}
                >
                  <RefreshCw data-icon="inline-start" />
                  {copy.readRetry}
                </OperatorRetryButton>
              }
            />
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
                        {catalogFieldLabel(
                          copy,
                          change.field as CatalogFieldKey,
                        )}
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
          {mutationError ? (
            <OperatorCallout intent="danger" description={mutationError} />
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            type="button"
            disabled={
              !preview ||
              busy ||
              loading ||
              !preview.provider_id ||
              !preview.catalog_model_id
            }
            onClick={() => {
              if (!preview) return;
              setMutationError(null);
              void runAction(
                () =>
                  modelsApi.catalog.refreshCommit(modelConfigId, {
                    expected_provider_id: preview.provider_id ?? "",
                    expected_catalog_model_id: preview.catalog_model_id ?? "",
                    expected_binding_updated_at: preview.binding_updated_at,
                    expected_catalog_revision: preview.catalog_revision,
                  }),
                onClose,
                setMutationError,
              );
            }}
          >
            {copy.refreshApply}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
