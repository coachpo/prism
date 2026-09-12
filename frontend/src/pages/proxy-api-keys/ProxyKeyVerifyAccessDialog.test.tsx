import { render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import type { ComponentProps } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import type { ModelConfigListItem } from "@/lib/types"
import { ProxyKeyVerifyAccessDialog } from "./ProxyKeyVerifyAccessDialog"

const mocks = vi.hoisted(() => ({ reconcile: vi.fn() }))
vi.mock("@/features/runtime-self-test/selfTestRunner", async (original) => ({
  ...await original<typeof import("@/features/runtime-self-test/selfTestRunner")>(),
  reconcileSelfTestTelemetry: mocks.reconcile,
}))
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, search, children, ...props }: { to: string; search?: Record<string, string>; children: React.ReactNode }) => (
    <a href={`${to}${search ? `?${new URLSearchParams(search)}` : ""}`} {...props}>{children}</a>
  ),
}))

const model = (id: number, model_id: string, direct_request_enabled: boolean, is_enabled = true) => ({
  id, model_id, direct_request_enabled, is_enabled,
  api_family: "openai", display_name: null,
  openai_accepted_format: "responses_only", openai_image_operations: null,
}) as ModelConfigListItem

function renderDialog(props: Partial<ComponentProps<typeof ProxyKeyVerifyAccessDialog>> = {}) {
  render(<LocaleProvider><ProxyKeyVerifyAccessDialog
    models={[model(1, "internal-target", false), model(2, "disabled-entry", true, false), model(3, "direct-entry", true)]}
    authEnabled={false} modelsError={false} modelsLoading={false}
    onOpenChange={vi.fn()} onRetryModels={vi.fn()} open {...props}
  /></LocaleProvider>)
}

function response(status: number, ingressId: string | null = null) {
  return new Response('{"error":{"message":"SQL internal.path UNTRUSTED_ERROR"}}', {
    status, headers: ingressId ? { "X-Prism-Ingress-Request-Id": ingressId } : {},
  })
}

