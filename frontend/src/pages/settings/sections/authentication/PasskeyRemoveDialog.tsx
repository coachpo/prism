import { Fingerprint } from "lucide-react";
import { useLocale } from "@/i18n/useLocale";
import { Badge } from "@/components/ui/badge";
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
import { buildPasskeyMetadata, getPasskeyStateBadge } from "./passkeyMetadata";
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
  const passkeyName = passkeyToRemove?.device_name || copy.passkeyFallbackName(passkeyToRemove?.id ?? "");
  const stateBadge = passkeyToRemove ? getPasskeyStateBadge(passkeyToRemove) : null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{copy.removePasskey}</DialogTitle>
          <DialogDescription>
            {copy.removePasskeyConfirmation(passkeyName)}
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          {passkeyToRemove ? (
            <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
              <div className="flex items-start gap-4">
                <div className="flex size-11 shrink-0 items-center justify-center rounded-xl border border-destructive/20 bg-background">
                  <Fingerprint className="size-5" />
                </div>

                <div className="flex min-w-0 flex-1 flex-col gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <p className="truncate text-sm font-medium text-foreground">{passkeyName}</p>
                    {stateBadge ? (
                      <Badge variant="outline" className={stateBadge.className}>
                        {stateBadge.label}
                      </Badge>
                    ) : null}
                  </div>

                  <p className="text-sm leading-6 text-muted-foreground">
                    {buildPasskeyMetadata(passkeyToRemove)}
                  </p>
                </div>
              </div>
            </div>
          ) : null}
        </DialogBody>

        <DialogFooter className="sm:justify-between">
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
