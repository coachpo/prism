import { ExportPresetsPanel } from "./ExportPresetsPanel";
import { catalogFailureMessage } from "../catalog/catalogFailureMessage";
import { useState, type ReactNode } from "react";
import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import { useTimezone } from "@/hooks/useTimezone";
import {
  OperatorCallout,
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorLoadingState,
  OperatorPageHeader,
  OperatorPageShell,
  OperatorRetryButton,
  OperatorStalenessBadge,
} from "@/shared/design-system";
import { OpenCodeRepairPanel } from "./OpenCodeRepairPanel";
import { ExportKeyDialog } from "./ExportKeyDialog";
import { ExportResultSheet } from "./ExportResultSheet";
import { ModelExportDestinationPanel } from "./ModelExportDestinationPanel";
import { ModelExportSelectionPanel } from "./ModelExportSelectionPanel";
import { OpenCodeExportModelTable } from "./OpenCodeExportModelTable";
import { useOpenCodeExportSource } from "./useOpenCodeExportSource";
import { useOpenCodeExportRender } from "./useOpenCodeExportRender";

/** Composition of the OpenCode-only source and ephemeral result owners. */
export function OpenCodeExportPage({
  targetControl,
}: {
  targetControl: ReactNode;
}) {
  const { messages } = useLocale();
  const { format } = useTimezone();
  const copy = messages.modelExportPage;
  const oc = messages.opencodeExport;
  const source = useOpenCodeExportSource();
  const [repairId, setRepairId] = useState<number | null>(null);
  const query = source.sourceQuery;
  const repairModel = query.data?.models.find((model) => model.model_config_id === repairId);
  const render = useOpenCodeExportRender({
    source: query.data,
    selectedIds: source.selectedIds,
    sourceActionsBlocked: source.sourceActionsBlocked || repairId !== null,
    refetchSource: query.refetch,
    renderFailedMessage: copy.renderFailed,
  });
  const blocked = query.isError
    ? oc.sourceActionsBlocked
    : !source.selectedIds.size
      ? copy.blockedNoSelection
      : render.gatewayOriginInvalid || render.providerIdInvalid
        ? copy.blockedDestinationInvalid
        : null;
  const renderError = render.renderStale ? copy.sourceDrifted : render.renderError;

  return (
    <OperatorPageShell>
      <OperatorPageHeader title={copy.title} description={oc.description}>
        <Button
          onClick={render.openKeyDialog}
          disabled={render.openKeyDialogDisabled}
          aria-describedby={blocked ? "opencode-export-blocked" : undefined}
        >
          {copy.generateButton}
          {source.selectedIds.size ? ` (${source.selectedIds.size})` : ""}
        </Button>
      </OperatorPageHeader>
      <div className="flex flex-col gap-4">
        {targetControl}
        {blocked && !query.isLoading ? (
          <OperatorCallout
            id="opencode-export-blocked"
            intent="warning"
            title={copy.blockedTitle}
            description={blocked}
          />
        ) : null}
        <ExportPresetsPanel client="opencode" models={source.sourceQuery.data?.models ?? []} selectedIds={source.selectedIds} gatewayOrigin={render.gatewayOrigin} providerId={render.providerId} blocked={source.sourceActionsBlocked} onApply={(ids, destination) => { source.replaceSelection(ids); render.setGatewayOrigin(destination.gatewayOrigin); render.setProviderId(destination.providerId); }} />
        <ModelExportDestinationPanel
          target="opencode"
          gatewayOrigin={render.gatewayOrigin}
          gatewayOriginInvalid={render.gatewayOriginInvalid}
          onGatewayOriginChange={render.setGatewayOrigin}
          providerId={render.providerId}
          providerIdInvalid={render.providerIdInvalid}
          onProviderIdChange={render.setProviderId}
        />
        <ModelExportSelectionPanel sourceState={source} />
        {query.isLoading ? (
          <OperatorLoadingState title={copy.loadingSource} />
        ) : null}
        {query.isError && !query.data ? (
          <OperatorErrorState
            title={copy.loadFailed}
            description={catalogFailureMessage(query.error)}
            action={
              <OperatorRetryButton onClick={() => void query.refetch()}>
                {copy.retry}
              </OperatorRetryButton>
            }
          />
        ) : null}
        {query.isError && query.data ? (
          <OperatorStalenessBadge
            label={messages.honesty.lastSuccessful(
              format(new Date(query.dataUpdatedAt).toISOString()),
            )}
            reason={catalogFailureMessage(query.error)}
          />
        ) : null}
        {query.data ? (
          <>
            {repairId !== null && !repairModel ? <OperatorCallout intent="warning" description={messages.clientReadiness.modelRemoved} action={<Button variant="outline" onClick={() => setRepairId(null)}>{messages.clientReadiness.back}</Button>} /> : null}
            {repairModel ? <OpenCodeRepairPanel key={repairId} model={repairModel} pending={query.isFetching} failed={query.isError} onRefresh={() => void query.refetch()} onClose={() => { document.getElementById(`opencode-model-${repairId}`)?.focus(); setRepairId(null); void query.refetch(); }} /> : null}
            <OpenCodeExportModelTable source={source} onRepair={(id) => { render.clearResult(); setRepairId(id); }} />
            <OperatorCallout intent="warning" description={oc.costZeroDisclaimer} />
            <OperatorInsetPanel title={copy.sourceEvidenceTitle}>
              <dl className="grid gap-2 text-xs sm:grid-cols-[auto_1fr]">
                <dt>{copy.targetVersionLabel}</dt>
                <dd className="font-mono">{query.data.target_version}</dd>
                <dt>{copy.sourceReadAtLabel}</dt>
                <dd className="font-mono">
                  {format(new Date(query.dataUpdatedAt).toISOString())}
                </dd>
              </dl>
            </OperatorInsetPanel>
          </>
        ) : null}
        {renderError && !render.keyDialogOpen ? (
          <OperatorCallout intent="danger" description={renderError} />
        ) : null}
        <ExportKeyDialog
          target="opencode"
          open={render.keyDialogOpen}
          selectedCount={source.selectedIds.size}
          riskSummary={source.selectedRiskSummary}
          error={query.isError ? oc.sourceActionsBlocked : renderError}
          confirmDisabled={render.openKeyDialogDisabled}
          onClose={render.closeKeyDialog}
          onConfirm={render.handleGenerate}
        />
        <ExportResultSheet
          target="opencode"
          result={render.renderResult}
          onClose={render.clearResult}
        />
      </div>
    </OperatorPageShell>
  );
}
