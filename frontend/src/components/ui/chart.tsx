"use client"

import * as React from "react"
import * as RechartsPrimitive from "recharts"

import { formatNumber, getCurrentLocale } from "@/i18n/format"
import { cn } from "@/lib/utils"

// Format: { THEME_NAME: CSS_SELECTOR }
const THEMES = { light: "", dark: ".dark" } as const

const INITIAL_DIMENSION = { width: 320, height: 200 } as const

const CHART_SCOPED_TOKENS = {
  "--chart-axis": "color-mix(in oklab, var(--muted-foreground) 92%, transparent)",
  "--chart-grid": "color-mix(in oklab, var(--border) 70%, transparent)",
  "--chart-cursor": "color-mix(in oklab, var(--border) 88%, transparent)",
  "--chart-cursor-fill": "color-mix(in oklab, var(--muted) 72%, transparent)",
  "--chart-tooltip-border": "color-mix(in oklab, var(--border) 78%, transparent)",
  "--chart-tooltip-background": "color-mix(in oklab, var(--background) 92%, transparent)",
  "--chart-legend-border": "color-mix(in oklab, var(--border) 72%, transparent)",
  "--chart-legend-background": "color-mix(in oklab, var(--background) 82%, transparent)",
} as const

type TooltipValueType = number | string | Array<number | string>
type TooltipNameType = number | string

export type ChartConfig = Record<
  string,
  {
    label?: React.ReactNode
    icon?: React.ComponentType
  } & (
    | { color?: string; theme?: never }
    | { color?: never; theme: Record<keyof typeof THEMES, string> }
  )
>

type ChartContextProps = {
  config: ChartConfig
}

const ChartContext = React.createContext<ChartContextProps | null>(null)

function useChart() {
  const context = React.useContext(ChartContext)

  if (!context) {
    throw new Error("useChart must be used within a <ChartContainer />")
  }

  return context
}

function ChartContainer({
  id,
  className,
  children,
  config,
  initialDimension = INITIAL_DIMENSION,
  ...props
}: React.ComponentProps<"div"> & {
  config: ChartConfig
  children: React.ComponentProps<
    typeof RechartsPrimitive.ResponsiveContainer
  >["children"]
  initialDimension?: {
    width: number
    height: number
  }
}) {
  const uniqueId = React.useId()
  const chartId = `chart-${id ?? uniqueId.replace(/:/g, "")}`

  return (
    <ChartContext.Provider value={{ config }}>
      <div
        data-slot="chart"
        data-chart={chartId}
        className={cn(
          "relative flex aspect-video w-full justify-center text-xs [&_.recharts-cartesian-axis-line]:stroke-[var(--chart-grid)] [&_.recharts-cartesian-axis-tick_line]:stroke-[var(--chart-grid)] [&_.recharts-cartesian-axis-tick_text]:fill-[var(--chart-axis)] [&_.recharts-cartesian-grid_line]:stroke-[var(--chart-grid)] [&_.recharts-curve.recharts-tooltip-cursor]:stroke-[var(--chart-cursor)] [&_.recharts-dot[stroke='#fff']]:stroke-transparent [&_.recharts-layer]:outline-hidden [&_.recharts-polar-angle-axis-tick_text]:fill-[var(--chart-axis)] [&_.recharts-polar-grid_[stroke='#ccc']]:stroke-[var(--chart-grid)] [&_.recharts-radial-bar-background-sector]:fill-muted/50 [&_.recharts-rectangle.recharts-tooltip-cursor]:fill-[var(--chart-cursor-fill)] [&_.recharts-reference-line_[stroke='#ccc']]:stroke-[var(--chart-cursor)] [&_.recharts-sector]:outline-hidden [&_.recharts-sector[stroke='#fff']]:stroke-transparent [&_.recharts-surface]:outline-hidden",
          className
        )}
        {...props}
      >
        <ChartStyle id={chartId} config={config} />
        <RechartsPrimitive.ResponsiveContainer
          initialDimension={initialDimension}
        >
          {children}
        </RechartsPrimitive.ResponsiveContainer>
      </div>
    </ChartContext.Provider>
  )
}

