import { Button } from "@/components/ui/button";
import {
  OperatorCallout,
  OperatorPageHeader,
  OperatorPageShell,
} from "@/shared/design-system";
import { useLocale } from "@/i18n/useLocale";
import { ExportResultSheet } from "./ExportResultSheet";
import { ModelExportDestinationPanel } from "./ModelExportDestinationPanel";
import { ModelExportSelectionPanel } from "./ModelExportSelectionPanel";
import { ModelExportSourcePanel } from "./ModelExportSourcePanel";
import { ModelExportUploadPanel } from "./ModelExportUploadPanel";
import { PlatformKeyDialog } from "./PlatformKeyDialog";
import type { ExportPlatform } from "./exportTypes";
import { useModelExportRender } from "./useModelExportRender";
import { useModelExportSource } from "./useModelExportSource";
import { useModelExportUploadReview } from "./useModelExportUploadReview";

/**
 * Page composition for the client export route. Source selection, uploaded
 * config review, and render/result lifecycles keep their own state; this page
 * only connects those owners to the existing presentation surfaces.
 */
export function ModelExportPage() {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const source = useModelExportSource();
  const upload = useModelExportUploadReview({
    models: source.models,
    noExtractedMatchMessage: copy.noExtractedMatch,
    platform: source.platform,
    selectedIds: source.selectedIds,
  });
  const render = useModelExportRender({
    defaultModelConfigId: source.defaultModelConfigId,
    enhancements: upload.enhancements,
    platform: source.platform,
    refetchSource: source.sourceQuery.refetch,
    renderFailedMessage: copy.renderFailed,
    selectedCount: source.selectedCount,
    selectedIds: source.selectedIds,
    source: source.sourceQuery.data,
  });

  const handlePlatformSwitch = (nextPlatform: ExportPlatform) => {
    if (nextPlatform === source.platform) return;
    source.handlePlatformSwitch(nextPlatform);
    upload.resetForPlatform();
    render.resetForPlatform();
  };

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
          defaultModelConfigId={source.defaultModelConfigId}
          gatewayOrigin={render.gatewayOrigin}
          gatewayOriginInvalid={render.gatewayOriginInvalid}
          onDefaultModelChange={source.setDefaultModelConfigId}
          onGatewayOriginChange={render.setGatewayOrigin}
          onPlatformChange={handlePlatformSwitch}
          onProviderIdChange={render.setProviderId}
          platform={source.platform}
          providerId={render.providerId}
          providerIdInvalid={render.providerIdInvalid}
          selectedModels={source.selectedModels}
        />

        <ModelExportSelectionPanel sourceState={source} />
        <ModelExportUploadPanel platform={source.platform} review={upload} />
        <ModelExportSourcePanel
          sourceState={source}
          enhancements={upload.enhancements}
        />

        {render.renderStale && (
          <OperatorCallout intent="danger" description={copy.sourceDrifted} />
        )}
        {render.renderError && !render.renderStale && (
          <OperatorCallout intent="danger" description={render.renderError} />
        )}

        <PlatformKeyDialog
          open={render.keyDialogOpen}
          selectedCount={source.selectedCount}
          onClose={() => render.setKeyDialogOpen(false)}
          onConfirm={render.handleGenerate}
        />

        <ExportResultSheet
          result={render.renderResult}
          onClose={render.clearResult}
          platform={source.platform}
        />
      </div>
    </OperatorPageShell>
  );
}

export default ModelExportPage;
