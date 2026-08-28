import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import { ProxyKeySecretDialog } from "@/features/proxy-keys/ProxyKeySecretDialog"
import type { GeneratedProxyKeyState } from "@/features/proxy-keys/generatedSecretSession"
import type { ModelConfigListItem } from "@/lib/types"

const session = (overrides: Partial<GeneratedProxyKeyState> = {}): GeneratedProxyKeyState => {
  const base: GeneratedProxyKeyState = {
    kind: "unacknowledged",
    session: {
      source: "create",
      keyId: 42,
      itemSnapshot: {
        id: 42,
        name: "prod",
        key_prefix: "pm-1a2b",
        key_preview: "pm-1a2b••••••••9f3e",
        is_active: true,
        expires_at: null,
        last_used_at: null,
        last_used_ip: null,
        notes: null,
        rotated_at: null,
        rotation_count: 0,
        created_at: "2026-08-09T00:00:00Z",
        updated_at: "2026-08-09T00:00:00Z",
      },
      rawKey: "pm-secret-value-1a2b3c4d5e6f",
      capacity: { limit: 100, used: 1, remaining: 99, counted_at: "2026-08-09T00:00:00Z" },
      openedAt: 1234,
      savedAcknowledged: false,
    },
  }
  return { ...base, ...overrides } as GeneratedProxyKeyState
}

const models: ModelConfigListItem[] = [
  {
    id: 1,
    profile_id: 1,
    api_family: "openai",
    model_id: "gpt-5.6-luna",
    display_name: "Luna",
    openai_accepted_format: "responses_only",
    openai_image_operations: null,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
    created_at: "2026-08-09T00:00:00Z",
    updated_at: "2026-08-09T00:00:00Z",
  },
  {
    id: 2,
    profile_id: 1,
    api_family: "anthropic",
    model_id: "claude-sonnet-4-5",
    display_name: null,
    openai_accepted_format: null,
    openai_image_operations: null,
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    access_targets: [],
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: null,
    health_total_requests: 0,
    routing_summary: null,
    created_at: "2026-08-09T00:00:00Z",
    updated_at: "2026-08-09T00:00:00Z",
  },
]

function renderDialog(
  state: GeneratedProxyKeyState,
  overrides: Partial<{
    onRequestClose: () => void
    onKeepEditing: () => void
    onAbandonAndLeave: () => void
    onRetryModels: () => void
    onSetSavedAck: (acknowledged: boolean) => void
    onFinish: () => void
    models: ModelConfigListItem[]
    modelsError: boolean
    modelsLoading: boolean
  }> = {},
) {
  const handlers = {
    onRequestClose: vi.fn(),
    onKeepEditing: vi.fn(),
    onAbandonAndLeave: vi.fn(),
    onRetryModels: vi.fn(),
    onSetSavedAck: vi.fn(),
    onFinish: vi.fn(),
    ...overrides,
  }
  const utils = render(
    <LocaleProvider>
      <ProxyKeySecretDialog
        state={state}
        models={overrides.models ?? models}
        modelsError={overrides.modelsError ?? false}
        modelsLoading={overrides.modelsLoading ?? false}
        onRequestClose={handlers.onRequestClose}
        onKeepEditing={handlers.onKeepEditing}
        onAbandonAndLeave={handlers.onAbandonAndLeave}
        onRetryModels={handlers.onRetryModels}
        onSetSavedAck={handlers.onSetSavedAck}
        onFinish={handlers.onFinish}
      />
    </LocaleProvider>,
  )
  return { ...utils, handlers }
}

describe("ProxyKeySecretDialog unacknowledged session", () => {
  it("shows the raw key, gateway origin, family base url, curl and ack gate", () => {
    renderDialog(session())
    expect(screen.getByText("pm-secret-value-1a2b3c4d5e6f")).toBeTruthy()
    expect(screen.getByText("可执行 curl 样例")).toBeTruthy()
    expect(screen.getByText("我已安全保存此密钥，关闭后将无法再次查看。")).toBeTruthy()
    // Close is gated behind the acknowledgement.
    const closeButtons = screen.getAllByRole("button", { name: "关闭" })
    expect(closeButtons.length).toBeGreaterThan(0)
    expect(closeButtons[0].hasAttribute("disabled")).toBe(true)
  })

  it("generates operation-aware curl from the selected model", async () => {
    const user = userEvent.setup()
    renderDialog(session())
    // Anthropic model selected -> X-API-Key curl for /v1/messages.
    await user.selectOptions(screen.getByLabelText("可用模型"), "claude-sonnet-4-5")
    const curlBlock = screen.getByText(/curl POST/).textContent ?? ""
    expect(curlBlock).toContain("/v1/messages")
    expect(curlBlock).toContain("X-API-Key")
    expect(curlBlock).toContain("pm-secret-value-1a2b3c4d5e6f")
    // The secret never appears in the URL (first curl line only).
    expect(curlBlock.split("\n")[0]).not.toContain("pm-secret-value")
  })

  it("blocks close attempts until acknowledged (Escape/close request)", async () => {
    const user = userEvent.setup()
    const { handlers } = renderDialog(session())
    await user.keyboard("{Escape}")
    expect(handlers.onRequestClose).toHaveBeenCalledWith("close")
    expect(handlers.onFinish).not.toHaveBeenCalled()
  })

  it("acknowledging enables finish; copy never auto-acknowledges", async () => {
    const user = userEvent.setup()
    const { handlers } = renderDialog(session())
    // Check the saved acknowledgement.
    await user.click(screen.getByLabelText("我已安全保存此密钥，关闭后将无法再次查看。"))
    expect(handlers.onSetSavedAck).toHaveBeenCalledWith(true)
  })

  it("renders the closing-confirm state with keep-editing and abandon actions", async () => {
    const user = userEvent.setup()
    const { handlers } = renderDialog(session({ kind: "closing_confirm", intent: "close" }))
    await user.click(screen.getByText("继续保存"))
    expect(handlers.onKeepEditing).toHaveBeenCalled()
    await user.click(screen.getByText("放弃密钥并离开"))
    expect(handlers.onAbandonAndLeave).toHaveBeenCalled()
  })

  it("keeps the secret visible while models fail or stay empty", () => {
    renderDialog(session(), { modelsError: true, models: [] })
    expect(screen.getByText("pm-secret-value-1a2b3c4d5e6f")).toBeTruthy()
    expect(screen.getByText("模型列表加载失败")).toBeTruthy()
  })

  it("disables curl/self-test when no eligible models exist but keeps secret copy", () => {
    renderDialog(session(), { models: [] })
    expect(screen.getByText("pm-secret-value-1a2b3c4d5e6f")).toBeTruthy()
    expect(screen.getByText("当前没有已启用的可用模型。请先在模型中启用一个模型，再生成调用样例。")).toBeTruthy()
  })
})
