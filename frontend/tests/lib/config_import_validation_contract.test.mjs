import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { ConfigImportSchema } = load(
  path.join(frontendDir, "src/lib/configImportValidation.ts")
);
const removedRetryAttemptsKey = ["retry", "max", "attempts"].join("_");
const removedBanMode = ["man", "ual"].join("");
const liveAuthoringCapabilityDefaults = {
  context_window_tokens: null,
  default_output_token_reserve: 4096,
  max_context_utilization: 0.9,
  preferred_context_utilization_threshold: null,
};
const facadePolicyDefaults = {
  facade_enabled: true,
  facade_selection_policy: "weighted_eligible_context",
  facade_fallback_policy: "redistribute_ineligible_weight",
};

function buildValidConfigImport() {
  return {
    version: 3,
    bundle_kind: "profile_config",
    endpoints: [
      {
        name: "OpenAI",
        base_url: "https://api.openai.com/v1",
        position: 0,
      },
    ],
    pricing_templates: [
      {
        name: "Default pricing",
        pricing_unit: "PER_1M",
        pricing_currency_code: "USD",
        input_price: "0",
        output_price: "0",
        cached_input_price: "0",
        cache_creation_price: "0",
        reasoning_price: "0",
        version: 1,
      },
    ],
    connections: [
      {
        ref: "openai-primary",
        api_family: "openai",
        endpoint_name: "OpenAI",
        ...liveAuthoringCapabilityDefaults,
        openai_text_capability: "responses_only",
        pricing_template_name: "Default pricing",
        is_active: true,
        priority: 0,
      },
    ],
    loadbalance_strategies: [
      {
        name: "Default single",
        legacy_strategy_type: "single",
        failure_status_codes: [429, 500],
        ban_mode: "until_reset",
        retry_base_delay_ms: 60_000,
        retry_backoff_multiplier: 2,
        retry_jitter_ratio: 0.2,
        retry_max_delay_ms: 900_000,
        cycle_retry_attempt_limit: 2,
        ban_cumulative_retry_attempt_threshold: 4,
        ban_duration_seconds: 0,
      },
    ],
    models: [
      {
        api_family: "openai",
        model_id: "gpt-4o-mini",
        display_name: "GPT 4o Mini",
        loadbalance_strategy_name: "Default single",
        ...liveAuthoringCapabilityDefaults,
        is_enabled: true,
        access_targets: [
          {
            position: 0,
            is_enabled: true,
            target_type: "connection",
            connection_ref: "openai-primary",
          },
        ],
      },
    ],
    profile_settings: {
      report_currency_code: "USD",
      report_currency_symbol: "$",
      endpoint_fx_mappings: [
        {
          model_id: "gpt-4o-mini",
          connection_ref: "openai-primary",
          fx_rate: "1",
        },
      ],
    },
    header_blocklist_rules: [],
    user_agent_client_rules: [],
    secret_payload: {
      kind: "encrypted",
      cipher: "fernet-v1",
      key_id: "test-key",
      entries: [],
    },
  };
}

test("config import schema accepts current profile bundle v3 Ban Policy payloads", () => {
  const parsed = ConfigImportSchema.parse(buildValidConfigImport());

  assert.equal(parsed.version, 3);
  assert.equal(parsed.endpoints[0].name, "OpenAI");
  assert.equal(parsed.loadbalance_strategies[0].ban_mode, "until_reset");
  assert.equal(parsed.loadbalance_strategies[0].cycle_retry_attempt_limit, 2);
  assert.equal(parsed.loadbalance_strategies[0].ban_cumulative_retry_attempt_threshold, 4);
  assert.equal(parsed.connections[0].ref, "openai-primary");
  assert.equal(parsed.connections[0].openai_text_capability, "responses_only");
  assert.deepEqual(
    {
      context_window_tokens: parsed.connections[0].context_window_tokens,
      default_output_token_reserve: parsed.connections[0].default_output_token_reserve,
      max_context_utilization: parsed.connections[0].max_context_utilization,
      preferred_context_utilization_threshold: parsed.connections[0].preferred_context_utilization_threshold,
    },
    liveAuthoringCapabilityDefaults
  );
  assert.deepEqual(
    {
      context_window_tokens: parsed.models[0].context_window_tokens,
      default_output_token_reserve: parsed.models[0].default_output_token_reserve,
      max_context_utilization: parsed.models[0].max_context_utilization,
      preferred_context_utilization_threshold: parsed.models[0].preferred_context_utilization_threshold,
    },
    liveAuthoringCapabilityDefaults
  );
  assert.equal(parsed.models[0].access_targets[0].connection_ref, "openai-primary");
  assert.ok(!Object.hasOwn(parsed.loadbalance_strategies[0], removedRetryAttemptsKey));
  assert.ok(!Object.hasOwn(parsed.models[0], "connections"));
});

