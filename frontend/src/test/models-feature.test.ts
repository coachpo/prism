import { describe, expect, it } from "vitest"
import { DEFAULT_MODEL_FORM_DATA, toModelCreatePayload, validateModelFormData } from "@/pages/models/modelFormState"
import { modelsQueryKeys } from "@/features/models/queryKeys"

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
      openai_image_operations: null,
      is_enabled: true,
    })
    expect(Object.prototype.hasOwnProperty.call(payload, "access_targets")).toBe(false)
  })

  // Both authoring surfaces (models list and model detail) validate through
  // this helper directly, so it is the path that decides whether a form saves.
  it("allows enabled state through model CRUD validation without target payloads", () => {
    expect(validateModelFormData({
      ...baseForm,
      is_enabled: true,
    })).toBe(null)
  })
})
