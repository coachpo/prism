import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const requestCalls = [];
const timestamp = "2026-06-01T12:00:00Z";

function buildLoadbalanceStrategySummary(overrides = {}) {
  return {
    id: 9,
    name: "Context aware routing",
    legacy_strategy_type: "round-robin",
    failure_status_codes: [503, 429, 503],
    ban_mode: "off",
    retry_base_delay_ms: 60_000,
    retry_backoff_multiplier: 2,
    retry_jitter_ratio: 0.2,
    retry_max_delay_ms: 900_000,
    cycle_retry_attempt_limit: 2,
    ban_cumulative_retry_attempt_threshold: 4,
    ban_duration_seconds: 0,
    ...overrides,
  };
}

function buildEndpoint(overrides = {}) {
  return {
    id: 11,
    profile_id: 7,
    name: "Primary endpoint",
    base_url: "https://demo.invalid",
    has_api_key: true,
    masked_api_key: "••••demo",
    position: 0,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides,
  };
}

function buildContextCapabilityOverrides(overrides = {}) {
  return {
    context_window_tokens: null,
    default_output_token_reserve: null,
    max_context_utilization: null,
    preferred_context_utilization_threshold: null,
    ...overrides,
  };
}

function buildOwnedConnection(overrides = {}) {
  return {
    id: 77,
    profile_id: 7,
    model_config_id: 42,
    api_family: "openai",
    endpoint_id: 11,
    endpoint: buildEndpoint(),
    is_active: true,
    priority: 0,
    name: "Primary connection",
    auth_type: "bearer",
    custom_headers: { "x-test": "1" },
    openai_probe_endpoint_variant: "responses_minimal",
    context_window_tokens: 131_072,
    default_output_token_reserve: 4_096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.72,
    context_capability_overrides: buildContextCapabilityOverrides({
      default_output_token_reserve: 4_096,
    }),
    pricing_template_id: null,
    qps_limit: 25,
    max_in_flight_non_stream: 4,
    max_in_flight_stream: 8,
    pricing_template: null,
    health_status: "healthy",
    health_detail: null,
    last_health_check: timestamp,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides,
  };
}

function buildPublicConnection(overrides = {}) {
  const { context_capability_overrides: _ignored, ...connection } = buildOwnedConnection(overrides);
  return connection;
}

function buildAccessTarget(overrides = {}) {
  return {
    id: 501,
    target_type: "connection",
    target_model_id: null,
    connection_id: 77,
    terminal_target_id: 77,
    position: 0,
    is_enabled: true,
    target_model: null,
    connection: buildOwnedConnection(),
    terminal_target: buildOwnedConnection({
      context_capability_overrides: buildContextCapabilityOverrides({
        context_window_tokens: 65_536,
        max_context_utilization: 0.95,
        preferred_context_utilization_threshold: 0.8,
      }),
    }),
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides,
  };
}

function buildModelListItemPayload(overrides = {}) {
  return {
    id: 42,
    profile_id: 7,
    vendor_id: 5,
    vendor: { id: 5, key: "openai", name: "OpenAI" },
    api_family: "openai",
    model_id: "gpt-4.1",
    display_name: "GPT-4.1",
    loadbalance_strategy_id: 9,
    loadbalance_strategy: buildLoadbalanceStrategySummary(),
    context_window_tokens: 200_000,
    default_output_token_reserve: 4_096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.74,
    access_targets: [buildAccessTarget()],
    is_enabled: true,
    connection_count: 1,
    active_connection_count: 1,
    health_success_rate: 99.9,
    health_total_requests: 12,
    created_at: timestamp,
    updated_at: timestamp,
    ...overrides,
  };
}

function buildModelDetailPayload(overrides = {}) {
  const {
    connection_count: _connectionCount,
    active_connection_count: _activeConnectionCount,
    health_success_rate: _healthSuccessRate,
    health_total_requests: _healthTotalRequests,
    ...detail
  } = buildModelListItemPayload(overrides);

  return detail;
}

function pickCapabilityFields(payload) {
  return {
    context_window_tokens: payload.context_window_tokens,
    default_output_token_reserve: payload.default_output_token_reserve,
    max_context_utilization: payload.max_context_utilization,
    preferred_context_utilization_threshold: payload.preferred_context_utilization_threshold,
  };
}

const coreMock = {
  request: async (requestPath, options = {}) => {
    requestCalls.push({ requestPath, options });

    if (requestPath === "/api/models") {
      return [buildModelListItemPayload()];
    }
    if (requestPath === "/api/models/42") {
      return buildModelDetailPayload();
    }
    if (requestPath === "/api/models/42/connections") {
      return [buildOwnedConnection()];
    }
    if (requestPath === "/api/connections") {
      return [buildPublicConnection()];
    }

    throw new Error(`Unexpected request path: ${requestPath}`);
  },
};

