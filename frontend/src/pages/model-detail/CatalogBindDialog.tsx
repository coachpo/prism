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
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
import type { CatalogCandidate } from "@/lib/types";
import { CatalogCandidatePicker } from "@/features/models/catalog/CatalogCandidatePicker";
import {
  OperatorErrorState,
  OperatorCallout,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorRetryButton,
} from "@/shared/design-system";
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
  onError?: (message: string) => void,
) => Promise<void>;

/**
 * models.dev bind/rebind dialog. The preview read gets a first-class
 * error+retry surface: while the dialog is open a failed read only produces
 * inline feedback, never a fabricated match or a silent empty state.
 *
 * Candidate paging (replace/append/retry/dedupe/rollover) goes through the
 * shared {@link CatalogCandidatePicker} on top of the shared pager; this
 * dialog owns only the manual-coordinate form and the bind payload.
 *
 * Every bind payload carries the Prism identity (model_id + api_family) the
 * operator confirmed plus the catalog revision; the backend re-verifies all
 * of it under the model row lock, so a concurrent rename rejects with 409.
 */
export function CatalogBindDialog({
  isOpen,
  modelConfigId,
  prismModelId,
  apiFamily,
  busy,
  onClose,
  runAction,
}: {
  isOpen: boolean;
  modelConfigId: number;
  prismModelId: string;
  apiFamily: string;
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
  const [mutationError, setMutationError] = useState<string | null>(null);
  const [manualProvider, setManualProvider] = useState("");
  const [manualModel, setManualModel] = useState("");
  const [candidateQuery, setCandidateQuery] = useState("");
  const [selectedCandidateKey, setSelectedCandidateKey] = useState<
    string | null
  >(null);
  const [selectedCandidateRevision, setSelectedCandidateRevision] = useState<
    string | null
  >(null);
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

  useEffect(() => {
    if (!candidates.revisionRolledOver) return;
    setSelectedCandidateKey(null);
    setSelectedCandidateRevision(null);
  }, [candidates.revisionRolledOver]);

  const uniqueMatch = preview?.committable ? preview : null;

  return (
    <Dialog open={isOpen} onOpenChange={(open) => !open && !busy && onClose()}>
      <DialogContent size="md">
        <DialogHeader>
          <DialogTitle>{copy.bindDialogTitle}</DialogTitle>
          <DialogDescription>{copy.bindDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-[var(--density-inline-gap)]">
          {loading && (
            <OperatorLoadingState
              testId="catalog-bind-preview-loading"
              title={copy.readLoadingTitle}
              className="py-3"
            />
          )}
          {previewError && !loading && (
            <OperatorErrorState
              testId="catalog-bind-preview-error"
              title={copy.previewFailedTitle}
              description={previewError}
              action={
                <OperatorRetryButton onClick={() => void loadPreview()}>
                  <RefreshCw data-icon="inline-start" />
                  {copy.readRetry}
                </OperatorRetryButton>
              }
            />
          )}
          {uniqueMatch && (
            <OperatorInsetPanel title={copy.uniqueMatchFound}>
              <p className="font-mono text-sm text-muted-foreground">
                {uniqueMatch.provider_id} / {uniqueMatch.catalog_model_id}
              </p>
              <Button
                type="button"
                size="sm"
                disabled={busy}
                onClick={() => {
                  setMutationError(null);
                  void runAction(
                    () =>
                      modelsApi.catalog.bind(modelConfigId, {
                        expected_catalog_revision: uniqueMatch.catalog_revision,
                        expected_prism_model_id: prismModelId,
                        expected_api_family: apiFamily,
                      }),
                    onClose,
                    setMutationError,
                  );
                }}
              >
                {copy.applyUniqueMatch}
              </Button>
            </OperatorInsetPanel>
          )}
          {preview && !uniqueMatch && !loading && (
            <OperatorCallout
              intent="warning"
              title={
                preview.reason === "ambiguous"
                  ? copy.ambiguousMatch
                  : copy.noMatch
              }
            >
              <div className="flex flex-col gap-1">
                <ul className="list-inside list-disc text-xs">
                  {preview.candidates.slice(0, 5).map((candidate) => (
                    <li
                      key={candidate.provider_id + "/" + candidate.model_id}
                      className="font-mono"
                    >
                      {candidate.provider_id} / {candidate.model_id}
                    </li>
                  ))}
                </ul>
                <p className="text-xs">{copy.explicitBindHint}</p>
              </div>
            </OperatorCallout>
          )}

          <OperatorInsetPanel title={copy.manualBindTitle}>
            <FieldGroup className="grid grid-cols-2 gap-2">
              <Field>
                <FieldLabel htmlFor="catalog-bind-provider">
                  {copy.providerLabel}
                </FieldLabel>
                <Input
                  id="catalog-bind-provider"
                  value={manualProvider}
                  onChange={(event) => {
                    setManualProvider(event.target.value.trim());
                    setSelectedCandidateKey(null);
                    setSelectedCandidateRevision(null);
                  }}
                  placeholder="openai"
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="catalog-bind-model">
                  {copy.modelIdLabel}
                </FieldLabel>
                <Input
                  id="catalog-bind-model"
                  value={manualModel}
                  onChange={(event) => {
                    setManualModel(event.target.value.trim());
                    setSelectedCandidateKey(null);
                    setSelectedCandidateRevision(null);
                  }}
                  placeholder="gpt-4o"
                />
              </Field>
            </FieldGroup>
            <Button
              type="button"
              size="sm"
              disabled={busy || !preview || !manualProvider || !manualModel}
              onClick={() => {
                if (!preview) return;
                setMutationError(null);
                void runAction(
                  () =>
                    modelsApi.catalog.bind(modelConfigId, {
                      provider_id: manualProvider,
                      catalog_model_id: manualModel,
                      expected_catalog_revision:
                        selectedCandidateRevision ?? preview.catalog_revision,
                      expected_prism_model_id: prismModelId,
                      expected_api_family: apiFamily,
                    }),
                  onClose,
                  setMutationError,
                );
              }}
            >
              {copy.bindExplicitAction}
            </Button>
          </OperatorInsetPanel>

          <div className="flex flex-col gap-2">
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="catalog-candidate-search">
                  {copy.candidateSearchLabel}
                </FieldLabel>
                <Input
                  id="catalog-candidate-search"
                  value={candidateQuery}
                  onChange={(event) => {
                    setCandidateQuery(event.target.value);
                    setSelectedCandidateKey(null);
                    setSelectedCandidateRevision(null);
                  }}
                  placeholder={copy.candidateSearchPlaceholder}
                />
                <FieldDescription>{copy.candidateSearchHint}</FieldDescription>
              </Field>
            </FieldGroup>
            <CatalogCandidatePicker
              pager={candidates}
              itemKey={(candidate) =>
                `${candidate.provider_id}/${candidate.model_id}`
              }
              renderCandidate={(candidate) => (
                <span className="flex min-w-0 items-baseline gap-2">
                  <span className="truncate font-mono text-sm">
                    {candidate.provider_id}/{candidate.model_id}
                  </span>
                  <span className="truncate text-sm text-muted-foreground">
                    {candidate.name}
                  </span>
                </span>
              )}
              selectedKey={selectedCandidateKey}
              onSelect={(key) => {
                setSelectedCandidateKey(key);
                setSelectedCandidateRevision(
                  key
                    ? (candidates.revision?.replace(/^models\.dev:/, "") ?? null)
                    : null,
                );
                const picked = candidates.items.find(
                  (candidate) =>
                    `${candidate.provider_id}/${candidate.model_id}` === key,
                );
                if (picked) {
                  setManualProvider(picked.provider_id);
                  setManualModel(picked.model_id);
                }
              }}
              disabled={busy}
              testIdPrefix="catalog-candidate"
              labels={{
                loading: copy.candidateLoading,
                loadFailed: copy.candidateLoadFailed,
                retry: copy.candidateRetry,
                empty: copy.candidateEmpty,
                loadMore: copy.loadMoreCandidates,
                loadingMore: tableCopy.loadingMore,
                retryLoadMore: tableCopy.retryLoadMore,
                count: copy.candidateCount,
                liveLoading: copy.loadingMoreCandidates,
                revisionRollover: copy.revisionRolloverNotice,
                revisionRolloverAcknowledge:
                  copy.revisionRolloverAcknowledge,
                listboxLabel: copy.candidateSearchLabel,
              }}
            />
          </div>
          {mutationError ? (
            <OperatorCallout intent="danger" description={mutationError} />
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
            {messages.settingsDialogs.cancel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
