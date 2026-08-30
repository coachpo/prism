import { describe, expect, it } from "vitest"
import { buildCompositeModelCreatePayload } from "@/pages/models/compositeModelCreatePayload"
import { modelsQueryKeys } from "@/features/models/queryKeys"

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

  it("omits OpenAI-only keys from non-OpenAI composite creates", () => {
    const payload = buildCompositeModelCreatePayload({
      apiFamily: "anthropic",
      modelId: "claude-test",
      displayName: "Claude Test",
      loadbalanceStrategyId: 11,
      configureLater: false,
      openAIAcceptedFormat: null,
      openAIImageOperations: null,
      initialTerminalTarget: { endpoint_id: 3, is_active: true },
    })

    expect(payload).toEqual({
      api_family: "anthropic",
      model_id: "claude-test",
      display_name: "Claude Test",
      loadbalance_strategy_id: 11,
      is_enabled: true,
      initial_terminal_target: { endpoint_id: 3, is_active: true },
    })
    expect(JSON.stringify(payload)).not.toContain("openai_")
  })
})
