import { useMemo, useState } from "react"
import { ChevronDown, ChevronRight, Copy, ExternalLink, Loader2, Pencil, Plus, Trash2 } from "lucide-react"

import { IconActionButton, IconActionGroup } from "@/components/IconActionGroup"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useLocale } from "@/i18n/useLocale"
import { formatNumber } from "@/i18n/format"
import type { Endpoint, EndpointReferenceItem, EndpointReferenceSummary } from "@/lib/types"
import { copyTextToClipboard } from "@/lib/clipboard"
import { cn } from "@/lib/utils"
import { OperatorCallout } from "@/shared/design-system"
import { SortableTableHead, type OperationalSortState } from "@/shared/table/operationalTable"
import { type EndpointReferenceDetailState, type EndpointReferenceSummaryState } from "./useEndpointReferences"

export type EndpointSortColumn = "name" | "updated_at" | "direct_reference_count"

type EndpointTableProps = {
  endpoints: Endpoint[]
  details: Record<number, EndpointReferenceDetailState>
  filterDisabled: boolean
  formatTime: (isoString: string, options?: Intl.DateTimeFormatOptions) => string
  hasIntegrityError: boolean
  onAttach: (endpoint: Endpoint) => void
  onDelete: (endpoint: Endpoint) => void
  onDuplicate: (endpoint: Endpoint) => void
  onEdit: (endpoint: Endpoint) => void
  onLoadMore: (endpointId: number) => void
  onOpenReferences: (endpointId: number) => void
  onOrphanCleanup: (endpoint: Endpoint, item: EndpointReferenceItem) => void
  sort: OperationalSortState<EndpointSortColumn>
  summaries: Record<number, EndpointReferenceSummaryState>
  onSort: (column: EndpointSortColumn) => void
}

function summaryFor(summary: EndpointReferenceSummaryState | undefined): EndpointReferenceSummary | null {
  if (!summary) return null
  if (summary.status === "ready" || summary.status === "stale") return summary.value
  return null
}

