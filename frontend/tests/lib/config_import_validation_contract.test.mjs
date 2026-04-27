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

function buildValidConfigImport() {
  return {
    version: 1,
    bundle_kind: "profile_config",
    vendor_refs: [],
    endpoints: [
      {
        name: "Demo endpoint",
        base_url: "https://demo.invalid",
        position: 0,
      },
    ],
    pricing_templates: [],
    loadbalance_strategies: [
      {
        name: "Default legacy routing",
        strategy_type: "legacy",
        legacy_strategy_type: "round-robin",
        auto_recovery: { mode: "disabled" },
      },
    ],
    models: [
      {
        api_family: "openai",
        model_id: "demo-native",
        model_type: "native",
        proxy_targets: [],
        loadbalance_strategy_name: "Default legacy routing",
        connections: [],
      },
      {
        api_family: "openai",
        model_id: "demo-proxy",
        model_type: "proxy",
        proxy_targets: [{ target_model_id: "demo-native", position: 0 }],
        loadbalance_strategy_name: null,
        connections: [],
      },
    ],
    secret_payload: {
      kind: "encrypted",
      cipher: "fernet-v1",
      key_id: "test-key",
      entries: [],
    },
  };
}

test("config import schema accepts the current timeout-free rollback payload", () => {
  const parsed = ConfigImportSchema.parse(buildValidConfigImport());

  assert.equal(parsed.endpoints[0].name, "Demo endpoint");
  assert.equal(parsed.loadbalance_strategies[0].strategy_type, "legacy");
  assert.ok(!Object.hasOwn(parsed.endpoints[0], "write_timeout"));
  assert.ok(!Object.hasOwn(parsed.loadbalance_strategies[0], "timeout_policy"));
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

test("config import schema rejects native models with proxy targets", () => {
  const payload = buildValidConfigImport();
  payload.models[0].proxy_targets = [{ target_model_id: "demo-native", position: 0 }];

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects proxy models with non-null strategy names", () => {
  const payload = buildValidConfigImport();
  payload.models[1].loadbalance_strategy_name = "Default legacy routing";

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects proxy models with non-contiguous proxy target positions", () => {
  const payload = buildValidConfigImport();
  payload.models[1].proxy_targets = [{ target_model_id: "demo-native", position: 2 }];

  assert.throws(() => ConfigImportSchema.parse(payload));
});

test("config import schema rejects proxy models with duplicate target ids", () => {
  const payload = buildValidConfigImport();
  payload.models[1].proxy_targets = [
    { target_model_id: "demo-native", position: 0 },
    { target_model_id: "demo-native", position: 1 },
  ];

  assert.throws(() => ConfigImportSchema.parse(payload));
});
