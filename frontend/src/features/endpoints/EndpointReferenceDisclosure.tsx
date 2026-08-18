import { ChevronDown, ChevronRight, ExternalLink, Loader2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { TableCell, TableRow } from "@/components/ui/table"
import { formatNumber } from "@/i18n/format"
import { useLocale } from "@/i18n/useLocale"
import type { Endpoint, EndpointReferenceItem } from "@/lib/types"
import { cn } from "@/lib/utils"
import {
  OperatorCallout,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorStatusBadge,
  type OperatorStatusTier,
} from "@/shared/design-system"
import { summaryFor, type EndpointReferenceDetailState, type EndpointReferenceSummaryState } from "./useEndpointReferences"

function ReferenceCell({
  endpoint,
  summaryState,
  detailState,
  onOpen,
  onRetryRow,
}: {
  endpoint: Endpoint
  summaryState: EndpointReferenceSummaryState | undefined
  detailState: EndpointReferenceDetailState | undefined
  onOpen: () => void
  onRetryRow: () => void
}) {
  const { messages } = useLocale()
  const copy = messages.endpoints
  const expanded = detailState != null && detailState.status !== "idle"

  const content = (() => {
    if (!summaryState || summaryState.status === "loading") {
      return (
        <span role="status" className="flex items-center gap-1.5 text-xs text-muted-foreground">
          <Loader2 className="size-3 animate-spin" />
          {copy.referencesLoading}
        </span>
      )
    }
    if (summaryState.status === "error") {
      return null
    }
    const value = summaryState.value
    if (value.direct_reference_count === 0) {
      return <span className="text-xs text-muted-foreground">{copy.refsZero}</span>
    }
    return (
      <span className="flex flex-col gap-0.5">
        <span className="text-xs font-medium text-foreground">
          {copy.refsSummary(formatNumber(value.direct_reference_count), formatNumber(value.referencing_model_count))}
        </span>
        <span className="text-[11px] text-muted-foreground">
          {copy.refsEnabled(formatNumber(value.enabled_reference_count))}
        </span>
        {summaryState.status === "stale" ? (
          <span className="text-[11px] text-degraded">{copy.referencesMayBeStale}</span>
        ) : null}
      </span>
    )
  })()

  const canExpand = Boolean(summaryState && (summaryState.status === "ready" || summaryState.status === "stale"))

  // The failure is this row's, so its recovery is this row's too — and it
  // replaces the expand control rather than nesting inside it.
  if (summaryState?.status === "error") {
    return (
      <TableCell>
        <div className="flex flex-col items-start gap-1">
          <span className="text-xs text-failing" title={messages.endpointsPage.referenceUnknownRowReason}>
            {messages.endpointsPage.referenceUnknownRow}
          </span>
          <Button type="button" variant="outline" size="xs" onClick={onRetryRow}>
            {messages.endpointsPage.referenceRetryRow}
          </Button>
        </div>
      </TableCell>
    )
  }

  return (
    <TableCell>
      <button
        type="button"
        disabled={!canExpand}
        aria-expanded={expanded}
        aria-controls={`endpoint-references-${endpoint.id}`}
        aria-label={messages.endpointsUi.openReferences(
          endpoint.name,
          summaryFor(summaryState)?.direct_reference_count == null
            ? messages.endpoints.referencesLoading
            : String(summaryFor(summaryState)?.direct_reference_count),
        )}
        className={cn(
          "flex min-w-0 items-center gap-1 text-left disabled:cursor-default",
          canExpand && "hover:text-foreground",
        )}
        onClick={canExpand ? onOpen : undefined}
      >
        {canExpand ? expanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" /> : null}
        <span className="min-w-0">{content}</span>
      </button>
    </TableCell>
  )
}

type ReferenceFlag = { intent: OperatorStatusTier; label: string }

/** Backend flag keys never reach the screen; each becomes a labelled tier. */
function referenceFlags(
  item: EndpointReferenceItem,
  copy: ReturnType<typeof useLocale>["messages"]["endpointsUi"],
): ReferenceFlag[] {
  const flags: ReferenceFlag[] = []
  if (item.kind === "owned_terminal_target") {
    if (item.owner_model) {
      flags.push(
        item.owner_model.is_enabled
          ? { intent: "healthy", label: copy.flagModelEnabled }
          : { intent: "idle", label: copy.flagModelDisabled },
      )
    }
    if (item.access_target) {
      flags.push(
        item.access_target.is_enabled
          ? { intent: "healthy", label: copy.flagTargetEnabled }
          : { intent: "idle", label: copy.flagTargetDisabled },
      )
    }
  }
  flags.push(
    item.connection_is_active
      ? { intent: "healthy", label: copy.flagConnectionActive }
      : { intent: "degraded", label: copy.flagConnectionInactive },
  )
  return flags
}
function ReferenceDisclosureRow({
  endpoint,
  detailState,
  onLoadMore,
  onOrphanCleanup,
}: {
  endpoint: Endpoint
  detailState: EndpointReferenceDetailState | undefined
  onLoadMore: (endpointId: number) => void
  onOrphanCleanup: (endpoint: Endpoint, item: EndpointReferenceItem) => void
}) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi

  return (
    <TableRow id={`endpoint-references-${endpoint.id}`} data-testid={`endpoint-references-${endpoint.id}`}>
      {/* Six, matching the header. Chrome clamps an over-wide colspan, so the
          seventh was invisible, but it still misreports the row's span to
          assistive technology. */}
      <TableCell colSpan={6} className="bg-inset/60 p-0">
        <div className="px-4 py-3">
          {detailState?.status === "loading" ? (
            <div role="status" className="flex items-center gap-2 text-xs text-muted-foreground">
              <Loader2 className="size-3 animate-spin" />
              {copy.deleteChecking}
            </div>
          ) : detailState?.status === "error" || detailState?.status === "stale" ? (
            <OperatorCallout intent="warning" title={copy.deleteCheckError} description={messages.endpoints.referencesMayBeStale} />
          ) : detailState && detailState.status === "ready" ? (
            <div className="flex flex-col gap-3">
              <p className="text-xs text-muted-foreground">
                {copy.loadedItemsOfTotal(String(detailState.value.loaded_items.length), String(detailState.value.total_count))}
              </p>
              <div className="grid gap-2 lg:grid-cols-2">
                {detailState.value.loaded_items.map((item) => (
                  <OperatorInsetPanel key={item.connection_id} data-testid={`reference-row-${item.connection_id}`}>
                    <div className="flex min-w-0 items-start justify-between gap-2">
                      <div className="flex min-w-0 flex-col gap-0.5">
                        <span className="truncate text-[0.8125rem] font-medium">
                          {item.kind === "orphan_connection"
                            ? copy.orphanRowLabel(String(item.connection_id))
                            : (item.owner_model?.display_name ?? item.owner_model?.model_id ?? copy.referenceModelLabel)}
                        </span>
                        {item.owner_model ? (
                          <code className="truncate font-mono text-[11px] text-muted-foreground">
                            {item.owner_model.model_id}
                          </code>
                        ) : null}
                      </div>
                      <div className="flex shrink-0 items-center gap-1">
                        {item.kind === "orphan_connection" ? (
                          <Button type="button" size="sm" variant="outline" onClick={() => onOrphanCleanup(endpoint, item)}>
                            {copy.orphanCleanup}
                          </Button>
                        ) : item.owner_model ? (
                          <Button type="button" size="sm" variant="ghost" asChild>
                            <a href={`/route/models/${item.owner_model.id}`} data-testid={`model-link-${item.owner_model.id}`}>
                              <ExternalLink className="size-3.5" />
                              {copy.modelLink}
                            </a>
                          </Button>
                        ) : null}
                      </div>
                    </div>

                    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
                      <dt className="text-muted-foreground">{copy.referenceTargetLabel}</dt>
                      <dd className="min-w-0 truncate font-mono">
                        {item.terminal_target_name ?? `#${item.terminal_target_id}`}
                      </dd>
                      <dt className="text-muted-foreground">{copy.referenceProtocolLabel}</dt>
                      <dd className="min-w-0 truncate font-mono">
                        {item.api_family}
                        {item.openai_text_capability ? ` · ${item.openai_text_capability}` : ""}
                      </dd>
                      <dt className="text-muted-foreground">{copy.referencePricingLabel}</dt>
                      <dd className="min-w-0 truncate">
                        {item.pricing_template ? (
                          <>
                            {item.pricing_template.name}
                            <span className="ml-1 font-mono text-[11px]">v{item.pricing_template.current_version}</span>
                          </>
                        ) : (
                          <OperatorMissingValue reason={copy.pricingNotSet} />
                        )}
                      </dd>
                      <dt className="text-muted-foreground">{copy.referenceStateLabel}</dt>
                      <dd className="flex min-w-0 flex-wrap gap-1">
                        {item.enabled ? null : (
                          <span className="text-degraded">
                            {item.inactive_reasons.map((reason) => reasonLabel(copy, reason)).join(" · ")}
                          </span>
                        )}
                        {referenceFlags(item, copy).map((flag) => (
                          <OperatorStatusBadge
                            key={flag.label}
                            intent={flag.intent}
                            label={flag.label}
                            preserveLabel
                          />
                        ))}
                      </dd>
                    </dl>
                  </OperatorInsetPanel>
                ))}
              </div>
              {detailState.value.next_cursor ? (
                <Button type="button" size="sm" variant="outline" onClick={() => onLoadMore(endpoint.id)}>
                  {copy.loadMore}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      </TableCell>
    </TableRow>
  )
}

function reasonLabel(copy: Record<string, string | ((...args: never[]) => string)>, reason: string): string {
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
      return reason
  }
}

function MobileReferenceDisclosure({
  endpoint,
  detailState,
  onLoadMore,
  onOrphanCleanup,
}: {
  endpoint: Endpoint
  detailState: EndpointReferenceDetailState | undefined
  onLoadMore: (endpointId: number) => void
  onOrphanCleanup: (endpoint: Endpoint, item: EndpointReferenceItem) => void
}) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  if (detailState?.status === "loading") {
    return (
      <div role="status" className="flex items-center gap-2 px-3 py-2 text-xs text-muted-foreground">
        <Loader2 className="size-3 animate-spin" />
        {copy.deleteChecking}
      </div>
    )
  }
  if (detailState?.status === "error" || detailState?.status === "stale") {
    return (
      <div className="px-3 py-2">
        <OperatorCallout intent="warning" title={copy.deleteCheckError} description={messages.endpoints.referencesMayBeStale} />
      </div>
    )
  }
  if (!detailState || detailState.status !== "ready") return null
  return (
    <div className="divide-y divide-border">
      <p className="px-3 py-2 text-xs text-muted-foreground">
        {copy.loadedItemsOfTotal(String(detailState.value.loaded_items.length), String(detailState.value.total_count))}
      </p>
      {detailState.value.loaded_items.map((item) => (
        <dl key={item.connection_id} className="flex flex-col gap-1 px-3 py-2 text-xs" data-testid={`reference-row-${item.connection_id}`}>
          <dt className="sr-only">{copy.referenceModelLabel}</dt>
          <dd className="font-medium text-foreground">
            {item.kind === "orphan_connection"
              ? copy.orphanRowLabel(String(item.connection_id))
              : item.owner_model?.display_name ?? item.owner_model?.model_id ?? ""}
          </dd>
          <dt className="sr-only">{copy.referenceTargetLabel}</dt>
          <dd className="text-muted-foreground">{item.terminal_target_name ?? `#${item.terminal_target_id}`}</dd>
          <dt className="sr-only">{copy.referenceStateLabel}</dt>
          <dd className="text-muted-foreground">
            {item.enabled ? copy.enabled : item.inactive_reasons.map((reason) => reasonLabel(copy, reason)).join(" · ")}
          </dd>
          <dt className="sr-only">{copy.referenceProtocolLabel}</dt>
          <dd className="font-mono text-muted-foreground">{item.api_family}{item.openai_text_capability ? ` · ${item.openai_text_capability}` : ""}</dd>
          {item.kind === "orphan_connection" ? (
            <dd>
              <Button type="button" size="sm" variant="outline" onClick={() => onOrphanCleanup(endpoint, item)}>
                {copy.orphanCleanup}
              </Button>
            </dd>
          ) : null}
        </dl>
      ))}
      {detailState.value.next_cursor ? (
        <div className="px-3 py-2">
          <Button type="button" size="sm" variant="outline" onClick={() => onLoadMore(endpoint.id)}>
            {copy.loadMore}
          </Button>
        </div>
      ) : null}
    </div>
  )
}

export { MobileReferenceDisclosure, ReferenceCell, ReferenceDisclosureRow }
