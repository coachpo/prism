import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const requestCalls = [];
const coreMock = {
  request: async (url, init) => {
    requestCalls.push({ url, init });
    if (url === "/api/sidecars/7/auth-files/auth_primary") {
      return {
        state: "succeeded_sync_failed",
        snapshot: {
          id: 11,
          sidecar_id: 7,
          auth_id: "auth_primary",
          name: "primary-oauth.json",
          observed_at: "2026-05-10T12:00:00Z",
        },
        sync_error: "delete refresh failed",
      };
    }

    if (url === "/api/sidecars/7/auth-files/auth_primary/fields") {
      return {
        state: "succeeded_sync_failed",
        snapshot: {
          id: 11,
          sidecar_id: 7,
          auth_id: "auth_primary",
          name: "primary-oauth.json",
          observed_at: "2026-05-10T12:00:00Z",
          priority: 44,
        },
        sync_status: {
          sidecar_id: 7,
          enabled: true,
          sync_interval_seconds: 300,
          management_auth_state: "valid",
          last_sync_at: "2026-05-10T12:00:00Z",
          last_successful_sync_at: "2026-05-10T12:00:00Z",
          snapshot_stale_after: "2099-05-10T12:00:00Z",
          last_sync_error: "detail refresh failed",
          stale: true,
          due: false,
          paused: false,
        },
        sync_error: "detail refresh failed",
      };
    }

    if (url === "/api/sidecars/7/sync") {
      return {
        state: "succeeded",
        sidecar: {
          id: 7,
          name: "CLIProxyAPI primary",
          base_url: "https://cliproxyapi.internal:8443",
          base_url_canonical: "https://cliproxyapi.internal:8443",
          enabled: true,
          request_timeout_seconds: 30,
          sync_interval_seconds: 300,
          allow_private_network: false,
          allow_insecure_http: false,
          skip_tls_verify: false,
          management_auth_state: "valid",
          credential_state: { management_password_configured: true },
          last_sync_at: "2026-05-10T12:00:00Z",
          last_successful_sync_at: "2026-05-10T12:00:00Z",
          snapshot_stale_after: "2099-05-10T12:00:00Z",
          created_at: "2026-05-10T12:00:00Z",
          updated_at: "2026-05-10T12:00:00Z",
        },
        sync_status: {
          sidecar_id: 7,
          enabled: true,
          sync_interval_seconds: 300,
          management_auth_state: "valid",
          last_sync_at: "2026-05-10T12:00:00Z",
          last_successful_sync_at: "2026-05-10T12:00:00Z",
          snapshot_stale_after: "2099-05-10T12:00:00Z",
          last_sync_error: undefined,
          stale: false,
          due: false,
          paused: false,
        },
        auth_snapshot_count: 1,
        provider_snapshot_count: 1,
      };
    }

    throw new Error(`Unexpected request: ${url}`);
  },
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "./core": coreMock,
  },
});
const { sidecars } = load(path.join(frontendDir, "src/lib/api/sidecars.ts"));

test("sidecars auth mutation contract exposes sync failure truth model", async () => {
  requestCalls.length = 0;
  const response = await sidecars.updateAuthFileFields(7, "auth_primary", { priority: 44 });

  assert.equal(response.state, "succeeded_sync_failed");
  assert.equal(response.sync_error, "detail refresh failed");
  assert.equal(response.sync_status?.stale, true);
  assert.equal(response.snapshot?.priority, 44);
  assert.deepEqual(requestCalls, [
    {
      url: "/api/sidecars/7/auth-files/auth_primary/fields",
      init: {
        method: "PATCH",
        body: JSON.stringify({ priority: 44 }),
      },
    },
  ]);
});

test("sidecars single auth-file delete sends typed confirmation body", async () => {
  requestCalls.length = 0;
  const response = await sidecars.deleteAuthFile(7, "auth_primary", { confirm_name: "primary-oauth.json" });

  assert.equal(response.state, "succeeded_sync_failed");
  assert.equal(response.sync_error, "delete refresh failed");
  assert.equal(response.snapshot?.name, "primary-oauth.json");
  assert.deepEqual(requestCalls, [
    {
      url: "/api/sidecars/7/auth-files/auth_primary",
      init: {
        method: "DELETE",
        body: JSON.stringify({ confirm_name: "primary-oauth.json" }),
      },
    },
  ]);
});

test("sidecars sync contract stays on the typed facade", async () => {
  requestCalls.length = 0;
  const response = await sidecars.sync(7);

  assert.equal(response.state, "succeeded");
  assert.equal(response.auth_snapshot_count, 1);
  assert.equal(response.provider_snapshot_count, 1);
  assert.equal(requestCalls.at(-1)?.url, "/api/sidecars/7/sync");
});
