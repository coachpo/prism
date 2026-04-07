import type { Endpoint } from "@/lib/types";
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

interface DeleteEndpointDialogProps {
  deleteTarget: Endpoint | null;
  displayTarget?: Endpoint | null;
  isDeletingEndpoint: boolean;
  onConfirm: (id: number) => void | Promise<void>;
  onOpenChange: (open: boolean) => void;
}

export function DeleteEndpointDialog({
  deleteTarget,
  displayTarget = deleteTarget,
  isDeletingEndpoint,
  onConfirm,
  onOpenChange,
}: DeleteEndpointDialogProps) {
  const { messages } = useLocale();
  const copy = messages.endpointsUi;
  const dialogTarget = deleteTarget ?? displayTarget;
  const apiKeySummary = dialogTarget?.has_api_key
    ? dialogTarget.masked_api_key ?? messages.common.unavailable
    : copy.none;

  return (
    <Dialog open={Boolean(deleteTarget)} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{copy.deleteEndpoint}</DialogTitle>
          <DialogDescription>{copy.deleteEndpointDescription(dialogTarget?.name ?? "")}</DialogDescription>
        </DialogHeader>

        <DialogBody>
          {dialogTarget ? (
            <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
              <div className="flex flex-col gap-2">
                <p className="text-sm font-medium text-foreground">{messages.settingsDialogs.deletionSummary}</p>
                <p className="truncate text-sm font-medium text-foreground">{dialogTarget.name}</p>
                <code className="rounded-md border bg-background px-2 py-1 text-xs font-medium break-all text-foreground">
                  {dialogTarget.base_url}
                </code>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.baseUrl}</p>
                  <p className="break-all text-sm text-foreground">{dialogTarget.base_url}</p>
                </div>
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{messages.proxyApiKeys.apiKey}</p>
                  <p className="truncate text-sm text-foreground">{apiKeySummary}</p>
                </div>
              </div>
            </div>
          ) : null}
        </DialogBody>

        <DialogFooter className="sm:justify-between">
          <Button variant="outline" disabled={isDeletingEndpoint} onClick={() => onOpenChange(false)}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button
            variant="destructive"
            disabled={isDeletingEndpoint || !deleteTarget}
            onClick={() => {
              if (!deleteTarget) {
                return;
              }
              void onConfirm(deleteTarget.id);
            }}
          >
            {isDeletingEndpoint ? messages.settingsDialogs.deleting : messages.settingsDialogs.delete}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
