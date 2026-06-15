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

export type OperatorCalloutIntent = "info" | "success" | "warning" | "danger" | "muted"

type OperatorStateProps = {
  title: string
  description?: ReactNode
  action?: ReactNode
  className?: string
  icon?: ReactNode
  testId?: string
}

export function OperatorEmptyState({
  action,
  className,
  description,
  icon,
  testId,
  title,
}: OperatorStateProps) {
  return (
    <Empty className={cn("operator-state-surface border", className)} data-testid={testId}>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          {icon ?? <SearchXIcon />}
        </EmptyMedia>
        <EmptyTitle>{title}</EmptyTitle>
        {description ? <EmptyDescription>{description}</EmptyDescription> : null}
      </EmptyHeader>
      {action ? <EmptyContent>{action}</EmptyContent> : null}
    </Empty>
  )
}

export function OperatorLoadingState({
  title,
  description = "Loading the latest operator state.",
  className,
}: Omit<OperatorStateProps, "action">) {
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn("operator-state-surface flex flex-col gap-4 rounded-xl border p-[var(--density-card-pad-x)]", className)}
    >
      <div className="flex items-center gap-2 text-sm font-medium text-foreground">
        <Loader2Icon className="animate-spin" />
        <span>{title}</span>
      </div>
      <p className="text-sm text-muted-foreground">{description}</p>
      <div className="grid gap-2" aria-hidden="true">
        <Skeleton className="h-3 w-4/5" />
        <Skeleton className="h-3 w-3/5" />
        <Skeleton className="h-3 w-2/5" />
      </div>
    </div>
  )
}
export function OperatorErrorState({
  title,
  description,
  action,
  className,
}: OperatorStateProps) {
  return (
    <Alert role="alert" variant="destructive" className={cn("operator-state-surface", className)}>
      <AlertCircleIcon />
      <AlertTitle>{title}</AlertTitle>
      {description || action ? (
        <AlertDescription>
          {description ? <p>{description}</p> : null}
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
  info: "border-info/25 bg-info/10 text-info [&_[data-slot=alert-description]]:text-info/90",
  success: "border-success/25 bg-success/10 text-success [&_[data-slot=alert-description]]:text-success/90",
  warning: "border-warning/30 bg-warning/10 text-warning [&_[data-slot=alert-description]]:text-warning/90",
  danger: "border-destructive/30 bg-destructive/10 text-destructive [&_[data-slot=alert-description]]:text-destructive/90",
  muted: "border-outline-variant bg-surface-container-low text-foreground [&_[data-slot=alert-description]]:text-muted-foreground",
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
