import { Link } from "@tanstack/react-router"

import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { useLocale } from "@/i18n/useLocale"
import { useTimezone } from "@/hooks/useTimezone"
import type { PricingTemplate, PricingTemplateConnectionUsageItem, PricingTemplateRevision } from "@/lib/types"
import { cn } from "@/lib/utils"
import {
  OperatorErrorState,
  OperatorInsetPanel,
  OperatorMissingValue,
  OperatorRetryButton,
  OperatorStatusBadge,
  OperatorValueBadge,
} from "@/shared/design-system"
import { normalizeTemplatePrice } from "./pricingSchemas"

function RateCell({
  symbol,
  value,
  specialty,
}: {
  symbol?: string
  value: string | null | undefined
  specialty?: boolean
}) {
  const { messages } = useLocale()
  const copy = messages.pricingTemplatesUi
  const normalized = normalizeTemplatePrice(value)

  if (normalized === "") {
    if (!specialty) return <OperatorMissingValue className="text-xs" />
    return (
      <span className="inline-flex items-center justify-end gap-1">
        <OperatorMissingValue className="text-xs" reason={copy.rateUnconfiguredReason} />
        <OperatorStatusBadge intent="idle" preserveLabel label={copy.rateUnconfigured} />
      </span>
    )
  }

  return (
    <span className="font-mono text-xs tabular-nums">
      {/* A one-character symbol hugs the number ($1.50); a currency code needs
          a gap so it does not read as part of the digits (USD 1.50). */}
      {symbol ? (
        <span className={cn("text-muted-foreground", symbol.length > 1 && "mr-1")}>{symbol}</span>
      ) : null}
      {normalized}
    </span>
  )
}
function UsagePanel({
  error,
  loading,
  onRetry,
  rows,
}: {
  error: string | null
  loading: boolean
  onRetry: () => void
  rows: PricingTemplateConnectionUsageItem[]
}) {
  const { messages } = useLocale()
  const copy = messages.pricingTemplatesUi

  if (loading) return <Skeleton className="h-20 rounded-md" />
  if (error) {
    return (
      <OperatorErrorState
        title={messages.pricingTemplatesData.loadUsageFailed}
        description={error}
        action={<OperatorRetryButton onClick={onRetry}>{messages.common.retry}</OperatorRetryButton>}
      />
    )
  }
  if (rows.length === 0) {
    return <OperatorInsetPanel><p className="text-xs text-muted-foreground">{copy.templateUnused}</p></OperatorInsetPanel>
  }

  return (
    <OperatorInsetPanel className="p-0">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{copy.model}</TableHead>
            <TableHead>{copy.endpoint}</TableHead>
            <TableHead>{copy.terminalTargetColumn}</TableHead>
            <TableHead className="text-right">{copy.actions}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row) => (
            <TableRow key={`${row.connection_id}-${row.model_config_id}`}>
              <TableCell>
                <Link
                  to="/route/models/$modelId"
                  params={{ modelId: String(row.model_config_id) }}
                  aria-label={copy.openModel(row.model_id)}
                  className="font-mono text-xs underline-offset-2 hover:underline"
                >
                  {row.model_id}
                </Link>
              </TableCell>
              <TableCell>
                <Link
                  to="/route/endpoints"
                  aria-label={copy.openEndpoint(row.endpoint_name)}
                  className="text-xs underline-offset-2 hover:underline"
                >
                  {row.endpoint_name}
                </Link>
              </TableCell>
              <TableCell className="text-xs">{row.connection_name ?? copy.unnamed}</TableCell>
              <TableCell className="text-right">
                <Button asChild type="button" variant="outline" size="sm">
                  <Link to="/route/models/$modelId" params={{ modelId: String(row.model_config_id) }}>
                    {copy.rebindToOtherTemplate}
                  </Link>
                </Button>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </OperatorInsetPanel>
  )
}

