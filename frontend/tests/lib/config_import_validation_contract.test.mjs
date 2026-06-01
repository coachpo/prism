import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { ConfigImportSchema, VendorCatalogImportSchema } = load(
  path.join(frontendDir, "src/lib/configImportValidation.ts")
);
const removedRetryAttemptsKey = ["retry", "max", "attempts"].join("_");
const removedBanMode = ["man", "ual"].join("");

function buildValidConfigImport() {
  return {
    version: 3,
    bundle_kind: "profile_config",
    vendor_refs: [{ key: "openai", name_hint: "OpenAI" }],
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
        vendor_key: "openai",
        api_family: "openai",
        model_id: "gpt-4o-mini",
        display_name: "GPT 4o Mini",
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
  assert.equal(parsed.models[0].access_targets[0].connection_ref, "openai-primary");
  assert.ok(!Object.hasOwn(parsed.loadbalance_strategies[0], removedRetryAttemptsKey));
  assert.ok(!Object.hasOwn(parsed.models[0], "connections"));
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

test("config import schema rejects profile bundles before v3", () => {
  const payload = buildValidConfigImport();
  payload.version = payload.version - 1;

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects vendor catalog bundles on the profile path", () => {
  assert.throws(() =>
    ConfigImportSchema.parse({
      version: 1,
      bundle_kind: "vendor_catalog",
      vendors: [],
    })
  );
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

test("vendor catalog import schema accepts the canonical vendor bundle", () => {
  const parsed = VendorCatalogImportSchema.parse({
    version: 1,
    bundle_kind: "vendor_catalog",
    vendors: [
      {
        key: "openai",
        name: "OpenAI",
        description: "Primary vendor",
        icon_key: "openai",
        audit_enabled: true,
        audit_capture_bodies: false,
      },
    ],
  });

  assert.equal(parsed.bundle_kind, "vendor_catalog");
  assert.equal(parsed.vendors[0].key, "openai");
});

test("vendor catalog import schema rejects profile bundles on the vendor path", () => {
  assert.throws(() =>
    VendorCatalogImportSchema.parse({
      version: 1,
      bundle_kind: "profile_config",
      vendors: [],
    })
  );
});

test("vendor catalog import schema rejects non-v1 vendor bundles", () => {
  assert.throws(() =>
    VendorCatalogImportSchema.parse({
      version: 2,
      bundle_kind: "vendor_catalog",
      vendors: [],
    })
  );
});

test("vendor catalog import schema requires explicit icon_key boundaries", () => {
  assert.throws(() =>
    VendorCatalogImportSchema.parse({
      version: 1,
      bundle_kind: "vendor_catalog",
      vendors: [
        {
          key: "openai",
          name: "OpenAI",
          description: null,
          audit_enabled: true,
          audit_capture_bodies: false,
        },
      ],
    })
  );
});

test("config import schema rejects model access targets with non-contiguous positions", () => {
  const payload = buildValidConfigImport();
  payload.models[0].access_targets[0].position = 2;

  assert.throws(() => ConfigImportSchema.parse(payload));
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

test("config import schema rejects duplicate connection_ref ownership across models", () => {
  const payload = buildValidConfigImport();
  payload.models.push({
    vendor_key: "openai",
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

  assert.throws(() => ConfigImportSchema.parse(payload), /already owned/);
});

test("config import schema rejects ownerless top-level connection refs", () => {
  const payload = buildValidConfigImport();
  payload.models[0].access_targets = [];

  assert.throws(
    () => ConfigImportSchema.parse(payload),
    /private connection ref openai-primary must be owned by a model access target/
  );
});
