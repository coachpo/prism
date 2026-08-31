import { useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Spinner } from "@/components/ui/spinner";
import { OperatorCallout, OperatorMissingValue } from "@/shared/design-system";
import { CatalogCandidatePicker } from "@/features/models/catalog/CatalogCandidatePicker";
import type { PiCandidateWire } from "@/lib/types";
import { piBindingCoordinateKey } from "./piBindingCoordinate";
import type { PiBindingController } from "./usePiBindingController";

type Copy = Record<string, string>;

/**
 * Paged pi.dev directory search panel. The query edit only clears field/read
 * errors; the explicit "search" action commits the condition to the shared
 * pager (`sourceKey: "pi.dev"`), which owns replace/append, late-response
 * isolation, single-flight, dedupe, retry, and revision rollover (a rollover
 * re-reads offset 0 and clears the operator's selection upstream).
 *
 * Results are permanent evidence: nothing is preselected, a single hit is
 * still only a candidate, and stale catalog evidence stays readable but never
 * confirmable.
 */
export function PiDirectorySearchPanel({
  copy,
  modelConfigId,
  modelId,
  piApi,
  controller,
  selectedKey,
  onSelect,
  disabled = false,
}: {
  copy: Copy;
  modelConfigId: number;
  modelId: string;
  piApi: string;
  controller: PiBindingController;
  selectedKey: string | null;
  onSelect: (key: string | null) => void;
  disabled?: boolean;
}) {
  const [queryDraft, setQueryDraft] = useState("");
  const search = controller.directorySearch;
  const pager = search.pager;
  const ownsModel = search.activeModelConfigId === modelConfigId;
  const fieldError =
    search.fieldError === "model_id_query"
      ? copy.directorySearchQueryRequired
      : search.fieldError;
  const hasQuery =
    ownsModel && (pager.revision !== null || search.activeQuery !== "");

  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-xs font-medium">{copy.directorySearchTitle}</h3>
      <Field data-invalid={Boolean(fieldError)}>
        <FieldLabel htmlFor="pi-directory-search-query">
          {copy.directorySearchLabel}
        </FieldLabel>
        <Input
          id="pi-directory-search-query"
          value={queryDraft}
          spellCheck={false}
          placeholder={copy.directorySearchPlaceholder}
          aria-invalid={Boolean(fieldError)}
          onChange={(event) => {
            setQueryDraft(event.target.value);
            search.clearErrors();
          }}
        />
        {fieldError ? (
          <FieldError>{fieldError}</FieldError>
        ) : (
          <FieldDescription>{copy.directorySearchHint}</FieldDescription>
        )}
      </Field>
      <div>
        <Button
          size="sm"
          variant="outline"
          disabled={disabled || search.pending || controller.actionsBlocked}
          onClick={() => {
            onSelect(null);
            controller.directorySearch.start(
              { modelConfigId, modelId, piApi },
              queryDraft,
            );
          }}
        >
          {search.pending ? <Spinner data-icon="inline-start" /> : null}
          {copy.directorySearchAction}
        </Button>
      </div>
      {ownsModel && search.status === "stale" ? (
        <OperatorCallout
          intent="warning"
          description={copy.directorySearchStaleReadOnly}
        />
      ) : null}
      {ownsModel && search.activeQuery !== "" ? (
        <CatalogCandidatePicker
          pager={pager}
          testIdPrefix="pi-directory"
          itemKey={piBindingCoordinateKey}
          renderCandidate={(candidate: PiCandidateWire) => (
            <span className="flex min-w-0 flex-col gap-0.5 py-0.5">
              <span className="break-all font-mono text-xs">
                {candidate.provider_id}/{candidate.model_id}
              </span>
              <span className="truncate text-xs">
                {copy.catalogApiLabel}: {candidate.api} ·{" "}
                {candidate.name ?? copy.candidateFieldAbsent}
              </span>
            </span>
          )}
          selectedKey={selectedKey}
          onSelect={onSelect}
          disabled={disabled}
          labels={{
            loading: copy.directorySearchLoading,
            loadFailed: copy.directorySearchFailed,
            retry: copy.directorySearchRetry,
            empty: copy.directorySearchEmpty,
            loadMore: copy.loadMoreCandidates,
            loadingMore: copy.loadingMoreCandidates,
            retryLoadMore: copy.retryLoadMore,
            count: (shown: number, total: number) =>
              `${copy.directorySearchCountLabel} ${shown} / ${total}`,
            liveLoading: copy.loadingMoreCandidates,
            revisionRollover: copy.directorySearchRollover,
            revisionRolloverAcknowledge:
              copy.directorySearchRolloverAcknowledge,
            listboxLabel: copy.directorySearchResultsLabel,
          }}
        />
      ) : null}
      {!hasQuery ? (
        <OperatorMissingValue reason={copy.directorySearchHint} />
      ) : null}
    </section>
  );
}