function TierPanel({ template }: { template: PricingTemplate }) {
  const { messages } = useLocale()
  const copy = messages.pricingTemplatesUi
  if (!template.tier) {
    return <OperatorInsetPanel><p className="text-xs text-muted-foreground">{copy.tierUnconfiguredReason}</p></OperatorInsetPanel>
  }
  const tier = template.tier
  return (
    <OperatorInsetPanel>
      <div className="flex flex-col gap-1">
        <p className="text-sm font-medium text-foreground">{copy.tierDetailsTitle}</p>
        <p className="text-xs text-muted-foreground">{copy.tierDetailsDescription(tier.input_tokens_above)}</p>
      </div>
      <div className="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-5">
        <div><p className="text-xs text-muted-foreground">{copy.rateInput}</p><RateCell symbol={template.active_currency_symbol} value={tier.input_price} /></div>
        <div><p className="text-xs text-muted-foreground">{copy.rateOutput}</p><RateCell symbol={template.active_currency_symbol} value={tier.output_price} /></div>
        <div><p className="text-xs text-muted-foreground">{copy.rateCachedInput}</p><RateCell specialty symbol={template.active_currency_symbol} value={tier.cached_input_price} /></div>
        <div><p className="text-xs text-muted-foreground">{copy.rateCacheCreation}</p><RateCell specialty symbol={template.active_currency_symbol} value={tier.cache_creation_price} /></div>
        <div><p className="text-xs text-muted-foreground">{copy.rateReasoning}</p><RateCell specialty symbol={template.active_currency_symbol} value={tier.reasoning_price} /></div>
      </div>
    </OperatorInsetPanel>
  )
}

/**
 * Revision history shows all five rates, not just the two base ones, and marks
 * which of them actually changed between consecutive versions.
 */
function HistoryPanel({ loading, revisions }: { loading: boolean; revisions: PricingTemplateRevision[] }) {
  const { messages } = useLocale()
  const { format: formatTime } = useTimezone()
  const copy = messages.pricingTemplatesUi
  const historyCopy = messages.pricingTemplatesHistory

  if (loading) return <Skeleton className="h-20 rounded-md" />
  if (revisions.length === 0) {
    return <OperatorInsetPanel><p className="text-xs text-muted-foreground">{historyCopy.empty}</p></OperatorInsetPanel>
  }

  return (
    <OperatorInsetPanel className="p-0">
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{copy.columnVersion}</TableHead>
              <TableHead className="text-right">{copy.rateInput}</TableHead>
              <TableHead className="text-right">{copy.rateOutput}</TableHead>
              <TableHead className="text-right">{copy.rateCachedInput}</TableHead>
              <TableHead className="text-right">{copy.rateCacheCreation}</TableHead>
              <TableHead className="text-right">{copy.rateReasoning}</TableHead>
              <TableHead>{historyCopy.effectiveAt}</TableHead>
              <TableHead>{historyCopy.createdBy}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {revisions.map((revision, index) => {
              const previous = revisions[index + 1]
              const changed = (field: keyof PricingTemplateRevision) =>
                previous ? normalizeTemplatePrice(String(revision[field] ?? "")) !== normalizeTemplatePrice(String(previous[field] ?? "")) : false
              return (
                <TableRow key={revision.revision_id}>
                  <TableCell>
                    <OperatorValueBadge label={`v${revision.version}`} className="text-xs" />
                  </TableCell>
                  <HistoryRate symbol={revision.currency_code} changed={changed("input_price")} value={revision.input_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("output_price")} value={revision.output_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("cached_input_price")} specialty value={revision.cached_input_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("cache_creation_price")} specialty value={revision.cache_creation_price} />
                  <HistoryRate symbol={revision.currency_code} changed={changed("reasoning_price")} specialty value={revision.reasoning_price} />
                  <TableCell className="font-mono text-xs tabular-nums">
                    {revision.effective_at ? formatTime(revision.effective_at) : <OperatorMissingValue className="text-xs" />}
                  </TableCell>
                  <TableCell className="text-xs">
                    {historyCopy.createdByKind(revision.created_by_kind)}
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
      </div>
    </OperatorInsetPanel>
  )
}

function HistoryRate({
  changed,
  specialty,
  symbol,
  value,
}: {
  changed: boolean
  specialty?: boolean
  symbol?: string
  value: string | null
}) {
  return (
    <TableCell className={cn("text-right", changed && "bg-primary-soft/40")}>
      <RateCell specialty={specialty} symbol={symbol} value={value} />
    </TableCell>
  )
}

export { HistoryPanel, RateCell, TierPanel, UsagePanel }
