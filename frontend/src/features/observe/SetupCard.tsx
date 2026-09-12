import { useTimezone } from "@/hooks/useTimezone"
import { CheckCircle2, ChevronDown, ChevronRight, CircleAlert, Loader2, MinusCircle } from "lucide-react"
import type { FocusEvent, RefObject } from "react"
import type { SetupCoordinatorState, SetupFact } from "@/lib/types"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import {
  OperatorCallout,
  OperatorErrorState,
  OperatorLoadingState,
  OperatorSectionCard,
  OperatorStalenessBadge,
} from "@/shared/design-system"
import { useLocale } from "@/i18n/useLocale"

interface SetupCardProps {
  state: SetupCoordinatorState
  collapsed: boolean
  cardRef: RefObject<HTMLDivElement | null>
  onBlurCapture: (event: FocusEvent<HTMLDivElement>) => void
  onRetry: () => void
  onToggle: () => void
}

type SetupCopy = ReturnType<typeof useLocale>["messages"]["setup"]

function factStatus(facts: readonly SetupFact[], copy: SetupCopy): string {
  if (facts.some((fact) => fact.fetch_quality === "loading")) return copy.checking
  if (facts.some((fact) => fact.fetch_quality === "error")) return copy.error
  if (facts.some((fact) => fact.fetch_quality === "stale")) return copy.stale
  if (facts.length === 0 || facts.some((fact) => fact.fetch_quality === "unknown" || fact.result === null)) return copy.unknown
  if (facts.every((fact) => fact.result === "complete")) return copy.complete
  if (facts.every((fact) => fact.result === "skipped")) return copy.skipped
  return copy.incomplete
}

function TaskRow({ title, status, description, action, href }: {
  title: string
  status: string
  description: string
  action: string
  href: string
}) {
  const { messages } = useLocale()
  const copy = messages.setup
  const Icon = status === copy.checking ? Loader2
    : status === copy.error || status === copy.stale ? CircleAlert
      : status === copy.complete || status === copy.skipped ? CheckCircle2 : MinusCircle
  return (
    <li className="flex min-w-0 flex-wrap items-start gap-3 border-b border-border/70 py-3 last:border-b-0">
      <Icon className={cn("mt-0.5 size-4 shrink-0 text-muted-foreground", status === copy.checking && "animate-spin")} aria-hidden="true" />
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-1">
          <span className="font-medium">{title}</span>
          <span className="text-xs text-muted-foreground">{status}</span>
        </div>
        <p className="mt-1 text-xs text-muted-foreground">{description}</p>
      </div>
      <a href={href} className="inline-flex min-h-8 shrink-0 items-center gap-1 rounded-md px-2 text-xs font-medium text-primary underline-offset-4 hover:underline">
        {action}<ChevronRight className="size-3.5" aria-hidden="true" />
      </a>
    </li>
  )
}

function connectionDescription(facts: readonly SetupFact[], copy: SetupCopy): string {
  if (facts.some((fact) => fact.fetch_quality !== "fresh")) return copy.connectionUnknown
  const endpoint = facts.find((fact) => fact.id === "endpoints")
  const model = facts.find((fact) => fact.id === "models")
  const target = facts.find((fact) => fact.id === "terminal_targets")
  const strategy = facts.find((fact) => fact.id === "routing")
  if (endpoint?.result === "incomplete") return copy.connectionStart
  if (model?.result === "incomplete") return copy.connectionMissingModel
  if (target?.result === "incomplete") return copy.connectionMissingTarget
  if (strategy?.result === "incomplete") return copy.connectionMissingStrategy
  if (facts.length === 4 && facts.every((fact) => fact.result === "complete")) {
    return target?.detail === copy.scheduleLimited ? `${copy.connectionReady} ${copy.scheduleLimited}` : copy.connectionReady
  }
  return copy.connectionUnknown
}