const loadbalanceRoutingPolicyMock = {
  normalizeFailureStatusCodes: (statusCodes) =>
    Array.from(new Set(statusCodes.filter((statusCode) => typeof statusCode === "number"))).sort(
      (left, right) => left - right,
    ),
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "./core": coreMock,
    "../loadbalanceRoutingPolicy": loadbalanceRoutingPolicyMock,
    "@/lib/types": {
      getTerminalTarget: (target) => target.terminal_target ?? target.connection ?? null,
      getTerminalTargetId: (target) => target.terminal_target_id ?? target.connection_id ?? null,
      isTerminalTargetAccessTargetType: (targetType) => targetType === "connection",
    },
    "@/i18n/staticMessages": {
      getStaticMessages: () => ({
        modelDetail: {
          connectionFallback: (connectionId) => `Connection ${connectionId}`,
          orderedPriorityRouting: "Ordered priority routing",
        },
      }),
    },
    "./connectionProbeBehavior": {
      normalizeOpenAIProbeEndpointVariant: (value) => value ?? "responses_minimal",
    },
  },
});

const { connections, models } = load(path.join(frontendDir, "src/lib/api/management.ts"));
const { toModelListItem } = load(path.join(frontendDir, "src/pages/models/modelFormState.ts"));
const { patchModelListItemFromDetail } = load(
  path.join(frontendDir, "src/pages/model-detail/useModelDetailDataSupport.ts"),
);

test("management model normalization preserves snake_case capability fields", async () => {
  requestCalls.length = 0;

  const [listItem] = await models.list();
  const detail = await models.get(42);

  assert.deepEqual(pickCapabilityFields(listItem), {
    context_window_tokens: 200_000,
    default_output_token_reserve: 4_096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.74,
  });
  assert.ok(!Object.hasOwn(listItem, "contextWindowTokens"));
  assert.deepEqual(detail.access_targets[0].connection.context_capability_overrides, {
    context_window_tokens: null,
    default_output_token_reserve: 4_096,
    max_context_utilization: null,
    preferred_context_utilization_threshold: null,
  });
  assert.deepEqual(detail.access_targets[0].terminal_target.context_capability_overrides, {
    context_window_tokens: 65_536,
    default_output_token_reserve: null,
    max_context_utilization: 0.95,
    preferred_context_utilization_threshold: 0.8,
  });
  assert.deepEqual(detail.loadbalance_strategy.failure_status_codes, [429, 503]);
  assert.deepEqual(requestCalls.map((call) => call.requestPath), ["/api/models", "/api/models/42"]);
});

test("management connection reads preserve owner-scoped overrides and keep public metadata optional", async () => {
  requestCalls.length = 0;

  const [ownedConnection] = await models.connections.list(42);
  const [publicConnection] = await connections.list();

  assert.deepEqual(pickCapabilityFields(ownedConnection), {
    context_window_tokens: 131_072,
    default_output_token_reserve: 4_096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.72,
  });
  assert.deepEqual(ownedConnection.context_capability_overrides, {
    context_window_tokens: null,
    default_output_token_reserve: 4_096,
    max_context_utilization: null,
    preferred_context_utilization_threshold: null,
  });
  assert.equal(publicConnection.context_capability_overrides, undefined);
  assert.ok(!Object.hasOwn(ownedConnection, "contextCapabilityOverrides"));
  assert.deepEqual(requestCalls.map((call) => call.requestPath), [
    "/api/models/42/connections",
    "/api/connections",
  ]);
});

test("model list cache helpers keep capability fields during detail hydration", () => {
  const existing = buildModelListItemPayload({
    context_window_tokens: 16_384,
    default_output_token_reserve: 1_024,
    max_context_utilization: 0.75,
    preferred_context_utilization_threshold: 0.65,
    health_success_rate: 88,
    health_total_requests: 25,
  });
  const updatedModel = buildModelDetailPayload({
    context_window_tokens: null,
    default_output_token_reserve: 8_192,
    max_context_utilization: 0.92,
    preferred_context_utilization_threshold: null,
  });

  const listItem = toModelListItem(updatedModel, existing);
  const [patchedItem] = patchModelListItemFromDetail([existing], updatedModel);

  assert.deepEqual(pickCapabilityFields(listItem), {
    context_window_tokens: null,
    default_output_token_reserve: 8_192,
    max_context_utilization: 0.92,
    preferred_context_utilization_threshold: null,
  });
  assert.deepEqual(pickCapabilityFields(patchedItem), pickCapabilityFields(listItem));
  assert.equal(patchedItem.health_success_rate, 88);
  assert.equal(patchedItem.health_total_requests, 25);
  assert.equal(patchedItem.connection_count, 1);
  assert.equal(patchedItem.active_connection_count, 1);
});