describe("ProxyKeyVerifyAccessDialog", () => {
  const fetchMock = vi.fn()
  beforeEach(() => {
    fetchMock.mockReset()
    mocks.reconcile.mockReset().mockResolvedValue({ detail: null, state: "timed_out" })
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => vi.unstubAllGlobals())

  it("offers only enabled direct entries and preselects the requested available model", () => {
    renderDialog({ initialModelId: "second-entry", models: [model(1, "internal-target", false), model(3, "direct-entry", true), model(4, "second-entry", true)] })
    expect(screen.getByRole("combobox", { name: "可用模型" })).toHaveTextContent("second-entry")
    expect(screen.getByRole("checkbox", { name: "不带密钥测试" })).toBeChecked()
  })

  it("sends once from the form and keeps a failed response separate from missing records", async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(response(404, "request-123"))
    renderDialog()
    await user.click(screen.getByRole("button", { name: "发送测试请求" }))
    expect(await screen.findByText("测试请求失败")).toBeVisible()
    expect(await screen.findByRole("button", { name: "重新读取记录" })).toBeEnabled()
    expect(screen.queryByText("模型已响应")).not.toBeInTheDocument()
    expect(screen.queryByText(/UNTRUSTED_ERROR|SQL|运行时自检|Content-Type|Authorization/)).not.toBeInTheDocument()
    expect(screen.getAllByRole("dialog")).toHaveLength(1)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).model).toBe("direct-entry")
    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBeUndefined()
    expect(screen.getByRole("link", { name: "查看此模型的近期请求" })).toHaveAttribute("href", "/observe/requests?view=ingress_chains&ingress_model_id=direct-entry&time_range=1h")
    await user.click(screen.getByRole("button", { name: "重新读取记录" }))
    await waitFor(() => expect(mocks.reconcile).toHaveBeenCalledTimes(2))
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("shows the received result even when its record cannot load, then retries only the record", async () => {
    const user = userEvent.setup()
    fetchMock.mockResolvedValue(response(200, "request-456"))
    mocks.reconcile.mockRejectedValueOnce(new Error("SQL private.path")).mockResolvedValueOnce({
      state: "ready",
      finalized: { request_log_id: "501", final_result: "completed", final_status_code: 200, endpoint: { id: 3, label: "我的服务" }, final_pricing_status: "priced", final_pricing_evidence_trust: "trusted", total_cost_user_currency_micros: 1250, report_currency_symbol: "$" },
      detail: {
        request: { ingress_request_id: "request-456", proxy_api_key_attribution_state: "identified", proxy_api_key_id: 42 },
        summary: { gateway_status_code: 200 },
        routing: { endpoint_label: "我的服务" },
        pricing: { pricing_status: "priced", total_cost_user_currency_micros: 1250, report_currency_symbol: "$" },
      },
    })
    renderDialog()
    await user.click(screen.getByRole("button", { name: "发送测试请求" }))
    expect(await screen.findByText("已收到响应")).toBeVisible()
    expect(screen.queryByText("模型已响应")).not.toBeInTheDocument()
    expect(await screen.findByText(/请求结果已收到，但记录读取失败/)).toBeVisible()
    expect(screen.queryByText(/SQL private.path/)).not.toBeInTheDocument()
    await user.click(screen.getByRole("button", { name: "重新读取记录" }))
    await waitFor(() => expect(mocks.reconcile).toHaveBeenCalledTimes(2))
    expect(await screen.findByText("本次费用：$0.00125")).toBeVisible()
    expect(screen.getByText("模型已响应")).toBeVisible()
    expect(screen.getByRole("link", { name: "查看本次请求记录" })).toHaveAttribute("href", "/observe/requests?view=ingress_chains&ingress_request_id=request-456")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it.each(["request", "record"] as const)("cancelling a pending %s restores controls and permits another attempt", async (phase) => {
    const user = userEvent.setup()
    if (phase === "request") {
      fetchMock.mockImplementationOnce((_url, options: RequestInit) => new Promise((_resolve, reject) => {
        options.signal?.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")))
      }))
    } else {
      fetchMock.mockResolvedValueOnce(response(200, "waiting-record"))
      mocks.reconcile.mockImplementationOnce(() => new Promise(() => {}))
    }
    fetchMock.mockResolvedValueOnce(response(200))
    renderDialog()
    await user.click(screen.getByRole("button", { name: "发送测试请求" }))
    if (phase === "record") expect(await screen.findByText("已收到响应")).toBeVisible()
    await user.click(screen.getByRole("button", { name: "取消等待" }))
    expect(await screen.findByText(phase === "request" ? "已停止等待" : /已停止读取记录/)).toBeVisible()
    expect(screen.queryByText(/记录读取失败/)).not.toBeInTheDocument()
    expect(screen.getByRole("combobox", { name: "可用模型" })).toBeEnabled()
    await user.click(screen.getByRole("button", { name: "再次发送" }))
    expect(await screen.findByText("已收到响应")).toBeVisible()
    expect(fetchMock).toHaveBeenCalledTimes(2)
  })

  it.each(["failed", "client_disconnected"])("does not show success when HTTP 200 finalized as %s", async (final_result) => {
    fetchMock.mockResolvedValue(response(200, "incomplete"))
    mocks.reconcile.mockResolvedValue({
      state: "ready", finalized: { final_result, final_status_code: 200, final_pricing_status: "ineligible" },
      detail: { request: { ingress_request_id: "incomplete" } },
    })
    renderDialog()
    await userEvent.setup().click(screen.getByRole("button", { name: "发送测试请求" }))
    expect(await screen.findByText("测试请求失败")).toBeVisible()
    expect(screen.queryByText("模型已响应")).not.toBeInTheDocument()
    expect(screen.getByRole("link", { name: "查看本次请求记录" })).toHaveAttribute("href", "/observe/requests?view=ingress_chains&ingress_request_id=incomplete")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it("requires a key when authentication is enabled and blocks sending while models fail", async () => {
    const user = userEvent.setup()
    renderDialog({ authEnabled: true, modelsError: true })
    expect(screen.getByRole("checkbox", { name: "不带密钥测试" })).toBeDisabled()
    await user.type(screen.getByLabelText("客户端密钥"), "client-test-key")
    expect(screen.getByRole("button", { name: "发送测试请求" })).toBeDisabled()
    expect(screen.getByRole("button", { name: "重试" })).toBeEnabled()
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
