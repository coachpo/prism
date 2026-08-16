import { AlertTriangle } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Spinner } from "@/components/ui/spinner";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyApiKey } from "@/lib/types";
import { OperatorDestructiveDialog } from "@/shared/design-system";
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
}: ProxyKeyDeleteAlertDialogProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const dialogKey = displayedDeleteConfirm ?? deleteConfirm;
  const deleting = deletingProxyKeyId !== null;

  return (
    <OperatorDestructiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={copy.deleteProxyApiKey}
      description={copy.deleteProxyApiKeyDescription(dialogKey?.name ?? "", dialogKey?.key_prefix ?? "")}
      cancelLabel={messages.settingsDialogs.cancel}
      confirmLabel={copy.deleteKey}
      confirmingLabel={
        <>
          <Spinner aria-hidden="true" data-icon="inline-start" />
          {messages.settingsDialogs.deleting}
        </>
      }
      confirming={deleting}
      cancelDisabled={deleting}
      onCancel={onClose}
      onConfirm={onDelete}
    >
      {dialogKey ? (
        <div className="flex flex-col gap-4 rounded-lg border border-destructive/25 bg-destructive/5 p-4">
          <div className="flex flex-col gap-2">
            <p className="text-sm font-medium text-foreground">{messages.settingsDialogs.deletionSummary}</p>
            <div className="flex flex-wrap items-center gap-2">
              <p className="truncate text-sm font-medium text-foreground">{dialogKey.name}</p>
              <Badge variant="outline">
                {getProxyKeyLifecycleLabel(dialogKey, authEnabled)}
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

          <Separator />
          <p className="text-sm text-destructive">{messages.common.thisActionCannotBeUndone}</p>
        </div>
      ) : null}
    </OperatorDestructiveDialog>
  );
}
