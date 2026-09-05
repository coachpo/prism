import { useEffect, useRef } from "react"
import { AlertCircle, Loader2, RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/i18n/useLocale"
import { OperatorCallout, OperatorDestructiveDialog } from "@/shared/design-system"
import type { DeleteDialogState } from "@/features/endpoints/useEndpointDeletion"
import type { EndpointReferenceItem } from "@/lib/types"

type DeleteEndpointDialogProps = {
  state: DeleteDialogState
  onConfirm: (endpoint: { id: number }) => void
  onLoadMore: (endpointId: number) => void
  onOpenChange: (open: boolean) => void
  onOrphanCleanup: (endpoint: { id: number; name: string; base_url: string }, item: EndpointReferenceItem) => void
  onRetry: () => void
}

function reasonLabel(copy: Record<string, unknown>, reason: string): string {
  switch (reason) {
    case "model_disabled":
      return copy.inactiveReasonModelDisabled as string
    case "access_target_disabled":
      return copy.inactiveReasonAccessTargetDisabled as string
    case "connection_inactive":
      return copy.inactiveReasonConnectionInactive as string
    case "orphaned":
      return copy.inactiveReasonOrphaned as string
    case "configuration_integrity_error":
      return copy.inactiveReasonIntegrityError as string
    default:
      // 后端加一个新枚举时屏幕上出现的必须是中文兜底，不是标识符本身。
      return copy.inactiveReasonUnknown as string
  }
}

function BlockerList({
  detail,
  endpoint,
  onLoadMore,
  onOrphanCleanup,
}: {
  detail: NonNullable<Extract<DeleteDialogState, { phase: "blocked" }>["detail"]>
  endpoint: { id: number; name: string; base_url: string }
  onLoadMore: (endpointId: number) => void
  onOrphanCleanup: (endpoint: { id: number; name: string; base_url: string }, item: EndpointReferenceItem) => void
}) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const loaded = detail.reference_page.items.length
  const total = detail.reference_page.total_count
  return (
    <div className="flex flex-col gap-3" data-testid="delete-blockers">
      <p className="text-sm font-medium text-foreground">{copy.deleteBlockedTotal(String(total))}</p>
      <p className="text-xs text-muted-foreground">{copy.deleteBlockedDescription(endpoint.name)}</p>
      <div className="overflow-hidden rounded-lg border border-border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="text-xs">{copy.referenceModelLabel}</TableHead>
              <TableHead className="text-xs">{copy.referenceTargetLabel}</TableHead>
              <TableHead className="text-xs">{copy.referenceStateLabel}</TableHead>
              <TableHead className="text-right text-xs">{messages.endpoints.actions}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {detail.reference_page.items.map((item) => (
              <BlockerRow key={item.connection_id} item={item} endpoint={endpoint} onOrphanCleanup={onOrphanCleanup} />
            ))}
          </TableBody>
        </Table>
      </div>
      <p className="text-xs text-muted-foreground">{copy.loadedItemsOfTotal(String(loaded), String(total))}</p>
      {detail.reference_page.next_cursor ? (
        <Button type="button" variant="outline" size="sm" onClick={() => onLoadMore(detail.endpoint_id)}>
          {copy.loadMore}
        </Button>
      ) : null}
    </div>
  )
}

function BlockerRow({ item, endpoint, onOrphanCleanup }: { item: EndpointReferenceItem; endpoint: { id: number; name: string; base_url: string }; onOrphanCleanup: (endpoint: { id: number; name: string; base_url: string }, item: EndpointReferenceItem) => void }) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  return (
    <TableRow data-testid={`delete-blocker-${item.connection_id}`}>
      <TableCell>
        {item.kind === "orphan_connection" ? (
          <span className="text-xs text-muted-foreground">{copy.orphanRowLabel(String(item.connection_id))}</span>
        ) : item.owner_model ? (
          <span className="flex flex-col gap-0.5 text-xs">
            <span className="font-medium text-foreground">{item.owner_model.display_name ?? item.owner_model.model_id}</span>
            <code className="font-mono text-[11px] text-muted-foreground">{item.owner_model.model_id}</code>
          </span>
        ) : null}
      </TableCell>
      <TableCell>
        <span className="flex flex-col gap-0.5 text-xs">
          <span className="text-foreground">{item.terminal_target_name ?? `#${item.terminal_target_id}`}</span>
          <code className="font-mono text-[11px] text-muted-foreground">#{item.terminal_target_id}</code>
        </span>
      </TableCell>
      <TableCell>
        <span className="text-[11px] text-muted-foreground">
          {item.enabled ? copy.enabled : item.inactive_reasons.map((reason) => reasonLabel(copy as Record<string, unknown>, reason)).join(" · ")}
        </span>
      </TableCell>
      <TableCell className="text-right">
        {item.kind === "orphan_connection" ? (
          <Button type="button" size="sm" variant="outline" onClick={() => onOrphanCleanup(endpoint, item)}>
            {copy.orphanCleanup}
          </Button>
        ) : null}
      </TableCell>
    </TableRow>
  )
}

