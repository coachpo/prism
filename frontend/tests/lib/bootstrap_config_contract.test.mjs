import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

class MockApiError extends Error {}

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "@/lib/api": { ApiError: MockApiError },
  },
});

const { normalizeBootstrapValues } = load(
  path.join(frontendDir, "src/features/settings/startup/startupFieldMetadata.ts"),
);

function buildBootstrapValues() {
  const pool = { max_conns: 1, min_idle_conns: 0 };
  return {
    server: { host: "0.0.0.0", port: 8000 },
    database: {
      pools: {
        total_max_conns: 7,
        management: pool,
        runtime_execution: pool,
        runtime_telemetry: pool,
        runtime_feedback: pool,
        realtime: pool,
        cache_refresh: pool,
        background_jobs: pool,
      },
      management_admission: { m2_max_concurrent: 3, m3_max_concurrent: 2 },
    },
    runtime: {
      transport: {
        max_idle_conns: 100,
        max_idle_conns_per_host: 16,
        max_conns_per_host: 16,
        idle_conn_timeout: "90s",
        request_timeout: "300s",
        response_header_timeout: "0s",
        tls_handshake_timeout: "10s",
        expect_continue_timeout: "1s",
      },
      side_effects: { attempt_timeout: "10s" },
    },
    http: { cors_allowed_origins: ["http://localhost:5173"] },
    auth: {
      access_token_ttl_seconds: 900,
      refresh_token_ttl_seconds: 86_400,
      reset_code_ttl_seconds: 900,
      access_cookie_name: "prism_access",
      refresh_cookie_name: "prism_refresh",
      cookie_secure: false,
    },
    mail: { enabled: false, from: null, reply_to: null, smtp: null },
    telemetry: { enabled: false, exporter: null, metrics: null, traces: null },
  };
}

test("bootstrap value normalization drops deleted OpenAI terminal translation routing field", () => {
  const payload = buildBootstrapValues();
  const staleRoutingField = ["openai", "terminal", "translation", "mode"].join("_");
  payload.runtime.routing = {
    [staleRoutingField]: "retired",
  };

  const normalized = normalizeBootstrapValues(payload);

  assert.equal(Object.hasOwn(normalized.runtime, "routing"), false);
});

test("bootstrap value normalization does not invent routing defaults for incomplete payloads", () => {
  const payload = buildBootstrapValues();

  const normalized = normalizeBootstrapValues(payload);

  assert.equal(Object.hasOwn(normalized.runtime, "routing"), false);
});
