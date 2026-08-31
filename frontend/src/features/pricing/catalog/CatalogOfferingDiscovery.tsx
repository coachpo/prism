import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { CatalogCandidatePicker } from "@/features/models/catalog/CatalogCandidatePicker";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Field,
  FieldDescription,
  FieldGroup,
  FieldLabel,
} from "@/components/ui/field";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { api } from "@/lib/api";
import type {
  CatalogCandidate,
  ModelCatalogMatchPreviewResponse,
  ModelConfigListItem,
} from "@/lib/types";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorStatusBadge,
} from "@/shared/design-system";
import { useCatalogCandidates } from "@/pages/model-detail/useCatalogCandidates";

import type { CatalogPricingSource } from "./useCatalogPricingImport";

function modelLabel(model: ModelConfigListItem): string {
  const display = model.display_name?.trim() || model.model_id;
  return `${display} · ${model.model_id} · ${formatApiFamily(model.api_family)}`;
}

/**
 * The pricing page's offering discovery step.
 *
 * Discovery is automatic, selection is not:
 * - A unique exact match advances straight into the price preview, because there
 *   is exactly one thing it can mean.
 * - Zero or multiple matches stop here. The operator searches the bounded
 *   candidate list and picks coordinates explicitly. The first candidate is
 *   never taken on the operator's behalf, and nothing is ever committed from
 *   this step.
 *
 * Importing prices never binds the model: the resolved coordinates are used for
 * this import only.
 */