export function DeleteEndpointDialog({
  state,
  onConfirm,
  onLoadMore,
  onOpenChange,
  onOrphanCleanup,
  onRetry,
}: DeleteEndpointDialogProps) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const open = state.phase !== "closed"
  const endpoint = state.phase === "closed" ? null : state.endpoint
  const blockerHeadingRef = useRef<HTMLHeadingElement>(null)
  const integrityHeadingRef = useRef<HTMLHeadingElement>(null)

  // Focus moves to the blocker/integrity heading when a race 409 arrives.
  useEffect(() => {
    if (state.phase === "blocked") {
      blockerHeadingRef.current?.focus()
    }
    if (state.phase === "integrity_error") {
      integrityHeadingRef.current?.focus()
    }
  }, [state])

  return (
    <OperatorDestructiveDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) onOpenChange(false)
      }}
      title={copy.deleteEndpoint}
      description={endpoint ? copy.deleteEndpointDescription(endpoint.name) : ""}
      cancelLabel={messages.settingsDialogs.cancel}
      confirmLabel={messages.settingsDialogs.delete}
      confirmingLabel={<span className="inline-flex items-center gap-2"><Loader2 className="size-4 animate-spin" />{messages.settingsDialogs.deleting}</span>}
      confirming={state.phase === "deleting"}
      confirmDisabled={state.phase !== "eligible"}
      cancelDisabled={state.phase === "deleting" || state.phase === "checking"}
      showConfirmButton={state.phase === "eligible" || state.phase === "deleting"}
      showCloseButton={state.phase !== "deleting"}
      confirmTestId="delete-endpoint-confirm"
      contentClassName="sm:max-w-2xl"
      bodyClassName="flex min-h-0 flex-col gap-4 overflow-y-auto"
      onConfirm={() => {
        if (endpoint) onConfirm({ id: endpoint.id })
      }}
      onCancel={() => onOpenChange(false)}
    >
      {state.phase === "checking" ? (
        <div role="status" className="flex items-center gap-2 text-sm text-muted-foreground" data-testid="delete-checking">
          <Loader2 className="size-4 animate-spin" />
          {copy.deleteChecking}
        </div>
      ) : null}

      {state.phase === "eligible" ? (
        <OperatorCallout intent="warning" title={copy.deleteEligibleConfirm} description={endpoint ? endpoint.name : ""} />
      ) : null}

      {state.phase === "blocked" ? (
        <div className="flex flex-col gap-3">
          <h3
            ref={blockerHeadingRef}
            tabIndex={-1}
            className="text-base font-semibold text-destructive"
            data-testid="delete-blocked-heading"
          >
            {copy.deleteBlockedHeading}
          </h3>
          <BlockerList detail={state.detail} endpoint={state.endpoint} onLoadMore={onLoadMore} onOrphanCleanup={onOrphanCleanup} />
        </div>
      ) : null}

      {state.phase === "check_error" ? (
        <OperatorCallout intent="danger" role="alert" title={copy.deleteCheckError} description={state.error.message} action={<Button type="button" variant="outline" size="sm" onClick={onRetry}><RefreshCw />{copy.deleteRetry}</Button>} />
      ) : null}

      {state.phase === "integrity_error" ? (
        <div className="flex flex-col gap-3">
          <h3 ref={integrityHeadingRef} tabIndex={-1} className="text-base font-semibold text-destructive" data-testid="delete-integrity-error-heading">
            <AlertCircle className="mr-1 inline size-4" />
            {copy.deleteIntegrityError}
          </h3>
          <OperatorCallout intent="danger" role="alert" description={state.error.message} action={<Button type="button" variant="outline" size="sm" onClick={onRetry}><RefreshCw />{copy.deleteRetry}</Button>} />
        </div>
      ) : null}
    </OperatorDestructiveDialog>
  )
}
