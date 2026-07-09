import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const { load } = createTsModuleLoader({ rootDir: frontendDir });
const dashboardQueryParams = load(
  path.join(frontendDir, "src/pages/dashboard/queryParams.ts"),
);

test("dashboard exposes only overview and analytics tabs", () => {
  assert.deepEqual(dashboardQueryParams.DASHBOARD_TAB_OPTIONS, [
    "overview",
    "analytics",
  ]);
});

test("removed routing bookmarks normalize to the overview tab", () => {
  assert.deepEqual(dashboardQueryParams.parsePageSearch({ tab: "routing" }), {
    tab: "overview",
  });
});
