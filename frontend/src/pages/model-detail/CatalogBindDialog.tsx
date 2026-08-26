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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
import type { CatalogCandidate } from "@/lib/types";

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
  const [preview, setPreview] = useState<CatalogMatchPreview | null>(null);
  const [loading, setLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [manualProvider, setManualProvider] = useState("");
  const [manualModel, setManualModel] = useState("");
  const [candidateQuery, setCandidateQuery] = useState("");
  const [candidates, setCandidates] = useState<CatalogCandidate[]>([]);
  const [candidatesTotal, setCandidatesTotal] = useState(0);

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

  // 有界候选查询：手动绑定时的搜索范围覆盖全部 provider。
  useEffect(() => {
    const handle = setTimeout(() => {
      modelsApi.catalog
        .candidates(modelConfigId, {
          q: candidateQuery || undefined,
          scope: candidateQuery ? "all" : "family",
          limit: 20,
        })
        .then((response) => {
          setCandidates(Array.isArray(response.items) ? response.items : []);
          setCandidatesTotal(
            Number.isFinite(response.total) ? response.total : 0,
          );
        })
        .catch(() => {
          setCandidates([]);
        });
    }, 250);
    return () => clearTimeout(handle);
  }, [modelConfigId, candidateQuery]);

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
                  onChange={(event) => setManualModel(event.target.value.trim())}
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
            <ul className="max-h-40 overflow-y-auto text-sm">
              {candidates.map((candidate) => (
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
            <p className="text-xs text-muted-foreground">
              {copy.candidateCount(candidates.length, candidatesTotal)}
            </p>
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
