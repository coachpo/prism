import type { ComponentProps, ReactNode } from "react"
import { render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { LocaleProvider } from "@/i18n/LocaleProvider"
import type { ManagedModelConfigListItem } from "@/lib/api/models"
import { entryModelListItem } from "./modelExitMapping.test-fixtures"
import { ModelsTable } from "./ModelsTable"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#detail">{children}</a>,
  useNavigate: () => vi.fn(),
}))

vi.mock("@/context/ReportingCurrencyContext", () => ({
  useReportingCurrencyContext: () => ({ currencyState: { currency: { symbol: "$" } } }),
}))

function renderModels(models: ManagedModelConfigListItem[]) {
  const noop = vi.fn()
  const props: ComponentProps<typeof ModelsTable> = {
    scope: "ingress", filtered: models, hasActiveFilters: false,
    metricsFailed: false, metricsLoading: false,
    modelMetrics24h: {}, modelSpend30dMicros: {}, page: 1,
    selectedIds: new Set(), sortBy: "name", sortOrder: "asc",
    togglingModelIds: new Set(), view: "model_targets",
    onClearFilters: noop, onCreate: noop, onEdit: noop,
    onPageChange: noop, onPageSizeChange: noop, onSelectionChange: noop,
    onSetEnabled: vi.fn(async () => true),
    onSetManyEnabled: vi.fn(async () => undefined),
    onShowEntries: noop, onSort: noop, setDeleteTarget: noop,
  }
  render(
    <LocaleProvider>
      <ModelsTable {...props} />
    </LocaleProvider>,
  )
}

describe("ModelsTable internal-model identity", () => {
  it.each([
    [2, "被 2 个模型目标引用"],
    [0, "未被模型目标引用"],
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

})
