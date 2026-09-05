import * as React from "react"
import { Slot } from "radix-ui"

import { cn } from "@/lib/utils"

function Card({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card"
      className={cn(
        "bg-panel text-card-foreground flex flex-col rounded-lg border border-border shadow-none",
        "gap-[var(--density-card-gap)] py-[var(--density-card-pad-y)]",
        className
      )}
      {...props}
    />
  )
}

function CardHeader({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        // 动作区只有在卡片本身够宽时才与标题同行。窄容器里强行同行会把标题
        // 压成一字一行 —— 这是窄屏下「访问目标」区块头高 365px 的原因。
        "@container/card-header grid auto-rows-min grid-rows-[auto_auto] items-start gap-2 px-[var(--density-card-pad-x)] has-data-[slot=card-action]:@md/card-header:grid-cols-[1fr_auto] [.border-b]:pb-[var(--density-card-pad-y)]",
        className
      )}
      {...props}
    />
  )
}

function CardTitle({
  className,
  asChild = false,
  ...props
}: React.ComponentProps<"div"> & { asChild?: boolean }) {
  const Comp = asChild ? Slot.Root : "div"
  return (
    <Comp
      data-slot="card-title"
      className={cn("leading-none font-semibold", className)}
      {...props}
    />
  )
}

function CardDescription({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-description"
      className={cn("text-muted-foreground text-sm", className)}
      {...props}
    />
  )
}

function CardAction({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn(
        // 与 CardHeader 的容器查询配套：窄容器里它落到第二行并左对齐。
        "col-start-1 row-start-3 justify-self-start self-start @md/card-header:col-start-2 @md/card-header:row-span-2 @md/card-header:row-start-1 @md/card-header:justify-self-end",
        className
      )}
      {...props}
    />
  )
}

function CardContent({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-content"
      className={cn("px-[var(--density-card-pad-x)]", className)}
      {...props}
    />
  )
}

function CardFooter({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn(
        "flex items-center px-[var(--density-card-pad-x)] [.border-t]:pt-[var(--density-card-pad-y)]",
        className
      )}
      {...props}
    />
  )
}

export {
  Card,
  CardHeader,
  CardFooter,
  CardTitle,
  CardAction,
  CardDescription,
  CardContent,
}
