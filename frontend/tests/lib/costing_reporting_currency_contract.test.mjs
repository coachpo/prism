import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadCostingModule(activeCurrency = { code: "EUR", symbol: "€" }) {
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@/lib/reportingCurrency": {
        getActiveReportingCurrency: () => activeCurrency,
      },
    },
  });

  return load(path.join(frontendDir, "src/lib/costing.ts"));
}

test("formatMoneyMicros uses active reporting currency when symbol and code are omitted", () => {
  const costing = loadCostingModule({ code: "GBP", symbol: "£" });

  assert.equal(costing.formatMoneyMicros(1234567, undefined, undefined, 2, 2, "en"), "£1.23 GBP");
});

test("formatMoneyMicros preserves explicit symbol and code overrides", () => {
  const costing = loadCostingModule({ code: "GBP", symbol: "£" });

  assert.equal(costing.formatMoneyMicros(1234567, "$", undefined, 2, 2, "en"), "$1.23");
  assert.equal(costing.formatMoneyMicros(1234567, undefined, "CAD", 2, 2, "en"), "£1.23 CAD");
  assert.equal(costing.formatMoneyMicros(1234567, "$", "CAD", 2, 2, "en"), "$1.23 CAD");
});