const ChartStyle = ({ id, config }: { id: string; config: ChartConfig }) => {
  const cssText = Object.entries(THEMES)
    .map(
      ([theme, prefix]) => `${prefix} [data-chart="${id}"] {
${Object.entries(CHART_SCOPED_TOKENS)
  .map(([key, value]) => `  ${key}: ${value};`)
  .join("\n")}
${Object.entries(config)
  .map(([key, itemConfig]) => {
    const color =
      itemConfig.theme?.[theme as keyof typeof itemConfig.theme] ??
      itemConfig.color

    return color ? `  --color-${key}: ${color};` : null
  })
  .filter(Boolean)
  .join("\n")}
}`
    )
    .join("\n")

  return <style dangerouslySetInnerHTML={{ __html: cssText }} />
}

const ChartTooltip = RechartsPrimitive.Tooltip

function ChartTooltipContent({
  active,
  payload,
  className,
  indicator = "dot",
  hideLabel = false,
  hideIndicator = false,
  label,
  labelFormatter,
  labelClassName,
  formatter,
  color,
  nameKey,
  labelKey,
}: React.ComponentProps<typeof RechartsPrimitive.Tooltip> &
  React.ComponentProps<"div"> & {
    hideLabel?: boolean
    hideIndicator?: boolean
    indicator?: "line" | "dot" | "dashed"
    nameKey?: string
    labelKey?: string
  } & Omit<
    RechartsPrimitive.DefaultTooltipContentProps<
      TooltipValueType,
      TooltipNameType
    >,
    "accessibilityLayer"
  >) {
  const { config } = useChart()
  const locale = getCurrentLocale()

  const tooltipLabel = React.useMemo(() => {
    if (hideLabel || !payload?.length) {
      return null
    }

    const [item] = payload
    const key = `${labelKey ?? item?.dataKey ?? item?.name ?? "value"}`
    const itemConfig = getPayloadConfigFromPayload(config, item, key)
    const value =
      !labelKey && typeof label === "string"
        ? (config[label]?.label ?? label)
        : itemConfig?.label

    if (labelFormatter) {
      return (
        <div className={cn("font-medium text-foreground", labelClassName)}>
          {labelFormatter(value, payload)}
        </div>
      )
    }

    if (value == null || value === "") {
      return null
    }

    return (
      <div className={cn("font-medium text-foreground", labelClassName)}>
        {value}
      </div>
    )
  }, [
    label,
    labelFormatter,
    payload,
    hideLabel,
    labelClassName,
    config,
    labelKey,
  ])

  if (!active || !payload?.length) {
    return null
  }

  const nestLabel = payload.length === 1 && indicator !== "dot"

  return (
    <div
      className={cn(
        "grid min-w-[8.5rem] items-start gap-2 rounded-lg border border-(--chart-tooltip-border) bg-(--chart-tooltip-background) px-3 py-2 text-xs shadow-md backdrop-blur-sm",
        className
      )}
    >
      {!nestLabel ? tooltipLabel : null}
      <div className="grid gap-2">
        {payload
          .filter((item) => item.type !== "none")
          .map((item, index) => {
            const key = `${nameKey ?? item.name ?? item.dataKey ?? "value"}`
            const itemConfig = getPayloadConfigFromPayload(config, item, key)
            const indicatorColor =
              color ?? getPayloadColor(item.payload) ?? item.color

            return (
              <div
                key={`${item.dataKey ?? item.name ?? index}`}
                className={cn(
                  "flex w-full flex-wrap items-stretch gap-2.5 [&>svg]:size-2.5 [&>svg]:text-muted-foreground",
                  indicator === "dot" && "items-center"
                )}
              >
                {formatter && item.value !== undefined && item.name != null ? (
                  formatter(item.value, item.name, item, index, item.payload)
                ) : (
                  <>
                    {itemConfig?.icon ? (
                      <itemConfig.icon />
                    ) : (
                      !hideIndicator && (
                        <div
                          className={cn(
                            "shrink-0 rounded-[3px] border-(--color-border) bg-(--color-bg)",
                            {
                              "size-2.5": indicator === "dot",
                              "w-1": indicator === "line",
                              "w-0 border-[1.5px] border-dashed bg-transparent":
                                indicator === "dashed",
                              "my-0.5": nestLabel && indicator === "dashed",
                            }
                          )}
                          style={
                            {
                              "--color-bg": indicatorColor,
                              "--color-border": indicatorColor,
                            } as React.CSSProperties
                          }
                        />
                      )
                    )}
                    <div
                      className={cn(
                        "flex flex-1 items-center justify-between gap-3 leading-none",
                        nestLabel && "items-end"
                      )}
                    >
                      <div className="grid min-w-0 gap-1">
                        {nestLabel ? tooltipLabel : null}
                        <span className="truncate text-muted-foreground">
                          {itemConfig?.label ?? item.name}
                        </span>
                      </div>
                      {item.value != null && (
                        <span className="font-mono font-medium text-foreground tabular-nums">
                          {formatTooltipValue(item.value, locale)}
                        </span>
                      )}
                    </div>
                  </>
                )}
              </div>
            )
          })}
      </div>
    </div>
  )
}

