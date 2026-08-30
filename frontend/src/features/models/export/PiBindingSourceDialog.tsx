import { useMemo, useState } from "react";

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
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Spinner } from "@/components/ui/spinner";
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorMissingValue,
} from "@/shared/design-system";
import type {
  ExportSourceModelRow,
  PiCandidateWire,
  PiCatalogSearchResponse,
} from "./exportTypes";
import { piBindingCoordinateKey } from "./piBindingCoordinate";
import { PiCandidateEvidence } from "./PiCandidateEvidence";
import {
  isModelExportSourceReconciliationError,
  type ModelExportSourceState,
} from "./useModelExportSource";

type Copy = Record<string, string>;
type DiscoveryLayer = "exact" | "search";

interface PendingChoice {
  key: string;
  layer: DiscoveryLayer;
}

interface SearchEvidence {
  response: PiCatalogSearchResponse;
  sourceRevisionAtRequest: string;
}

function apiErrorDetail(error: unknown, copy: Copy): string {
  if (isModelExportSourceReconciliationError(error)) {
    return copy.sourceReconciliationFailed;
  }
  return error instanceof Error ? error.message : String(error);
}

function candidateOption(candidate: PiCandidateWire, copy: Copy) {
  const absent = copy.candidateFieldAbsent;
  const reasoning =
    candidate.reasoning === undefined
      ? absent
      : candidate.reasoning
        ? copy.overrideBooleanTrue
        : copy.overrideBooleanFalse;
  const input = candidate.input?.join(", ") || absent;
  const thinking = candidate.thinking_level_map
    ? String(Object.keys(candidate.thinking_level_map).length)
    : absent;
  const compat = candidate.compat
    ? String(Object.keys(candidate.compat).length)
    : absent;
  const dropped = String(candidate.dropped_fields?.length ?? 0);

  return (
    <div className="flex min-w-0 flex-col gap-0.5 py-0.5">
      <span className="break-all font-mono text-xs">
        {candidate.provider_id}/{candidate.model_id} ({candidate.api})
      </span>
      <span className="truncate text-xs">{candidate.name ?? absent}</span>
      <span className="break-words text-xs text-muted-foreground">
        {copy.overrideReasoningLabel}: {reasoning} · {copy.overrideInputLabel}: {input}{" "}
        · {copy.overrideContextWindowLabel}: {candidate.context_window ?? absent}{" "}
        · {copy.overrideMaxTokensLabel}: {candidate.max_tokens ?? absent}
      </span>
      <span className="break-words text-xs text-muted-foreground">
        {copy.overrideThinkingLevelMapLabel}: {thinking} · {copy.overrideCompatLabel}: {compat}{" "}
        · {copy.droppedFieldsLabel}: {dropped}
      </span>
    </div>
  );
}

/**
 * PiBindingSourceDialog is the single binding-authoring entry for every model
 * whose final Pi API is determinable, bound or unbound.
 *
 * It keeps the two discovery layers visibly separate so an operator can never
 * mistake them: the default exact candidates (complete Prism model_id,
 * case-sensitive, same final Pi API) and an explicit bounded directory search
 * that may legitimately name a different model id. Nothing is preselected in
 * either layer, a search that returns one hit is still not a choice, and the
 * confirm action is inert until the operator names a coordinate or accepts the
 * sole exact candidate.
 */
