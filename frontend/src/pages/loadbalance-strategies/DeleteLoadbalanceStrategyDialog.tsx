import { useEffect } from "react";
import { useLocale } from "@/i18n/useLocale";
import { getLoadbalanceStrategyDetailLabel } from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceStrategy } from "@/lib/types";
import type { StrategyImpactState } from "@/features/loadbalance/useStrategyImpactPager";
import { Button } from "@/components/ui/button";
import {
  OperatorDestructiveDialog,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorTypeBadge,
} from "@/shared/design-system";
import { LoadMoreControl } from "@/shared/table/paginationControls";
import { RefreshCw } from "lucide-react";

interface DeleteLoadbalanceStrategyDialogProps {
  deleteLoadbalanceStrategyConfirm: LoadbalanceStrategy | null;
  displayedDeleteLoadbalanceStrategyConfirm?: LoadbalanceStrategy | null;
  loadbalanceStrategyDeleting: boolean;
  loadbalanceStrategyDeleteError?: { message: string; attachedModelCount: number | null; defaultStrategyId: number | null } | null;
  onClose: () => void;
  onDelete: () => Promise<void>;
  open?: boolean;
  /** 绑定名单的分页状态；被绑定时它就是挡住删除的那份证据。 */
  impactState?: StrategyImpactState;
  onLoadAttachedModels?: (strategyId: number) => void;
  onLoadMoreAttachedModels?: (strategyId: number) => void;
}

export function DeleteLoadbalanceStrategyDialog({
  deleteLoadbalanceStrategyConfirm,
  displayedDeleteLoadbalanceStrategyConfirm,
  loadbalanceStrategyDeleting,
  loadbalanceStrategyDeleteError,
  onClose,
  onDelete,
  open,
  impactState,
  onLoadAttachedModels,
  onLoadMoreAttachedModels,
}: DeleteLoadbalanceStrategyDialogProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.routingStrategyTable;
  const legacyCopy = messages.loadbalanceStrategiesTable;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const dialogStrategy = displayedDeleteLoadbalanceStrategyConfirm ?? deleteLoadbalanceStrategyConfirm;
  const dialogOpen = open ?? deleteLoadbalanceStrategyConfirm !== null;
  // An unknown binding count is not evidence that nothing is bound. A preflight
  // that cannot produce complete facts blocks the delete rather than passing.
  const attachedModelCount = dialogStrategy?.attached_model_count ?? null;
  const bindingsUnknown = dialogStrategy != null && attachedModelCount === null;
  const isInUse = (attachedModelCount ?? 0) > 0;
  const isDefaultBlocked = loadbalanceStrategyDeleteError?.defaultStrategyId != null;
  const isBlocked = isInUse || isDefaultBlocked || bindingsUnknown;
  const strategyTypeLabel = dialogStrategy
    ? getLoadbalanceStrategyDetailLabel(dialogStrategy, strategyCopy)
    : "";
  const strategyId = dialogStrategy?.id ?? null;

  // 阻塞的删除必须把拦路的引用列在对话框里，否则操作者得关掉对话框、回表格、
  // 再展开名单才知道要动哪几个模型。名单还没读过就在这里读第一页。
  useEffect(() => {
    if (!dialogOpen || !isInUse || strategyId === null || impactState) return;
    onLoadAttachedModels?.(strategyId);
  }, [dialogOpen, isInUse, strategyId, impactState, onLoadAttachedModels]);

  return (
    <OperatorDestructiveDialog
      open={dialogOpen}
      onOpenChange={(nextOpen) => !nextOpen && onClose()}
      title={legacyCopy.deleteStrategy}
      description={legacyCopy.deleteStrategyDescription(dialogStrategy?.name ?? "")}
      cancelLabel={isBlocked ? messages.common.close : messages.settingsDialogs.cancel}
      confirmLabel={legacyCopy.deleteStrategy}
      confirmingLabel={messages.settingsDialogs.deleting}
      confirming={loadbalanceStrategyDeleting}
      confirmDisabled={isBlocked}
      showConfirmButton={!isBlocked}
      onCancel={onClose}
      onConfirm={onDelete}
    >
      {dialogStrategy ? (
        <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
          <div className="flex flex-col gap-2">
            <p className="text-sm font-medium text-foreground">{messages.settingsDialogs.deletionSummary}</p>
            <div className="flex flex-wrap items-center gap-2">
              <p className="truncate text-sm font-medium text-foreground">{dialogStrategy.name}</p>
              <code className="inline-flex items-center rounded-md border bg-background px-2 py-1 text-xs font-medium text-foreground">
                {strategyTypeLabel}
              </code>
            </div>
          </div>
        </div>
      ) : null}

      {isInUse ? (
        <div className="flex flex-col gap-3">
          <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive" role="alert">
            <p>{legacyCopy.deleteStrategyInUse(formatNumber(attachedModelCount ?? 0))}</p>
            <p className="mt-1">{copy.deleteInUseNextStep}</p>
          </div>
          {strategyId !== null ? (
            <AttachedModelList
              strategyId={strategyId}
              impactState={impactState}
              onLoadMore={onLoadMoreAttachedModels}
              onRetry={onLoadAttachedModels}
            />
          ) : null}
        </div>
      ) : bindingsUnknown ? (
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive" role="alert">
          {copy.deleteBindingsUnknown}
        </div>
      ) : isDefaultBlocked ? (
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive" role="alert">
          {copy.deleteDefaultBlocked}
        </div>
      ) : (
        <p className="text-sm text-muted-foreground">{messages.common.thisActionCannotBeUndone}</p>
      )}
      {loadbalanceStrategyDeleteError && !isInUse && !isDefaultBlocked ? (
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive" role="alert">
          {loadbalanceStrategyDeleteError.message}
        </div>
      ) : null}
    </OperatorDestructiveDialog>
  );
}

