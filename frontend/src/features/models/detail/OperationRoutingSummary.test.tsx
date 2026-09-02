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
    resolved_stage: null,
    access_target_ids: [],
    ...overrides,
  }
}

function diagnostics(routes: RoutingDiagnosticRoute[]): RoutingDiagnosticsResponse {
  return {
    model_config_id: 10,
    direct_request_enabled: true,
    openai_accepted_format: "chat_completions_only",
    strategy: { id: 3, type: "fill-first" },
    accepted_operations: routes.filter((item) => item.accepted).map((item) => item.operation_name),
    stages: [],
    targets: [],
    operation_routes: routes,
    operation_coverage: [],
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

  it("gives non-OpenAI operations one row each with a localized label", () => {
    renderSummary([
      route("anthropic.messages", { accepted: true, configured_leaf_exists: true, statically_routable: true }),
      route("anthropic.count_tokens", { accepted: true }),
    ])
    const list = screen.getByTestId("routing-operation-list")
    const rows = within(list).getAllByRole("listitem")
    expect(rows).toHaveLength(2)
    expect(screen.getByTestId("routing-operation-anthropic.messages")).toHaveTextContent("可路由")
    expect(screen.getByTestId("routing-operation-anthropic.count_tokens")).toHaveTextContent("无静态路由")

    // The registry name is an internal enum key. It may key a test id, but it
    // must not be what the operator reads.
    expect(list.textContent).toContain("Anthropic 消息")
    expect(list.textContent).toContain("Anthropic 计数令牌")
    expect(list.textContent).not.toContain("anthropic.messages")
    expect(list.textContent).not.toContain("anthropic.count_tokens")
  })

  it("names every registered non-OpenAI operation rather than falling through", () => {
    const registered = [
      "anthropic.messages",
      "anthropic.count_tokens",
      "gemini.generate_content",
      "gemini.stream_generate_content",
      "gemini.count_tokens",
    ]
    renderSummary(registered.map((name) => route(name, { accepted: true })))
    const list = screen.getByTestId("routing-operation-list")
    for (const name of registered) {
      expect(list.textContent).not.toContain(name)
    }
    // A genuinely unregistered operation still must not leak its key.
    renderSummary([route("anthropic.future_operation", { accepted: true })])
    const lists = screen.getAllByTestId("routing-operation-list")
    const latest = lists[lists.length - 1]
    expect(latest.textContent).not.toContain("anthropic.future_operation")
    expect(latest.textContent).toContain("未知操作")
  })
})
