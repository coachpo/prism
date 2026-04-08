import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { getAdaptiveRoutingObjectiveLabel } from "@/lib/loadbalanceRoutingPolicy";
import type { LoadbalanceStrategy } from "@/lib/types";

interface DeleteLoadbalanceStrategyDialogProps {
  deleteLoadbalanceStrategyConfirm: LoadbalanceStrategy | null;
  displayedDeleteLoadbalanceStrategyConfirm?: LoadbalanceStrategy | null;
  loadbalanceStrategyDeleting: boolean;
  onClose: () => void;
  onDelete: () => Promise<void>;
  open?: boolean;
}

export function DeleteLoadbalanceStrategyDialog({
  deleteLoadbalanceStrategyConfirm,
  displayedDeleteLoadbalanceStrategyConfirm,
  loadbalanceStrategyDeleting,
  onClose,
  onDelete,
  open,
}: DeleteLoadbalanceStrategyDialogProps) {
  const { formatNumber, messages } = useLocale();
  const copy = messages.loadbalanceStrategiesTable;
  const strategyCopy = messages.loadbalanceStrategyCopy;
  const dialogStrategy = displayedDeleteLoadbalanceStrategyConfirm ?? deleteLoadbalanceStrategyConfirm;
  const dialogOpen = open ?? deleteLoadbalanceStrategyConfirm !== null;
  const attachedModelCount = dialogStrategy?.attached_model_count ?? 0;
  const isInUse = attachedModelCount > 0;

  const strategyTypeLabel = dialogStrategy
    ? dialogStrategy.strategy_type === "adaptive"
      ? `${strategyCopy.adaptiveFamilyLabel} • ${getAdaptiveRoutingObjectiveLabel(dialogStrategy.routing_policy.routing_objective, strategyCopy)}`
      : dialogStrategy.legacy_strategy_type === "single"
        ? `${strategyCopy.legacyFamilyLabel} • ${strategyCopy.singleLabel}`
        : dialogStrategy.legacy_strategy_type === "fill-first"
          ? `${strategyCopy.legacyFamilyLabel} • ${strategyCopy.fillFirstLabel}`
          : `${strategyCopy.legacyFamilyLabel} • ${strategyCopy.roundRobinLabel}`
    : "";

  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) {
          onClose();
        }
      }}
    >
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{copy.deleteStrategy}</DialogTitle>
          <DialogDescription>{copy.deleteStrategyDescription(dialogStrategy?.name ?? "")}</DialogDescription>
        </DialogHeader>

        <DialogBody>
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
              {copy.deleteStrategyInUse(formatNumber(attachedModelCount))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">{messages.vendorManagement.thisActionCannotBeUndone}</p>
          )}
        </DialogBody>

        <DialogFooter className="sm:justify-between">
          {isInUse ? (
            <Button variant="outline" onClick={onClose}>
              {messages.common.close}
            </Button>
          ) : (
            <>
              <Button variant="outline" onClick={onClose}>
                {messages.settingsDialogs.cancel}
              </Button>
              <Button
                variant="destructive"
                onClick={() => void onDelete()}
                disabled={loadbalanceStrategyDeleting}
              >
                {loadbalanceStrategyDeleting ? messages.settingsDialogs.deleting : messages.settingsDialogs.delete}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