/** 挡住删除的那份名单，与表格行内展开读同一个分页器。 */
function AttachedModelList({
  strategyId,
  impactState,
  onLoadMore,
  onRetry,
}: {
  strategyId: number;
  impactState?: StrategyImpactState;
  onLoadMore?: (strategyId: number) => void;
  onRetry?: (strategyId: number) => void;
}) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.routingStrategyTable;
  const tableCopy = messages.operationalTable;
  const fragment = impactState?.fragment;
  const data = fragment?.data;

  // A failed append keeps the loaded models on screen; only a read that never
  // produced anything replaces the list with an error surface.
  if (fragment?.phase === "error" && (!data || data.items.length === 0)) {
    return (
      <OperatorErrorState
        title={copy.attachedModelsFailed}
        description={fragment.error ?? ""}
        action={
          <Button type="button" variant="outline" size="sm" onClick={() => onRetry?.(strategyId)}>
            <RefreshCw data-icon="inline-start" />
            {copy.retry}
          </Button>
        }
      />
    );
  }
  if (!data || data.items.length === 0) {
    // 读完了却一条都没有，就说没有；只有还在读的时候才画加载态。
    if (!fragment || fragment.phase === "idle" || fragment.phase === "loading") {
      return <OperatorLoadingState title={copy.attachedModels} description={tableCopy.loadingFirstPage} />;
    }
    return <p className="text-sm text-muted-foreground">{copy.attachedModelsEmpty}</p>;
  }
  const appendFailed = fragment?.error != null && fragment.phase !== "error";
  return (
    <div className="flex flex-col gap-2">
      <p className="text-xs font-medium text-muted-foreground">{copy.attachedModels}</p>
      {appendFailed ? (
        <p role="alert" className="text-xs text-failing">
          {fragment?.error}
        </p>
      ) : null}
      <ul className="flex max-h-40 flex-col gap-1 overflow-y-auto text-sm">
        {data.items.map((item) => (
          <li key={item.model_config_id} className="flex flex-wrap items-center gap-2">
            <span className="min-w-0 truncate font-medium">{item.display_name || item.model_id}</span>
            <span className="font-mono text-xs text-muted-foreground">{item.model_id}</span>
            <OperatorTypeBadge label={item.is_enabled ? copy.enabled : copy.disabled} />
          </li>
        ))}
      </ul>
      <LoadMoreControl
        testId={`delete-strategy-impact-more-${strategyId}`}
        pending={fragment?.phase === "loading"}
        error={appendFailed ? fragment?.error ?? null : null}
        hasMore={Boolean(data.has_more)}
        labels={{
          loadMore: copy.attachedModelsExpand(formatNumber(data.attached_model_count)),
          loading: tableCopy.loadingMore,
          retry: tableCopy.retryLoadMore,
        }}
        onLoadMore={() => (appendFailed ? onRetry?.(strategyId) : onLoadMore?.(strategyId))}
      />
    </div>
  );
}
