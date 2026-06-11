import { Loader2, Trash2 } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { SidecarInstance } from "@/lib/types";

interface DeleteSidecarDialogProps {
  deleteSidecarConfirm: SidecarInstance | null;
  displayedDeleteSidecarConfirm?: SidecarInstance | null;
  onClose: () => void;
  onDelete: () => Promise<void>;
  open?: boolean;
  sidecarDeleting: boolean;
}

export function DeleteSidecarDialog({
  deleteSidecarConfirm,
  displayedDeleteSidecarConfirm,
  onClose,
  onDelete,
  open,
  sidecarDeleting,
}: DeleteSidecarDialogProps) {
  const { messages } = useLocale();
  const copy = messages.sidecarsPage;
  const dialogSidecar = displayedDeleteSidecarConfirm ?? deleteSidecarConfirm;
  const dialogOpen = open ?? deleteSidecarConfirm !== null;

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
          <DialogTitle>{copy.deleteTitle}</DialogTitle>
          <DialogDescription>{copy.deleteDescription(dialogSidecar?.name ?? "")}</DialogDescription>
        </DialogHeader>

        <DialogBody>
          <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4 text-sm">
            <div className="flex items-start gap-3">
              <div className="mt-0.5 rounded-full bg-destructive/10 p-2 text-destructive">
                <Trash2 className="h-4 w-4" />
              </div>
              <div className="space-y-1">
                <p className="font-medium text-destructive">{copy.deleteWarningTitle}</p>
                <p className="text-muted-foreground">{copy.deleteWarningDescription}</p>
                {dialogSidecar ? (
                  <p className="font-mono text-xs text-muted-foreground">{dialogSidecar.base_url_canonical}</p>
                ) : null}
              </div>
            </div>
          </div>
        </DialogBody>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={onClose} disabled={sidecarDeleting}>
            {copy.cancel}
          </Button>
          <Button type="button" variant="destructive" disabled={sidecarDeleting} onClick={() => void onDelete()}>
            {sidecarDeleting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
            {sidecarDeleting ? copy.deleting : copy.deleteAction}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
