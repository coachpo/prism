import { useState } from "react"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { describe, expect, it, vi } from "vitest"
import { LocaleProvider } from "@/i18n/LocaleProvider"
import { getStaticMessages } from "@/i18n/staticMessages"
import { api, ApiError } from "@/lib/api"
import type { Endpoint } from "@/lib/types"
import { EndpointDialog } from "./EndpointDialog"
import { useEndpointFormMutations } from "./useEndpointFormMutations"

const copy = getStaticMessages().endpointsUi
const endpoint: Endpoint = {
  id: 21, name: "测试服务", base_url: "https://service.test", has_api_key: true,
  api_key_fingerprint: "test-key", api_key_updated_at: "2026-09-12T00:00:00Z",
  config_revision: 4, created_at: "2026-09-12T00:00:00Z", updated_at: "2026-09-12T00:00:00Z",
}

function Harness({ onContinue = vi.fn() }: { onContinue?: (endpoint: Endpoint) => void }) {
  const [open, setOpen] = useState(true)
  const forms = useEndpointFormMutations({ commitEndpoints: vi.fn(), references: { addEndpoint: vi.fn(), invalidateEndpoint: vi.fn() } })
  return <LocaleProvider><EndpointDialog open={open} mode="create" onOpenChange={setOpen} onSubmit={forms.handleCreate} onVerify={forms.handleVerify} onContinue={onContinue} serverError={forms.endpointDialogError} fieldErrors={forms.endpointFieldErrors} /></LocaleProvider>
}

function fillDraft() {
  fireEvent.change(screen.getByRole("textbox", { name: /服务名称/ }), { target: { value: endpoint.name } })
  fireEvent.change(screen.getByRole("textbox", { name: /服务接入地址/ }), { target: { value: endpoint.base_url } })
  fireEvent.change(screen.getByLabelText(/API 密钥/), { target: { value: "test-only-key" } })
}

describe("service save workflow", () => {
  it("validates before either action and keeps save independent of verification", async () => {
    const create = vi.spyOn(api.endpoints, "create").mockResolvedValue(endpoint)
    const verify = vi.spyOn(api.endpoints, "verify")
    render(<Harness />)
    fireEvent.click(screen.getByRole("button", { name: copy.saveAndVerify }))
    expect(await screen.findByText(copy.nameRequired)).toBeVisible()
    expect(screen.getByText(copy.baseUrlInvalid)).toBeVisible()
    expect(create).not.toHaveBeenCalled()
    fillDraft()
    fireEvent.change(screen.getByRole("textbox", { name: /服务接入地址/ }), { target: { value: `${endpoint.base_url}/v1` } })
    expect(screen.getByText(copy.baseUrlVersionWarning)).toBeVisible()
    fireEvent.click(screen.getByRole("button", { name: copy.useServiceRoot }))
    expect(screen.getByRole("textbox", { name: /服务接入地址/ })).toHaveValue(endpoint.base_url)
    fireEvent.click(screen.getByRole("button", { name: copy.saveAndVerify }))
    expect(await screen.findByRole("alert")).toHaveTextContent(copy.verifyFamilyRequired)
    fireEvent.click(screen.getByRole("button", { name: copy.saveOnly }))
    expect(await screen.findByText(copy.savedTitle)).toBeVisible()
    expect(verify).not.toHaveBeenCalled()
    expect(create).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole("button", { name: copy.saveOnly })).not.toBeInTheDocument()
    expect(screen.getAllByRole("dialog")).toHaveLength(1)
  })

  it("retains a failed draft and clears the error on successful retry before handing off", async () => {
    const create = vi.spyOn(api.endpoints, "create").mockRejectedValueOnce(new ApiError("sql: secret internal path", 503, {})).mockResolvedValueOnce(endpoint)
    const onContinue = vi.fn()
    render(<Harness onContinue={onContinue} />)
    fillDraft()
    fireEvent.click(screen.getByRole("button", { name: copy.saveOnly }))
    expect(await screen.findByTestId("endpoint-form-server-error")).toHaveTextContent("若刚才保存过内容，请先刷新确认结果")
    expect(screen.queryByText(/sql:/)).not.toBeInTheDocument()
    expect(screen.getByRole("textbox", { name: /服务名称/ })).toHaveValue(endpoint.name)
    expect(screen.getByLabelText(/API 密钥/)).toHaveValue("test-only-key")
    fireEvent.click(screen.getByRole("button", { name: copy.saveOnly }))
    await screen.findByText(copy.savedTitle)
    expect(screen.queryByTestId("endpoint-form-server-error")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: getStaticMessages().endpointsPage.attachToModel }))
    expect(onContinue).toHaveBeenCalledWith(endpoint)
    expect(create).toHaveBeenCalledTimes(2)
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument())
  })

  it("verifies the saved revision and reports failed verification without another create", async () => {
    HTMLElement.prototype.hasPointerCapture ??= () => false
    HTMLElement.prototype.setPointerCapture ??= () => {}
    HTMLElement.prototype.releasePointerCapture ??= () => {}
    HTMLElement.prototype.scrollIntoView ??= () => {}
    const user = userEvent.setup()
    const create = vi.spyOn(api.endpoints, "create").mockResolvedValue(endpoint)
    const verify = vi.spyOn(api.endpoints, "verify").mockResolvedValue({ endpoint_id: endpoint.id, api_family: "openai", config_revision: 4, api_key_fingerprint: "test-key", is_current: true, outcome: "unreachable", probe_path: "/internal-probe", upstream_status: null, duration_ms: 1, error_summary: "dial tcp internal.test: sql" })
    render(<Harness />)
    fillDraft()
    await user.click(screen.getByRole("combobox", { name: copy.verifyFamily }))
    await user.click(screen.getByRole("option", { name: "OpenAI" }))
    await user.click(screen.getByRole("button", { name: copy.saveAndVerify }))
    expect(await screen.findByTestId("verify-result")).toHaveTextContent("无法连接模型服务")
    expect(verify).toHaveBeenCalledWith(endpoint.id, { api_family: "openai", expected_config_revision: 4 })
    expect(create).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/internal/)).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: copy.returnToServices })).toBeEnabled()
    await user.click(screen.getByRole("button", { name: copy.verifyRetry }))
    await waitFor(() => expect(verify).toHaveBeenCalledTimes(2))
    expect(create).toHaveBeenCalledTimes(1)
  })
})
