import type { ComponentProps, ReactNode } from "react"

import { cn } from "@/lib/utils"

export type OperatorPageShellProps = ComponentProps<"main"> & {
  width?: "default" | "wide" | "full"
}

const pageWidthClasses = {
  default: "max-w-7xl",
  wide: "max-w-[96rem]",
  full: "max-w-none",
} as const

export function OperatorPageShell({
  children,
  className,
  width = "full",
  ...props
}: OperatorPageShellProps) {
  return (
    <main
      className={cn(
        "operator-page-transition flex w-full min-w-0 flex-col gap-[var(--density-page-gap)]",
        pageWidthClasses[width],
        className,
      )}
      {...props}
    >
      {children}
    </main>
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
      className={cn("flex min-w-0 flex-col gap-3 sm:flex-row sm:items-start sm:justify-between", className)}
      {...props}
    >
      <div className="flex min-w-0 flex-col gap-1">
        <h1 className="text-2xl font-semibold tracking-tight sm:text-[1.75rem]">{title}</h1>
        {description ? <p className="max-w-3xl text-sm text-muted-foreground">{description}</p> : null}
      </div>
      {resolvedActions ? <div className="flex flex-wrap items-center gap-2 sm:justify-end">{resolvedActions}</div> : null}
    </div>
  )
}
