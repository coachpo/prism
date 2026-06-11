import type { ComponentProps, ReactNode } from "react"
import { AlertCircleIcon, Loader2Icon, SearchXIcon } from "lucide-react"

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

type OperatorStateProps = {
  title: string
  description?: string
  action?: ReactNode
  className?: string
}

export function OperatorEmptyState({
  title,
  description,
  action,
  className,
}: OperatorStateProps) {
  return (
    <Empty className={cn("operator-state-surface border", className)}>
      <EmptyHeader>
        <EmptyMedia variant="icon">
          <SearchXIcon />
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
    <div className={cn("operator-state-surface flex flex-col gap-4 rounded-xl border p-[var(--density-card-pad-x)]", className)}>
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
    <Alert variant="destructive" className={cn("operator-state-surface", className)}>
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
