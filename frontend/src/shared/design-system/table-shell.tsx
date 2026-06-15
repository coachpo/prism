import type { ReactNode } from "react"

import {
  Card,
  CardAction,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card"
import { cn } from "@/lib/utils"
import type { OperatorDensityMode } from "./foundation"

type OperatorTableShellProps = {
  title: string
  description?: string
  actions?: ReactNode
  children: ReactNode
  density?: OperatorDensityMode
  className?: string
  contentClassName?: string
}

export function OperatorTableShell({
  title,
  description,
  actions,
  children,
  density = "balanced",
  className,
  contentClassName,
}: OperatorTableShellProps) {
  return (
    <Card
      data-density={density}
      className={cn("operator-table-shell overflow-hidden rounded-xl", className)}
    >
      <CardHeader className="border-b">
        <CardTitle>{title}</CardTitle>
        {description ? <CardDescription>{description}</CardDescription> : null}
        {actions ? <CardAction>{actions}</CardAction> : null}
      </CardHeader>
      <CardContent className={cn("px-0", contentClassName)}>
        {children}
      </CardContent>
    </Card>
  )
}
