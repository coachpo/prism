import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { formatApiFamily } from "@/components/apiFamilyPresentation";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { useLocale } from "@/i18n/useLocale";
import { models as modelsApi } from "@/lib/api/models";
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

import type { CatalogPricingSource } from "./useCatalogPricingImport";

/** Candidate page size. The backend caps a bounded search at 100 rows. */
const CANDIDATE_LIMIT = 20;

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
  const [candidates, setCandidates] = useState<CatalogCandidate[]>([]);
  const [candidatesTotal, setCandidatesTotal] = useState(0);
  const [candidatesError, setCandidatesError] = useState<string | null>(null);
  const [candidatesLoading, setCandidatesLoading] = useState(false);
  const [candidatesAttempt, setCandidatesAttempt] = useState(0);
  const [pickedKey, setPickedKey] = useState<string | null>(null);
  const matchGeneration = useRef(0);
  const candidateGeneration = useRef(0);

  const selectedModel = useMemo(
    () => models.find((model) => String(model.id) === modelId) ?? null,
    [modelId, models],
  );

  // Changing or clearing the model invalidates everything derived from it.
  useEffect(() => {
    matchGeneration.current += 1;
    candidateGeneration.current += 1;
    setMatch(null);
    setMatchError(null);
    setMatching(false);
    setQuery("");
    setCandidates([]);
    setCandidatesTotal(0);
    setCandidatesError(null);
    setCandidatesLoading(false);
    setPickedKey(null);
    onResolved(null);
  }, [modelId, onResolved]);

  const runMatch = useCallback(
    async (modelConfigId: number) => {
      const generation = ++matchGeneration.current;
      setMatching(true);
      setMatchError(null);
      try {
        const result = await modelsApi.catalog.matchPreview(modelConfigId);
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

  // A human-driven bounded search. A keyword widens the scope to every provider
  // so aggregator-style offerings stay reachable; an empty query stays inside
  // the api_family's mapped providers.
  useEffect(() => {
    const generation = ++candidateGeneration.current;
    if (!selectedModel) return;
    setCandidatesLoading(true);
    setCandidatesError(null);
    const handle = setTimeout(() => {
      void (async () => {
        try {
          const response = await modelsApi.catalog.candidates(
            selectedModel.id,
            {
              q: query.trim() || undefined,
              scope: query.trim() ? "all" : "family",
              limit: CANDIDATE_LIMIT,
            },
          );
          if (generation !== candidateGeneration.current) return;
          setCandidates(Array.isArray(response.items) ? response.items : []);
          setCandidatesTotal(
            Number.isFinite(response.total) ? response.total : 0,
          );
        } catch (cause) {
          if (generation !== candidateGeneration.current) return;
          setCandidates([]);
          setCandidatesTotal(0);
          setCandidatesError(
            cause instanceof Error ? cause.message : String(cause),
          );
        } finally {
          if (generation === candidateGeneration.current) {
            setCandidatesLoading(false);
          }
        }
      })();
    }, 250);
    return () => {
      clearTimeout(handle);
      if (generation === candidateGeneration.current) {
        candidateGeneration.current += 1;
      }
    };
  }, [candidatesAttempt, query, selectedModel]);

  const needsManualChoice =
    match !== null && !matching && match.reason !== "unique_match";

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
          <Label htmlFor="catalog-import-candidates">
            {copy.candidateSearchLabel}
          </Label>
          <Input
            id="catalog-import-candidates"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={copy.candidateSearchPlaceholder}
          />
          {candidatesLoading ? (
            <OperatorLoadingState title={copy.catalogCandidatesLoading} />
          ) : null}
          {candidatesError && !candidatesLoading ? (
            <OperatorCallout
              intent="danger"
              title={copy.catalogCandidatesFailed}
              description={candidatesError}
              action={
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => setCandidatesAttempt((attempt) => attempt + 1)}
                >
                  {copy.catalogCandidatesRetry}
                </Button>
              }
            />
          ) : null}
          {!candidatesLoading && !candidatesError ? (
            <>
              <ul className="flex max-h-48 min-w-0 flex-col gap-1 overflow-y-auto text-sm">
                {candidates.map((candidate) => {
                  const key = `${candidate.provider_id}/${candidate.model_id}`;
                  return (
                    <li key={key}>
                      <button
                        type="button"
                        className="min-h-7 w-full truncate rounded px-1 py-0.5 text-left hover:bg-muted data-[picked=true]:bg-primary-soft"
                        aria-pressed={pickedKey === key}
                        data-picked={pickedKey === key}
                        data-testid={`catalog-candidate-${candidate.provider_id}-${candidate.model_id}`}
                        onClick={() => pickCandidate(candidate)}
                      >
                        <span className="font-mono text-xs">
                          {candidate.provider_id}/{candidate.model_id}
                        </span>
                        <span className="ml-2 text-muted-foreground">
                          {candidate.name}
                        </span>
                      </button>
                    </li>
                  );
                })}
                {candidates.length === 0 ? (
                  <li className="px-1 text-sm text-muted-foreground">
                    {copy.catalogNoCandidates}
                  </li>
                ) : null}
              </ul>
              <p className="text-xs text-muted-foreground">
                {copy.candidateCount(candidates.length, candidatesTotal)}
              </p>
            </>
          ) : null}
        </div>
      ) : null}
    </OperatorInsetPanel>
  );
}

export default CatalogOfferingDiscovery;
