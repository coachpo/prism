import { useLocale } from "@/i18n/useLocale";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import type { PasskeyCredential } from "./types";

interface PasskeyRemoveDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  passkeyToRemove: PasskeyCredential | null;
  onConfirmRemove: () => void;
  removing: boolean;
}

export function PasskeyRemoveDialog({
  open,
  onOpenChange,
  passkeyToRemove,
  onConfirmRemove,
  removing,
}: PasskeyRemoveDialogProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{copy.removePasskey}</DialogTitle>
          <DialogDescription>
            {copy.removePasskeyConfirmation(
              passkeyToRemove?.device_name || copy.passkeyFallbackName(passkeyToRemove?.id ?? ""),
            )}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={removing}
          >
            {messages.settingsDialogs.cancel}
          </Button>
          <Button variant="destructive" onClick={onConfirmRemove} disabled={removing}>
            {removing ? copy.removing : copy.removePasskey}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
