import { describe, expect, it } from "vitest";
import {
  DEFAULT_MODEL_FORM_DATA,
  normalizeProxyTargets,
  setModelIdOnForm,
  toModelCreatePayload,
  toModelUpdatePayload,
  type ModelFormData,
} from "../modelFormState";

function buildForm(overrides: Partial<ModelFormData> = {}): ModelFormData {
  return {
    ...DEFAULT_MODEL_FORM_DATA,
    model_id: "gpt-5.4",
    display_name: "",
    model_type: "native",
    proxy_targets: [],
    loadbalance_strategy_id: 42,
    is_enabled: true,
    ...overrides,
  };
}

describe("modelFormState", () => {
  it("trims, deduplicates, and reindexes proxy targets", () => {
    expect(
      normalizeProxyTargets([
        { target_model_id: " native-a ", position: 9 },
        { target_model_id: "native-a", position: 1 },
        { target_model_id: "", position: 2 },
        { target_model_id: "native-b", position: 5 },
      ]),
    ).toEqual([
      { target_model_id: "native-a", position: 0 },
      { target_model_id: "native-b", position: 1 },
    ]);
  });

  it("auto-syncs the display name until the user overrides it", () => {
    const synced = setModelIdOnForm(buildForm({ last_auto_display_name: "" }), "gpt-5.5");

    expect(synced.display_name).toBe("gpt-5.5");
    expect(synced.last_auto_display_name).toBe("gpt-5.5");

    const resynced = setModelIdOnForm(synced, "gpt-5.6");

    expect(resynced.display_name).toBe("gpt-5.6");
    expect(resynced.last_auto_display_name).toBe("gpt-5.6");

    const preserved = setModelIdOnForm(
      { ...resynced, display_name: "Friendly name", last_auto_display_name: "gpt-5.6" },
      "gpt-5.7",
    );

    expect(preserved.display_name).toBe("Friendly name");
    expect(preserved.last_auto_display_name).toBe("gpt-5.7");
  });

  it("builds native create payloads with a fallback display name and strategy", () => {
    expect(
      toModelCreatePayload(
        buildForm({
          display_name: "   ",
          loadbalance_strategy_id: 99,
          model_type: "native",
          proxy_targets: [{ target_model_id: "unused", position: 0 }],
        }),
      ),
    ).toEqual({
      vendor_id: null,
      api_family: "openai",
      model_id: "gpt-5.4",
      display_name: "gpt-5.4",
      model_type: "native",
      is_enabled: true,
      proxy_targets: [],
      loadbalance_strategy_id: 99,
    });
  });

  it("builds proxy update payloads without leaking native routing fields", () => {
    expect(
      toModelUpdatePayload(
        buildForm({
          model_id: "proxy-router",
          display_name: "",
          model_type: "proxy",
          proxy_targets: [{ target_model_id: "native-a", position: 0 }],
          loadbalance_strategy_id: 42,
        }),
      ),
    ).toEqual({
      vendor_id: null,
      api_family: "openai",
      display_name: null,
      model_id: "proxy-router",
      model_type: "proxy",
      is_enabled: true,
      loadbalance_strategy_id: null,
    });
  });
});
