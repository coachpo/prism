import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function createDeferred() {
  let resolve;
  let reject;
  const promise = new Promise((nextResolve, nextReject) => {
    resolve = nextResolve;
    reject = nextReject;
  });
  return { promise, resolve, reject };
}

function loadReportingCurrencyModule(getImplementation) {
  const getCalls = [];
  const apiMock = {
    api: {
      settings: {
        costing: {
          get: async () => {
            getCalls.push("get");
            return getImplementation();
          },
        },
      },
    },
  };

  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@/lib/api": apiMock,
    },
  });

  return {
    getCalls,
    ...load(path.join(frontendDir, "src/lib/reportingCurrency.ts")),
  };
}

test("reporting currency loader caches normalized values per profile scope", async () => {
  const reportingCurrency = loadReportingCurrencyModule(async () => ({
    report_currency_code: " eur ",
    report_currency_symbol: "€",
  }));

  const first = await reportingCurrency.getReportingCurrency("profile:7");
  const second = await reportingCurrency.getReportingCurrency("profile:7");

  assert.deepEqual(first, { code: "EUR", symbol: "€" });
  assert.deepEqual(second, { code: "EUR", symbol: "€" });
  assert.equal(reportingCurrency.getCalls.length, 1);
});

test("reporting currency can be manually primed and synchronously activated", async () => {
  const reportingCurrency = loadReportingCurrencyModule(async () => {
    throw new Error("settings request should not run for a primed scope");
  });

  const primed = reportingCurrency.primeReportingCurrency("profile:9", {
    report_currency_code: " cad ",
    report_currency_symbol: "C$ ",
  });

  const active = reportingCurrency.setActiveReportingCurrency("profile:9");
  const cached = await reportingCurrency.getReportingCurrency("profile:9");

  assert.deepEqual(primed, { code: "CAD", symbol: "C$ " });
  assert.deepEqual(active, { code: "CAD", symbol: "C$ " });
  assert.deepEqual(reportingCurrency.getActiveReportingCurrency(), {
    code: "CAD",
    symbol: "C$ ",
  });
  assert.deepEqual(cached, { code: "CAD", symbol: "C$ " });
  assert.equal(reportingCurrency.getCalls.length, 0);
});

test("reporting currency clear resets the active scope back to the default", async () => {
  const reportingCurrency = loadReportingCurrencyModule(async () => ({
    report_currency_code: "gbp",
    report_currency_symbol: "£",
  }));

  await reportingCurrency.getReportingCurrency("profile:11");
  reportingCurrency.setActiveReportingCurrency("profile:11");
  assert.deepEqual(reportingCurrency.getActiveReportingCurrency(), {
    code: "GBP",
    symbol: "£",
  });

  reportingCurrency.clearReportingCurrency("profile:11");
  assert.deepEqual(
    reportingCurrency.getActiveReportingCurrency(),
    reportingCurrency.DEFAULT_REPORTING_CURRENCY,
  );

  reportingCurrency.clearReportingCurrency();
  assert.deepEqual(
    reportingCurrency.getActiveReportingCurrency(),
    reportingCurrency.DEFAULT_REPORTING_CURRENCY,
  );
});

test("reporting currency reuses one in-flight request per profile scope", async () => {
  const deferred = createDeferred();
  const reportingCurrency = loadReportingCurrencyModule(() => deferred.promise);

  const first = reportingCurrency.getReportingCurrency("profile:13");
  const second = reportingCurrency.getReportingCurrency("profile:13");

  assert.strictEqual(first, second);
  assert.equal(reportingCurrency.getCalls.length, 1);

  deferred.resolve({
    report_currency_code: "jpy",
    report_currency_symbol: "¥",
  });

  const [firstResult, secondResult] = await Promise.all([first, second]);
  assert.deepEqual(firstResult, { code: "JPY", symbol: "¥" });
  assert.deepEqual(secondResult, { code: "JPY", symbol: "¥" });
});

test("reporting currency fails open to the default when an empty scope fetch fails", async () => {
  const reportingCurrency = loadReportingCurrencyModule(async () => {
    throw new Error("costing settings unavailable");
  });

  reportingCurrency.setActiveReportingCurrency("profile:15");
  const result = await reportingCurrency.getReportingCurrency("profile:15");
  const cached = await reportingCurrency.getReportingCurrency("profile:15");

  assert.deepEqual(result, reportingCurrency.DEFAULT_REPORTING_CURRENCY);
  assert.deepEqual(cached, reportingCurrency.DEFAULT_REPORTING_CURRENCY);
  assert.deepEqual(
    reportingCurrency.getActiveReportingCurrency(),
    reportingCurrency.DEFAULT_REPORTING_CURRENCY,
  );
  assert.equal(reportingCurrency.getCalls.length, 1);
});
