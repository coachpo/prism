import { Fingerprint } from "lucide-react";
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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

interface PasskeyRegisterDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  deviceName: string;
  setDeviceName: (value: string) => void;
  onSubmit: () => void;
  registering: boolean;
}

export function PasskeyRegisterDialog({
  open,
  onOpenChange,
  deviceName,
  setDeviceName,
  onSubmit,
  registering,
}: PasskeyRegisterDialogProps) {
  const { messages } = useLocale();
  const copy = messages.settingsAuthentication;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{copy.registerPasskey}</DialogTitle>
          <DialogDescription>{copy.registerPasskeyDescription}</DialogDescription>
        </DialogHeader>

        <form
          className="flex flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault();
            onSubmit();
          }}
        >
          <DialogBody>
            <div className="flex items-start gap-4 rounded-lg border bg-muted/20 p-4">
              <div className="flex size-11 shrink-0 items-center justify-center rounded-xl border border-border/70 bg-background">
                <Fingerprint className="size-5" />
              </div>
              <div className="flex flex-col gap-1">
                <p className="text-sm font-medium text-foreground">{messages.auth.signInWithPasskey}</p>
                <p className="text-sm leading-6 text-muted-foreground">{copy.registerPasskeyDescription}</p>
              </div>
            </div>

            <div className="flex flex-col gap-2 rounded-lg border p-4">
              <Label htmlFor="device-name">{copy.deviceName}</Label>
              <Input
                id="device-name"
                name="device_name"
                autoComplete="off"
                placeholder={copy.deviceNamePlaceholder}
                value={deviceName}
                onChange={(event) => setDeviceName(event.target.value)}
                autoFocus
              />
            </div>
          </DialogBody>

          <DialogFooter className="sm:justify-between">
            <Button
              type="button"
              variant="outline"
              onClick={() => onOpenChange(false)}
              disabled={registering}
            >
              {messages.settingsDialogs.cancel}
            </Button>
            <Button type="submit" disabled={registering || !deviceName.trim()}>
              {registering ? copy.registering : copy.continue}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
