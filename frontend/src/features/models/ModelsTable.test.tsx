import type { ComponentProps, ReactNode } from "react"
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { LocaleProvider } from "@/i18n/LocaleProvider"
import { TooltipProvider } from "@/components/ui/tooltip"
import type { ManagedModelConfigListItem } from "@/lib/api/models"
import {
  entryModelListItem,
  routingSummary,
  terminalTargetRow,
} from "./modelExitMapping.test-fixtures"
import { ModelsTable } from "./ModelsTable"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#detail">{children}</a>,
  useNavigate: () => vi.fn(),
}))

vi.mock("@/context/ReportingCurrencyContext", () => ({
  useReportingCurrencyContext: () => ({ currencyState: { currency: { symbol: "$" } } }),
}))

function renderModels(
  models: ManagedModelConfigListItem[],
  view: ComponentProps<typeof ModelsTable>["view"] = "model_targets",
) {
  const noop = vi.fn()
  const props: ComponentProps<typeof ModelsTable> = {
    scope: "ingress", filtered: models, hasActiveFilters: false,
    metricsFailed: false, metricsLoading: false,
    modelMetrics24h: {}, modelSpend30dMicros: {}, page: 1,
    selectedIds: new Set(), sortBy: "name", sortOrder: "asc",
    togglingModelIds: new Set(), view,
    onClearFilters: noop, onCreate: noop, onEdit: noop,
    onPageChange: noop, onPageSizeChange: noop, onSelectionChange: noop,
    onSetEnabled: vi.fn(async () => true),
    onSetManyEnabled: vi.fn(async () => undefined),
    onSort: noop, setDeleteTarget: noop,
  }
  render(
    <LocaleProvider>
      <TooltipProvider>
        <ModelsTable {...props} />
      </TooltipProvider>
    </LocaleProvider>,
  )
}

describe("ModelsTable internal-model identity", () => {
  it.each([
    [2, "被 2 个其他模型引用"],
    [0, "未被其他模型引用"],
  ] as const)("renders incoming-reference evidence for count %i", (incoming, expected) => {
    renderModels([{
      ...entryModelListItem([]),
      direct_request_enabled: false,
      incoming_model_target_count: incoming,
      configuration_warnings: [],
    }])

    const row = within(screen.getByTestId("models-table-row-1"))
    expect(row.getByText(expected)).toBeVisible()
  })

  it.each([
    ["entries", "还没有配置客户端模型"],
    ["model_targets", "还没有仅供其他模型使用"],
    ["all", "还没有模型"],
  ] as const)("keeps the %s empty state distinct", (view, title) => {
    renderModels([], view)
    expect(screen.getByText(title)).toBeVisible()
  })
})

describe("ModelsTable routing coverage", () => {
  it.each([
    ["full", null],
    ["partial", "部分覆盖"],
    ["none", "不兼容"],
  ] as const)("marks only coverage deviations (%s)", (coverage, expected) => {
    renderModels(
      [{
        ...entryModelListItem([terminalTargetRow(11, 0)]),
        routing_summary: routingSummary({
          coverage,
          enabled_access_target_count: 1,
          total_access_target_count: 1,
        }),
      }],
      "entries",
    )

    const row = within(screen.getByTestId("models-table-row-1"))
    expect(row.queryByText("完整覆盖")).not.toBeInTheDocument()
    if (expected) expect(row.getByText(expected)).toBeVisible()
  })
})
