import type { ComponentProps, ReactNode } from "react"

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

  return (
    <Card className={cn("operator-section-surface min-w-0", className)} {...props}>
      {hasHeader ? (
        <CardHeader className="border-b">
          <div className="flex min-w-0 items-center gap-2">
            {icon ? <span className="shrink-0 text-muted-foreground">{icon}</span> : null}
            <div className="min-w-0">
              {title ? <CardTitle className="text-sm font-medium">{title}</CardTitle> : null}
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
      className={cn("flex min-w-0 flex-col gap-3 rounded-lg border border-outline-variant bg-surface-container-low p-4", className)}
      {...props}
    >
      {hasHeader ? (
        <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div className="min-w-0">
            {title ? <h3 className="text-sm font-medium">{title}</h3> : null}
            {description ? <p className="text-xs text-muted-foreground">{description}</p> : null}
          </div>
          {actions ? <div className="shrink-0">{actions}</div> : null}
        </div>
      ) : null}
      {children}
    </section>
  )
}
