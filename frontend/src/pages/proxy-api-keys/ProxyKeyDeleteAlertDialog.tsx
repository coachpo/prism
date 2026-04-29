import { AlertTriangle } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyApiKey } from "@/lib/types";
import {
  formatDateTime,
  formatLastUsed,
  getProxyKeyLifecycleLabel,
} from "./proxyKeyFormatting";

interface ProxyKeyDeleteAlertDialogProps {
  authEnabled: boolean;
  deleteConfirm: ProxyApiKey | null;
  displayedDeleteConfirm?: ProxyApiKey | null;
  deletingProxyKeyId: number | null;
  open: boolean;
  onClose: () => void;
  onDelete: () => void;
  onOpenChange: (open: boolean) => void;
  successorId: number | null;
}

export function ProxyKeyDeleteAlertDialog({
  authEnabled,
  deleteConfirm,
  displayedDeleteConfirm,
  deletingProxyKeyId,
  open,
  onClose,
  onDelete,
  onOpenChange,
  successorId,
}: ProxyKeyDeleteAlertDialogProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const dialogKey = displayedDeleteConfirm ?? deleteConfirm;

  return (
    <AlertDialog open={open} onOpenChange={onOpenChange}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{copy.deleteProxyApiKey}</AlertDialogTitle>
          <AlertDialogDescription>
            {copy.deleteProxyApiKeyDescription(dialogKey?.name ?? "", dialogKey?.key_prefix ?? "")}
          </AlertDialogDescription>
        </AlertDialogHeader>

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
                <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{copy.lastUsed}</p>
                <p className="text-sm text-foreground">{formatLastUsed(dialogKey.last_used_at)}</p>
              </div>
              <div className="flex min-w-0 flex-col gap-1">
                <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{copy.created}</p>
                <p className="text-sm text-foreground">{formatDateTime(dialogKey.created_at)}</p>
              </div>
              <div className="flex min-w-0 flex-col gap-1">
                <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{copy.updated}</p>
                <p className="text-sm text-foreground">{formatDateTime(dialogKey.updated_at)}</p>
              </div>
            </div>

            {dialogKey.notes?.trim() ? (
              <div className="flex min-w-0 flex-col gap-1">
                <p className="text-xs font-medium uppercase tracking-wide text-muted-foreground">{copy.notes}</p>
                <p className="text-sm text-foreground">{dialogKey.notes}</p>
              </div>
            ) : null}

            {authEnabled ? (
              <Alert className="border-destructive/25 bg-background">
                <AlertTriangle />
                <AlertTitle>{copy.deleteTrafficWarningTitle}</AlertTitle>
                <AlertDescription>{copy.deleteTrafficWarningDescription}</AlertDescription>
              </Alert>
            ) : null}

            {successorId !== null ? (
              <Alert className="bg-background">
                <AlertTriangle />
                <AlertTitle>{copy.deleteSuccessorWarningTitle}</AlertTitle>
                <AlertDescription>{copy.deleteSuccessorWarningDescription(successorId)}</AlertDescription>
              </Alert>
            ) : null}

            <Separator />
            <p className="text-sm text-destructive">{messages.vendorManagement.thisActionCannotBeUndone}</p>
          </div>
        ) : null}

        <AlertDialogFooter>
          <AlertDialogCancel onClick={onClose} disabled={deletingProxyKeyId !== null}>
            {messages.settingsDialogs.cancel}
          </AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={deletingProxyKeyId !== null}
            onClick={(event) => {
              event.preventDefault();
              onDelete();
            }}
          >
            {deletingProxyKeyId !== null ? <Spinner aria-hidden="true" data-icon="inline-start" /> : null}
            {deletingProxyKeyId !== null ? messages.settingsDialogs.deleting : copy.deleteKey}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