export function SetupCard({ state, collapsed, cardRef, onBlurCapture, onRetry, onToggle }: SetupCardProps) {
  const { messages } = useLocale()
  const copy = messages.setup
  const { format } = useTimezone()
  const coreFacts = state.facts.filter((fact) => fact.kind === "required")
  const pricing = state.facts.filter((fact) => fact.id === "pricing")
  const client = state.facts.filter((fact) => fact.id === "proxy_keys")
  const configured = state.route_configured_count === 4
  const strategyOnly = coreFacts.some((fact) => fact.id === "routing" && fact.result === "incomplete")
    && coreFacts.filter((fact) => fact.id !== "routing").every((fact) => fact.result === "complete" && fact.fetch_quality === "fresh")
  const headline = state.route_configured_count === null ? state.phase === "loading" ? copy.checkingHeadline : copy.unknownTitle : copy.routeConfigured(state.route_configured_count)
  const pricingStatus = factStatus(pricing, copy)
  const clientStatus = factStatus(client, copy)

  if (state.phase === "loading" && state.facts.every((fact) => fact.fetch_quality === "loading")) {
    return <OperatorLoadingState title={copy.checkingHeadline} description={copy.checkingDescription} />
  }

  return (
    <div ref={cardRef} onBlurCapture={onBlurCapture} data-testid="setup-card">
      <OperatorSectionCard
        title={copy.title}
        description={configured ? copy.readyDescription : copy.description}
        actions={
          <Button type="button" variant="ghost" size="sm" onClick={onToggle} aria-expanded={!collapsed} aria-controls="prism-setup-facts">
            {collapsed ? copy.expand : copy.collapse}
            <ChevronDown data-icon="inline-end" className={cn("transition-transform", collapsed && "-rotate-90")} aria-hidden="true" />
          </Button>
        }
      >
        {state.last_success_at && <p className="text-xs text-muted-foreground">{messages.observe.setupReadCompleted}：<span className="font-mono tabular-nums">{format(state.last_success_at)}</span></p>}
        {state.last_success_at && state.phase === "degraded" && <OperatorStalenessBadge label={messages.observe.staleDataNote} reason={copy.degradedDescription} />}
        <div className="flex flex-wrap items-center justify-between gap-3 pb-2" aria-live="polite">
          <p className="text-sm font-semibold">{headline}</p>
          {state.phase === "degraded" || state.phase === "unknown" || state.phase === "error" ? (
            <Button type="button" variant="outline" size="sm" onClick={onRetry}>{copy.retry}</Button>
          ) : null}
        </div>
        {!collapsed ? (
          <div id="prism-setup-facts">
            {state.phase === "degraded" || state.phase === "error" ? (
              <OperatorErrorState title={copy.degradedTitle} description={copy.degradedDescription} action={<Button type="button" variant="outline" size="sm" onClick={onRetry}>{copy.retry}</Button>} className="mb-3" />
            ) : null}
            {state.phase === "unknown" ? <OperatorCallout intent="warning" title={copy.unknownTitle} description={copy.unknownDescription} className="mb-3" /> : null}
            <ol aria-label={copy.factsLabel}>
              <TaskRow title={copy.connectionTitle} status={factStatus(coreFacts, copy)} description={connectionDescription(coreFacts, copy)}
                action={strategyOnly ? copy.configureStrategy : configured ? copy.manageModels : copy.addModel}
                href={strategyOnly ? "/route/ban-policies" : configured ? "/route/models" : "/route/models?action=create"} />
              <TaskRow title={copy.pricingTitle} status={pricingStatus}
                description={pricingStatus === copy.complete ? copy.pricingReady : pricingStatus === copy.incomplete ? copy.pricingIncomplete : copy.pricingUnknown}
                action={copy.configurePricing} href="/route/pricing" />
              <TaskRow title={copy.clientTitle} status={clientStatus}
                description={clientStatus === copy.skipped ? copy.clientOpenAccess : clientStatus === copy.complete ? copy.clientReady : clientStatus === copy.incomplete ? copy.clientIncomplete : copy.clientUnknown}
                action={copy.openClient} href="/system/proxy-keys" />
            </ol>
          </div>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-2" data-testid="setup-card-summary">
            <div className="min-w-0 flex-1 text-xs text-muted-foreground">
              <p>{copy.collapsedSummary}</p>
              {pricingStatus !== copy.complete && <p className="mt-1">{copy.pricingIncomplete}</p>}
              {clientStatus === copy.skipped && <p className="mt-1">{copy.clientOpenAccess}</p>}
              {clientStatus === copy.incomplete && <p className="mt-1">{copy.clientIncomplete}</p>}
              {(clientStatus === copy.unknown || clientStatus === copy.error) && <p className="mt-1">{copy.clientUnknown}</p>}
            </div>
            <a href="/system/proxy-keys" className="inline-flex min-h-8 items-center gap-1 text-xs font-medium text-primary hover:underline">{copy.openClient}<ChevronRight className="size-3.5" aria-hidden="true" /></a>
          </div>
        )}
      </OperatorSectionCard>
    </div>
  )
}
