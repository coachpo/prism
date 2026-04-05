import { useEffect, useState } from "react";
import type { Endpoint } from "@/lib/types";
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

interface DeleteEndpointDialogProps {
  deleteTarget: Endpoint | null;
  isDeletingEndpoint: boolean;
  onConfirm: (id: number) => void | Promise<void>;
  onOpenChange: (open: boolean) => void;
}

export function DeleteEndpointDialog({
  deleteTarget,
  isDeletingEndpoint,
  onConfirm,
  onOpenChange,
}: DeleteEndpointDialogProps) {
  const { messages } = useLocale();
  const copy = messages.endpointsUi;
  const [displayTarget, setDisplayTarget] = useState<Endpoint | null>(deleteTarget);

  useEffect(() => {
    if (deleteTarget) {
      setDisplayTarget(deleteTarget);
    }
  }, [deleteTarget]);

  const dialogTarget = deleteTarget ?? displayTarget;

  return (
    <Dialog open={Boolean(deleteTarget)} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>{copy.deleteEndpoint}</DialogTitle>
          <DialogDescription>{copy.deleteEndpointDescription(dialogTarget?.name ?? "")}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" disabled={isDeletingEndpoint} onClick={() => onOpenChange(false)}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            variant="destructive"
            disabled={isDeletingEndpoint || !dialogTarget}
            onClick={() => {
              if (!dialogTarget) {
                return;
              }
              void onConfirm(dialogTarget.id);
            }}
          >
            {isDeletingEndpoint ? messages.settingsDialogs.deleting : messages.settingsDialogs.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
