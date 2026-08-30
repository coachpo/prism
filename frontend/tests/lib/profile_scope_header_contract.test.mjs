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
  return (
    !row.invalidates_auth &&
    !row.invalidates_active_profile &&
    !row.invalidates_planning &&
    !row.invalidates_all_planning
  );
}

test("profile scope helper matches profile-scoped rows in the route contract manifest", () => {
  const scopedRows = routeContract.filter((row) => row.profile_scoped);
  const scopedNonInvalidatingRows = scopedRows.filter(isNonInvalidating);

  // 141 rows: two static /api/models/exports/pi/* routes, the bounded
  // /api/models/{model_config_id}/pi/search directory lookup, and six persisted
  // /api/models/{model_config_id}/pi* binding routes. Every binding write is
  // planning-neutral; source/render stay profile-scoped without a platform
  // parameter or resolve step.
  // Earlier: 140 rows, before the pi.dev model-id directory search route was
  // added as the unified discovery entry for cross-directory binding.
  // Earlier: 133 rows, up from 124, for the models.dev catalog integration:
  // nine /api/models/{model_config_id}/catalog* management routes (all
  // declared none:true — metadata writes never invalidate planning) plus two
  // /api/pricing-templates/catalog/* routes (preview none:true, commit
  // planning:true). Earlier: 124 rows, up from the older 100, when the
  // manifest moved to one row per admission-table entry and the unmounted
  // /api/endpoints/{endpoint_id}/position duplicates were dropped. The lock is
  // here to force a deliberate decision whenever the manifest changes, so it
  // moves only with an explanation like this one.
  assert.equal(
    routeContract.length,
    141,
    "manifest row count should stay locked",
  );
  assert.ok(
    scopedRows.length > 0,
    "manifest should include profile-scoped rows",
  );
  assert.ok(
    scopedNonInvalidatingRows.length > 0,
    "manifest should include profile-scoped non-invalidating reads",
  );

  for (const row of scopedRows) {
    const route = samplePath(row.route_pattern);
    assert.equal(
      isProfileScopedManagementRoute(route),
      true,
      `${route} should be profile-scoped`,
    );
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
  assert.equal(
    isProfileScopedManagementRoute("/api/models/exports/pi/source"),
    true,
  );
  assert.equal(
    isProfileScopedManagementRoute("/api/models/exports/pi/render"),
    true,
  );
  assert.equal(isProfileScopedManagementRoute("/api/settings/timezone"), false);
});

test("profile scope helper keeps non-profile-scoped manifest rows and runtime routes global", () => {
  const globalRows = routeContract.filter((row) => !row.profile_scoped);

  assert.ok(
    globalRows.length > 0,
    "manifest should include global management rows",
  );
  assert.ok(
    globalRows.some((row) => row.invalidates_auth),
    "manifest should include global auth invalidation rows",
  );
  for (const row of globalRows) {
    const route = samplePath(row.route_pattern);
    assert.equal(
      isProfileScopedManagementRoute(route),
      false,
      `${route} should stay global`,
    );
  }

  for (const route of [
    "/v1/chat/completions",
    "/v1beta/models/gemini:generateContent",
  ]) {
    assert.equal(
      isProfileScopedManagementRoute(route),
      false,
      `${route} should stay global or runtime`,
    );
  }
});
