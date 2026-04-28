import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadCostingModule(
  activeCurrencyState = { currency: { code: "EUR", symbol: "€" }, trust: "verified" },
) {
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@/lib/reportingCurrency": {
        getActiveReportingCurrency: () => activeCurrencyState.currency,
        getActiveReportingCurrencyState: () => activeCurrencyState,
      },
    },
  });

  return load(path.join(frontendDir, "src/lib/costing.ts"));
}

test("formatMoneyMicros uses active reporting currency when symbol and code are omitted", () => {
  const costing = loadCostingModule({
    currency: { code: "GBP", symbol: "£" },
    trust: "verified",
  });

  assert.equal(costing.formatMoneyMicros(1234567, undefined, undefined, 2, 2, "en"), "£1.23 GBP");
});

test("formatMoneyMicros preserves explicit symbol and code overrides", () => {
  const costing = loadCostingModule({
    currency: { code: "GBP", symbol: "£" },
    trust: "verified",
  });

  assert.equal(costing.formatMoneyMicros(1234567, "$", undefined, 2, 2, "en"), "$1.23");
  assert.equal(costing.formatMoneyMicros(1234567, undefined, "CAD", 2, 2, "en"), "£1.23 CAD");
  assert.equal(costing.formatMoneyMicros(1234567, "$", "CAD", 2, 2, "en"), "$1.23 CAD");
});

test("resolveSpendTrustState uses verified or fallback currency trust for priced spend", () => {
  const verifiedCosting = loadCostingModule({
    currency: { code: "EUR", symbol: "€" },
    trust: "verified",
  });
  const fallbackCosting = loadCostingModule({
    currency: { code: "USD", symbol: "$" },
    trust: "fallback",
  });

  assert.equal(verifiedCosting.resolveSpendTrustState({ costMicros: 0 }), "verified");
  assert.equal(fallbackCosting.resolveSpendTrustState({ costMicros: 250000 }), "fallback");
});


test("resolveSpendTrustState marks explicit unpriced and missing-cost aggregate contexts", () => {
  const costing = loadCostingModule({
    currency: { code: "USD", symbol: "$" },
    trust: "fallback",
  });

  assert.equal(
    costing.resolveSpendTrustState({ unpricedReason: "MISSING_PRICE_DATA", costMicros: null }),
    "unpriced",
  );
  assert.equal(
    costing.resolveSpendTrustState({ pricedRequestCount: 0, unpricedRequestCount: 3 }),
    "unpriced",
  );
  assert.equal(
    costing.resolveSpendTrustState({ pricedRequestCount: 2, unpricedRequestCount: 3, costMicros: 0 }),
    "fallback",
  );
});

test("getActiveSpendTrustContext returns both currency trust and resolved spend trust", () => {
  const costing = loadCostingModule({
    currency: { code: "JPY", symbol: "¥" },
    trust: "fallback",
  });

  assert.deepEqual(costing.getActiveSpendTrustContext({ costMicros: 900000 }), {
    currency: { code: "JPY", symbol: "¥" },
    currencyTrust: "fallback",
    spendTrust: "fallback",
  });

  assert.deepEqual(costing.getActiveSpendTrustContext({ unpricedReason: "PRICING_DISABLED" }), {
    currency: { code: "JPY", symbol: "¥" },
    currencyTrust: "fallback",
    spendTrust: "unpriced",
  });
});
