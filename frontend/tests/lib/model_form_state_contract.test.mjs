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
  DEFAULT_MODEL_FORM_DATA,
  createEditModelFormData,
  createNewModelFormData,
  setApiFamilyOnForm,
  toModelCreatePayload,
  toModelUpdatePayload,
  validateModelFormData,
} = load(path.join(frontendDir, "src/pages/models/modelFormState.ts"));

test("new model defaults start as disabled drafts", () => {
  const formData = createNewModelFormData([], 42);

  assert.equal(DEFAULT_MODEL_FORM_DATA.is_enabled, false);
  assert.equal(formData.is_enabled, false);
  assert.deepEqual(formData.access_targets, []);
  assert.equal(formData.loadbalance_strategy_id, 42);
});

test("disabled drafts validate with no access targets", () => {
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "draft-model",
      display_name: "Draft Model",
      loadbalance_strategy_id: 9,
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Draft Model",
    }),
    null,
  );
});

test("disabled drafts still reject invalid present targets when keys are provided", () => {
  assert.equal(
    validateModelFormData(
      {
        vendor_id: null,
        api_family: "openai",
        model_id: "draft-model",
        display_name: "Draft Model",
        loadbalance_strategy_id: 9,
        access_targets: [
          { target_type: "connection", connection_id: 77, position: 0, is_enabled: true },
          { target_type: "model", target_model_id: "stale-model", position: 1, is_enabled: true },
        ],
        is_enabled: false,
        last_auto_display_name: "Draft Model",
      },
      ["model:other-model"],
    ),
    "access_target_required",
  );
});

test("enabled models require at least one enabled valid access target", () => {
  const enabledDraft = {
    vendor_id: null,
    api_family: "openai",
    model_id: "live-model",
    display_name: "Live Model",
    loadbalance_strategy_id: 9,
    access_targets: [],
    is_enabled: true,
    last_auto_display_name: "Live Model",
  };

  assert.equal(validateModelFormData(enabledDraft), "access_target_required");
  assert.equal(
    validateModelFormData(
      {
        ...enabledDraft,
        access_targets: [{ target_type: "model", target_model_id: "target-model", position: 0, is_enabled: false }],
      },
      ["model:target-model"],
    ),
    "access_target_required",
  );
  assert.equal(
    validateModelFormData(
      {
        ...enabledDraft,
        access_targets: [{ target_type: "model", target_model_id: "target-model", position: 0, is_enabled: true }],
      },
      ["model:target-model"],
    ),
    null,
  );
});

test("enabled forms reject any stale disabled target when keys are provided", () => {
  assert.equal(
    validateModelFormData(
      {
        vendor_id: null,
        api_family: "openai",
        model_id: "live-model",
        display_name: "Live Model",
        loadbalance_strategy_id: 9,
        access_targets: [
          { target_type: "connection", connection_id: 77, position: 0, is_enabled: true },
          { target_type: "model", target_model_id: "stale-model", position: 1, is_enabled: false },
        ],
        is_enabled: true,
        last_auto_display_name: "Live Model",
      },
      ["model:other-model"],
    ),
    "access_target_required",
  );
});

test("changing api family clears incompatible access targets", () => {
  const formData = setApiFamilyOnForm(
    {
      vendor_id: 11,
      api_family: "openai",
      model_id: "targeted-model",
      display_name: "Targeted Model",
      loadbalance_strategy_id: 17,
      access_targets: [{ target_type: "connection", connection_id: 88, position: 0, is_enabled: true }],
      is_enabled: false,
      last_auto_display_name: "Targeted Model",
    },
    "anthropic",
  );

  assert.equal(formData.api_family, "anthropic");
  assert.deepEqual(formData.access_targets, []);
});

test("payload shaping omits private connection targets and keeps normalized model targets", () => {
  const formData = {
    vendor_id: 11,
    api_family: "openai",
    model_id: "live-model",
    display_name: "  Live Model  ",
    loadbalance_strategy_id: 17,
    access_targets: [
      { target_type: "connection", connection_id: 77, position: 4, is_enabled: true },
      { target_type: "model", target_model_id: "target-model", position: 9, is_enabled: true },
      { target_type: "model", target_model_id: "target-model", position: 10, is_enabled: false },
    ],
    is_enabled: true,
    last_auto_display_name: "Live Model",
  };

  const expectedAccessTargets = [
    { target_type: "model", target_model_id: "target-model", position: 0, is_enabled: true },
  ];

  assert.deepEqual(toModelCreatePayload(formData), {
    vendor_id: 11,
    api_family: "openai",
    model_id: "live-model",
    display_name: "Live Model",
    is_enabled: true,
    loadbalance_strategy_id: 17,
    access_targets: expectedAccessTargets,
  });

  assert.deepEqual(toModelUpdatePayload(formData), {
    vendor_id: 11,
    api_family: "openai",
    display_name: "  Live Model  ",
    model_id: "live-model",
    is_enabled: true,
    loadbalance_strategy_id: 17,
    access_targets: expectedAccessTargets,
  });
});

test("edit model form keeps existing access targets normalized for shared validation", () => {
  const formData = createEditModelFormData({
    id: 7,
    vendor_id: 11,
    vendor: null,
    api_family: "openai",
    model_id: "native-model",
    display_name: "Native Model",
    loadbalance_strategy_id: 21,
    access_targets: [
      { target_type: "connection", connection_id: 88, position: 5, is_enabled: true, connection: null },
      { target_type: "connection", connection_id: 88, position: 2, is_enabled: true, connection: null },
    ],
    is_enabled: false,
    connection_count: 0,
    active_connection_count: 0,
    health_success_rate: null,
    health_total_requests: 0,
    created_at: "2026-04-20T00:00:00Z",
    updated_at: "2026-04-20T00:00:00Z",
  });

  assert.equal(formData.is_enabled, false);
  assert.deepEqual(formData.access_targets, [
    { target_type: "connection", connection_id: 88, position: 0, is_enabled: true },
  ]);
});
