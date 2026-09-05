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
import { usePiBindingController } from "@/features/models/catalog/pi/usePiBindingController";
import { useModelExportRender } from "./useModelExportRender";
import { useModelExportSource } from "./useModelExportSource";

// 禁用的主按钮不可聚焦，说明必须自己站在页头下方，并被按钮 describedby 指向。
const BLOCKED_NOTICE_ID = "model-export-blocked-notice";

export function ModelExportPage() {
  const { messages } = useLocale();
  const copy = messages.modelExportPage;
  const source = useModelExportSource();
  // The shared Pi binding controller: mutations always reconcile through the
  // host's authoritative source re-read; dialogs stay inert while the source
  // read is unavailable.
  const piController = usePiBindingController({
    reconcile: source.reconcileSource,
    actionsBlocked: source.sourceActionsBlocked,
  });
  const render = useModelExportRender({
    refetchSource: source.sourceQuery.refetch,
    renderFailedMessage: copy.renderFailed,
    selectedIds: source.selectedIds,
    source: source.sourceQuery.data,
    sourceActionsBlocked: source.sourceActionsBlocked,
  });
  const dialogError = source.sourceQuery.isError
    ? copy.sourceActionsBlocked
    : render.renderStale
      ? copy.sourceDrifted
      : render.renderError;
  // 主操作被禁用时必须当场说清原因：一个灰掉的主按钮既不可点也不可聚焦，
  // 等同于不在场。源还没读到（首读中/读取失败）已有各自的状态面负责，
  // 刷新在途是瞬时的，这两种情况不在页头重复一遍。
  const blockedDescription =
    render.blockReason === "unbound_selection"
      ? copy.blockedUnboundSelection.replace(
          "{count}",
          String(render.unboundSelectedCount),
        )
      : render.blockReason === "no_selection"
        ? copy.blockedNoSelection
        : render.blockReason === "destination_invalid"
          ? copy.blockedDestinationInvalid
          : render.blockReason === "source_actions_blocked" &&
              source.sourceQuery.isError
            ? copy.sourceActionsBlocked
            : null;

  return (
    <OperatorPageShell>
      <OperatorPageHeader title={copy.title} description={copy.description}>
        <Button
          onClick={render.openKeyDialog}
          disabled={render.openKeyDialogDisabled}
          aria-describedby={blockedDescription ? BLOCKED_NOTICE_ID : undefined}
        >
          {copy.generateButton}
          {source.selectedCount > 0 ? ` (${source.selectedCount})` : ""}
        </Button>
      </OperatorPageHeader>

      <div className="flex flex-col gap-4">
        {blockedDescription ? (
          <OperatorCallout
            id={BLOCKED_NOTICE_ID}
            intent="warning"
            title={copy.blockedTitle}
            description={blockedDescription}
            action={
              render.blockReason === "unbound_selection" &&
              render.renderableSelectedIds.size > 0 ? (
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() =>
                    source.retainSelection(render.renderableSelectedIds)
                  }
                >
                  {copy.blockedKeepRenderable.replace(
                    "{count}",
                    String(render.renderableSelectedIds.size),
                  )}
                </Button>
              ) : null
            }
          />
        ) : null}

        <ModelExportDestinationPanel
          gatewayOrigin={render.gatewayOrigin}
          gatewayOriginInvalid={render.gatewayOriginInvalid}
          onGatewayOriginChange={render.setGatewayOrigin}
          onProviderIdChange={render.setProviderId}
          providerId={render.providerId}
          providerIdInvalid={render.providerIdInvalid}
        />

        <ModelExportSelectionPanel sourceState={source} />
        <ModelExportSourcePanel
          controller={piController}
          sourceState={source}
        />

        {render.renderStale && !render.keyDialogOpen && (
          <OperatorCallout intent="danger" description={copy.sourceDrifted} />
        )}
        {render.renderError && !render.renderStale && !render.keyDialogOpen && (
          <OperatorCallout intent="danger" description={render.renderError} />
        )}

        <ExportKeyDialog
          open={render.keyDialogOpen}
          selectedCount={source.selectedCount}
          error={dialogError}
          confirmDisabled={source.sourceActionsBlocked}
          onClose={render.closeKeyDialog}
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
