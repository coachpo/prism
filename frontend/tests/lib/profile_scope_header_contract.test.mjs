import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { isProfileScopedManagementRoute } = load(
  path.join(frontendDir, "src/lib/api/profileScope.ts"),
);

test("profile scope helper marks profile config routes for X-Profile-Id attachment", () => {
  const scopedRoutes = [
    "/api/models",
    "/api/models/14/connections/2/health",
    "/api/loadbalance/strategies/defaults",
    "/api/endpoints/connections",
    "/api/connections/12/references",
    "/api/pricing-templates/7/connections",
    "/api/stats/requests?limit=20",
    "/api/audit/logs/9",
    "/api/loadbalance/current-state?model_config_id=4",
    "/api/loadbalance/current-state/5/reset",
    "/api/loadbalance/events?model_id=gpt-4o",
    "/api/settings/costing",
    "/api/settings/timezone",
    "/api/config/profile/export",
    "/api/config/profile/export/with-secrets",
    "/api/config/profile/import",
    "/api/config/profile/import/preview",
    "/api/config/profile/import/preview?format=v1",
    "/api/config/header-blocklist-rules/3",
    "/api/config/user-agent-client-rules/8",
  ];

  for (const route of scopedRoutes) {
    assert.equal(isProfileScopedManagementRoute(route), true, `${route} should be profile-scoped`);
  }
});

test("profile scope helper keeps vendor config and global routes without X-Profile-Id", () => {
  const globalOrRuntimeRoutes = [
    "/api/profiles",
    "/api/profiles/bootstrap",
    "/api/profiles/active",
    "/api/vendors",
    "/api/vendors/2/models",
    "/api/auth/session",
    "/api/settings/auth",
    "/api/settings/auth/proxy-keys/4/rotate",
    "/api/settings/log-retention",
    "/api/maintenance/log-retention/jobs",
    "/api/config/bootstrap",
    "/api/config/vendors/export",
    "/api/config/vendors/import/preview",
    "/api/config/vendors/import",
    "/api/config/vendors/import?dry_run=false",
    "/api/realtime/ws",
    "/v1/chat/completions",
    "/v1beta/models/gemini:generateContent",
  ];

  for (const route of globalOrRuntimeRoutes) {
    assert.equal(isProfileScopedManagementRoute(route), false, `${route} should stay global or runtime`);
  }
});
