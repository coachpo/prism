// Pi-only page composition. No platform switching, no upload enhancement.
import { Button } from "@/components/ui/button";
import {
  OperatorCallout,
  OperatorPageHeader,
  OperatorPageShell,
} from "@/shared/design-system";
import { useLocale } from "@/i18n/useLocale";
import { ExportKeyDialog } from "./ExportKeyDialog";
import { ExportResultSheet } from "./ExportResultSheet";
import { ModelExportDestinationPanel } from "./ModelExportDestinationPanel";
import { ModelExportSelectionPanel } from "./ModelExportSelectionPanel";
import { ModelExportSourcePanel } from "./ModelExportSourcePanel";
import { useModelExportRender } from "./useModelExportRender";
import { useModelExportSource } from "./useModelExportSource";

export function ModelExportPage() {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const source = useModelExportSource();
  const render = useModelExportRender({
    refetchSource: source.sourceQuery.refetch,
    renderFailedMessage: copy.renderFailed,
    selectedIds: source.selectedIds,
    source: source.sourceQuery.data,
  });

  return (
    <OperatorPageShell>
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Button
          onClick={() => render.setKeyDialogOpen(true)}
          disabled={render.openKeyDialogDisabled}
        >
          {copy.generateButton}
          {source.selectedCount > 0 ? ` (${source.selectedCount})` : ""}
        </Button>
      </OperatorPageHeader>

      <div className="flex flex-col gap-4">
        <ModelExportDestinationPanel
          gatewayOrigin={render.gatewayOrigin}
          gatewayOriginInvalid={render.gatewayOriginInvalid}
          onGatewayOriginChange={render.setGatewayOrigin}
          onProviderIdChange={render.setProviderId}
          providerId={render.providerId}
          providerIdInvalid={render.providerIdInvalid}
        />

        <ModelExportSelectionPanel sourceState={source} />
        <ModelExportSourcePanel sourceState={source} />

        {render.renderStale && (
          <OperatorCallout intent="danger" description={copy.sourceDrifted} />
        )}
        {render.renderError && !render.renderStale && (
          <OperatorCallout intent="danger" description={render.renderError} />
        )}

        <ExportKeyDialog
          open={render.keyDialogOpen}
          selectedCount={source.selectedCount}
          onClose={() => render.setKeyDialogOpen(false)}
          onConfirm={render.handleGenerate}
        />

        <ExportResultSheet
          result={render.renderResult}
          onClose={render.clearResult}
        />
      </div>
    </OperatorPageShell>
  );
}

export default ModelExportPage;
