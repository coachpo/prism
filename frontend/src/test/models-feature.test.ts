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
  it("includes selected profile and filters in the model list query key", () => {
    expect(modelsQueryKeys.list(7, { search: " gpt ", api_family: "openai", status: "enabled" })).toEqual([
      "rewrite",
      "selected-profile",
      "7",
      "models",
      "list",
      { search: "gpt", api_family: "openai", status: "enabled" },
    ])
  })

  it("preserves backend field names in create payload transforms", () => {
    const payload = toModelCreatePayload({
      ...baseForm,
      display_name: "GPT Entry",
      access_targets: [{ target_type: "model", target_model_id: "gpt-large", position: 0, is_enabled: true }],
      is_enabled: true,
    })

    expect(payload).toEqual({
      api_family: "openai",
      model_id: "gpt-entry",
      display_name: "GPT Entry",
      loadbalance_strategy_id: 11,
      openai_accepted_format: "dual_native",
      access_targets: [{ target_type: "model", target_model_id: "gpt-large", position: 0, is_enabled: true }],
      is_enabled: true,
    })
    expect(Object.prototype.hasOwnProperty.call(payload.access_targets[0], "weight")).toBe(false)
    expect(Object.prototype.hasOwnProperty.call(payload.access_targets[0], "target_priority")).toBe(false)
  })

  it("requires enabled models to have a valid same-family access target", () => {
    expect(validateModelAuthoringValues({
      ...baseForm,
      is_enabled: true,
      access_targets: [],
    })).toBe("access_target_required")
  })
  it("exposes React Hook Form options backed by the Zod authoring schema", () => {
    const options = createModelAuthoringFormOptions(baseForm)

    expect(options.defaultValues.model_id).toBe("gpt-entry")
    expect(options.mode).toBe("onSubmit")
    expect(options.resolver).toBeTypeOf("function")
  })
})
