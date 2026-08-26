import { useLocale } from "@/i18n/useLocale";
import {
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorRetryButton,
} from "@/shared/design-system";
import type { ExportSourceResponse } from "./exportTypes";
import { ModelExportModelTable } from "./ModelExportModelTable";
import type { ModelExportSourceState } from "./useModelExportSource";
import type { EnhancementDraft } from "./useModelExportUploadReview";

export function ModelExportSourcePanel({
  enhancements,
  sourceState,
}: {
  enhancements: Record<number, EnhancementDraft>;
  sourceState: ModelExportSourceState;
}) {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const {
    selectedIds,
    selectedRiskSummary,
    sourceQuery,
    toggleModel,
    visibleModels,
  } = sourceState;
  const source: ExportSourceResponse | undefined = sourceQuery.data;

  return (
    <>
      {sourceQuery.isLoading && <OperatorLoadingState title={copy.loadingSource} />}
      {sourceQuery.isError && (
        <OperatorErrorState
          title={copy.loadFailed}
          description={String(sourceQuery.error)}
          action={
            <OperatorRetryButton onClick={() => void sourceQuery.refetch()}>
              {copy.retry}
            </OperatorRetryButton>
          }
        />
      )}
      {source && (
        <ModelExportModelTable
          visibleModels={visibleModels}
          selectedIds={selectedIds}
          enhancements={enhancements}
          onToggle={toggleModel}
          summary={copy.modelSummary
            .replace("{visible}", String(visibleModels.length))
            .replace("{selected}", String(selectedIds.size))}
        />
      )}
      {source && (
        <OperatorInsetPanel title={copy.riskSummaryTitle}>
          <dl className="grid gap-3 text-xs sm:grid-cols-3">
            <div>
              <dt className="text-muted-foreground">{copy.riskMetadataMissing}</dt>
              <dd
                className="font-mono text-base tabular-nums"
                data-testid="export-risk-metadata-count"
              >
                {selectedRiskSummary.metadataIncomplete}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">{copy.riskCostOmitted}</dt>
              <dd
                className="font-mono text-base tabular-nums"
                data-testid="export-risk-cost-count"
              >
                {selectedRiskSummary.costOmitted}
              </dd>
            </div>
            <div>
              <dt className="text-muted-foreground">
                {copy.riskEnrichmentUnavailable}
              </dt>
              <dd
                className="font-mono text-base tabular-nums"
                data-testid="export-risk-enrichment-count"
              >
                {selectedRiskSummary.enrichmentUnavailable}
              </dd>
            </div>
          </dl>
          <p className="mt-3 text-xs text-muted-foreground">
            {copy.riskSummaryHint}
          </p>
        </OperatorInsetPanel>
      )}
      {source && (
        <OperatorInsetPanel title={copy.sourceEvidenceTitle}>
          <dl className="grid gap-2 text-xs sm:grid-cols-[auto_1fr]">
            <dt className="text-muted-foreground">{copy.targetVersionLabel}</dt>
            <dd className="font-mono">{source.target_version}</dd>
            <dt className="text-muted-foreground">{copy.digestLabel}</dt>
            <dd className="min-w-0 break-all font-mono">{source.source_digest}</dd>
          </dl>
        </OperatorInsetPanel>
      )}
    </>
  );
}
