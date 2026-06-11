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

const priceFields = [
  "input_price",
  "output_price",
  "cached_input_price",
  "cache_creation_price",
  "reasoning_price",
];

test("new pricing template forms default all price fields to zero strings", () => {
  for (const field of priceFields) {
    assert.equal(DEFAULT_PRICING_TEMPLATE_FORM[field], "0", `${field} should default to 0`);
  }
});

test("template edit hydration converts legacy null and blank prices to zero strings", () => {
  const hydrated = pricingTemplateFormStateFromTemplate({
    id: 1,
    profile_id: 1,
    name: "Legacy template",
    description: null,
    pricing_unit: "PER_1M",
    pricing_currency_code: "USD",
    input_price: null,
    output_price: "   ",
    cached_input_price: null,
    cache_creation_price: "",
    reasoning_price: undefined,
    version: 1,
    created_at: "2026-04-13T00:00:00Z",
    updated_at: "2026-04-13T00:00:00Z",
  });

  for (const field of priceFields) {
    assert.equal(hydrated[field], "0", `${field} should hydrate to 0`);
  }
});

test("save normalization emits five explicit trimmed price strings", () => {
  const normalized = normalizePricingTemplateFormPrices({
    ...DEFAULT_PRICING_TEMPLATE_FORM,
    input_price: "   ",
    output_price: "\t",
    cached_input_price: " 0.125 ",
    cache_creation_price: "",
    reasoning_price: " 4.5 ",
  });

  assert.deepEqual(normalized, {
    input_price: "0",
    output_price: "0",
    cached_input_price: "0.125",
    cache_creation_price: "0",
    reasoning_price: "4.5",
  });
  for (const field of priceFields) {
    assert.equal(typeof normalized[field], "string", `${field} should be a string`);
  }
});

test("price normalizer feeds decimal validation after blanks become zero", () => {
  assert.equal(normalizeTemplatePrice("  "), "0");
  assert.equal(isNonNegativeDecimalString(normalizeTemplatePrice("  ")), true);
  assert.equal(isNonNegativeDecimalString(normalizeTemplatePrice(" -1 ")), false);
});
