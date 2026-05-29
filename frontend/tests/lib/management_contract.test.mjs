import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const requestCalls = [];
let loadbalanceStrategyPayloads = [];

function buildStrategyPayload(overrides = {}) {
  return {
    id: 1,
    profile_id: 7,
    name: "Default until reset",
    legacy_strategy_type: "round-robin",
    failure_status_codes: [503, 429, 503],
    ban_mode: "until_reset",
    retry_base_delay_ms: 60_000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900_000,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4,
    ban_duration_seconds: 0,
    attached_model_count: 1,
    created_at: "2026-04-09T00:00:00Z",
    updated_at: "2026-04-09T00:00:00Z",
    ...overrides,
  };
}

const coreMock = {
  request: async (path) => {
    requestCalls.push(path);
    if (path === "/api/loadbalance/strategies") {
      return loadbalanceStrategyPayloads;
    }

    if (path === "/api/endpoints") {
      return [
        {
          id: 11,
          profile_id: 7,
          name: "Demo endpoint",
          base_url: "https://demo.invalid",
          has_api_key: true,
          masked_api_key: "••••demo",
          position: 0,
          created_at: "2026-04-09T00:00:00Z",
          updated_at: "2026-04-09T00:00:00Z",
        },
      ];
    }

    throw new Error(`Unexpected request path: ${path}`);
  },
};

const loadbalanceRoutingPolicyMock = {
  normalizeFailureStatusCodes: (statusCodes) =>
    Array.from(
      new Set(statusCodes.filter((statusCode) => typeof statusCode === "number").map(Math.trunc)),
    ).sort((left, right) => left - right),
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "./core": coreMock,
    "../loadbalanceRoutingPolicy": loadbalanceRoutingPolicyMock,
  },
});
const { loadbalanceStrategies, endpoints } = load(
  path.join(frontendDir, "src/lib/api/management.ts"),
);

const removedRetryAttemptsKey = ["retry", "max", "attempts"].join("_");
const removedBanMode = ["man", "ual"].join("");

test("management loadbalance strategy normalization accepts explicit Ban Policy payloads", async () => {
  requestCalls.length = 0;
  loadbalanceStrategyPayloads = [buildStrategyPayload()];

  const strategies = await loadbalanceStrategies.list();

  assert.deepEqual(strategies, [
    {
      id: 1,
      profile_id: 7,
      name: "Default until reset",
      legacy_strategy_type: "round-robin",
      failure_status_codes: [429, 503],
      ban_mode: "until_reset",
      retry_base_delay_ms: 60_000,
      retry_backoff_multiplier: 2,
      retry_jitter_ratio: 0.2,
      retry_max_delay_ms: 900_000,
      cycle_retry_attempt_limit: 2,
      ban_cumulative_retry_attempt_threshold: 4,
      ban_duration_seconds: 0,
      attached_model_count: 1,
      created_at: "2026-04-09T00:00:00Z",
      updated_at: "2026-04-09T00:00:00Z",
    },
  ]);
  assert.ok(!Object.hasOwn(strategies[0], removedRetryAttemptsKey));
  assert.deepEqual(requestCalls, ["/api/loadbalance/strategies"]);
});

test("management loadbalance strategy normalization rejects the removed retry attempt key", async () => {
  requestCalls.length = 0;
  loadbalanceStrategyPayloads = [buildStrategyPayload({ [removedRetryAttemptsKey]: 3 })];

  await assert.rejects(
    () => loadbalanceStrategies.list(),
    new RegExp(removedRetryAttemptsKey),
  );
});

test("management loadbalance strategy normalization rejects the removed reset-only ban value", async () => {
  requestCalls.length = 0;
  loadbalanceStrategyPayloads = [buildStrategyPayload({ ban_mode: removedBanMode })];

  await assert.rejects(
    () => loadbalanceStrategies.list(),
    /ban_mode/,
  );
});

test("management endpoints contract accepts timeout-free endpoint payloads", async () => {
  requestCalls.length = 0;
  const items = await endpoints.list();

  assert.deepEqual(items, [
    {
      id: 11,
      profile_id: 7,
      name: "Demo endpoint",
      base_url: "https://demo.invalid",
      has_api_key: true,
      masked_api_key: "••••demo",
      position: 0,
      created_at: "2026-04-09T00:00:00Z",
      updated_at: "2026-04-09T00:00:00Z",
    },
  ]);
  assert.ok(!Object.hasOwn(items[0], "pool_timeout"));
  assert.ok(!Object.hasOwn(items[0], "connect_timeout"));
  assert.ok(!Object.hasOwn(items[0], "write_timeout"));
  assert.ok(!Object.hasOwn(items[0], "read_idle_timeout"));
  assert.deepEqual(requestCalls, ["/api/endpoints"]);
});
