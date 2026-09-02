import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { LocaleProvider } from "@/i18n/LocaleProvider"
import type { ModelConfigListItem } from "@/lib/types"
import { ProxyKeyVerifyAccessDialog } from "./ProxyKeyVerifyAccessDialog"

const model = (id: number, model_id: string, direct_request_enabled: boolean, is_enabled = true) => ({
  id, model_id, direct_request_enabled, is_enabled,
  api_family: "openai", display_name: null,
  openai_accepted_format: "responses_only", openai_image_operations: null,
}) as ModelConfigListItem

describe("ProxyKeyVerifyAccessDialog", () => {
  it("offers only enabled direct entries in the standing self-test", () => {
    render(
      <LocaleProvider>
        <ProxyKeyVerifyAccessDialog
          models={[model(1, "internal-target", false), model(2, "disabled-entry", true, false), model(3, "direct-entry", true)]}
          modelsError={false}
          modelsLoading={false}
          onOpenChange={vi.fn()}
          onRetryModels={vi.fn()}
          open
        />
      </LocaleProvider>,
    )

    const modelSelect = screen.getByRole("combobox", { name: "可用模型" })
    expect(modelSelect).toHaveTextContent("direct-entry")
  })
})
