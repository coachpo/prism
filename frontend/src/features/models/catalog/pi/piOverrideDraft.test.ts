import { describe, expect, it } from "vitest";

import { buildPiOverrideFields, type PiOverrideDraft } from "./piOverrideDraft";

describe("buildPiOverrideFields", () => {
  it("emits only explicitly changed fields and preserves restore null", () => {
    const result = buildPiOverrideFields({
      name: { mode: "value", raw: "Operator name" },
      reasoning: { mode: "restore" },
    });

    expect(result).toEqual({
      fields: { name: "Operator name", reasoning: null },
      errors: {},
    });
  });

  it("parses all seven safe Pi metadata fields", () => {
    const draft: PiOverrideDraft = {
      name: { mode: "value", raw: "Operator name" },
      reasoning: { mode: "value", raw: "false" },
      input: { mode: "value", raw: "text, image" },
      context_window: { mode: "value", raw: "200000" },
      max_tokens: { mode: "value", raw: "8192" },
      thinking_level_map: {
        mode: "value",
        raw: '{"low":"low","max":null}',
      },
      compat: {
        mode: "value",
        raw: '{"supportsStore":true}',
      },
    };

    expect(buildPiOverrideFields(draft)).toEqual({
      fields: {
        name: "Operator name",
        reasoning: false,
        input: ["text", "image"],
        context_window: 200000,
        max_tokens: 8192,
        thinking_level_map: { low: "low", max: null },
        compat: { supportsStore: true },
      },
      errors: {},
    });
  });

  it.each([
    ["name", "", "name_required"],
    ["reasoning", "", "boolean_required"],
    ["input", "audio", "input_invalid"],
    ["context_window", "-1", "integer_invalid"],
    ["max_tokens", "0", "integer_invalid"],
    ["thinking_level_map", "[]", "object_required"],
    ["thinking_level_map", '{"turbo":true}', "thinking_map_invalid"],
    ["compat", "{", "json_invalid"],
  ] as const)("rejects invalid %s drafts", (field, raw, expectedError) => {
    const result = buildPiOverrideFields({
      [field]: { mode: "value", raw },
    });
    expect(result.fields).toEqual({});
    expect(result.errors).toEqual({ [field]: expectedError });
  });
});
