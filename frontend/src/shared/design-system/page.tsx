import type { ComponentProps, ReactNode } from "react"

import { cn } from "@/lib/utils"

export type OperatorPageShellProps = ComponentProps<"div"> & {
  width?: "default" | "wide" | "full"
}

/**
 * 默认封顶 1536px。不封顶时超宽屏上一行的 label 会离它的控件一千多像素，
 * 那不是一行任何人会读的键值对；需要整幅铺满的页面显式传 `full`。
 */
const pageWidthClasses = {
  default: "max-w-7xl",
  wide: "max-w-[96rem]",
  full: "max-w-none",
} as const

export function OperatorPageShell({
  children,
  className,
  width = "wide",
  ...props
}: OperatorPageShellProps) {
  return (
    // 壳层的 SidebarInset 已经是页面唯一的 main landmark，这里只做布局容器。
    <div
      className={cn(
        "operator-page-transition flex w-full min-w-0 flex-col gap-[var(--density-page-gap)]",
        pageWidthClasses[width],
        className,
      )}
      {...props}
    >
      {children}
    </div>
  )
}

export type OperatorPageHeaderProps = ComponentProps<"div"> & {
  title: string
  description?: ReactNode
  actions?: ReactNode
}

export function OperatorPageHeader({
  actions,
  children,
  className,
  description,
  title,
  ...props
}: OperatorPageHeaderProps) {
  const resolvedActions = actions ?? children

  return (
    <div
      className={cn("flex min-w-0 flex-col gap-2 sm:flex-row sm:items-start sm:justify-between", className)}
      {...props}
    >
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="text-[1.375rem] font-semibold leading-7 tracking-tight">{title}</h1>
        {description ? (
          <p className="max-w-3xl text-[0.8125rem] leading-5 text-muted-foreground">{description}</p>
        ) : null}
      </div>
      {resolvedActions ? <div className="flex flex-wrap items-center gap-2 sm:justify-end">{resolvedActions}</div> : null}
    </div>
  )
}
