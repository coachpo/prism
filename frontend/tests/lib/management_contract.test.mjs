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
  request: async (path) => {
    requestCalls.push(path);
    if (path === "/api/loadbalance/strategies") {
      return [
        {
          id: 1,
          profile_id: 7,
          name: "Legacy strategy",
          strategy_type: "legacy",
          legacy_strategy_type: "round-robin",
          auto_recovery: { mode: "disabled" },
          attached_model_count: 1,
          created_at: "2026-04-09T00:00:00Z",
          updated_at: "2026-04-09T00:00:00Z",
        },
        {
          id: 2,
          profile_id: 7,
          name: "Adaptive strategy",
          strategy_type: "adaptive",
          routing_policy: {
            kind: "adaptive",
            routing_objective: "minimize_latency",
            hedge: {
              enabled: false,
              delay_ms: 1500,
              max_additional_attempts: 1,
            },
            circuit_breaker: {
              failure_status_codes: [503, 429, 503],
              base_open_seconds: 60,
              failure_threshold: 2,
              backoff_multiplier: 2,
              max_open_seconds: 900,
              ban_mode: "off",
              max_open_strikes_before_ban: 0,
              ban_duration_seconds: 0,
            },
            admission: {
              respect_qps_limit: true,
              respect_in_flight_limits: true,
            },
          },
          attached_model_count: 2,
          created_at: "2026-04-09T00:00:00Z",
          updated_at: "2026-04-09T00:00:00Z",
        },
      ];
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

test("management loadbalance strategy normalization accepts timeout-free API payloads", async () => {
  const strategies = await loadbalanceStrategies.list();

  assert.deepEqual(strategies, [
    {
      id: 1,
      profile_id: 7,
      name: "Legacy strategy",
      strategy_type: "legacy",
      legacy_strategy_type: "round-robin",
      auto_recovery: { mode: "disabled" },
      attached_model_count: 1,
      created_at: "2026-04-09T00:00:00Z",
      updated_at: "2026-04-09T00:00:00Z",
    },
    {
      id: 2,
      profile_id: 7,
      name: "Adaptive strategy",
      strategy_type: "adaptive",
      routing_policy: {
        kind: "adaptive",
        routing_objective: "minimize_latency",
        hedge: {
          enabled: false,
          delay_ms: 1500,
          max_additional_attempts: 1,
        },
        circuit_breaker: {
          failure_status_codes: [429, 503],
          base_open_seconds: 60,
          failure_threshold: 2,
          backoff_multiplier: 2,
          max_open_seconds: 900,
          ban_mode: "off",
          max_open_strikes_before_ban: 0,
          ban_duration_seconds: 0,
        },
        admission: {
          respect_qps_limit: true,
          respect_in_flight_limits: true,
        },
      },
      attached_model_count: 2,
      created_at: "2026-04-09T00:00:00Z",
      updated_at: "2026-04-09T00:00:00Z",
    },
  ]);
  for (const strategy of strategies) {
    assert.ok(!Object.hasOwn(strategy, "timeout_policy"));
  }
});

test("management endpoints contract accepts timeout-free endpoint payloads", async () => {
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
  assert.deepEqual(requestCalls, ["/api/loadbalance/strategies", "/api/endpoints"]);
});