const ChartLegend = RechartsPrimitive.Legend

function ChartLegendContent({
  className,
  hideIcon = false,
  payload,
  verticalAlign = "bottom",
  nameKey,
}: React.ComponentProps<"div"> & {
  hideIcon?: boolean
  nameKey?: string
} & RechartsPrimitive.DefaultLegendContentProps) {
  const { config } = useChart()

  if (!payload?.length) {
    return null
  }

  return (
    <div
      className={cn(
        "flex flex-wrap items-center justify-center gap-2.5 text-xs",
        verticalAlign === "top" ? "pb-4" : "pt-4",
        className
      )}
    >
      {payload
        .filter((item) => item.type !== "none")
        .map((item, index) => {
          const key = `${nameKey ?? item.dataKey ?? "value"}`
          const itemConfig = getPayloadConfigFromPayload(config, item, key)
          const indicatorColor = item.color ?? getPayloadColor(item.payload)

          return (
            <div
              key={`${item.value ?? item.dataKey ?? index}`}
              className="flex items-center gap-1.5 rounded-md border border-(--chart-legend-border) bg-(--chart-legend-background) px-2.5 py-1 text-muted-foreground [&>svg]:size-3 [&>svg]:text-muted-foreground"
            >
              {itemConfig?.icon && !hideIcon ? (
                <itemConfig.icon />
              ) : (
                <div
                  className="size-2 shrink-0 rounded-[3px]"
                  style={{
                    backgroundColor: indicatorColor,
                  }}
                />
              )}
              <span className="truncate">{itemConfig?.label ?? item.value}</span>
            </div>
          )
        })}
    </div>
  )
}

function getPayloadColor(payload: unknown) {
  if (typeof payload !== "object" || payload === null) {
    return undefined
  }

  const candidate = (payload as { fill?: unknown }).fill

  return typeof candidate === "string" ? candidate : undefined
}

function formatTooltipValue(
  value: TooltipValueType,
  locale: ReturnType<typeof getCurrentLocale>
) {
  if (Array.isArray(value)) {
    return value
      .map((entry) =>
        typeof entry === "number" ? formatNumber(entry, locale) : String(entry)
      )
      .join(" / ")
  }

  return typeof value === "number" ? formatNumber(value, locale) : String(value)
}

// Helper to extract item config from a payload.
function getPayloadConfigFromPayload(
  config: ChartConfig,
  payload: unknown,
  key: string
) {
  if (typeof payload !== "object" || payload === null) {
    return undefined
  }

  const payloadPayload =
    "payload" in payload &&
    typeof payload.payload === "object" &&
    payload.payload !== null
      ? payload.payload
      : undefined

  let configLabelKey: string = key

  if (
    key in payload &&
    typeof payload[key as keyof typeof payload] === "string"
  ) {
    configLabelKey = payload[key as keyof typeof payload] as string
  } else if (
    payloadPayload &&
    key in payloadPayload &&
    typeof payloadPayload[key as keyof typeof payloadPayload] === "string"
  ) {
    configLabelKey = payloadPayload[
      key as keyof typeof payloadPayload
    ] as string
  }

  return configLabelKey in config ? config[configLabelKey] : config[key]
}

export {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  ChartLegend,
  ChartLegendContent,
  ChartStyle,
}
