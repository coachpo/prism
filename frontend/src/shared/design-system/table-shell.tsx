import type { ReactNode } from "react"

import { Card, CardAction, CardContent, CardHeader } from "@/components/ui/card"
import { cn } from "@/lib/utils"

type OperatorTableShellProps = {
  /**
   * State summary for the rows below, such as `8 个端点 · 1 个降级 · 1 个故障`.
   * Never the page title — the page header owns that.
   */
  summary?: ReactNode
  /** This card's own secondary controls. Primary actions live in the page header. */
  actions?: ReactNode
  children: ReactNode
  className?: string
  contentClassName?: string
  "data-testid"?: string
}

export function OperatorTableShell({
  actions,
  children,
  className,
  contentClassName,
  summary,
  ...props
}: OperatorTableShellProps) {
  const hasHeader = Boolean(summary || actions)

  return (
    // 没有卡头时表格贴住卡片上沿，否则卡片的上内边距会变成表头上方的一条空带。
    <Card className={cn("operator-table-shell gap-0 overflow-hidden rounded-lg", !hasHeader && "pt-0", className)} {...props}>
      {hasHeader ? (
        <CardHeader className="min-h-[var(--density-control-h)] border-b py-2">
          {summary ? (
            <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
              {summary}
            </div>
          ) : null}
          {actions ? <CardAction className="flex items-center gap-2">{actions}</CardAction> : null}
        </CardHeader>
      ) : null}
      <CardContent className={cn("px-0", contentClassName)}>{children}</CardContent>
    </Card>
  )
}
