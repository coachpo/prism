import { render, screen } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import { ProxyKeyExpiryField, type ResolvedExpiryInput } from "@/pages/proxy-api-keys/ProxyKeyExpiryField"

function renderField(mode: "create" | "edit", timezone: string, currentInstant: string | null, onChange: (value: ResolvedExpiryInput) => void) {
  return render(
    <LocaleProvider>
      <ProxyKeyExpiryField
        mode={mode}
        timezone={timezone}
        timezoneLoading={false}
        currentInstant={currentInstant}
        onChange={onChange}
      />
    </LocaleProvider>,
  )
}

// US Eastern: DST gap on 2026-03-08 02:00→03:00, overlap on 2026-11-01 01:00→01:00.
describe("ProxyKeyExpiryField timezone contract", () => {
  it("parses wall clock in the Settings timezone and submits RFC3339", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    renderField("create", "America/New_York", null, onChange)
    await user.click(screen.getByLabelText("永不过期"))
    const input = screen.getByLabelText("过期时间")
    await user.clear(input)
    await user.type(input, "2026-08-09T15:30")
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as ResolvedExpiryInput
    expect(lastCall.gapError).toBe(false)
    expect(lastCall.instant).not.toBeNull()
    // 15:30 EDT == 19:30 UTC.
    expect(lastCall.instant).toMatch(/^2026-08-09T19:30:00/)
  })

  it("blocks DST gap wall times with a field error", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    renderField("create", "America/New_York", null, onChange)
    await user.click(screen.getByLabelText("永不过期"))
    const input = screen.getByLabelText("过期时间")
    await user.clear(input)
    await user.type(input, "2026-03-08T02:30")
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as ResolvedExpiryInput
    expect(lastCall.gapError).toBe(true)
    expect(lastCall.instant).toBeNull()
    expect(screen.getByRole("alert").textContent).toContain("夏令时跳变")
  })

  it("resolves DST overlap to the earlier occurrence with a notice", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    renderField("create", "America/New_York", null, onChange)
    await user.click(screen.getByLabelText("永不过期"))
    const input = screen.getByLabelText("过期时间")
    await user.clear(input)
    await user.type(input, "2026-11-01T01:30")
    const lastCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as ResolvedExpiryInput
    expect(lastCall.gapError).toBe(false)
    expect(lastCall.overlapNotice).toBe(true)
    // Earlier occurrence: 01:30 EDT == 05:30 UTC.
    expect(lastCall.instant).toMatch(/^2026-11-01T05:30:00/)
  })

  it("edit mode supports preserve/set/clear tri-state", async () => {
    const user = userEvent.setup()
    const onChange = vi.fn()
    renderField("edit", "UTC", "2026-08-09T12:00:00Z", onChange)

    // Preserve (default) emits preserved.
    expect(onChange).not.toHaveBeenCalled()
    await user.click(screen.getByLabelText("清除过期时间"))
    const clearCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as ResolvedExpiryInput
    expect(clearCall.preserved).toBe(false)
    expect(clearCall.instant).toBeNull()

    await user.click(screen.getByLabelText("设置"))
    const setCall = onChange.mock.calls[onChange.mock.calls.length - 1][0] as ResolvedExpiryInput
    expect(setCall.preserved).toBe(false)
  })

  it("shows explicit UTC with a timezone-unavailable label when the zone cannot be loaded", () => {
    const onChange = vi.fn()
    render(
      <LocaleProvider>
        <ProxyKeyExpiryField mode="edit" timezone={null} timezoneLoading={false} currentInstant="2026-08-09T12:00:00Z" onChange={onChange} />
      </LocaleProvider>,
    )
    expect(screen.getByText(/Settings 时区不可用/)).toBeTruthy()
  })
})
