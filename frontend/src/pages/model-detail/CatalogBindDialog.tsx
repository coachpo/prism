import { useCallback, useEffect, useState } from "react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
import type { CatalogCandidate } from "@/lib/types";
import {
  OperatorEmptyState,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorRetryButton,
} from "@/shared/design-system";
import {
  LoadMoreControl,
  PaginationLiveStatus,
} from "@/shared/table/paginationControls";
import { useCatalogCandidates } from "./useCatalogCandidates";

interface CatalogMatchPreview {
  committable: boolean;
  provider_id?: string;
  catalog_model_id?: string;
  candidates: CatalogCandidate[];
  reason: string;
  catalog_revision: string;
  fetched_at: string;
}

type CatalogActionRunner = (
  action: () => Promise<unknown>,
  done?: () => void,
) => Promise<void>;

export function CatalogBindDialog({
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
  const tableCopy = messages.operationalTable;
  const [preview, setPreview] = useState<CatalogMatchPreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [manualProvider, setManualProvider] = useState("");
  const [manualModel, setManualModel] = useState("");
  const [candidateQuery, setCandidateQuery] = useState("");
  const candidates = useCatalogCandidates(modelConfigId, candidateQuery);

  const loadPreview = useCallback(async () => {
    setLoading(true);
    setPreviewError(null);
    try {
      const result = await modelsApi.catalog.matchPreview(modelConfigId);
      setPreview(result);
    } catch (cause) {
      setPreviewError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setLoading(false);
    }
  }, [modelConfigId]);

  useEffect(() => {
    void loadPreview();
  }, [loadPreview]);

  const uniqueMatch = preview?.committable ? preview : null;

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{copy.bindDialogTitle}</DialogTitle>
          <DialogDescription>{copy.bindDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-[var(--density-inline-gap)]">
          {loading && (
            <p className="text-sm text-muted-foreground">{copy.loadingText}</p>
          )}
          {previewError && (
            <p className="text-sm text-destructive" role="alert">
              {previewError}
            </p>
          )}
          {uniqueMatch && (
            <div className="rounded-md border p-3">
              <p className="text-sm font-medium">{copy.uniqueMatchFound}</p>
              <p className="font-mono text-sm text-muted-foreground">
                {uniqueMatch.provider_id} / {uniqueMatch.catalog_model_id}
              </p>
              <Button
                type="button"
                size="sm"
                className="mt-2"
                disabled={busy}
                onClick={() =>
                  runAction(
                    () =>
                      modelsApi.catalog.bind(modelConfigId, {
                        expected_catalog_revision: uniqueMatch.catalog_revision,
                      }),
                    onClose,
                  )
                }
              >
                {copy.applyUniqueMatch}
              </Button>
            </div>
          )}
          {preview && !uniqueMatch && !loading && (
            <div className="rounded-md border border-warning-foreground/40 bg-warning-background/30 p-3">
              <p className="text-sm font-medium">
                {preview.reason === "ambiguous"
                  ? copy.ambiguousMatch
                  : copy.noMatch}
              </p>
              <ul className="mt-1 list-inside list-disc text-xs text-muted-foreground">
                {preview.candidates.slice(0, 5).map((candidate) => (
                  <li
                    key={candidate.provider_id + "/" + candidate.model_id}
                    className="font-mono"
                  >
                    {candidate.provider_id} / {candidate.model_id}
                  </li>
                ))}
              </ul>
              <p className="mt-1 text-xs text-muted-foreground">
                {copy.explicitBindHint}
              </p>
            </div>
          )}

          <div className="flex flex-col gap-2 rounded-md border p-3">
            <p className="text-sm font-medium">{copy.manualBindTitle}</p>
            <div className="grid grid-cols-2 gap-2">
              <div className="flex flex-col gap-1">
                <Label htmlFor="catalog-bind-provider">
                  {copy.providerLabel}
                </Label>
                <Input
                  id="catalog-bind-provider"
                  value={manualProvider}
                  onChange={(event) =>
                    setManualProvider(event.target.value.trim())
                  }
                  placeholder="openai"
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor="catalog-bind-model">{copy.modelIdLabel}</Label>
                <Input
                  id="catalog-bind-model"
                  value={manualModel}
                  onChange={(event) =>
                    setManualModel(event.target.value.trim())
                  }
                  placeholder="gpt-4o"
                />
              </div>
            </div>
            <Button
              type="button"
              size="sm"
              disabled={busy || !preview || !manualProvider || !manualModel}
              onClick={() =>
                preview &&
                runAction(
                  () =>
                    modelsApi.catalog.bind(modelConfigId, {
                      provider_id: manualProvider,
                      catalog_model_id: manualModel,
                      expected_catalog_revision: preview.catalog_revision,
                    }),
                  onClose,
                )
              }
            >
              {copy.bindExplicitAction}
            </Button>
          </div>

          <div className="flex flex-col gap-2">
            <Label htmlFor="catalog-candidate-search">
              {copy.candidateSearchLabel}
            </Label>
            <Input
              id="catalog-candidate-search"
              value={candidateQuery}
              onChange={(event) => setCandidateQuery(event.target.value)}
              placeholder={copy.candidateSearchPlaceholder}
            />
            {candidates.phase === "loading" ? (
              <OperatorLoadingState
                title={copy.candidateLoading}
                className="py-3"
              />
            ) : candidates.phase === "error" ? (
              // 替换读取失败时候选集未知，不能降级成“没有匹配”的空结果。
              <OperatorErrorState
                testId="catalog-candidate-error"
                title={copy.candidateLoadFailed}
                description={candidates.error}
                action={
                  <OperatorRetryButton onClick={candidates.onRetry}>
                    <RefreshCw data-icon="inline-start" />
                    {copy.candidateRetry}
                  </OperatorRetryButton>
                }
              />
            ) : (
              <>
                <ul
                  className="max-h-40 overflow-y-auto text-sm"
                  aria-busy={candidates.appending || undefined}
                >
                  {candidates.items.map((candidate) => (
                    <li key={candidate.provider_id + "/" + candidate.model_id}>
                      <button
                        type="button"
                        className="w-full truncate rounded px-1 py-0.5 text-left hover:bg-muted"
                        onClick={() => {
                          setManualProvider(candidate.provider_id);
                          setManualModel(candidate.model_id);
                        }}
                      >
                        <span className="font-mono">
                          {candidate.provider_id}/{candidate.model_id}
                        </span>
                        <span className="ml-2 text-muted-foreground">
                          {candidate.name}
                        </span>
                      </button>
                    </li>
                  ))}
                </ul>
                {candidates.items.length === 0 ? (
                  <OperatorEmptyState
                    testId="catalog-candidate-empty"
                    title={copy.candidateEmpty}
                    description={copy.candidateEmptyDescription}
                    className="py-4"
                  />
                ) : (
                  <LoadMoreControl
                    testId="catalog-candidate-load-more"
                    pending={candidates.appending}
                    error={candidates.appendError}
                    hasMore={candidates.hasMore}
                    labels={{
                      loadMore: copy.loadMoreCandidates,
                      loading: tableCopy.loadingMore,
                      retry: tableCopy.retryLoadMore,
                    }}
                    onLoadMore={candidates.onLoadMore}
                  />
                )}
                <PaginationLiveStatus
                  message={
                    candidates.appending ? copy.loadingMoreCandidates : null
                  }
                />
                <p className="text-xs text-muted-foreground">
                  {copy.candidateCount(
                    candidates.items.length,
                    candidates.total,
                  )}
                </p>
              </>
            )}
          </div>
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
