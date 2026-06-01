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
  request: async (path, options = {}) => {
    requestCalls.push({ path, options });
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

    if (path === "/api/models/42/connections") {
      return options.method === "POST"
        ? { id: 77, model_config_id: 42 }
        : [{ id: 77, model_config_id: 42 }];
    }

    if (path === "/api/models/42/connections/77") {
      return options.method === "DELETE" ? undefined : { id: 77, model_config_id: 42 };
    }

    if (path === "/api/models/42/connections/77/health") {
      return { connection_id: 77, health_status: "healthy" };
    }

    if (path === "/api/connections/77/references") {
      return {
        connection_id: 77,
        items: [{ target_id: 12, model_config_id: 42, model_id: "demo", api_family: "openai", position: 0, is_enabled: true }],
      };
    }

    if (path === "/api/models/42/targets/12") {
      return [];
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
const { loadbalanceStrategies, endpoints, models, connections } = load(
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
  assert.deepEqual(requestCalls.map((call) => call.path), ["/api/loadbalance/strategies"]);
});

test("management loadbalance strategy normalization accepts cheapest_eligible_context payloads", async () => {
  requestCalls.length = 0;
  loadbalanceStrategyPayloads = [
    buildStrategyPayload({
      id: 2,
      name: "Cheapest eligible context",
      legacy_strategy_type: "cheapest_eligible_context",
    }),
  ];

  const [strategy] = await loadbalanceStrategies.list();

  assert.equal(strategy.legacy_strategy_type, "cheapest_eligible_context");
  assert.equal(strategy.name, "Cheapest eligible context");
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
  assert.deepEqual(requestCalls.map((call) => call.path), ["/api/endpoints"]);
});


test("management model connection helpers use owner-scoped route shapes", async () => {
  requestCalls.length = 0;

  await models.connections.list(42);
  await models.connections.create(42, { endpoint_id: 11, is_active: true });
  await models.connections.update(42, 77, { is_active: false });
  await models.connections.healthCheck(42, 77);
  await models.connections.delete(42, 77);

  assert.deepEqual(requestCalls.map((call) => [call.path, call.options.method ?? "GET"]), [
    ["/api/models/42/connections", "GET"],
    ["/api/models/42/connections", "POST"],
    ["/api/models/42/connections/77", "PATCH"],
    ["/api/models/42/connections/77/health", "POST"],
    ["/api/models/42/connections/77", "DELETE"],
  ]);
});

test("management owner target helper patches position and enabled state", async () => {
  requestCalls.length = 0;

  await models.targets.update(42, 12, { position: 2, is_enabled: false });

  assert.equal(requestCalls[0].path, "/api/models/42/targets/12");
  assert.equal(requestCalls[0].options.method, "PATCH");
  assert.equal(requestCalls[0].options.body, JSON.stringify({ position: 2, is_enabled: false }));
});

test("management connection reference helper uses the supported read route", async () => {
  requestCalls.length = 0;

  const response = await connections.references(77);

  assert.equal(response.items[0].model_config_id, 42);
  assert.deepEqual(requestCalls.map((call) => [call.path, call.options.method ?? "GET"]), [
    ["/api/connections/77/references", "GET"],
  ]);
});
