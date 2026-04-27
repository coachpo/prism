import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const { ConfigImportSchema } = load(path.join(frontendDir, "src/lib/configImportValidation.ts"));

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
    models: [],
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
