import { render, screen, within } from "@testing-library/react"
import { describe, expect, it } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import type { RoutingDiagnosticRoute, RoutingDiagnosticsResponse } from "@/lib/api/observability"
import { OperationRoutingSummary } from "./OperationRoutingSummary"

function route(operationName: string, overrides: Partial<RoutingDiagnosticRoute> = {}): RoutingDiagnosticRoute {
  return {
    operation_name: operationName,
    accepted: false,
    configured_leaf_exists: false,
    statically_routable: false,
    access_target_ids: [],
    ...overrides,
  }
}

function diagnostics(routes: RoutingDiagnosticRoute[]): RoutingDiagnosticsResponse {
  return {
    model_config_id: 10,
    openai_accepted_format: "chat_completions_only",
    strategy: { id: 3, type: "fill-first" },
    accepted_operations: routes.filter((item) => item.accepted).map((item) => item.operation_name),
    targets: [],
    operation_routes: routes,
    configuration_warnings: [],
  }
}

function renderSummary(routes: RoutingDiagnosticRoute[]) {
  return render(
    <LocaleProvider>
      <OperationRoutingSummary diagnostics={diagnostics(routes)} />
    </LocaleProvider>,
  )
}

const openaiRoutes = [
  route("openai.chat_completions", { accepted: true, configured_leaf_exists: true, statically_routable: true }),
  route("openai.responses"),
  route("openai.responses.input_tokens"),
  route("openai.responses.compact"),
  route("openai.images.generations"),
  route("openai.images.edits"),
]

describe("OperationRoutingSummary", () => {
  it("collapses the six OpenAI operations into three visible groups", () => {
    renderSummary(openaiRoutes)
    const rows = within(screen.getByTestId("routing-operation-list")).getAllByRole("listitem")
    expect(rows).toHaveLength(3)
    expect(screen.getAllByText("Responses")).toHaveLength(1)
    expect(screen.getByTestId("routing-operation-chat_completions")).toHaveTextContent("Chat Completions")
    expect(screen.getByTestId("routing-operation-responses")).toHaveTextContent("入口不接受")
    expect(screen.getByTestId("routing-operation-images")).toHaveTextContent("生图 / 改图")
  })

  it("keeps the group order the backend emitted", () => {
    renderSummary(openaiRoutes)
    const rows = within(screen.getByTestId("routing-operation-list")).getAllByRole("listitem")
    expect(rows.map((row) => row.getAttribute("data-testid"))).toEqual([
      "routing-operation-chat_completions",
      "routing-operation-responses",
      "routing-operation-images",
    ])
  })

  it("splits a group whose members disagree instead of reporting one aggregate state", () => {
    renderSummary([
      route("openai.images.generations", { accepted: true, configured_leaf_exists: true, statically_routable: true }),
      route("openai.images.edits"),
    ])
    const images = screen.getByTestId("routing-operation-images")
    expect(images).toHaveTextContent("生图：可路由")
    expect(images).toHaveTextContent("改图：入口不接受")
  })

  it("leaves non-OpenAI operations one row each under their registered name", () => {
    renderSummary([
      route("anthropic.messages", { accepted: true, configured_leaf_exists: true, statically_routable: true }),
      route("anthropic.count_tokens", { accepted: true }),
    ])
    const rows = within(screen.getByTestId("routing-operation-list")).getAllByRole("listitem")
    expect(rows).toHaveLength(2)
    expect(screen.getByTestId("routing-operation-anthropic.messages")).toHaveTextContent("可路由")
    expect(screen.getByTestId("routing-operation-anthropic.count_tokens")).toHaveTextContent("无静态路由")
  })
})
