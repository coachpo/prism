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
import { Separator } from "@/components/ui/separator";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyApiKey } from "@/lib/types";
import {
  formatDateTime,
  formatLastUsed,
  getProxyKeyLifecycleLabel,
} from "./proxyKeyFormatting";

type Props = {
  authEnabled: boolean;
  deleteConfirm: ProxyApiKey | null;
  displayedDeleteConfirm?: ProxyApiKey | null;
  deletingProxyKeyId: number | null;
  open?: boolean;
  onClose: () => void;
  onDelete: () => void;
  onOpenChange: (open: boolean) => void;
  successorId: number | null;
};

export function DeleteProxyKeyDialog({
  authEnabled,
  deleteConfirm,
  displayedDeleteConfirm,
  deletingProxyKeyId,
  open,
  onClose,
  onDelete,
  onOpenChange,
  successorId,
}: Props) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const dialogKey = displayedDeleteConfirm ?? deleteConfirm;
  const dialogOpen = open ?? deleteConfirm !== null;

  return (
    <Dialog open={dialogOpen} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md" showCloseButton={false}>
        <DialogHeader>
          <DialogTitle>{copy.deleteProxyApiKey}</DialogTitle>
          <DialogDescription>
            {copy.deleteProxyApiKeyDescription(dialogKey?.name ?? "", dialogKey?.key_prefix ?? "")}
          </DialogDescription>
        </DialogHeader>

        <DialogBody>
          {dialogKey ? (
            <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
              <div className="flex flex-col gap-2">
                <p className="text-sm font-medium text-foreground">{messages.settingsDialogs.deletionSummary}</p>
                <div className="flex flex-wrap items-center gap-2">
                  <p className="truncate text-sm font-medium text-foreground">{dialogKey.name}</p>
                  <Badge variant="outline">
                    {getProxyKeyLifecycleLabel(dialogKey, authEnabled, successorId)}
                  </Badge>
                </div>
                <p className="break-all font-mono text-xs text-muted-foreground">{dialogKey.key_preview}</p>
              </div>

              <div className="grid gap-3 sm:grid-cols-2">
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.lastUsed}</p>
                  <p className="text-sm text-foreground">{formatLastUsed(dialogKey.last_used_at)}</p>
                </div>
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.created}</p>
                  <p className="text-sm text-foreground">{formatDateTime(dialogKey.created_at)}</p>
                </div>
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.updated}</p>
                  <p className="text-sm text-foreground">{formatDateTime(dialogKey.updated_at)}</p>
                </div>
              </div>

              {dialogKey.notes?.trim() ? (
                <div className="flex min-w-0 flex-col gap-1">
                  <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{copy.notes}</p>
                  <p className="text-sm text-foreground">{dialogKey.notes}</p>
                </div>
              ) : null}

              <Separator />

              <p className="text-sm text-destructive">{messages.vendorManagement.thisActionCannotBeUndone}</p>
            </div>
          ) : null}
        </DialogBody>

        <DialogFooter className="sm:justify-between">
          <Button variant="outline" onClick={onClose} disabled={deletingProxyKeyId !== null}>
            {messages.settingsDialogs.cancel}
          </Button>
          <Button variant="destructive" onClick={onDelete} disabled={deletingProxyKeyId !== null}>
            {deletingProxyKeyId !== null ? messages.settingsDialogs.deleting : copy.deleteKey}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
