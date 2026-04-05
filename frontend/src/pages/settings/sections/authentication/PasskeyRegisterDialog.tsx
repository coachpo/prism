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
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{copy.registerPasskey}</DialogTitle>
          <DialogDescription>{copy.registerPasskeyDescription}</DialogDescription>
        </DialogHeader>
        <div className="space-y-4 py-4">
          <div className="space-y-2">
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
        </div>
        <DialogFooter>
          <Button
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={registering}
          >
            {messages.settingsDialogs.cancel}
          </Button>
          <Button onClick={onSubmit} disabled={registering || !deviceName.trim()}>
            {registering ? copy.registering : copy.continue}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