test("config import schema requires explicit OpenAI text capability for OpenAI connections", () => {
  const payload = buildValidConfigImport();
  delete payload.connections[0].openai_text_capability;

  assert.throws(() => ConfigImportSchema.parse(payload), /OpenAI connections must include openai_text_capability/);
});

test("config import schema rejects OpenAI-only fields on non-OpenAI connections", () => {
  const payload = buildValidConfigImport();
  payload.connections[0].api_family = "anthropic";

  assert.throws(() => ConfigImportSchema.parse(payload), /openai_text_capability is only valid for OpenAI connections/);

  payload.connections[0].openai_text_capability = null;
  payload.connections[0].openai_probe_endpoint_variant = "responses_minimal";

  assert.throws(() => ConfigImportSchema.parse(payload), /openai_probe_endpoint_variant is only valid for OpenAI connections/);
});

test("config import schema accepts cheapest_eligible_context loadbalance strategies", () => {
  const payload = buildValidConfigImport();
  payload.loadbalance_strategies[0].legacy_strategy_type = "cheapest_eligible_context";

  const parsed = ConfigImportSchema.parse(payload);

  assert.equal(
    parsed.loadbalance_strategies[0].legacy_strategy_type,
    "cheapest_eligible_context"
  );
});


test("config import schema accepts backend-exported facade model fields", () => {
  const payload = buildValidConfigImport();
  Object.assign(payload.models[0], facadePolicyDefaults);

  const parsed = ConfigImportSchema.parse(payload);

  assert.deepEqual(
    {
      facade_enabled: parsed.models[0].facade_enabled,
      facade_selection_policy: parsed.models[0].facade_selection_policy,
      facade_fallback_policy: parsed.models[0].facade_fallback_policy,
    },
    facadePolicyDefaults,
  );
});

test("config import schema accepts backend-exported overflow promotion target field", () => {
  const payload = buildValidConfigImport();
  payload.connections = [];
  payload.profile_settings.endpoint_fx_mappings = [];
  payload.models = [
    {
      api_family: "openai",
      model_id: "gpt-4o-router",
      display_name: "GPT 4o Router",
      loadbalance_strategy_name: "Default single",
      ...liveAuthoringCapabilityDefaults,
      context_overflow_promotion_target_id: "gpt-4o-terminal",
      is_enabled: true,
      access_targets: [
        {
          position: 0,
          is_enabled: true,
          target_type: "model",
          target_model_id: "gpt-4o-terminal",
          weight: 3,
          target_priority: 0,
        },
      ],
    },
    {
      api_family: "openai",
      model_id: "gpt-4o-terminal",
      display_name: "GPT 4o Terminal",
      loadbalance_strategy_name: "Default single",
      ...liveAuthoringCapabilityDefaults,
      is_enabled: true,
      access_targets: [],
    },
  ];

  const parsed = ConfigImportSchema.parse(payload);

  assert.equal(parsed.models[0].context_overflow_promotion_target_id, "gpt-4o-terminal");
});

test("config bundle TypeScript DTOs expose overflow promotion target and OpenAI capability fields", () => {
  const source = readFileSync(path.join(frontendDir, "src/lib/types/config-audit-settings.ts"), "utf8");

  assert.match(
    source,
    /interface ConfigConnectionExport \{[\s\S]*?openai_text_capability: OpenAITextCapability \| null;[\s\S]*?\n\}/,
  );
  assert.match(
    source,
    /interface ConfigConnectionImport \{[\s\S]*?openai_text_capability\?: OpenAITextCapability \| null;[\s\S]*?\n\}/,
  );
  assert.match(
    source,
    /interface ConfigModelExport \{[\s\S]*?context_overflow_promotion_target_id: string \| null;[\s\S]*?\n\}/,
  );
  assert.match(
    source,
    /interface ConfigModelImport \{[\s\S]*?context_overflow_promotion_target_id\?: string \| null;[\s\S]*?\n\}/,
  );
});

test("config import schema rejects profile bundles before v3", () => {
  const payload = buildValidConfigImport();
  payload.version = payload.version - 1;

  assert.throws(() => ConfigImportSchema.parse(payload));
});


test("config import schema rejects removed model-local connection arrays", () => {
  const payload = buildValidConfigImport();
  payload.models[0].connections = [];

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects the removed retry attempt key", () => {
  const payload = buildValidConfigImport();
  payload.loadbalance_strategies[0][removedRetryAttemptsKey] = 3;

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects the removed reset-only ban value", () => {
  const payload = buildValidConfigImport();
  payload.loadbalance_strategies[0].ban_mode = removedBanMode;

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema requires renamed Ban Policy threshold fields", () => {
  const payload = buildValidConfigImport();
  delete payload.loadbalance_strategies[0].cycle_retry_attempt_limit;

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects removed endpoint timeout fields", () => {
  const payload = buildValidConfigImport();
  payload.endpoints[0].write_timeout = 60;

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects removed strategy timeout policy fields", () => {
  const payload = buildValidConfigImport();
  payload.loadbalance_strategies[0].timeout_policy = {
    attempt_open_timeout_ms: 2_000,
  };

  assert.throws(() => ConfigImportSchema.parse(payload));
});





