import { useLocale } from "@/i18n/useLocale";
import { getLoadbalanceStrategyDetailLabel } from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceStrategy } from "@/lib/types";
import { OperatorDestructiveDialog } from "@/shared/design-system";

interface DeleteLoadbalanceStrategyDialogProps {
  deleteLoadbalanceStrategyConfirm: LoadbalanceStrategy | null;
  displayedDeleteLoadbalanceStrategyConfirm?: LoadbalanceStrategy | null;
  loadbalanceStrategyDeleting: boolean;
  loadbalanceStrategyDeleteError?: { message: string; attachedModelCount: number | null; defaultStrategyId: number | null } | null;
  onClose: () => void;
  onDelete: () => Promise<void>;
  open?: boolean;
}

export function DeleteLoadbalanceStrategyDialog({
  deleteLoadbalanceStrategyConfirm,
  displayedDeleteLoadbalanceStrategyConfirm,
  loadbalanceStrategyDeleting,
  loadbalanceStrategyDeleteError,
  onClose,
  onDelete,
  open,
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

  return (
    <OperatorDestructiveDialog
      open={dialogOpen}
      onOpenChange={(nextOpen) => !nextOpen && onClose()}
      title={legacyCopy.deleteStrategy}
      description={legacyCopy.deleteStrategyDescription(dialogStrategy?.name ?? "")}
      cancelLabel={isBlocked ? messages.common.close : messages.settingsDialogs.cancel}
      confirmLabel={messages.settingsDialogs.delete}
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
        <div className="rounded-lg border border-destructive/40 bg-destructive/10 px-4 py-3 text-sm text-destructive">
          {legacyCopy.deleteStrategyInUse(formatNumber(attachedModelCount ?? 0))}
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
