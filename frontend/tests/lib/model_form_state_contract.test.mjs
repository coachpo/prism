import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const { load } = createTsModuleLoader({ rootDir: frontendDir });
const {
  createEditModelFormData,
  setApiFamilyOnForm,
  toModelCreatePayload,
  toModelUpdatePayload,
} = load(path.join(frontendDir, "src/pages/models/modelFormState.ts"));

test("edit model form preserves and normalizes existing proxy targets", () => {
  const formData = createEditModelFormData({
    id: 7,
    vendor_id: 11,
    vendor: null,
    api_family: "openai",
    model_id: "proxy-gateway",
    display_name: "Proxy Gateway",
    model_type: "proxy",
    proxy_targets: [
      { target_model_id: "native-b", position: 5 },
      { target_model_id: "native-a", position: 2 },
      { target_model_id: "native-b", position: 7 },
    ],
    loadbalance_strategy_id: null,
    loadbalance_strategy: null,
    is_enabled: true,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: "2026-04-20T00:00:00Z",
    updated_at: "2026-04-20T00:00:00Z",
  });

  assert.deepEqual(formData.proxy_targets, [
    { target_model_id: "native-b", position: 0 },
    { target_model_id: "native-a", position: 1 },
  ]);
});

test("changing api family clears proxy targets for proxy models", () => {
  const formData = setApiFamilyOnForm(
    {
      vendor_id: 11,
      api_family: "openai",
      model_id: "proxy-gateway",
      display_name: "Proxy Gateway",
      model_type: "proxy",
      proxy_targets: [
        { target_model_id: "native-b", position: 0 },
        { target_model_id: "native-a", position: 1 },
      ],
      loadbalance_strategy_id: null,
      is_enabled: true,
      last_auto_display_name: "Proxy Gateway",
    },
    "anthropic",
  );

  assert.equal(formData.api_family, "anthropic");
  assert.deepEqual(formData.proxy_targets, []);
});

test("proxy model payloads keep ordered targets and clear native routing fields", () => {
  const formData = {
    vendor_id: 11,
    api_family: "openai",
    model_id: "proxy-gateway",
    display_name: "Proxy Gateway",
    model_type: "proxy",
    proxy_targets: [
      { target_model_id: "native-b", position: 4 },
      { target_model_id: "native-a", position: 1 },
    ],
    loadbalance_strategy_id: 99,
    is_enabled: true,
    last_auto_display_name: "Proxy Gateway",
  };

  assert.deepEqual(toModelCreatePayload(formData), {
    vendor_id: 11,
    api_family: "openai",
    model_id: "proxy-gateway",
    display_name: "Proxy Gateway",
    model_type: "proxy",
    proxy_targets: [
      { target_model_id: "native-b", position: 0 },
      { target_model_id: "native-a", position: 1 },
    ],
    loadbalance_strategy_id: null,
    is_enabled: true,
  });

  assert.deepEqual(toModelUpdatePayload(formData), {
    vendor_id: 11,
    api_family: "openai",
    model_id: "proxy-gateway",
    display_name: "Proxy Gateway",
    model_type: "proxy",
    proxy_targets: [
      { target_model_id: "native-b", position: 0 },
      { target_model_id: "native-a", position: 1 },
    ],
    loadbalance_strategy_id: null,
    is_enabled: true,
  });
});
