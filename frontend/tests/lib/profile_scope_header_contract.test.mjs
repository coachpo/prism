import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const repoRoot = path.resolve(frontendDir, "..");
const contractPath = path.join(
  repoRoot,
  "backend/internal/platform/http/management_route_contract.json",
);
const routeContract = JSON.parse(fs.readFileSync(contractPath, "utf8"));

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { isProfileScopedManagementRoute } = load(
  path.join(frontendDir, "src/lib/api/profileScope.ts"),
);

function samplePath(routePattern) {
  return routePattern.replaceAll(/\{[^/]+\}/g, "7");
}

function isNonInvalidating(row) {
  return !row.invalidates_auth &&
    !row.invalidates_active_profile &&
    !row.invalidates_planning &&
    !row.invalidates_all_planning;
}

test("profile scope helper matches profile-scoped rows in the route contract manifest", () => {
  const scopedRows = routeContract.filter((row) => row.profile_scoped);
  const scopedNonInvalidatingRows = scopedRows.filter(isNonInvalidating);

  assert.equal(routeContract.length, 60, "manifest row count should stay locked");
  assert.ok(scopedRows.length > 0, "manifest should include profile-scoped rows");
  assert.ok(
    scopedNonInvalidatingRows.length > 0,
    "manifest should include profile-scoped non-invalidating reads",
  );

  for (const row of scopedRows) {
    const route = samplePath(row.route_pattern);
    assert.equal(isProfileScopedManagementRoute(route), true, `${route} should be profile-scoped`);
  }

  for (const row of scopedNonInvalidatingRows) {
    const route = `${samplePath(row.route_pattern)}?contract_probe=1`;
    assert.equal(
      isProfileScopedManagementRoute(route),
      true,
      `${route} should remain profile-scoped even when it does not invalidate runtime caches`,
    );
  }

  assert.equal(isProfileScopedManagementRoute("/api/settings/audit"), true);
});

test("profile scope helper keeps non-profile-scoped manifest rows and runtime routes global", () => {
  const globalRows = routeContract.filter((row) => !row.profile_scoped);

  assert.ok(globalRows.length > 0, "manifest should include global management rows");
  assert.ok(
    globalRows.some((row) => row.invalidates_auth),
    "manifest should include global auth invalidation rows",
  );
  assert.ok(
    globalRows.some((row) => row.invalidates_active_profile),
    "manifest should include global active-profile invalidation rows",
  );
  for (const row of globalRows) {
    const route = samplePath(row.route_pattern);
    assert.equal(isProfileScopedManagementRoute(route), false, `${route} should stay global`);
  }

  for (const route of [
    "/api/realtime/ws",
    "/v1/chat/completions",
    "/v1beta/models/gemini:generateContent",
  ]) {
    assert.equal(isProfileScopedManagementRoute(route), false, `${route} should stay global or runtime`);
  }
});
