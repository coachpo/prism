import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import test from "node:test";

const here = path.dirname(fileURLToPath(import.meta.url));
const requestLogTypesPath = path.resolve(
  here,
  "../../src/lib/types/request-logs.ts",
);
const modelStatsTypesPath = path.resolve(
  here,
  "../../src/lib/types/model-stats.ts",
);
const requestLogTypes = readFileSync(requestLogTypesPath, "utf8");
const modelStatsTypes = readFileSync(modelStatsTypesPath, "utf8");

const requestLogDeclarations = [
  "RequestLogFilterModelOption",
  "RequestLogFilterClientOption",
  "RequestLogFilterResolvedTargetModelOption",
  "RequestLogListItem",
  "RequestGenerationParamsReasoning",
  "RequestGenerationParams",
  "RequestLogFilterEndpointOption",
  "RequestLogListResponse",
  "RequestStatusFamily",
  "STATS_FROM_TIME_PARAM",
  "STATS_TO_TIME_PARAM",
  "StatsRequestParams",
];

test("request-log declarations have one TypeScript owner", () => {
  for (const name of requestLogDeclarations) {
    assert.match(
      requestLogTypes,
      new RegExp(`(?:interface|type|const) ${name}\\b`),
      name,
    );
    assert.doesNotMatch(
      modelStatsTypes,
      new RegExp(`(?:interface|type|const) ${name}\\b`),
      name,
    );
  }
  assert.match(
    modelStatsTypes,
    /import type \{ StreamOutcome \} from "\.\/request-logs"/,
  );
});

test("request-log micros and revision IDs mirror JSON number fields", () => {
  assert.match(
    requestLogTypes,
    /total_cost_user_currency_micros: number \| null;/,
  );
  assert.match(
    requestLogTypes,
    /pricing_template_revision_id_used: number \| null;/,
  );
  assert.doesNotMatch(
    requestLogTypes,
    /total_cost_user_currency_micros: string \| null;/,
  );
  assert.doesNotMatch(
    requestLogTypes,
    /pricing_template_revision_id_used: string \| null;/,
  );
});

test("request-log type directory has no generation-suffix sibling", () => {
  const typeFileNames = readdirSync(path.resolve(here, "../../src/lib/types"));
  assert.equal(
    typeFileNames.some((name) => /v2/i.test(name)),
    false,
  );
});