test("config import schema accepts model access targets with sparse positions", () => {
  const payload = buildValidConfigImport();
  payload.models[0].access_targets[0].position = 4;

  const parsed = ConfigImportSchema.parse(payload);

  assert.equal(parsed.models[0].access_targets[0].position, 4);
});

test("config import schema accepts model target weight and target_priority metadata", () => {
  const payload = buildValidConfigImport();
  payload.connections = [];
  payload.profile_settings.endpoint_fx_mappings = [];
  payload.models = [
    {
      api_family: "openai",
      model_id: "gpt-4o-router",
      display_name: "GPT 4o Router",
      loadbalance_strategy_name: "Default single",
      ...liveAuthoringCapabilityDefaults,
      is_enabled: true,
      access_targets: [
        {
          position: 4,
          is_enabled: true,
          target_type: "model",
          target_model_id: "gpt-4o-mini-terminal",
          weight: 9,
          target_priority: 3,
        },
      ],
    },
    {
      api_family: "openai",
      model_id: "gpt-4o-mini-terminal",
      display_name: "GPT 4o Mini Terminal",
      loadbalance_strategy_name: "Default single",
      ...liveAuthoringCapabilityDefaults,
      is_enabled: true,
      access_targets: [],
    },
  ];

  const parsed = ConfigImportSchema.parse(payload);

  assert.equal(parsed.models[0].access_targets[0].position, 4);
  assert.equal(parsed.models[0].access_targets[0].weight, 9);
  assert.equal(parsed.models[0].access_targets[0].target_priority, 3);
});

test("config import schema rejects terminal target routing metadata", () => {
  const payload = buildValidConfigImport();
  payload.models[0].access_targets[0].weight = 2;
  payload.models[0].access_targets[0].target_priority = 1;

  assert.throws(() => ConfigImportSchema.parse(payload), /weight must be omitted for terminal targets/);
});

test("config import schema rejects invalid model target routing metadata", () => {
  const payload = buildValidConfigImport();
  payload.connections = [];
  payload.profile_settings.endpoint_fx_mappings = [];
  payload.models = [
    {
      api_family: "openai",
      model_id: "gpt-4o-router",
      display_name: "GPT 4o Router",
      loadbalance_strategy_name: "Default single",
      ...liveAuthoringCapabilityDefaults,
      is_enabled: true,
      access_targets: [
        {
          position: 2,
          is_enabled: true,
          target_type: "model",
          target_model_id: "gpt-4o-mini-terminal",
          weight: 0,
          target_priority: -1,
        },
      ],
    },
    {
      api_family: "openai",
      model_id: "gpt-4o-mini-terminal",
      display_name: "GPT 4o Mini Terminal",
      loadbalance_strategy_name: "Default single",
      ...liveAuthoringCapabilityDefaults,
      is_enabled: true,
      access_targets: [],
    },
  ];

  assert.throws(() => ConfigImportSchema.parse(payload), /weight must be greater than 0/);
});

test("config import schema rejects model access targets with duplicate references", () => {
  const payload = buildValidConfigImport();
  payload.models[0].access_targets = [
    {
      position: 0,
      is_enabled: true,
      target_type: "connection",
      connection_ref: "openai-primary",
    },
    {
      position: 1,
      is_enabled: true,
      target_type: "connection",
      connection_ref: "openai-primary",
    },
  ];

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects connection access targets without connection_ref", () => {
  const payload = buildValidConfigImport();
  delete payload.models[0].access_targets[0].connection_ref;

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects preferred thresholds above max context utilization", () => {
  const payload = buildValidConfigImport();
  payload.connections[0].preferred_context_utilization_threshold = 0.95;
  payload.connections[0].max_context_utilization = 0.9;

  assert.throws(() => ConfigImportSchema.parse(payload), /preferred_context_utilization_threshold/);
});

test("config import schema rejects duplicate connection_ref ownership across models", () => {
  const payload = buildValidConfigImport();
  payload.models.push({
    api_family: "openai",
    model_id: "gpt-4o-mini-shadow",
    display_name: "GPT 4o Mini Shadow",
    loadbalance_strategy_name: "Default single",
    is_enabled: true,
    access_targets: [
      {
        position: 0,
        is_enabled: true,
        target_type: "connection",
        connection_ref: "openai-primary",
      },
    ],
  });

  assert.throws(() => ConfigImportSchema.parse(payload), /owned by multiple models/);
});

test("config import schema rejects ownerless top-level connection refs", () => {
  const payload = buildValidConfigImport();
  payload.models[0].access_targets = [];

  assert.throws(
    () => ConfigImportSchema.parse(payload),
    /Connection ref 'openai-primary' must be owned by exactly one model access target/
  );
});
