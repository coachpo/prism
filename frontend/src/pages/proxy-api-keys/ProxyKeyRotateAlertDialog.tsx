import { Spinner } from "@/components/ui/spinner";
import { useLocale } from "@/i18n/useLocale";
import type { ProxyApiKey } from "@/lib/types";
import { OperatorDestructiveDialog, OperatorInsetPanel, OperatorStatusBadge } from "@/shared/design-system";
import { getProxyKeyLifecycleLabel, getProxyKeyLifecycleTier } from "./proxyKeyFormatting";

interface ProxyKeyRotateAlertDialogProps {
  authEnabled: boolean;
  open: boolean;
  rotateConfirm: ProxyApiKey | null;
  displayedRotateConfirm: ProxyApiKey | null;
  rotating: boolean;
  onCancel: () => void;
  onConfirm: () => void;
  onOpenChange: (open: boolean) => void;
}

/**
 * Rotation invalidates the live credential immediately, so it gets the same
 * confirmation weight as delete. The lifecycle comparison rows appear here —
 * at the moment of the decision — instead of inside a collapsed table above
 * the ledger.
 */
export function ProxyKeyRotateAlertDialog({
  authEnabled,
  displayedRotateConfirm,
  onCancel,
  onConfirm,
  onOpenChange,
  open,
  rotateConfirm,
  rotating,
}: ProxyKeyRotateAlertDialogProps) {
  const { messages } = useLocale();
  const copy = messages.proxyApiKeys;
  const target = displayedRotateConfirm ?? rotateConfirm;

  return (
    <OperatorDestructiveDialog
      open={open}
      onOpenChange={onOpenChange}
      title={copy.rotateConfirmTitle}
      description={copy.rotateConfirmDescription(target?.name ?? "")}
      cancelLabel={messages.settingsDialogs.cancel}
      confirmLabel={copy.rotateConfirmAction}
      confirmingLabel={
        <>
          <Spinner aria-hidden="true" data-icon="inline-start" />
          {copy.rotating}
        </>
      }
      confirming={rotating}
      cancelDisabled={rotating}
      onCancel={onCancel}
      onConfirm={onConfirm}
      confirmTestId="proxy-key-rotate-confirm"
    >
      {target ? (
        <div className="flex flex-col gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <p className="truncate text-sm font-medium text-foreground">{target.name}</p>
            <OperatorStatusBadge
              intent={getProxyKeyLifecycleTier(target, authEnabled)}
              label={getProxyKeyLifecycleLabel(target, authEnabled)}
              preserveLabel
            />
          </div>
          <p className="break-all font-mono text-xs text-muted-foreground">{target.key_preview}</p>

          <OperatorInsetPanel title={copy.rotateConfirmSummary}>
            <ul className="flex flex-col gap-1 text-xs text-muted-foreground">
              <li>{copy.lifecycleRotateCredential}</li>
              <li>{copy.lifecycleDeleteHistory}</li>
            </ul>
          </OperatorInsetPanel>
        </div>
      ) : null}
    </OperatorDestructiveDialog>
  );
}