export function PiBindingSourceDialog({
  copy,
  model,
  onClose,
  sourceState,
}: {
  copy: Copy;
  model: ExportSourceModelRow;
  onClose: () => void;
  sourceState: ModelExportSourceState;
}) {
  const [pendingChoice, setPendingChoice] = useState<PendingChoice | null>(null);
  const [query, setQuery] = useState("");
  const [searchEvidence, setSearchEvidence] = useState<SearchEvidence | null>(
    null,
  );
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const search = sourceState.catalogSearchMutation;
  const catalogRevision = sourceState.sourceQuery.data?.catalog.revision ?? "";
  const sourceCatalogFresh =
    sourceState.sourceQuery.data?.catalog.status === "fresh" &&
    catalogRevision !== "";

  const exactCandidates = model.pi_candidates;
  const searchResults = useMemo(
    () => searchEvidence?.response.results ?? [],
    [searchEvidence],
  );
  const chosen = useMemo(() => {
    if (!pendingChoice) return null;
    const candidates =
      pendingChoice.layer === "search" ? searchResults : exactCandidates;
    return (
      candidates.find(
        (candidate) =>
          piBindingCoordinateKey(candidate) === pendingChoice.key,
      ) ?? null
    );
  }, [exactCandidates, pendingChoice, searchResults]);
  const soleExactCandidate =
    exactCandidates.length === 1 ? exactCandidates[0] : null;
  // The preserved single-candidate shortcut: with nothing chosen and exactly
  // one default exact candidate, confirm applies that candidate implicitly and
  // the backend records bind_source=single_candidate.
  const shortcutSole = !pendingChoice && soleExactCandidate !== null;
  const effective = chosen ?? (shortcutSole ? soleExactCandidate : null);
  const effectiveLayer: DiscoveryLayer | null = chosen
    ? pendingChoice?.layer ?? null
    : shortcutSole
      ? "exact"
      : null;

  const searchResponse = searchEvidence?.response;
  const evidenceCatalog =
    effectiveLayer === "search"
      ? searchResponse?.catalog
      : sourceState.sourceQuery.data?.catalog;
  const evidenceIdentity =
    effectiveLayer === "search"
      ? searchResponse?.export_identity
      : {
          model_config_id: model.model_config_id,
          model_id: model.model_id,
          api: model.pi_api ?? "",
          provider_id_source: "operator_input",
        };
  const evidenceFresh =
    evidenceCatalog?.status === "fresh" &&
    Boolean(evidenceCatalog.revision);
  const searchEvidenceCurrent =
    effectiveLayer !== "search" ||
    (Boolean(searchEvidence) &&
      (catalogRevision === searchEvidence?.sourceRevisionAtRequest ||
        catalogRevision === searchResponse?.catalog.revision) &&
      searchResponse?.export_identity.model_config_id === model.model_config_id &&
      searchResponse?.export_identity.model_id === model.model_id &&
      searchResponse?.export_identity.api === model.pi_api);
  const candidateMatchesIdentity =
    Boolean(effective) && effective?.api === evidenceIdentity?.api;

  const selected = model.pi_selected;
  const coordinateChanges = Boolean(
    effective &&
      selected &&
      (effective.provider_id !== selected.provider_id ||
        effective.model_id !== selected.model_id ||
        effective.api !== selected.api),
  );
  const isCrossDirectory = Boolean(
    effective && effective.model_id !== model.model_id,
  );
  const clearsOverrides =
    coordinateChanges && Boolean(model.pi_binding_override);

  const canConfirm =
    evidenceFresh &&
    searchEvidenceCurrent &&
    candidateMatchesIdentity &&
    Boolean(effective) &&
    Boolean(model.pi_api) &&
    !sourceState.sourceActionsBlocked &&
    !sourceState.bindMutation.isPending &&
    !searching;

  // Built as one value so the rendered search panel never nests ternaries and
  // TS narrows the response without a helper boolean.
  let searchResultsPanel: React.ReactNode = null;
  if (searchResponse && searchResults.length === 0) {
    searchResultsPanel = (
      <p className="text-xs text-muted-foreground">
        {copy.directorySearchEmpty}
      </p>
    );
  } else if (searchResponse) {
    searchResultsPanel = (
      <>
        <Select
          disabled={sourceState.sourceActionsBlocked}
          value={pendingChoice?.layer === "search" ? pendingChoice.key : ""}
          onValueChange={(value) =>
            setPendingChoice(
              value ? { key: value, layer: "search" } : null,
            )
          }
        >
          <SelectTrigger
            size="sm"
            aria-label={copy.directorySearchResultsLabel}
          >
            <SelectValue placeholder={copy.directorySearchResultsPlaceholder} />
          </SelectTrigger>
          <SelectContent>
            <SelectGroup>
              {searchResults.map((candidate) => (
                <SelectItem
                  key={piBindingCoordinateKey(candidate)}
                  value={piBindingCoordinateKey(candidate)}
                  textValue={`${candidate.provider_id}/${candidate.model_id} (${candidate.api}) ${candidate.name ?? ""}`}
                >
                  {candidateOption(candidate, copy)}
                </SelectItem>
              ))}
            </SelectGroup>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          {copy.directorySearchCountLabel}{" "}
          <span className="font-mono">
            {searchResponse.returned} / {searchResponse.total}
          </span>
        </p>
        {searchResponse.truncated ? (
          <p className="text-xs text-muted-foreground">
            {copy.directorySearchTruncated}
          </p>
        ) : null}
      </>
    );
  }

  async function handleSearch() {
    setSearchError(null);
    const trimmed = query.trim();
    if (trimmed === "") {
      setSearchError(copy.directorySearchQueryRequired);
      return;
    }
    if (pendingChoice?.layer === "search") setPendingChoice(null);
    setSearchEvidence(null);
    setSearching(true);
    const sourceRevisionAtRequest = catalogRevision;
    try {
      const response = await search.mutateAsync({
        modelConfigId: model.model_config_id,
        query: trimmed,
      });
      setSearchEvidence({ response, sourceRevisionAtRequest });
    } catch (cause) {
      setSearchError(apiErrorDetail(cause, copy));
    } finally {
      setSearching(false);
    }
  }

  async function handleConfirm() {
    if (!effective || !evidenceIdentity?.api || !evidenceIdentity.model_id)
      return;
    setActionError(null);
    try {
      await sourceState.bindMutation.mutateAsync({
        modelConfigId: model.model_config_id,
        providerId: shortcutSole ? undefined : effective.provider_id,
        catalogModelId: shortcutSole ? undefined : effective.model_id,
        expectedCatalogRevision: evidenceCatalog?.revision ?? "",
        expectedPrismModelId: evidenceIdentity.model_id,
        expectedPiApi: evidenceIdentity.api,
      });
      onClose();
    } catch (cause) {
      setActionError(apiErrorDetail(cause, copy));
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !sourceState.bindMutation.isPending) onClose();
      }}
    >
      <DialogContent className="sm:max-w-[560px]">
        <DialogHeader>
          <DialogTitle>{copy.sourceDialogTitle}</DialogTitle>
          <DialogDescription>{copy.sourceDialogDescription}</DialogDescription>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-4">
          <OperatorInsetPanel title={copy.exportIdentityTitle}>
            <dl className="grid gap-x-3 gap-y-1 text-xs sm:grid-cols-[max-content_minmax(0,1fr)]">
              <dt className="text-muted-foreground">
                {copy.exportIdentityModelLabel}
              </dt>
              <dd className="break-all font-mono">{model.model_id}</dd>
              <dt className="text-muted-foreground">
                {copy.exportIdentityApiLabel}
              </dt>
              <dd className="break-all font-mono">
                {model.pi_api || copy.exportIdentityApiUnknown}
              </dd>
              <dt className="text-muted-foreground">
                {copy.exportIdentityProviderLabel}
              </dt>
              <dd>{copy.exportIdentityProviderHint}</dd>
            </dl>
            <p className="text-xs text-muted-foreground">
              {copy.exportIdentityNote}
            </p>
          </OperatorInsetPanel>

          {selected ? (
            <p className="text-xs text-muted-foreground">
              {copy.rebindCurrentLabel}:{" "}
              <span className="font-mono">
                {selected.provider_id}/{selected.model_id} ({selected.api})
              </span>
              {model.pi_binding_prism_model_id ? (
                <>
                  {" · "}
                  {copy.currentBindingIdentityLabel}:{" "}
                  <span className="font-mono">
                    {model.pi_binding_prism_model_id}
                  </span>
                </>
              ) : null}
            </p>
          ) : null}

          {!sourceCatalogFresh && effectiveLayer !== "search" ? (
            <OperatorCallout
              intent="warning"
              description={copy.catalogNotFreshBlocked}
            />
          ) : null}
          {effectiveLayer === "search" && !evidenceFresh ? (
            <OperatorCallout
              intent="warning"
              description={copy.catalogNotFreshBlocked}
            />
          ) : null}
          {effectiveLayer === "search" && !searchEvidenceCurrent ? (
            <OperatorCallout
              intent="danger"
              description={copy.directorySearchEvidenceChanged}
            />
          ) : null}

          <section className="flex flex-col gap-2">
            <h3 className="text-xs font-medium">{copy.exactCandidatesTitle}</h3>
            {exactCandidates.length === 0 ? (
              <p className="text-xs text-muted-foreground">
                {copy.exactCandidatesEmpty}
              </p>
            ) : (
              <Select
                disabled={sourceState.sourceActionsBlocked}
                value={
                  pendingChoice?.layer === "exact" ? pendingChoice.key : ""
                }
                onValueChange={(value) =>
                  setPendingChoice(
                    value ? { key: value, layer: "exact" } : null,
                  )
                }
              >
                <SelectTrigger size="sm" aria-label={copy.candidateSelectLabel}>
                  <SelectValue placeholder={copy.candidateSelectPlaceholder} />
                </SelectTrigger>
                <SelectContent>
                  <SelectGroup>
                    {exactCandidates.map((candidate) => (
                      <SelectItem
                        key={piBindingCoordinateKey(candidate)}
                        value={piBindingCoordinateKey(candidate)}
                      >
                        {candidate.provider_id}/{candidate.model_id} (
                        {candidate.api})
                      </SelectItem>
                    ))}
                  </SelectGroup>
                </SelectContent>
              </Select>
            )}
            {exactCandidates.length > 1 ? (
              <p className="text-xs text-destructive">
                {copy.candidateAmbiguousHint}
              </p>
            ) : null}
          </section>

          {model.pi_api ? (
            <section className="flex flex-col gap-2">
              <h3 className="text-xs font-medium">
                {copy.directorySearchTitle}
              </h3>
              <Field data-invalid={searchError !== null}>
                <FieldLabel htmlFor="pi-directory-search-query">
                  {copy.directorySearchLabel}
                </FieldLabel>
                <Input
                  id="pi-directory-search-query"
                  value={query}
                  spellCheck={false}
                  placeholder={copy.directorySearchPlaceholder}
                  aria-invalid={searchError !== null}
                  onChange={(event) => setQuery(event.target.value)}
                />
                {searchError ? (
                  <FieldError>{searchError}</FieldError>
                ) : (
                  <FieldDescription>{copy.directorySearchHint}</FieldDescription>
                )}
              </Field>
              <div>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={
                    searching || sourceState.sourceActionsBlocked
                  }
                  onClick={() => void handleSearch()}
                >
                  {searching ? (
                    <Spinner data-icon="inline-start" />
                  ) : null}
                  {copy.directorySearchAction}
                </Button>
              </div>
              {searchResponse && searchResponse.catalog.status !== "fresh" ? (
                <OperatorCallout
                  intent="warning"
                  description={copy.directorySearchStaleReadOnly}
                />
              ) : null}
              {searchResponse && !searching ? searchResultsPanel : null}
            </section>
          ) : null}

          {effective ? (
            <>
              <OperatorInsetPanel title={copy.chosenCoordinateTitle}>
                <dl className="grid gap-x-3 gap-y-1 text-xs sm:grid-cols-[max-content_minmax(0,1fr)]">
                  <dt className="text-muted-foreground">
                    {copy.catalogProviderLabel}
                  </dt>
                  <dd className="break-all font-mono">
                    {effective.provider_id}
                  </dd>
                  <dt className="text-muted-foreground">
                    {copy.directorySearchLabel}
                  </dt>
                  <dd className="break-all font-mono">{effective.model_id}</dd>
                  <dt className="text-muted-foreground">
                    {copy.catalogApiLabel}
                  </dt>
                  <dd className="break-all font-mono">{effective.api}</dd>
                </dl>
                <p className="text-xs text-muted-foreground">
                  {isCrossDirectory
                    ? copy.chosenCrossDirectory
                    : copy.chosenSameDirectory}
                </p>
              </OperatorInsetPanel>
              <PiCandidateEvidence candidate={effective} copy={copy} />
            </>
          ) : (
            <OperatorMissingValue reason={copy.noCoordinateChosen} />
          )}

          {clearsOverrides ? (
            <OperatorCallout
              intent="danger"
              description={copy.rebindClearsOverrides}
            />
          ) : null}
          {actionError ? (
            <OperatorCallout intent="danger" description={actionError} />
          ) : sourceState.sourceQuery.isError ? (
            <OperatorCallout
              intent="danger"
              description={copy.sourceActionsBlocked}
            />
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={onClose}
            disabled={sourceState.bindMutation.isPending}
          >
            {copy.cancel}
          </Button>
          <Button
            variant={clearsOverrides ? "destructive" : "default"}
            onClick={() => void handleConfirm()}
            disabled={!canConfirm}
          >
            {sourceState.bindMutation.isPending ? (
              <Spinner data-icon="inline-start" />
            ) : null}
            {clearsOverrides
              ? copy.rebindConfirmDestructiveAction
              : copy.sourceDialogConfirmAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
