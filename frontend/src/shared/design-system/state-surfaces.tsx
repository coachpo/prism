import type { ComponentProps, ReactNode } from "react"
import {
  AlertCircleIcon,
  CheckCircle2Icon,
  InfoIcon,
  Loader2Icon,
  SearchXIcon,
  TriangleAlertIcon,
} from "lucide-react"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Skeleton } from "@/components/ui/skeleton"
import { cn } from "@/lib/utils"
import type { OperatorCalloutIntent } from "./foundation"

export type { OperatorCalloutIntent }

type OperatorStateProps = {
  title: string
  description?: ReactNode
  action?: ReactNode
  className?: string
  icon?: ReactNode
  testId?: string
}

/**
 * Nothing is there. The description states the next step, not just the fact —
 * `还没有配置端点。先添加一个供应商端点，模型才能路由到它。`
 *
 * A read that failed is not this: it belongs to `OperatorErrorState`.
 */
export function OperatorEmptyState({
  action,
  className,
  description,
  icon,
  testId,
  title,
}: OperatorStateProps) {
  return (
    <Empty
      className={cn("operator-state-surface gap-2 rounded-lg border py-[var(--density-empty-pad-y)]", className)}
      data-testid={testId}
    >
      <EmptyHeader className="gap-1.5">
        <EmptyMedia variant="icon" className="size-12 bg-transparent text-text-disabled [&>svg]:size-12">
          {icon ?? <SearchXIcon />}
        </EmptyMedia>
        <EmptyTitle className="text-[0.9375rem] font-semibold">{title}</EmptyTitle>
        {description ? (
          <EmptyDescription className="max-w-prose text-xs">{description}</EmptyDescription>
        ) : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  )
}

/** Ships no default copy: the caller supplies localized text. */
export function OperatorLoadingState({
  title,
  description,
  className,
}: Omit<OperatorStateProps, "action">) {
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn("operator-state-surface flex flex-col gap-3 rounded-lg border p-[var(--density-card-pad-x)]", className)}
    >
      <div className="flex items-center gap-2 text-[0.8125rem] font-medium text-foreground">
        <Loader2Icon className="animate-spin" />
        <span>{title}</span>
      </div>
      {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
      <div className="grid gap-2" aria-hidden="true">
        <Skeleton className="h-3 w-4/5" />
        <Skeleton className="h-3 w-3/5" />
        <Skeleton className="h-3 w-2/5" />
      </div>
    </div>
  )
}
/**
 * A read failed. This never degrades to an empty state: an empty state claims
 * there is nothing, and that is a different fact. Status codes and traces go
 * behind `details` so the surface stays readable.
 */
export function OperatorErrorState({
  title,
  description,
  action,
  className,
  details,
  detailsLabel,
  testId,
}: OperatorStateProps & { details?: ReactNode; detailsLabel?: string }) {
  return (
    <Alert
      role="alert"
      variant="destructive"
      data-testid={testId}
      className={cn("rounded-lg border-destructive/40 bg-destructive/5", className)}
    >
      <AlertCircleIcon />
      <AlertTitle>{title}</AlertTitle>
      {description || action || details ? (
        <AlertDescription>
          {description ? <p>{description}</p> : null}
          {details && detailsLabel ? (
            <details className="mt-2">
              <summary className="cursor-pointer text-xs text-muted-foreground">{detailsLabel}</summary>
              <div className="mt-1 rounded-md border border-border bg-inset p-2 font-mono text-xs">
                {details}
              </div>
            </details>
          ) : null}
          {action ? <div className="mt-3 flex flex-wrap gap-2">{action}</div> : null}
        </AlertDescription>
      ) : null}
    </Alert>
  )
}

export function OperatorRetryButton({ children, ...props }: ComponentProps<typeof Button>) {
  return (
    <Button variant="outline" size="sm" {...props}>
      {children}
    </Button>
  )
}

const OPERATOR_CALLOUT_TONES: Record<OperatorCalloutIntent, string> = {
  info: "border-primary/25 bg-primary/10 text-primary [&_[data-slot=alert-description]]:text-primary/90",
  success: "border-healthy/25 bg-healthy/10 text-healthy [&_[data-slot=alert-description]]:text-healthy/90",
  warning: "border-degraded/30 bg-degraded/10 text-degraded [&_[data-slot=alert-description]]:text-degraded/90",
  danger: "border-destructive/30 bg-destructive/10 text-destructive [&_[data-slot=alert-description]]:text-destructive/90",
  muted: "border-border bg-inset text-foreground [&_[data-slot=alert-description]]:text-muted-foreground",
}

const OPERATOR_CALLOUT_ICONS = {
  info: InfoIcon,
  success: CheckCircle2Icon,
  warning: TriangleAlertIcon,
  danger: AlertCircleIcon,
  muted: InfoIcon,
} as const

export type OperatorCalloutProps = Omit<ComponentProps<typeof Alert>, "title"> & {
  intent?: OperatorCalloutIntent
  title?: ReactNode
  description?: ReactNode
  action?: ReactNode
  icon?: ReactNode
}

export function OperatorCallout({
  action,
  children,
  className,
  description,
  icon,
  intent = "info",
  role,
  title,
  ...props
}: OperatorCalloutProps) {
  const Icon = OPERATOR_CALLOUT_ICONS[intent]
  const content = description ?? children

  return (
    <Alert
      role={role ?? (intent === "danger" ? "alert" : "note")}
      className={cn("items-center", OPERATOR_CALLOUT_TONES[intent], className)}
      {...props}
    >
      {icon ?? <Icon />}
      <div className="col-start-2 flex min-w-0 flex-col gap-1 sm:flex-row sm:items-center sm:justify-between sm:gap-3">
        <div className="min-w-0">
          {title ? <AlertTitle>{title}</AlertTitle> : null}
          {content ? <AlertDescription>{content}</AlertDescription> : null}
        </div>
        {action ? <div className="shrink-0">{action}</div> : null}
      </div>
    </Alert>
  )
}
