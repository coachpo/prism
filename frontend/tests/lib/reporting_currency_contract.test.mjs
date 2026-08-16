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

  const first = await reportingCurrency.getReportingCurrencyState("profile:7");
  const second = await reportingCurrency.getReportingCurrencyState("profile:7");
  const wrappedCurrency = await reportingCurrency.getReportingCurrency("profile:7");

  assert.deepEqual(first, {
    currency: { code: "EUR", symbol: "€" },
    trust: "verified",
  });
  assert.deepEqual(second, {
    currency: { code: "EUR", symbol: "€" },
    trust: "verified",
  });
  assert.deepEqual(wrappedCurrency, { code: "EUR", symbol: "€" });
  assert.equal(reportingCurrency.getCalls.length, 1);
});

test("reporting currency can be manually primed and synchronously activated", async () => {
  const reportingCurrency = loadReportingCurrencyModule(async () => {
    throw new Error("settings request should not run for a primed scope");
  });

  const primed = reportingCurrency.primeReportingCurrencyState("profile:9", {
    report_currency_code: " cad ",
    report_currency_symbol: "C$ ",
  });

  const active = reportingCurrency.setActiveReportingCurrencyState("profile:9");
  const cached = await reportingCurrency.getReportingCurrencyState("profile:9");

  assert.deepEqual(primed, {
    currency: { code: "CAD", symbol: "C$ " },
    trust: "verified",
  });
  assert.deepEqual(active, primed);
  assert.deepEqual(cached, primed);
  assert.deepEqual(reportingCurrency.getActiveReportingCurrencyState(), primed);
  assert.deepEqual(await reportingCurrency.getReportingCurrency("profile:9"), {
    code: "CAD",
    symbol: "C$ ",
  });
  assert.equal(reportingCurrency.getCalls.length, 0);
});

test("reporting currency clear resets the active scope back to the default fallback state", async () => {
  const reportingCurrency = loadReportingCurrencyModule(async () => ({
    report_currency_code: "gbp",
    report_currency_symbol: "£",
  }));

  await reportingCurrency.getReportingCurrencyState("profile:11");
  reportingCurrency.setActiveReportingCurrencyState("profile:11");
  assert.deepEqual(reportingCurrency.getActiveReportingCurrencyState(), {
    currency: { code: "GBP", symbol: "£" },
    trust: "verified",
  });

  reportingCurrency.clearReportingCurrency("profile:11");
  assert.deepEqual(
    reportingCurrency.getActiveReportingCurrencyState(),
    reportingCurrency.DEFAULT_REPORTING_CURRENCY_STATE,
  );

  reportingCurrency.clearReportingCurrency();
  assert.deepEqual(
    reportingCurrency.getActiveReportingCurrencyState(),
    reportingCurrency.DEFAULT_REPORTING_CURRENCY_STATE,
  );
});

test("reporting currency reuses one in-flight request per profile scope", async () => {
  const deferred = createDeferred();
  const reportingCurrency = loadReportingCurrencyModule(() => deferred.promise);

  const first = reportingCurrency.getReportingCurrencyState("profile:13");
  const second = reportingCurrency.getReportingCurrencyState("profile:13");

  assert.strictEqual(first, second);
  assert.equal(reportingCurrency.getCalls.length, 1);

  deferred.resolve({
    report_currency_code: "jpy",
    report_currency_symbol: "¥",
  });

  const [firstResult, secondResult] = await Promise.all([first, second]);
  assert.deepEqual(firstResult, {
    currency: { code: "JPY", symbol: "¥" },
    trust: "verified",
  });
  assert.deepEqual(secondResult, {
    currency: { code: "JPY", symbol: "¥" },
    trust: "verified",
  });
});

test("reporting currency downgrades cached scopes to fallback trust when refresh fails", async () => {
  let shouldFail = false;
  const reportingCurrency = loadReportingCurrencyModule(async () => {
    if (shouldFail) {
      throw new Error("costing settings unavailable");
    }

    return {
      report_currency_code: "aud",
      report_currency_symbol: "A$",
    };
  });

  const verified = await reportingCurrency.getReportingCurrencyState("profile:15");
  shouldFail = true;
  reportingCurrency.setActiveReportingCurrencyState("profile:15");

  const fallback = await reportingCurrency.getReportingCurrencyState("profile:15", true);

  assert.deepEqual(verified, {
    currency: { code: "AUD", symbol: "A$" },
    trust: "verified",
  });
  assert.deepEqual(fallback, {
    currency: { code: "AUD", symbol: "A$" },
    trust: "fallback",
  });
  assert.deepEqual(reportingCurrency.getActiveReportingCurrencyState(), fallback);
  assert.equal(reportingCurrency.getCalls.length, 2);
});

test("reporting currency fetch failures expose fallback state instead of trusted default USD", async () => {
  const reportingCurrency = loadReportingCurrencyModule(async () => {
    throw new Error("costing settings unavailable");
  });

  reportingCurrency.setActiveReportingCurrencyState("profile:17");
  const result = await reportingCurrency.getReportingCurrencyState("profile:17");
  const wrappedCurrency = await reportingCurrency.getReportingCurrency("profile:17");

  assert.deepEqual(result, reportingCurrency.DEFAULT_REPORTING_CURRENCY_STATE);
  assert.deepEqual(wrappedCurrency, reportingCurrency.DEFAULT_REPORTING_CURRENCY);
  assert.deepEqual(
    reportingCurrency.getActiveReportingCurrencyState(),
    reportingCurrency.DEFAULT_REPORTING_CURRENCY_STATE,
  );
  assert.equal(reportingCurrency.getCalls.length, 1);
});
