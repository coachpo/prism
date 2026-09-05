import { useId, type ComponentProps, type ReactNode } from "react"

import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { cn } from "@/lib/utils"

export type OperatorSectionCardProps = Omit<ComponentProps<typeof Card>, "title"> & {
  title?: ReactNode
  description?: ReactNode
  icon?: ReactNode
  actions?: ReactNode
  footer?: ReactNode
  contentClassName?: string
}

export function OperatorSectionCard({
  actions,
  children,
  className,
  contentClassName,
  description,
  footer,
  icon,
  title,
  ...props
}: OperatorSectionCardProps) {
  const hasHeader = title || description || actions
  const titleId = useId()

  return (
    // 区块是长页的路标：标题必须是真正的 h2，卡片本身由它命名，
    // 否则读屏按 H 键会从页标题直接跳过整个区块。
    <Card
      className={cn("operator-section-surface min-w-0", className)}
      aria-labelledby={title ? titleId : undefined}
      {...props}
    >
      {hasHeader ? (
        <CardHeader className="border-b">
          <div className="flex min-w-0 items-center gap-2">
            {icon ? <span className="shrink-0 text-muted-foreground">{icon}</span> : null}
            <div className="min-w-0">
              {title ? (
                <CardTitle
                  asChild
                  className="text-[0.9375rem] font-semibold leading-5"
                >
                  <h2 id={titleId}>{title}</h2>
                </CardTitle>
              ) : null}
              {description ? <CardDescription className="text-xs">{description}</CardDescription> : null}
            </div>
          </div>
          {actions ? <CardAction>{actions}</CardAction> : null}
        </CardHeader>
      ) : null}
      <CardContent className={cn("min-w-0", contentClassName)}>{children}</CardContent>
      {footer ? <CardFooter className="border-t">{footer}</CardFooter> : null}
    </Card>
  )
}

export type OperatorInsetPanelProps = Omit<ComponentProps<"section">, "title"> & {
  title?: ReactNode
  description?: ReactNode
  actions?: ReactNode
}

export function OperatorInsetPanel({
  actions,
  children,
  className,
  description,
  title,
  ...props
}: OperatorInsetPanelProps) {
  const hasHeader = title || description || actions

  return (
    <section
      className={cn(
        "flex min-w-0 flex-col gap-2 rounded-md border border-border bg-inset p-[var(--density-card-pad-x)]",
        className,
      )}
      {...props}
    >
      {hasHeader ? (
        <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            {title ? <h3 className="text-[0.8125rem] font-semibold leading-[1.125rem]">{title}</h3> : null}
            {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
          </div>
          {actions ? <div className="shrink-0">{actions}</div> : null}
        </div>
      ) : null}
      {children}
    </section>
  )
}
