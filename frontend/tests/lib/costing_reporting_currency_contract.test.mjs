import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadStreamTelemetryModule() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });

  return load(path.join(frontendDir, "src/pages/request-logs/streamTelemetry.ts"));
}

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

test("formatUnpricedReasonLabel distinguishes stream usage gaps from missing token usage", () => {
  const costing = loadCostingModule();

  assert.equal(costing.formatUnpricedReasonLabel("MISSING_TOKEN_USAGE"), "Missing token usage");
  assert.equal(costing.formatUnpricedReasonLabel("STREAM_USAGE_UNAVAILABLE"), "Usage unavailable");
});

test("historical unknown stream rows are distinct from usage-unavailable pricing gaps", () => {
  const streamTelemetry = loadStreamTelemetryModule();

  assert.equal(streamTelemetry.isStreamUsageUnavailableReason("MISSING_TOKEN_USAGE"), false);
  assert.equal(streamTelemetry.isStreamUsageUnavailableReason("STREAM_USAGE_UNAVAILABLE"), true);
  assert.equal(streamTelemetry.hasStreamTelemetryOutcome("client_disconnected"), true);
  assert.equal(streamTelemetry.hasStreamTelemetryOutcome("completed"), true);
  assert.equal(streamTelemetry.hasStreamTelemetryOutcome("not_streaming"), false);
  assert.equal(streamTelemetry.isHistoricalUnknownStreamRow(true, "unknown"), true);
  assert.equal(streamTelemetry.isHistoricalUnknownStreamRow(true, "completed"), false);
  assert.equal(streamTelemetry.isHistoricalUnknownStreamRow(false, "unknown"), true);
});
