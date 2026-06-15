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

export type OperatorSectionCardProps = ComponentProps<typeof Card> & {
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
    <Card className={cn("min-w-0", className)} {...props}>
      {hasHeader ? (
        <CardHeader className="border-b">
          <div className="flex min-w-0 items-center gap-2">
            {icon ? <span className="shrink-0 text-muted-foreground">{icon}</span> : null}
            <div className="min-w-0">
              {title ? <CardTitle className="text-sm">{title}</CardTitle> : null}
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
