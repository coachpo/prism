import { useLocale } from "@/i18n/useLocale";
import {
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorRetryButton,
  OperatorStalenessBadge,
} from "@/shared/design-system";
import { useTimezone } from "@/hooks/useTimezone";
import type { ExportSourceResponse } from "@/lib/types";
import { ModelExportModelTable } from "./ModelExportModelTable";
import type { PiBindingController } from "@/features/models/catalog/pi/usePiBindingController";
import type { ModelExportSourceState } from "./useModelExportSource";

export function ModelExportSourcePanel({
  controller,
  sourceState,
}: {
  controller: PiBindingController;
  sourceState: ModelExportSourceState;
}) {
  const { messages } = useLocale();
  const { format: formatTime } = useTimezone();
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
      {sourceQuery.isLoading && (
        <OperatorLoadingState title={copy.loadingSource} />
      )}
      {sourceQuery.isError && !source && (
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
      {sourceQuery.isError && source ? (
        <OperatorStalenessBadge
          label={messages.honesty.lastSuccessful(
            formatTime(new Date(sourceQuery.dataUpdatedAt).toISOString()),
          )}
          reason={String(sourceQuery.error)}
        />
      ) : null}
      {source && (
        <ModelExportModelTable
          controller={controller}
          visibleModels={visibleModels}
          selectedIds={selectedIds}
          onToggle={toggleModel}
          sourceState={sourceState}
          summary={copy.modelSummary
            .replace("{visible}", String(visibleModels.length))
            .replace("{selected}", String(selectedIds.size))}
        />
      )}
      {source && (
        <OperatorInsetPanel title={copy.riskSummaryTitle}>
          <dl className="grid gap-3 text-xs sm:grid-cols-3">
            <div>
              <dt className="text-muted-foreground">
                {copy.riskMetadataMissing}
              </dt>
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
              <dt className="text-muted-foreground">{copy.riskUnbound}</dt>
              <dd
                className="font-mono text-base tabular-nums"
                data-testid="export-risk-unbound-count"
              >
                {selectedRiskSummary.unbound}
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
            <dt className="text-muted-foreground">{copy.catalogStatusLabel}</dt>
            <dd className="font-mono">
              {catalogStatusLabel(copy, source.catalog.status)}{" "}
              {source.catalog.revision
                ? `(${shortRevision(source.catalog.revision)})`
                : ""}
            </dd>
            <dt className="text-muted-foreground">{copy.digestLabel}</dt>
            <dd className="min-w-0 break-all font-mono">
              {source.source_digest}
            </dd>
          </dl>
        </OperatorInsetPanel>
      )}
    </>
  );
}

/**
 * 修订号是给操作者做重放核对的标识符，必须真的能标识。`sha256-…` 的前 7 位
 * 是算法名，不携带信息：截断只从摘要本体取 12 位十六进制。
 */
function shortRevision(revision: string): string {
  const separator = revision.indexOf("-");
  if (separator < 0) return revision.slice(0, 12);
  return revision.slice(0, separator + 13);
}

function catalogStatusLabel(
  copy: Record<string, string>,
  status: "fresh" | "stale" | "unavailable",
): string {
  switch (status) {
    case "fresh":
      return copy.catalogStatusFresh;
    case "stale":
      return copy.catalogStatusStale;
    case "unavailable":
      return copy.catalogStatusUnavailable;
    default:
      return status;
  }
}