export function CatalogOfferingDiscovery({
  models,
  onResolved,
}: {
  models: ModelConfigListItem[];
  onResolved: (source: CatalogPricingSource | null) => void;
}) {
  const { messages } = useLocale();
  const copy = messages.modelCatalog;

  const [modelId, setModelId] = useState<string>("");
  const [match, setMatch] = useState<ModelCatalogMatchPreviewResponse | null>(
    null,
  );
  const [matchError, setMatchError] = useState<string | null>(null);
  const [matching, setMatching] = useState(false);
  const [query, setQuery] = useState("");
  const [pickedKey, setPickedKey] = useState<string | null>(null);
  const matchGeneration = useRef(0);

  const selectedModel = useMemo(
    () => models.find((model) => String(model.id) === modelId) ?? null,
    [modelId, models],
  );

  // Changing or clearing the model invalidates everything derived from it.
  useEffect(() => {
    matchGeneration.current += 1;
    setMatch(null);
    setMatchError(null);
    setMatching(false);
    setQuery("");
    setPickedKey(null);
    onResolved(null);
  }, [modelId, onResolved]);

  const runMatch = useCallback(
    async (modelConfigId: number) => {
      const generation = ++matchGeneration.current;
      setMatching(true);
      setMatchError(null);
      try {
        const result = await api.models.catalog.matchPreview(modelConfigId);
        if (generation !== matchGeneration.current) return;
        setMatch(result);
        // Only a unique exact match may advance automatically. Anything else
        // leaves the operator in control of the choice.
        if (
          result.reason === "unique_match" &&
          result.provider_id &&
          result.catalog_model_id
        ) {
          onResolved({
            kind: "coordinates",
            providerId: result.provider_id,
            catalogModelId: result.catalog_model_id,
            modelConfigId,
          });
        } else {
          onResolved(null);
        }
      } catch (cause) {
        if (generation !== matchGeneration.current) return;
        setMatchError(cause instanceof Error ? cause.message : String(cause));
        onResolved(null);
      } finally {
        if (generation === matchGeneration.current) setMatching(false);
      }
    },
    [onResolved],
  );

  useEffect(() => {
    if (!selectedModel) return;
    void runMatch(selectedModel.id);
  }, [runMatch, selectedModel]);

  const needsManualChoice =
    match !== null && !matching && match.reason !== "unique_match";
  const candidates = useCatalogCandidates(
    needsManualChoice ? (selectedModel?.id ?? null) : null,
    query.trim(),
  );

  useEffect(() => {
    if (!candidates.revisionRolledOver) return;
    setPickedKey(null);
    onResolved(null);
  }, [candidates.revisionRolledOver, onResolved]);

  const pickCandidate = (candidate: CatalogCandidate) => {
    const key = `${candidate.provider_id}/${candidate.model_id}`;
    setPickedKey(key);
    if (!selectedModel) return;
    onResolved({
      kind: "coordinates",
      providerId: candidate.provider_id,
      catalogModelId: candidate.model_id,
      modelConfigId: selectedModel.id,
    });
  };

  return (
    <OperatorInsetPanel
      title={copy.catalogDiscoveryTitle}
      description={copy.catalogDiscoveryDescription}
    >
      <div className="flex flex-col gap-2">
        <Label htmlFor="catalog-import-model">
          {copy.catalogSelectModelLabel}
        </Label>
        <Select value={modelId} onValueChange={setModelId}>
          <SelectTrigger id="catalog-import-model" className="w-full">
            <SelectValue placeholder={copy.catalogSelectModelPlaceholder} />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {models.map((model) => (
                <SelectItem key={model.id} value={String(model.id)}>
                  {modelLabel(model)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
      </div>

      {matching ? <OperatorLoadingState title={copy.loadingText} /> : null}

      {matchError ? (
        <OperatorCallout
          intent="danger"
          title={copy.catalogDiscoveryFailed}
          description={matchError}
          action={
            selectedModel ? (
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => void runMatch(selectedModel.id)}
              >
                {copy.catalogDiscoveryRetry}
              </Button>
            ) : null
          }
        />
      ) : null}

      {match && !matching && !matchError ? (
        match.reason === "unique_match" ? (
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <OperatorStatusBadge
              intent="healthy"
              preserveLabel
              label={copy.uniqueMatchFound}
            />
            <span className="font-mono text-xs text-muted-foreground">
              {match.provider_id}/{match.catalog_model_id}
            </span>
          </div>
        ) : (
          <OperatorCallout
            intent="warning"
            title={
              match.reason === "ambiguous" ? copy.ambiguousMatch : copy.noMatch
            }
            description={copy.explicitBindHint}
          />
        )
      ) : null}

      {needsManualChoice ? (
        <div className="flex flex-col gap-2">
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="catalog-import-candidates">
                {copy.candidateSearchLabel}
              </FieldLabel>
              <Input
                id="catalog-import-candidates"
                value={query}
                onChange={(event) => {
                  setQuery(event.target.value);
                  setPickedKey(null);
                  onResolved(null);
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
                <span className="truncate font-mono text-xs">
                  {candidate.provider_id}/{candidate.model_id}
                </span>
                <span className="truncate text-sm text-muted-foreground">
                  {candidate.name}
                </span>
              </span>
            )}
            selectedKey={pickedKey}
            onSelect={(key) => {
              setPickedKey(key);
              const picked = candidates.items.find(
                (candidate) =>
                  `${candidate.provider_id}/${candidate.model_id}` === key,
              );
              if (picked) pickCandidate(picked);
              else onResolved(null);
            }}
            testIdPrefix="catalog-candidate"
            labels={{
              loading: copy.catalogCandidatesLoading,
              loadFailed: copy.catalogCandidatesFailed,
              retry: copy.catalogCandidatesRetry,
              empty: copy.catalogNoCandidates,
              emptyDescription: copy.candidateEmptyDescription,
              loadMore: copy.loadMoreCandidates,
              loadingMore: copy.loadingMoreCandidates,
              retryLoadMore: copy.candidateRetry,
              count: copy.candidateCount,
              liveLoading: copy.loadingMoreCandidates,
              revisionRollover: copy.revisionRolloverNotice,
              revisionRolloverAcknowledge:
                copy.revisionRolloverAcknowledge,
              listboxLabel: copy.candidateSearchLabel,
            }}
          />
        </div>
      ) : null}
    </OperatorInsetPanel>
  );
}

export default CatalogOfferingDiscovery;
