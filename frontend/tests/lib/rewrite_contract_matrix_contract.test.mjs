import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const {
  REQUIRED_CURRENT_ROUTES,
  REQUIRED_DESTRUCTIVE_SAFEGUARDS,
  assertRewriteContractMatrix,
  renderRewriteContractMatrixMarkdown,
  rewriteContractMatrix,
  validateRewriteContractMatrix,
} = load(path.join(frontendDir, "src/features/_contracts/rewriteContractMatrix.ts"));

function cloneMatrix(overrides = {}) {
  return {
    ...structuredClone(rewriteContractMatrix),
    ...overrides,
  };
}

function collectMatrixSourcePaths() {
  const paths = [];
  for (const route of rewriteContractMatrix.routes) {
    paths.push(route.component);
  }
  for (const safeguard of rewriteContractMatrix.destructiveSafeguards) {
    paths.push(...safeguard.evidence);
  }
  return paths.filter((value) => /^src\/.*\.[cm]?[tj]sx?$/.test(value));
}

test("rewrite contract matrix references existing frontend source files", () => {
  const missingPaths = collectMatrixSourcePaths().filter(
    (sourcePath) => !existsSync(path.join(frontendDir, sourcePath)),
  );

  assert.deepEqual(missingPaths, []);
});

test("rewrite contract matrix validates current frontend route and workflow coverage", () => {
  const result = validateRewriteContractMatrix(rewriteContractMatrix);

  assert.equal(result.valid, true, result.errors.join("\n"));
  const routePaths = new Set(rewriteContractMatrix.routes.map((route) => route.currentPath));
  assert.equal(routePaths.size, REQUIRED_CURRENT_ROUTES.length);
  for (const route of REQUIRED_CURRENT_ROUTES) {
    assert.ok(routePaths.has(route), `expected route ${route}`);
  }
  assert.equal(rewriteContractMatrix.routes.length, 14);
  assert.equal(
    rewriteContractMatrix.routes.find((route) => route.currentPath === "/models")?.scope,
    "protected-selected-profile",
  );
});

test("rewrite contract matrix covers API rules, safeguards, imports, history, realtime, and deletion criteria", () => {
  assertRewriteContractMatrix();

  const safeguardIds = new Set(rewriteContractMatrix.destructiveSafeguards.map((safeguard) => safeguard.id));
  for (const safeguardId of REQUIRED_DESTRUCTIVE_SAFEGUARDS) {
    assert.ok(safeguardIds.has(safeguardId), `expected safeguard ${safeguardId}`);
  }

  assert.ok(
    rewriteContractMatrix.apiScopeRules.some(
      (rule) => rule.id === "selected-profile-management" && rule.rule.includes("X-Profile-Id"),
    ),
  );
  assert.ok(
    rewriteContractMatrix.apiScopeRules.some(
      (rule) => rule.id === "runtime-bypass" && rule.paths.includes("/v1") && rule.paths.includes("/v1beta"),
    ),
  );
  assert.ok(rewriteContractMatrix.validationRules.length >= 10);
  assert.ok(rewriteContractMatrix.importExportFlows.length >= 4);
  assert.ok(rewriteContractMatrix.auditHistoryBehaviors.length >= 3);
  assert.ok(rewriteContractMatrix.realtimeBehaviors.length >= 2);
  assert.ok(rewriteContractMatrix.featureDeletionCriteria.length >= 5);
});

test("rewrite contract markdown renderer includes all major contract groups", () => {
  const markdown = renderRewriteContractMatrixMarkdown();

  for (const heading of [
    "## Routes",
    "## API Modules",
    "## API Scope Rules",
    "## Validation Rules",
    "## Destructive Safeguards",
    "## Import / Export Flows",
    "## Audit / History Behavior",
    "## Realtime Behavior",
    "## Feature Deletion Criteria",
    "## Assumptions",
  ]) {
    assert.match(markdown, new RegExp(heading.replaceAll("/", "\\/")));
  }
  assert.match(markdown, /X-Profile-Id/);
  assert.match(markdown, /X-Prism-Preview-Token/);
});

test("missing route fails validation and reports /proxy-api-keys", () => {
  const matrix = cloneMatrix({
    routes: rewriteContractMatrix.routes.filter((route) => route.currentPath !== "/proxy-api-keys"),
  });
  const result = validateRewriteContractMatrix(matrix);

  assert.equal(result.valid, false);
  assert.match(result.errors.join("\n"), /missing route \/proxy-api-keys/);
  assert.throws(
    () => assertRewriteContractMatrix(matrix),
    /missing route \/proxy-api-keys/,
  );
});
