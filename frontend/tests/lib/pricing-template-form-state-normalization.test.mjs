import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const staticMessagesStub = {
  getStaticMessages: () => ({
    pricingTemplatesData: {
      endpointWithId: (id) => `Endpoint #${id}`,
      unknownModel: "Unknown model",
    },
  }),
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "@/i18n/staticMessages": staticMessagesStub,
  },
});
const {
  DEFAULT_PRICING_TEMPLATE_FORM,
  isNonNegativeDecimalString,
  normalizePricingTemplateFormPrices,
  normalizeTemplatePrice,
  pricingTemplateFormStateFromTemplate,
} = load(path.join(frontendDir, "src/features/pricing/pricingSchemas.ts"));
const {
  pricingMinuteToTime,
  pricingTimeToMinute,
  pricingWindowEndMinute,
  togglePricingWeekday,
} = load(path.join(frontendDir, "src/features/pricing/pricingWindowDraft.ts"));

const priceFields = [
  "input_price",
  "output_price",
  "cached_input_price",
  "cache_creation_price",
  "reasoning_price",
];

test("new pricing template forms keep required prices empty and specialties unconfigured", () => {
  assert.equal(
    DEFAULT_PRICING_TEMPLATE_FORM.input_price,
    "",
    "input_price should default to empty (required)",
  );
  assert.equal(
    DEFAULT_PRICING_TEMPLATE_FORM.output_price,
    "",
    "output_price should default to empty (required)",
  );
  for (const field of [
    "cached_input_price",
    "cache_creation_price",
    "reasoning_price",
  ]) {
    assert.equal(
      DEFAULT_PRICING_TEMPLATE_FORM[field],
      "",
      `${field} should default to unconfigured`,
    );
  }
});

test("template edit hydration converts typed null prices to empty form strings", () => {
  const hydrated = pricingTemplateFormStateFromTemplate({
    id: 1,
    profile_id: 1,
    name: "Legacy template",
    description: null,
    pricing_unit: "PER_1M",
    pricing_currency_code: "USD",
    active_currency_symbol: "$",
    template_kind: "standard",
    card: {
      input_price: "1",
      output_price: "2",
      cached_input_price: null,
      cache_creation_price: null,
      reasoning_price: null,
    },
    version: 1,
    revision_id: 1,
    version_effective_at: null,
    reporting_currency_epoch: 1,
    revision_count: 1,
    created_at: "2026-04-13T00:00:00Z",
    updated_at: "2026-04-13T00:00:00Z",
  });

  assert.equal(hydrated.input_price, "1");
  assert.equal(hydrated.output_price, "2");
  for (const field of [
    "cached_input_price",
    "cache_creation_price",
    "reasoning_price",
  ]) {
    assert.equal(
      hydrated[field],
      "",
      `${field} should hydrate to unconfigured`,
    );
  }
});

test("save normalization emits base prices and explicit null specials", () => {
  const normalized = normalizePricingTemplateFormPrices({
    name: "T",
    description: "",
    input_price: "  1  ",
    output_price: " 2 ",
    cached_input_price: " 0.125 ",
    cache_creation_price: "",
    reasoning_price: " 4.5 ",
  });

  assert.deepEqual(normalized, {
    input_price: "1",
    output_price: "2",
    cached_input_price: "0.125",
    cache_creation_price: null,
    reasoning_price: "4.5",
  });
  assert.equal(typeof normalized.input_price, "string");
  assert.equal(typeof normalized.cached_input_price, "string");
  assert.equal(normalized.cache_creation_price, null);
});

test("price normalizer keeps blank strings blank (unconfigured is explicit)", () => {
  assert.equal(normalizeTemplatePrice("  "), "");
  assert.equal(isNonNegativeDecimalString("1"), true);
  assert.equal(isNonNegativeDecimalString("-1"), false);
});

test("pricing window draft converts weekday and cross-midnight values without presets", () => {
  assert.equal(pricingMinuteToTime(-1), "");
  assert.equal(pricingMinuteToTime(1_530), "01:30");
  assert.equal(pricingTimeToMinute("23:45"), 1_425);
  assert.equal(pricingWindowEndMinute("01:30", true), 1_530);
  assert.equal(togglePricingWeekday(0, 0, true), 1);
  assert.equal(togglePricingWeekday(1, 6, true), 65);
  assert.equal(togglePricingWeekday(65, 0, false), 64);
});
