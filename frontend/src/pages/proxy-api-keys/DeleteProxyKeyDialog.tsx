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
import type { ProxyApiKey } from "@/lib/types";

type Props = {
  deleteConfirm: ProxyApiKey | null;
  displayedDeleteConfirm?: ProxyApiKey | null;
  deletingProxyKeyId: number | null;
  open?: boolean;
  onClose: () => void;
  onDelete: () => void;
  onOpenChange: (open: boolean) => void;
};

export function DeleteProxyKeyDialog({
  deleteConfirm,
  displayedDeleteConfirm,
  deletingProxyKeyId,
  open,
  onClose,
  onDelete,
  onOpenChange,
}: Props) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const dialogKey = displayedDeleteConfirm ?? deleteConfirm;
  const dialogOpen = open ?? deleteConfirm !== null;
  return (
    <Dialog open={dialogOpen} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{copy.deleteProxyApiKey}</DialogTitle>
          <DialogDescription>
            {copy.deleteProxyApiKeyDescription(dialogKey?.name ?? "", dialogKey?.key_prefix ?? "")}
          </DialogDescription>
        </DialogHeader>

        {dialogKey ? (
          <div className="space-y-4 py-2">
            <div className="rounded-xl border border-destructive/30 bg-destructive/10 px-4 py-3">
              <p className="text-sm text-destructive">{messages.vendorManagement.thisActionCannotBeUndone}</p>
            </div>
          </div>
        ) : null}

        <DialogFooter>
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
