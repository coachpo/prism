import * as React from "react"

import { getStaticMessages } from "@/i18n/staticMessages"
import { cn } from "@/lib/utils"

function Table({ className, ...props }: React.ComponentProps<"table">) {
  const containerRef = React.useRef<HTMLDivElement>(null)
  // 只有真的溢出时才做成停靠点：给不滚动的表格加一个焦点位，
  // 只会让每张表多出一次没有内容的 Tab。
  const [overflows, setOverflows] = React.useState(false)

  React.useEffect(() => {
    const container = containerRef.current
    if (!container || typeof ResizeObserver === "undefined") return
    const measure = () => {
      setOverflows(container.scrollWidth > container.clientWidth + 1)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(container)
    const table = container.firstElementChild
    if (table) observer.observe(table)
    return () => observer.disconnect()
  }, [])

  const tableName =
    typeof props["aria-label"] === "string" ? props["aria-label"] : null
  const tableCopy = getStaticMessages().operationalTable

  return (
    <div
      ref={containerRef}
      data-slot="table-container"
      className="relative w-full overflow-x-auto"
      {...(overflows
        ? {
            tabIndex: 0,
            role: "region",
            "aria-label": tableName
              ? tableCopy.horizontalScrollRegion(tableName)
              : tableCopy.horizontalScrollRegionUnnamed,
          }
        : {})}
    >
      <table
        data-slot="table"
        className={cn("w-full caption-bottom text-sm", className)}
        {...props}
      />
    </div>
  )
}

function TableHeader({ className, ...props }: React.ComponentProps<"thead">) {
  return (
    <thead
      data-slot="table-header"
      className={cn(
        // sticky 只在表格最近的滚动祖先里生效。包一层 overflow-x-auto 的表格，
        // 那个 div 就成了包含块，而它不纵向滚动 —— 这条声明会静默失效。
        // 需要黏住表头的表格必须让同一个容器同时纵向滚动（见 ModelsTable）。
        // 表头高由密度变量决定：TableRow 自带的行高会盖过 TableHead，必须在这里压回来。
        "sticky top-0 z-10 bg-inset [&_tr]:h-[var(--density-table-head-h)] [&_tr]:border-b",
        className
      )}
      {...props}
    />
  )
}

function TableBody({ className, ...props }: React.ComponentProps<"tbody">) {
  return (
    <tbody
      data-slot="table-body"
      className={cn("[&_tr:last-child]:border-0", className)}
      {...props}
    />
  )
}

function TableFooter({ className, ...props }: React.ComponentProps<"tfoot">) {
  return (
    <tfoot
      data-slot="table-footer"
      className={cn(
        "bg-inset border-t border-border font-medium [&>tr]:last:border-b-0",
        className
      )}
      {...props}
    />
  )
}

function TableRow({ className, ...props }: React.ComponentProps<"tr">) {
  return (
    <tr
      data-slot="table-row"
      className={cn(
        "h-[var(--density-table-row-h)] border-b border-border transition-colors hover:bg-primary-soft/20 data-[state=selected]:bg-primary-soft",
        className
      )}
      {...props}
    />
  )
}

function TableHead({ className, ...props }: React.ComponentProps<"th">) {
  return (
    <th
      data-slot="table-head"
      className={cn(
        "h-[var(--density-table-head-h)] px-[var(--density-table-cell-px)] text-left align-middle text-xs font-medium normal-case text-muted-foreground whitespace-nowrap [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        className
      )}
      {...props}
    />
  )
}

function TableCell({ className, ...props }: React.ComponentProps<"td">) {
  return (
    <td
      data-slot="table-cell"
      className={cn(
        "px-[var(--density-table-cell-px)] py-[var(--density-table-cell-py)] align-middle text-[0.8125rem] leading-[var(--density-table-cell-lh)] whitespace-nowrap [&:has([role=checkbox])]:pr-0 [&>[role=checkbox]]:translate-y-[2px]",
        className
      )}
      {...props}
    />
  )
}

function TableCaption({
  className,
  ...props
}: React.ComponentProps<"caption">) {
  return (
    <caption
      data-slot="table-caption"
      className={cn("text-muted-foreground mt-4 text-sm", className)}
      {...props}
    />
  )
}

export {
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
}