function ReferenceCell({
  endpoint,
  summaryState,
  detailState,
  onOpen,
}: {
  endpoint: Endpoint
  summaryState: EndpointReferenceSummaryState | undefined
  detailState: EndpointReferenceDetailState | undefined
  onOpen: () => void
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
      return <span className="text-xs text-destructive">{copy.referencesLoadFailed}</span>
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
      </span>
    )
  })()

  const canExpand = Boolean(summaryState && (summaryState.status === "ready" || summaryState.status === "stale"))

  return (
    <TableCell>
      <button
        type="button"
        disabled={!canExpand}
        aria-expanded={expanded}
        aria-controls={`endpoint-references-${endpoint.id}`}
        aria-label={messages.endpointsUi.openReferences(endpoint.name, String(summaryFor(summaryState)?.direct_reference_count ?? 0))}
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

function KeyIdentityCell({ endpoint, formatTime }: { endpoint: Endpoint; formatTime: EndpointTableProps["formatTime"] }) {
  const { messages } = useLocale()
  const copy = messages.endpoints
  if (!endpoint.has_api_key) {
    return (
      <TableCell>
        <span className="text-xs text-muted-foreground">{copy.noApiKey}</span>
      </TableCell>
    )
  }
  return (
    <TableCell>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="font-mono text-xs text-foreground">{endpoint.api_key_fingerprint ?? "—"}</span>
        <span className="text-[11px] text-muted-foreground">
          {endpoint.api_key_updated_at ? copy.keyUpdatedAt(formatTime(endpoint.api_key_updated_at, { year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })) : copy.keyUpdatedUnknown}
        </span>
      </div>
    </TableCell>
  )
}

function BaseURLCell({ endpoint }: { endpoint: Endpoint }) {
  const { messages } = useLocale()
  const [copied, setCopied] = useState(false)
  return (
    <TableCell>
      <div className="flex min-w-0 items-center gap-1">
        <code
          tabIndex={0}
          title={endpoint.base_url}
          className="block min-w-0 flex-1 truncate rounded border border-transparent px-1 py-0.5 font-mono text-xs text-foreground/90 focus-visible:outline-2 focus-visible:outline-ring"
          aria-label={`${messages.endpoints.baseUrl}: ${endpoint.base_url}`}
        >
          {endpoint.base_url}
        </code>
        <IconActionButton
          type="button"
          size="icon"
          className="size-6"
          aria-label={`${messages.endpoints.baseUrl}: ${endpoint.base_url} — 复制`}
          title="复制"
          onClick={() => {
            void copyTextToClipboard(endpoint.base_url)
            setCopied(true)
            window.setTimeout(() => setCopied(false), 1200)
          }}
        >
          {copied ? <Badge variant="outline" className="h-4 px-1 text-[10px]">✓</Badge> : <Copy />}
        </IconActionButton>
      </div>
    </TableCell>
  )
}

function referenceFlags(item: EndpointReferenceItem): string[] {
  const flags: string[] = []
  if (item.kind === "owned_terminal_target") {
    if (item.owner_model) {
      flags.push(item.owner_model.is_enabled ? "model-enabled" : "model-disabled")
    }
    if (item.access_target) {
      flags.push(item.access_target.is_enabled ? "target-enabled" : "target-disabled")
    }
  }
  flags.push(item.connection_is_active ? "connection-active" : "connection-inactive")
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
      <TableCell colSpan={7} className="bg-surface-container-low/60 p-0">
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
              <div className="overflow-hidden rounded-lg border border-outline-variant">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="text-xs">模型</TableHead>
                      <TableHead className="text-xs">终端目标</TableHead>
                      <TableHead className="text-xs">状态</TableHead>
                      <TableHead className="text-xs">协议</TableHead>
                      <TableHead className="text-xs">价格模板</TableHead>
                      <TableHead className="text-right text-xs">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {detailState.value.loaded_items.map((item) => (
                      <TableRow key={item.connection_id} data-testid={`reference-row-${item.connection_id}`}>
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
                          <div className="flex flex-col gap-1 text-[11px] text-muted-foreground">
                            <span className={cn("inline-flex items-center gap-1", item.enabled && "text-success")}>
                              {item.enabled ? copy.enabled : item.inactive_reasons.map((reason) => reasonLabel(copy, reason)).join(" · ")}
                            </span>
                            {referenceFlags(item).length > 0 ? <span className="font-mono text-[10px]">{referenceFlags(item).join(" / ")}</span> : null}
                          </div>
                        </TableCell>
                        <TableCell>
                          <span className="font-mono text-xs text-foreground">{item.api_family}{item.openai_text_capability ? ` · ${item.openai_text_capability}` : ""}</span>
                        </TableCell>
                        <TableCell>
                          {item.pricing_template ? (
                            <span className="text-xs text-muted-foreground">
                              {item.pricing_template.name}
                              <span className="ml-1 font-mono text-[11px]">v{item.pricing_template.current_version}</span>
                            </span>
                          ) : (
                            <span className="text-xs text-muted-foreground">{copy.pricingNotSet}</span>
                          )}
                        </TableCell>
                        <TableCell className="text-right">
                          {item.kind === "orphan_connection" ? (
                            <Button type="button" size="sm" variant="outline" onClick={() => onOrphanCleanup(endpoint, item)}>
                              {copy.orphanCleanup}
                            </Button>
                          ) : item.owner_model ? (
                            <Button type="button" size="sm" variant="ghost" asChild>
                              <a href={`/models/${item.owner_model.id}`} data-testid={`model-link-${item.owner_model.id}`}>
                                <ExternalLink className="size-3.5" />
                                {copy.modelLink}
                              </a>
                            </Button>
                          ) : null}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
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

export function EndpointTable({
  endpoints,
  details,
  filterDisabled,
  formatTime,
  hasIntegrityError,
  onAttach,
  onDelete,
  onDuplicate,
  onEdit,
  onLoadMore,
  onOpenReferences,
  onOrphanCleanup,
  onSort,
  sort,
  summaries,
}: EndpointTableProps) {
  const { messages } = useLocale()
  const copy = messages.endpoints
  const uiCopy = messages.endpointsUi
  const [expandedIds, setExpandedIds] = useState<Set<number>>(new Set())

  const toggleExpanded = (endpointId: number) => {
    setExpandedIds((current) => {
      const next = new Set(current)
      if (next.has(endpointId)) {
        next.delete(endpointId)
      } else {
        next.add(endpointId)
        onOpenReferences(endpointId)
      }
      return next
    })
  }

  const sortable = useMemo(() => !filterDisabled, [filterDisabled])

  return (
    <div data-testid="endpoints-table" data-table-density="compact" className="overflow-hidden border-t border-outline-variant">
      {hasIntegrityError ? (
        <div className="border-b border-outline-variant px-4 py-3">
          <OperatorCallout intent="danger" title={uiCopy.deleteIntegrityError} />
        </div>
      ) : null}
      {/* Narrow viewport: semantic description-list row cards (no horizontal
          table scroll). The desktop table is hidden below sm. */}
      <div className="divide-y divide-outline-variant sm:hidden" data-testid="endpoints-mobile-cards">
        {endpoints.map((endpoint) => (
          <MobileEndpointCard
            key={endpoint.id}
            endpoint={endpoint}
            detailState={details[endpoint.id]}
            expanded={expandedIds.has(endpoint.id)}
            formatTime={formatTime}
            onAttach={onAttach}
            onDelete={onDelete}
            onDuplicate={onDuplicate}
            onEdit={onEdit}
            onOpenReferences={() => toggleExpanded(endpoint.id)}
            onOrphanCleanup={onOrphanCleanup}
            summaryState={summaries[endpoint.id]}
          />
        ))}
      </div>
      <div className="hidden sm:block" data-testid="endpoints-table-desktop">
      <Table>
        <TableHeader>
          <TableRow>
            <SortableTableHead sortKey="name" sort={sort} onSort={onSort}>{copy.name}</SortableTableHead>
            <TableHead className="hidden lg:table-cell">{copy.baseUrl}</TableHead>
            <TableHead className="hidden sm:table-cell">{copy.apiKey}</TableHead>
            <SortableTableHead sortKey="direct_reference_count" sort={sort} onSort={sortable ? onSort : () => {}}>{copy.directReferences}</SortableTableHead>
            <SortableTableHead sortKey="updated_at" sort={sort} onSort={onSort}>{copy.lastModified}</SortableTableHead>
            <TableHead className="text-right">{messages.endpoints.actions}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {endpoints.map((endpoint) => {
            const expanded = expandedIds.has(endpoint.id)
            const detailState = details[endpoint.id]
            return (
              <EndpointRowGroup
                key={endpoint.id}
                endpoint={endpoint}
                detailState={detailState}
                expanded={expanded}
                formatTime={formatTime}
                onAttach={onAttach}
                onDelete={onDelete}
                onDuplicate={onDuplicate}
                onEdit={onEdit}
                onLoadMore={onLoadMore}
                onOpenReferences={() => toggleExpanded(endpoint.id)}
                onOrphanCleanup={onOrphanCleanup}
                summaryState={summaries[endpoint.id]}
              />
            )
          })}
        </TableBody>
      </Table>
      </div>
    </div>
  )
}

function EndpointRowGroup({
  endpoint,
  detailState,
  expanded,
  formatTime,
  onAttach,
  onDelete,
  onDuplicate,
  onEdit,
  onLoadMore,
  onOpenReferences,
  onOrphanCleanup,
  summaryState,
}: {
  endpoint: Endpoint
  detailState: EndpointReferenceDetailState | undefined
  expanded: boolean
  formatTime: EndpointTableProps["formatTime"]
  onAttach: (endpoint: Endpoint) => void
  onDelete: (endpoint: Endpoint) => void
  onDuplicate: (endpoint: Endpoint) => void
  onEdit: (endpoint: Endpoint) => void
  onLoadMore: (endpointId: number) => void
  onOpenReferences: () => void
  onOrphanCleanup: (endpoint: Endpoint, item: EndpointReferenceItem) => void
  summaryState: EndpointReferenceSummaryState | undefined
}) {
  const { messages } = useLocale()
  const copy = messages.endpointsUi
  const pageCopy = messages.endpointsPage
  return (
    <>
      <TableRow data-testid={`endpoint-row-${endpoint.id}`} data-expanded={expanded ? "true" : undefined} className={cn(expanded && "bg-surface-container-low/40")}>
        <TableCell>
          <div className="flex min-w-0 flex-col gap-0.5">
            <span className="truncate text-sm font-medium text-foreground" title={endpoint.name}>{endpoint.name}</span>
            <span className="truncate font-mono text-[11px] text-muted-foreground lg:hidden" title={endpoint.base_url}>{endpoint.base_url}</span>
            <span className="text-[11px] text-muted-foreground sm:hidden">{copy.created(formatTime(endpoint.created_at, { year: "numeric", month: "short", day: "numeric" }))}</span>
          </div>
        </TableCell>
        <TableCell className="hidden lg:table-cell"><BaseURLCell endpoint={endpoint} /></TableCell>
        <TableCell className="hidden sm:table-cell"><KeyIdentityCell endpoint={endpoint} formatTime={formatTime} /></TableCell>
        <ReferenceCell
          endpoint={endpoint}
          summaryState={summaryState}
          detailState={detailState}
          onOpen={onOpenReferences}
        />
        <TableCell>
          <time dateTime={endpoint.updated_at} className="text-xs text-muted-foreground">
            {formatTime(endpoint.updated_at, { year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}
          </time>
        </TableCell>
        <TableCell className="text-right">
          <IconActionGroup className="justify-end">
            <IconActionButton type="button" size="icon" aria-label={`${pageCopy.attachToModel}: ${endpoint.name}`} title={pageCopy.attachToModel} onClick={() => onAttach(endpoint)}>
              <Plus />
            </IconActionButton>
            <IconActionButton type="button" size="icon" aria-label={copy.duplicateEndpoint(endpoint.name)} onClick={() => onDuplicate(endpoint)}>
              <Copy />
            </IconActionButton>
            <IconActionButton type="button" size="icon" aria-label={copy.editEndpoint(endpoint.name)} onClick={() => onEdit(endpoint)}>
              <Pencil />
            </IconActionButton>
            <IconActionButton type="button" size="icon" aria-label={copy.deleteEndpointDescription(endpoint.name)} destructive onClick={() => onDelete(endpoint)}>
              <Trash2 />
            </IconActionButton>
          </IconActionGroup>
        </TableCell>
      </TableRow>
      {expanded ? (
        <ReferenceDisclosureRow
          endpoint={endpoint}
          detailState={detailState}
          onLoadMore={onLoadMore}
          onOrphanCleanup={onOrphanCleanup}
        />
      ) : null}
    </>
  )
}

function MobileEndpointCard({
  endpoint,
  detailState,
  expanded,
  formatTime,
  onAttach,
  onDelete,
  onDuplicate,
  onEdit,
  onOpenReferences,
  onOrphanCleanup,
  summaryState,
}: {
  endpoint: Endpoint
  detailState: EndpointReferenceDetailState | undefined
  expanded: boolean
  formatTime: EndpointTableProps["formatTime"]
  onAttach: (endpoint: Endpoint) => void
  onDelete: (endpoint: Endpoint) => void
  onDuplicate: (endpoint: Endpoint) => void
  onEdit: (endpoint: Endpoint) => void
  onOpenReferences: () => void
  onOrphanCleanup: (endpoint: Endpoint, item: EndpointReferenceItem) => void
  summaryState: EndpointReferenceSummaryState | undefined
}) {
  const { messages } = useLocale()
  const copy = messages.endpoints
  const uiCopy = messages.endpointsUi
  const pageCopy = messages.endpointsPage
  const summary = summaryFor(summaryState)

  return (
    <article className="flex flex-col gap-3 px-4 py-3" data-testid={`endpoint-mobile-card-${endpoint.id}`}>
      <dl className="flex flex-col gap-2">
        <div className="flex items-start justify-between gap-2">
          <dt className="sr-only">{copy.name}</dt>
          <dd className="min-w-0 flex-1 truncate text-sm font-medium text-foreground">{endpoint.name}</dd>
          <IconActionGroup className="shrink-0">
            <IconActionButton type="button" size="icon" aria-label={`${pageCopy.attachToModel}: ${endpoint.name}`} title={pageCopy.attachToModel} onClick={() => onAttach(endpoint)}>
              <Plus />
            </IconActionButton>
            <IconActionButton type="button" size="icon" aria-label={uiCopy.duplicateEndpoint(endpoint.name)} onClick={() => onDuplicate(endpoint)}>
              <Copy />
            </IconActionButton>
            <IconActionButton type="button" size="icon" aria-label={uiCopy.editEndpoint(endpoint.name)} onClick={() => onEdit(endpoint)}>
              <Pencil />
            </IconActionButton>
            <IconActionButton type="button" size="icon" aria-label={uiCopy.deleteEndpointDescription(endpoint.name)} destructive onClick={() => onDelete(endpoint)}>
              <Trash2 />
            </IconActionButton>
          </IconActionGroup>
        </div>
        <div className="flex flex-col gap-1">
          <dt className="sr-only">{copy.baseUrl}</dt>
          <dd className="truncate font-mono text-xs text-muted-foreground" title={endpoint.base_url}>{endpoint.base_url}</dd>
        </div>
        <div className="flex flex-col gap-1">
          <dt className="sr-only">{copy.apiKey}</dt>
          <dd className="font-mono text-xs text-foreground">
            {endpoint.has_api_key ? (endpoint.api_key_fingerprint ?? "—") : copy.noApiKey}
          </dd>
        </div>
        <div className="flex flex-col gap-1">
          <dt className="sr-only">{copy.directReferences}</dt>
          <dd>
            <button
              type="button"
              disabled={!summary}
              aria-expanded={expanded}
              aria-controls={`endpoint-references-${endpoint.id}`}
              aria-label={uiCopy.openReferences(endpoint.name, String(summary?.direct_reference_count ?? 0))}
              className="flex min-w-0 items-center gap-1 text-left disabled:cursor-default"
              onClick={summary ? onOpenReferences : undefined}
            >
              {summary ? (expanded ? <ChevronDown className="size-3.5 shrink-0 text-muted-foreground" /> : <ChevronRight className="size-3.5 shrink-0 text-muted-foreground" />) : null}
              {summary?.direct_reference_count === 0 ? (
                <span className="text-xs text-muted-foreground">{copy.refsZero}</span>
              ) : summary ? (
                <span className="flex flex-col gap-0.5">
                  <span className="text-xs font-medium text-foreground">{copy.refsSummary(formatNumber(summary.direct_reference_count), formatNumber(summary.referencing_model_count))}</span>
                  <span className="text-[11px] text-muted-foreground">{copy.refsEnabled(formatNumber(summary.enabled_reference_count))}</span>
                </span>
              ) : (
                <span className="text-xs text-muted-foreground">{copy.referencesLoading}</span>
              )}
            </button>
          </dd>
        </div>
        <div className="flex flex-col gap-1">
          <dt className="sr-only">{copy.lastModified}</dt>
          <dd>
            <time dateTime={endpoint.updated_at} className="text-xs text-muted-foreground">
              {formatTime(endpoint.updated_at, { year: "numeric", month: "short", day: "numeric", hour: "numeric", minute: "2-digit" })}
            </time>
          </dd>
        </div>
      </dl>
      {expanded ? (
        <div className="overflow-hidden rounded-lg border border-outline-variant">
          <MobileReferenceDisclosure
            endpoint={endpoint}
            detailState={detailState}
            onOrphanCleanup={onOrphanCleanup}
          />
        </div>
      ) : null}
    </article>
  )
}

function MobileReferenceDisclosure({
  endpoint,
  detailState,
  onOrphanCleanup,
}: {
  endpoint: Endpoint
  detailState: EndpointReferenceDetailState | undefined
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
    <div className="divide-y divide-outline-variant">
      <p className="px-3 py-2 text-xs text-muted-foreground">
        {copy.loadedItemsOfTotal(String(detailState.value.loaded_items.length), String(detailState.value.total_count))}
      </p>
      {detailState.value.loaded_items.map((item) => (
        <dl key={item.connection_id} className="flex flex-col gap-1 px-3 py-2 text-xs" data-testid={`reference-row-${item.connection_id}`}>
          <dt className="sr-only">模型</dt>
          <dd className="font-medium text-foreground">
            {item.kind === "orphan_connection"
              ? copy.orphanRowLabel(String(item.connection_id))
              : item.owner_model?.display_name ?? item.owner_model?.model_id ?? ""}
          </dd>
          <dt className="sr-only">终端目标</dt>
          <dd className="text-muted-foreground">{item.terminal_target_name ?? `#${item.terminal_target_id}`}</dd>
          <dt className="sr-only">状态</dt>
          <dd className="text-muted-foreground">
            {item.enabled ? copy.enabled : item.inactive_reasons.map((reason) => reasonLabel(copy, reason)).join(" · ")}
          </dd>
          <dt className="sr-only">协议</dt>
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
          <Button type="button" size="sm" variant="outline" onClick={() => {}}>
            {copy.loadMore}
          </Button>
        </div>
      ) : null}
    </div>
  )
}

