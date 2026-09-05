import type { ReactNode } from "react"
import { RefreshCw } from "lucide-react"

import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { cn } from "@/lib/utils"

export type OperatorAutoRefreshOption = {
  label: string
  value: string
}

export type OperatorFreshnessBarProps = {
  /**
   * When the data on screen is from, already localized — typically
   * `上次更新于 14:32:07 (UTC+8)`. Pass `OperatorMissingValue` when there has
   * been no successful load yet; never substitute "just now".
   */
  updatedAt: ReactNode
  /** Optional statement of what window or basis the page is showing. */
  basis?: ReactNode
  autoRefresh?: {
    ariaLabel: string
    onChange: (value: string) => void
    options: readonly OperatorAutoRefreshOption[]
    value: string
  }
  refresh?: {
    label: string
    onRefresh: () => void
    pending?: boolean
  }
  /** Staleness, cache lag, and coverage badges. Present only when abnormal. */
  badges?: ReactNode
  className?: string
  "data-testid"?: string
}

/**
 * The 32px row directly under the page header on every page that carries a
 * time window. This is how the Honesty Contract's "when is this from" is
 * discharged, and the refresh control here performs a real refresh.
 */
export function OperatorFreshnessBar({
  autoRefresh,
  badges,
  basis,
  className,
  refresh,
  updatedAt,
  ...props
}: OperatorFreshnessBarProps) {
  return (
    <div
      data-slot="freshness-bar"
      className={cn(
        "flex min-h-8 flex-wrap items-center gap-x-3 gap-y-1 border-b border-border pb-2 text-xs text-muted-foreground",
        className,
      )}
      {...props}
    >
      <span className="inline-flex items-center gap-1 font-mono tabular-nums">{updatedAt}</span>

      {autoRefresh ? (
        <>
          <Separator />
          <Select value={autoRefresh.value} onValueChange={autoRefresh.onChange}>
            <SelectTrigger
              size="sm"
              aria-label={autoRefresh.ariaLabel}
              className="h-7 gap-1 border-0 bg-transparent px-1 text-xs shadow-none"
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectGroup>
                {autoRefresh.options.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
        </>
      ) : null}

      {refresh ? (
        <>
          <Separator />
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={refresh.onRefresh}
            disabled={refresh.pending}
            data-testid="freshness-refresh"
            // 命中区不低于 28×28：这是「这份数据读于何时」的唯一入口。
            className="h-7 gap-1 px-1.5 text-xs text-muted-foreground hover:text-foreground"
          >
            <RefreshCw aria-hidden="true" className={cn("size-3.5", refresh.pending && "animate-spin")} />
            {refresh.label}
          </Button>
        </>
      ) : null}

      {basis ? (
        <>
          <Separator />
          {/* 口径不截断：窄屏上它常常正好被切掉后半句，而 truncate 之后
              没有任何途径能读到完整那句（title 不算实现）。 */}
          <span className="min-w-0">{basis}</span>
        </>
      ) : null}

      {badges ? <div className="ml-auto flex shrink-0 flex-wrap items-center gap-1.5">{badges}</div> : null}
    </div>
  )
}

function Separator() {
  // 12px 的文本节点，既不是禁用控件也不是 ≥24px 装饰图标，
  // 拿不到 text-disabled 的两条豁免（亮色下只有 3.10:1）。
  return (
    <span aria-hidden="true" className="text-muted-foreground">
      ·
    </span>
  )
}
