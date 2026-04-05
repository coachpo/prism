import { Button } from "@/components/ui/button";
import { useLocale } from "@/i18n/useLocale";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
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
  const dialogStrategy = displayedDeleteLoadbalanceStrategyConfirm ?? deleteLoadbalanceStrategyConfirm;
  const dialogOpen = open ?? deleteLoadbalanceStrategyConfirm !== null;
  const attachedModelCount = dialogStrategy?.attached_model_count ?? 0;
  const isInUse = attachedModelCount > 0;

  return (
    <Dialog
      open={dialogOpen}
      onOpenChange={(open) => {
        if (!open) {
          onClose();
        }
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{copy.deleteStrategy}</DialogTitle>
          <DialogDescription>{copy.deleteStrategyDescription(dialogStrategy?.name ?? "")}</DialogDescription>
        </DialogHeader>

        <div className="py-4">
          {isInUse ? (
            <div className="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
              {copy.deleteStrategyInUse(formatNumber(attachedModelCount))}
            </div>
          ) : (
            <p className="text-sm">{messages.vendorManagement.thisActionCannotBeUndone}</p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            variant="destructive"
            onClick={() => void onDelete()}
            disabled={loadbalanceStrategyDeleting || isInUse}
          >
            {loadbalanceStrategyDeleting ? messages.settingsDialogs.deleting : messages.settingsDialogs.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
