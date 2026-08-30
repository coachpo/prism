import { describe, expect, it } from "vitest";
import {
  buildEndpointCreatePayload,
  buildEndpointUpdatePayload,
  canonicalBaseURLPreview,
  hasEndpointReviewFilters,
} from "@/features/endpoints/endpointSchemas";
import {
  buildPricingTemplateCreatePayload,
  buildPricingTemplateUpdatePayload,
  pricingTemplateFormSchema,
} from "@/features/pricing/pricingSchemas";
import { isPricingTemplateDeleteBlocked } from "@/features/pricing/pricingDeletion";
import type { PricingTemplate } from "@/lib/types";

const pricingTemplate: PricingTemplate = {
  id: 9,
  profile_id: 7,
  name: "GPT standard",
  description: null,
  pricing_unit: "PER_1M",
  pricing_currency_code: "USD",
  active_currency_symbol: "$",
  catalog_provider_id: null,
  catalog_model_id: null,
  revision_source: "manual",
  catalog_revision: null,
  template_kind: "standard",
  card: {
    input_price: "1",
    output_price: "2",
    cached_input_price: "0",
    cache_creation_price: "0",
    reasoning_price: "0",
  },
  version: 3,
  revision_id: 9,
  version_effective_at: null,
  reporting_currency_epoch: 1,
  revision_count: 1,
  created_at: "2026-06-11T00:00:00Z",
  updated_at: "2026-06-11T01:00:00Z",
};

describe("Task 10 endpoint feature contracts", () => {
  it("builds create and edit payloads without exposing masked secrets", () => {
    expect(
      buildEndpointCreatePayload({
        name: "  OpenAI ",
        base_url: " https://api.openai.test/v1 ",
        api_key: " sk-live-secret ",
      }),
    ).toEqual({
      name: "OpenAI",
      base_url: "https://api.openai.test/v1",
      api_key: "sk-live-secret",
    });
    expect(
      buildEndpointUpdatePayload({
        name: "OpenAI",
        base_url: "https://api.openai.test/v1",
        api_key: "   ",
      }),
    ).toEqual({
      name: "OpenAI",
      base_url: "https://api.openai.test/v1",
    });
  });

  it("normalizes the base URL preview without mutating keystrokes", () => {
    expect(canonicalBaseURLPreview(" https://api.openai.test/v1/ ")).toBe(
      "https://api.openai.test/v1",
    );
    expect(canonicalBaseURLPreview("https://api.openai.test/")).toBe(
      "https://api.openai.test",
    );
    expect(canonicalBaseURLPreview("https://")).toBe("https://");
  });

  it("tracks active review filters for reference-derived filtering", () => {
    expect(
      hasEndpointReviewFilters({ searchQuery: "openai", reviewFilter: "all" }),
    ).toBe(true);
    expect(
      hasEndpointReviewFilters({ searchQuery: "", reviewFilter: "referenced" }),
    ).toBe(true);
    expect(
      hasEndpointReviewFilters({ searchQuery: "", reviewFilter: "all" }),
    ).toBe(false);
  });
});

describe("Task 10 pricing feature contracts", () => {
  it("keeps blank specialty prices unconfigured and requires base prices", () => {
    const parsed = pricingTemplateFormSchema.parse({
      name: "Standard",
      description: " ",
      template_kind: "standard",
      input_price: "1.25",
      output_price: "2.5",
      cached_input_price: " ",
      cache_creation_price: "\t",
      reasoning_price: "0.75",
      tier: {
        input_tokens_above: "",
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      peak_card: {
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      offpeak_card: {
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      schedule_timezone: "",
      schedule_windows: [],
    });
    expect(parsed.cached_input_price.trim()).toBe("");
    expect(parsed.cache_creation_price.trim()).toBe("");
    expect(() =>
      pricingTemplateFormSchema.parse({ ...parsed, reasoning_price: "-1" }),
    ).toThrow();
    expect(() =>
      pricingTemplateFormSchema.parse({ ...parsed, input_price: " " }),
    ).toThrow();
  });

  it("validates a complete tier card and keeps whole-card payload semantics", () => {
    const values = {
      name: "Tiered",
      description: "",
      template_kind: "tiered" as const,
      input_price: "2",
      output_price: "5",
      cached_input_price: "1",
      cache_creation_price: "2",
      reasoning_price: "3",
      tier: {
        input_tokens_above: "272000",
        input_price: "4",
        output_price: "18",
        cached_input_price: "2",
        cache_creation_price: "5",
        reasoning_price: "20",
      },
      peak_card: {
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      offpeak_card: {
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      schedule_timezone: "",
      schedule_windows: [],
    };
    const payload = buildPricingTemplateCreatePayload(values);
    expect(payload.tier).toEqual({
      input_tokens_above: 272000,
      card: {
        input_price: "4",
        output_price: "18",
        cached_input_price: "2",
        cache_creation_price: "5",
        reasoning_price: "20",
      },
    });
    expect(() =>
      pricingTemplateFormSchema.parse({
        ...values,
        tier: { ...values.tier, reasoning_price: "" },
      }),
    ).toThrow();
  });

  it("builds create and CAS update payloads with backend snake_case fields", () => {
    const values = {
      name: " Standard ",
      description: " optional ",
      template_kind: "standard" as const,
      input_price: "1",
      output_price: "2",
      cached_input_price: "0.1",
      cache_creation_price: " ",
      reasoning_price: "3",
      tier: {
        input_tokens_above: "",
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      peak_card: {
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      offpeak_card: {
        input_price: "",
        output_price: "",
        cached_input_price: "",
        cache_creation_price: "",
        reasoning_price: "",
      },
      schedule_timezone: "",
      schedule_windows: [],
    };
    expect(buildPricingTemplateCreatePayload(values)).toEqual({
      name: "Standard",
      description: "optional",
      template_kind: "standard",
      card: {
        input_price: "1",
        output_price: "2",
        cached_input_price: "0.1",
        cache_creation_price: null,
        reasoning_price: "3",
      },
    });
    expect(
      buildPricingTemplateUpdatePayload(pricingTemplate, values)
        .expected_updated_at,
    ).toBe(pricingTemplate.updated_at);
  });

  it("blocks pricing template deletes while dependencies are present", () => {
    expect(
      isPricingTemplateDeleteBlocked({
        deleting: false,
        usageLoading: false,
        usageError: false,
        dependencyCount: 1,
      }),
    ).toBe(true);
    expect(
      isPricingTemplateDeleteBlocked({
        deleting: false,
        usageLoading: false,
        usageError: false,
        dependencyCount: 0,
      }),
    ).toBe(false);
  });
});
