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
        name: "Template A",
        pricing_currency_code: "USD",
        input_price: "1.25",
        output_price: "2.50",
      },
    ],
    connections: [
      {
        ref: "openai-primary",
        api_family: "openai",
        endpoint_name: "OpenAI",
        openai_text_capability: "responses_only",
        pricing_template_name: "Template A",
        is_active: true,
        priority: 0,
      },
    ],
    loadbalance_strategies: [
      {
        name: "Default round robin",
        ["legacy_" + ["strategy", "type"].join("_")]: "round-robin",
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
        model_id: "demo-native",
        loadbalance_strategy_name: "Default round robin",
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
    secret_payload: {
      kind: "encrypted",
      cipher: "fernet-v1",
      key_id: "test-key",
      entries: [],
    },
  };
}

function expectConfigImportToThrow(payload, reason) {
  try {
    ConfigImportSchema.parse(payload);
  } catch {
    return;
  }

  throw new Error(reason);
}

test("config import schema normalizes missing pricing fields to zero strings", () => {
  const payload = buildValidConfigImport();
  delete payload.pricing_templates[0].input_price;
  delete payload.pricing_templates[0].output_price;
  payload.pricing_templates[0].input_price = undefined;
  payload.pricing_templates[0].output_price = null;
  payload.pricing_templates[0].cached_input_price = undefined;
  payload.pricing_templates[0].cache_creation_price = null;
  delete payload.pricing_templates[0].reasoning_price;

  const parsed = ConfigImportSchema.parse(payload);
  const template = parsed.pricing_templates[0];

  if (template.input_price !== "0") throw new Error("expected input_price missing input to normalize to 0");
  if (template.output_price !== "0") throw new Error("expected output_price missing input to normalize to 0");
  if (template.cached_input_price !== "0") throw new Error("expected cached_input_price to normalize to 0");
  if (template.cache_creation_price !== "0") throw new Error("expected cache_creation_price to normalize to 0");
  if (template.reasoning_price !== "0") throw new Error("expected reasoning_price to normalize to 0");
});

test("config import schema normalizes blank pricing fields before decimal validation", () => {
  const payload = buildValidConfigImport();
  payload.pricing_templates[0].input_price = "   ";
  payload.pricing_templates[0].output_price = "\t";
  payload.pricing_templates[0].cached_input_price = "   ";
  payload.pricing_templates[0].cache_creation_price = "\t";
  payload.pricing_templates[0].reasoning_price = "\n";

  const parsed = ConfigImportSchema.parse(payload);
  const template = parsed.pricing_templates[0];

  if (template.input_price !== "0") throw new Error("expected input_price blank input to normalize to 0");
  if (template.output_price !== "0") throw new Error("expected output_price blank input to normalize to 0");
  if (template.cached_input_price !== "0") throw new Error("expected cached_input_price blank input to normalize to 0");
  if (template.cache_creation_price !== "0") throw new Error("expected cache_creation_price blank input to normalize to 0");
  if (template.reasoning_price !== "0") throw new Error("expected reasoning_price blank input to normalize to 0");
});

test("config import schema rejects malformed non-string component price inputs", () => {
  const numberPayload = buildValidConfigImport();
  numberPayload.pricing_templates[0].cached_input_price = 1;
  expectConfigImportToThrow(numberPayload, "expected numeric cached_input_price to fail validation");

  const booleanPayload = buildValidConfigImport();
  booleanPayload.pricing_templates[0].cache_creation_price = false;
  expectConfigImportToThrow(booleanPayload, "expected boolean cache_creation_price to fail validation");

  const objectPayload = buildValidConfigImport();
  objectPayload.pricing_templates[0].reasoning_price = { value: "1" };
  expectConfigImportToThrow(objectPayload, "expected object reasoning_price to fail validation");
});

test("config import schema keeps explicit decimal pricing strings intact", () => {
  const payload = buildValidConfigImport();
  payload.pricing_templates[0].cached_input_price = "0.125";
  payload.pricing_templates[0].cache_creation_price = "3";
  payload.pricing_templates[0].reasoning_price = "4.5";

  const parsed = ConfigImportSchema.parse(payload);
  const template = parsed.pricing_templates[0];

  if (template.cached_input_price !== "0.125") throw new Error("expected cached_input_price to preserve explicit decimal string");
  if (template.cache_creation_price !== "3") throw new Error("expected cache_creation_price to preserve explicit integer string");
  if (template.reasoning_price !== "4.5") throw new Error("expected reasoning_price to preserve explicit decimal string");
});
