import { describe, expect, it } from "vitest";

import {
  buildCatalogOverridePatch,
  type CatalogOverrideDraft,
} from "./catalogOverrideDraft";
import { CATALOG_FIELD_ORDER } from "./catalogMetadataPresentation";

describe("catalog override draft", () => {
  it("expresses all 18 fields and preserves missing/null/empty-value states", () => {
    const draft: CatalogOverrideDraft = {
      name: { mode: "value", raw: "" },
      description: { mode: "value", raw: "description" },
      family: { mode: "restore" },
      release_date: { mode: "value", raw: "2026-08" },
      last_updated: { mode: "value", raw: "2026-08-28" },
      knowledge: { mode: "value", raw: "" },
      attachment: { mode: "value", raw: "true" },
      reasoning: { mode: "value", raw: "false" },
      tool_call: { mode: "restore" },
      structured_output: { mode: "value", raw: "true" },
      temperature: { mode: "value", raw: "false" },
      modalities_input: { mode: "value", raw: "text, image" },
      modalities_output: { mode: "value", raw: "" },
      limit_context: { mode: "value", raw: "0" },
      limit_input: { mode: "value", raw: "128000" },
      limit_output: { mode: "restore" },
      open_weights: { mode: "value", raw: "false" },
      status: { mode: "value", raw: "beta" },
    };

    const result = buildCatalogOverridePatch(draft);
    expect(CATALOG_FIELD_ORDER).toHaveLength(18);
    expect(result.errors).toEqual({});
    expect(Object.keys(result.patch)).toHaveLength(18);
    expect(result.patch).toMatchObject({
      name: "",
      family: null,
      knowledge: "",
      attachment: true,
      reasoning: false,
      tool_call: null,
      modalities_input: ["text", "image"],
      modalities_output: [],
      limit_context: 0,
      limit_output: null,
      status: "beta",
    });

    const missing = buildCatalogOverridePatch({ name: draft.name });
    expect(Object.hasOwn(missing.patch, "description")).toBe(false);
  });

  it("rejects invalid typed values without emitting those keys", () => {
    const result = buildCatalogOverridePatch({
      release_date: { mode: "value", raw: "August" },
      attachment: { mode: "value", raw: "maybe" },
      limit_context: { mode: "value", raw: "-1" },
      status: { mode: "value", raw: "stable" },
      description: { mode: "value", raw: "界".repeat(167) },
    });

    expect(Object.keys(result.errors)).toEqual([
      "release_date",
      "attachment",
      "limit_context",
      "status",
      "description",
    ]);
    expect(result.patch).toEqual({});
  });
});
