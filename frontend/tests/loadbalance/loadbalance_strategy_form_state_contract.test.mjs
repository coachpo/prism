import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const routingPolicyStub = {
  createDefaultAdaptiveRoutingPolicy: () => ({
    kind: "adaptive",
    routing_objective: "minimize_latency",
    hedge: {
      enabled: false,
      delay_ms: 1500,
      max_additional_attempts: 1,
    },
    circuit_breaker: {
      failure_status_codes: [403, 429, 500],
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
  }),
  getDefaultAutoRecovery: () => ({ mode: "disabled" }),
  normalizeFailureStatusCodes: (statusCodes) =>
    Array.from(
      new Set(statusCodes.filter((statusCode) => Number.isFinite(statusCode)).map(Math.trunc)),
    ).sort((left, right) => left - right),
};

const staticMessagesStub = {
  getStaticMessages: () => ({
    loadbalanceStrategyValidation: {
      nameRequired: "nameRequired",
      addStatusCode: "addStatusCode",
      statusCodesUnique: "statusCodesUnique",
      statusCodesValidHttp: "statusCodesValidHttp",
      statusCodeIntegerRange: "statusCodeIntegerRange",
      statusCodeExists: "statusCodeExists",
      baseOpenSecondsMin: "baseOpenSecondsMin",
      failureThresholdMin: "failureThresholdMin",
      backoffMultiplierMin: "backoffMultiplierMin",
      maxOpenSecondsMin: "maxOpenSecondsMin",
      banStrikesMin: "banStrikesMin",
      banDurationMin: "banDurationMin",
    },
  }),
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "@/lib/loadbalanceRoutingPolicy": routingPolicyStub,
    "@/i18n/staticMessages": staticMessagesStub,
  },
});
const { toLoadbalanceStrategyPayload } = load(
  path.join(frontendDir, "src/pages/loadbalance-strategies/loadbalanceStrategyFormState.ts"),
);

test("adaptive form payload preserves routing_policy and omits retired timeout fields", () => {
  const routing_policy = {
    kind: "adaptive",
    routing_objective: "maximize_availability",
    hedge: {
      enabled: true,
      delay_ms: 2500,
      max_additional_attempts: 2,
    },
    circuit_breaker: {
      failure_status_codes: [503, 429, 503],
      base_open_seconds: 75,
      failure_threshold: 3,
      backoff_multiplier: 2,
      max_open_seconds: 600,
      ban_mode: "temporary",
      max_open_strikes_before_ban: 2,
      ban_duration_seconds: 120,
    },
    admission: {
      respect_qps_limit: true,
      respect_in_flight_limits: false,
    },
  };

  const payload = toLoadbalanceStrategyPayload({
    name: "  Adaptive routing  ",
    strategy_type: "adaptive",
    routing_policy,
    circuit_breaker_status_code_input: "",
  });

  assert.deepEqual(payload, {
    name: "Adaptive routing",
    strategy_type: "adaptive",
    routing_policy,
  });
  assert.ok(!Object.hasOwn(payload, "timeout_policy"));
  assert.ok(!Object.hasOwn(payload, "legacy_strategy_type"));
  assert.ok(!Object.hasOwn(payload, "auto_recovery"));
});

test("legacy form payload trims names and normalizes recovery values without timeout policy", () => {
  const payload = toLoadbalanceStrategyPayload({
    name: "  Legacy routing  ",
    strategy_type: "legacy",
    legacy_strategy_type: "round-robin",
    auto_recovery: {
      mode: "enabled",
      status_codes: [504, 429, 504, 500.9],
      status_code_input: "",
      cooldown: {
        base_seconds: 60.8,
        failure_threshold: 2.9,
        backoff_multiplier: 2.5,
        max_cooldown_seconds: 900.4,
      },
      ban: {
        mode: "temporary",
        max_cooldown_strikes_before_ban: 3.6,
        ban_duration_seconds: 120.7,
      },
    },
  });

  assert.deepEqual(payload, {
    name: "Legacy routing",
    strategy_type: "legacy",
    legacy_strategy_type: "round-robin",
    auto_recovery: {
      mode: "enabled",
      status_codes: [429, 500, 504],
      cooldown: {
        base_seconds: 60,
        failure_threshold: 2,
        backoff_multiplier: 2.5,
        max_cooldown_seconds: 900,
      },
      ban: {
        mode: "temporary",
        max_cooldown_strikes_before_ban: 3,
        ban_duration_seconds: 120,
      },
    },
  });
  assert.ok(!Object.hasOwn(payload, "routing_policy"));
  assert.ok(!Object.hasOwn(payload, "timeout_policy"));
});
