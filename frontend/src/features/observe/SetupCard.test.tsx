import { createRef } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import type { SetupCoordinatorState } from "@/lib/types"
import { SetupCard } from "./SetupCard"
import { createInitialSetupState } from "./setupCoordinator"

vi.mock("@/hooks/useTimezone", () => ({ useTimezone: () => ({ format: (value: string) => value }) }))

function state(complete = false): SetupCoordinatorState {
  const initial = createInitialSetupState()
  return {
    ...initial,
    phase: "fresh",
    route_configured_count: complete ? 4 : 1,
    facts: initial.facts.map(fact => ({
      ...fact,
      fetch_quality: "fresh",
      result: fact.id === "proxy_keys" ? "skipped" : complete || fact.id === "routing" ? "complete" : "incomplete",
      reason_codes: ["no_route_witness"],
    })),
  }
}

function show(value: SetupCoordinatorState, onRetry = vi.fn()) {
  return render(<LocaleProvider><SetupCard state={value} collapsed={false} cardRef={createRef()} onBlurCapture={vi.fn()} onRetry={onRetry} onToggle={vi.fn()} /></LocaleProvider>)
}

describe("setup task guidance", () => {
  it("gives an empty installation an actionable model entry and explains open access without internal reasons", () => {
    show(state())
    expect(screen.getByRole("link", { name: "接入模型" })).toHaveAttribute("href", "/route/models?action=create")
    expect(screen.getByRole("link", { name: "客户端接入与验证" })).toHaveAttribute("href", "/system/proxy-keys")
    expect(screen.getByText(/任何能访问此地址的人也能管理配置/)).toBeVisible()
    expect(screen.queryByText(/no_route_witness|4 \/ 4|核心硬项/)).not.toBeInTheDocument()
  })

  it("separates saved configuration from a verified service connection and retains schedule limits", () => {
    const ready = state(true)
    ready.facts = ready.facts.map(fact => fact.id === "terminal_targets" ? { ...fact, detail: "该模型仅在你配置的时段内接受请求。" } : fact)
    show(ready)
    expect(screen.getByRole("link", { name: "管理模型" })).toHaveAttribute("href", "/route/models")
    expect(screen.getByText(/配置检查通过不代表服务已经连接成功/)).toBeVisible()
    expect(screen.getByText(/该模型仅在你配置的时段内接受请求/)).toBeVisible()
  })

  it("keeps raw read errors out of product guidance and lets the user retry", () => {
    const failed = state()
    failed.phase = "error"
    failed.route_configured_count = null
    failed.error = "SELECT * FROM model_configs: SQLSTATE 42P01 /internal/readiness"
    failed.facts = failed.facts.map(fact => ({ ...fact, fetch_quality: "error", detail: failed.error }))
    const retry = vi.fn()
    show(failed, retry)
    expect(screen.getByText(/这不代表原配置已丢失/)).toBeVisible()
    expect(screen.queryByText(/SELECT|SQLSTATE|internal\/readiness/)).not.toBeInTheDocument()
    fireEvent.click(screen.getAllByRole("button", { name: "重新检查" })[0])
    expect(retry).toHaveBeenCalledOnce()
  })
})
