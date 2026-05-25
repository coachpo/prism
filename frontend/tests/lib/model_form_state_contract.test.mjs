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
  validateModelFormData,
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
    proxy_selection_strategy: "priority_static",
    proxy_targets: [
      { target_model_id: "native-b", position: 5, weight: 4, target_priority: 7 },
      { target_model_id: "native-a", position: 2, weight: 2, target_priority: 1 },
      { target_model_id: "native-b", position: 7, weight: 9, target_priority: 3 },
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

  assert.equal(formData.proxy_selection_strategy, "priority_static");
  assert.deepEqual(formData.proxy_targets, [
    { target_model_id: "native-b", position: 0, weight: 4, target_priority: 7 },
    { target_model_id: "native-a", position: 1, weight: 2, target_priority: 1 },
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
      proxy_selection_strategy: "weighted_static",
      proxy_targets: [
        { target_model_id: "native-b", position: 0, weight: 3, target_priority: 5 },
        { target_model_id: "native-a", position: 1, weight: 2, target_priority: 1 },
      ],
      loadbalance_strategy_id: null,
      is_enabled: true,
      last_auto_display_name: "Proxy Gateway",
    },
    "anthropic",
  );

  assert.equal(formData.api_family, "anthropic");
  assert.equal(formData.proxy_selection_strategy, "weighted_static");
  assert.deepEqual(formData.proxy_targets, []);
});

test("proxy model payloads keep ordered targets and clear native routing fields", () => {
  const formData = {
    vendor_id: 11,
    api_family: "openai",
    model_id: "proxy-gateway",
    display_name: "Proxy Gateway",
    model_type: "proxy",
    proxy_selection_strategy: "ordered_fallback",
    proxy_targets: [
      { target_model_id: "native-b", position: 4, weight: 8, target_priority: 6 },
      { target_model_id: "native-a", position: 1, weight: 3, target_priority: 2 },
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
    proxy_selection_strategy: "ordered_fallback",
    proxy_targets: [
      { target_model_id: "native-b", position: 0, weight: 8, target_priority: 6 },
      { target_model_id: "native-a", position: 1, weight: 3, target_priority: 2 },
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
    proxy_selection_strategy: "ordered_fallback",
    proxy_targets: [
      { target_model_id: "native-b", position: 0, weight: 8, target_priority: 6 },
      { target_model_id: "native-a", position: 1, weight: 3, target_priority: 2 },
    ],
    loadbalance_strategy_id: null,
    is_enabled: true,
  });
});

test("shared validation requires same-family proxy targets and native strategies", () => {
  assert.equal(
    validateModelFormData(
      {
        vendor_id: 11,
        api_family: "openai",
        model_id: "proxy-gateway",
        display_name: "Proxy Gateway",
        model_type: "proxy",
        proxy_selection_strategy: "ordered_fallback",
        proxy_targets: [{ target_model_id: "native-b", position: 0, weight: 1, target_priority: 0 }],
        loadbalance_strategy_id: null,
        is_enabled: true,
        last_auto_display_name: "Proxy Gateway",
      },
      ["native-a"],
    ),
    "proxy_target_required",
  );

  assert.equal(
    validateModelFormData({
      vendor_id: 11,
      api_family: "openai",
      model_id: "native-gateway",
      display_name: "Native Gateway",
      model_type: "native",
      proxy_selection_strategy: null,
      proxy_targets: [],
      loadbalance_strategy_id: null,
      is_enabled: true,
      last_auto_display_name: "Native Gateway",
    }),
    "loadbalance_strategy_required",
  );
});
