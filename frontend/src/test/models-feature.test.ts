import { describe, expect, it } from "vitest"
import { DEFAULT_MODEL_FORM_DATA, toModelCreatePayload } from "@/pages/models/modelFormState"
import { createModelAuthoringFormOptions } from "@/features/models/modelForm"
import { modelsQueryKeys } from "@/features/models/queryKeys"
import { validateModelAuthoringValues } from "@/features/models/modelSchemas"

const baseForm = {
  ...DEFAULT_MODEL_FORM_DATA,
  model_id: "gpt-entry",
  loadbalance_strategy_id: 11,
}

describe("models feature contracts", () => {
  it("includes the pinned profile and filters in the model list query key", () => {
    expect(modelsQueryKeys.list(1, { search: " gpt ", api_family: "openai", status: "enabled" })).toEqual([
      "rewrite",
      "selected-profile",
      "1",
      "models",
      "list",
      { search: "gpt", api_family: "openai", status: "enabled" },
    ])
  })

  it("preserves backend field names in create payload transforms", () => {
    const payload = toModelCreatePayload({
      ...baseForm,
      display_name: "GPT Entry",
      is_enabled: true,
    })

    expect(payload).toEqual({
      api_family: "openai",
      model_id: "gpt-entry",
      display_name: "GPT Entry",
      loadbalance_strategy_id: 11,
      openai_accepted_format: "dual_native",
      is_enabled: true,
    })
    expect(Object.prototype.hasOwnProperty.call(payload, "access_targets")).toBe(false)
  })

  it("allows enabled state through model CRUD validation without target payloads", () => {
    expect(validateModelAuthoringValues({
      ...baseForm,
      is_enabled: true,
    })).toBe(null)
  })
  it("exposes React Hook Form options backed by the Zod authoring schema", () => {
    const options = createModelAuthoringFormOptions(baseForm)

    expect(options.defaultValues.model_id).toBe("gpt-entry")
    expect(options.mode).toBe("onSubmit")
    expect(options.resolver).toBeTypeOf("function")
  })
})
