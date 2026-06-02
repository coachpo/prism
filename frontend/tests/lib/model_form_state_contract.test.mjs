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

const baseEditModel = {
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
  context_window_tokens: 8192,
  default_output_token_reserve: 4096,
  max_context_utilization: 0.9,
  preferred_context_utilization_threshold: 0.72,
  connection_count: 0,
  active_connection_count: 0,
  health_success_rate: null,
  health_total_requests: 0,
  created_at: "2026-04-20T00:00:00Z",
  updated_at: "2026-04-20T00:00:00Z",
};

test("new model defaults seed canonical capability strings", () => {
  const formData = createNewModelFormData([], 42);

  assert.equal(DEFAULT_MODEL_FORM_DATA.is_enabled, false);
  assert.equal(DEFAULT_MODEL_FORM_DATA.context_window_tokens, "");
  assert.equal(DEFAULT_MODEL_FORM_DATA.default_output_token_reserve, "4096");
  assert.equal(DEFAULT_MODEL_FORM_DATA.max_context_utilization, "0.90");
  assert.equal(DEFAULT_MODEL_FORM_DATA.preferred_context_utilization_threshold, "");
  assert.equal(formData.is_enabled, false);
  assert.deepEqual(formData.access_targets, []);
  assert.equal(formData.loadbalance_strategy_id, 42);
});

test("edit hydration seeds capability strings from list and detail payloads", () => {
  const fromList = createEditModelFormData(baseEditModel);
  const fromDetail = createEditModelFormData({
    ...baseEditModel,
    display_name: null,
    context_window_tokens: null,
    default_output_token_reserve: 12288,
    max_context_utilization: 1,
    preferred_context_utilization_threshold: null,
    access_targets: [
      { target_type: "connection", connection_id: 88, position: 5, is_enabled: true, connection: null },
    ],
  });

  assert.equal(fromList.context_window_tokens, "8192");
  assert.equal(fromList.default_output_token_reserve, "4096");
  assert.equal(fromList.max_context_utilization, "0.9");
  assert.equal(fromList.preferred_context_utilization_threshold, "0.72");
  assert.equal(fromDetail.context_window_tokens, "");
  assert.equal(fromDetail.default_output_token_reserve, "12288");
  assert.equal(fromDetail.max_context_utilization, "1");
  assert.equal(fromDetail.preferred_context_utilization_threshold, "");
  assert.equal(fromDetail.display_name, "");
});

test("disabled drafts validate with no access targets", () => {
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "draft-model",
      display_name: "Draft Model",
      loadbalance_strategy_id: 9,
      context_window_tokens: "",
      default_output_token_reserve: "4096",
      max_context_utilization: "0.90",
      preferred_context_utilization_threshold: "",
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Draft Model",
    }),
    null,
  );
});

test("validation rejects blank model id before other form work proceeds", () => {
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "   ",
      display_name: "Draft Model",
      loadbalance_strategy_id: 9,
      context_window_tokens: "",
      default_output_token_reserve: "4096",
      max_context_utilization: "0.90",
      preferred_context_utilization_threshold: "",
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Draft Model",
    }),
    "model_id_required",
  );
});

test("validation rejects blank reserve/utilization and invalid capability values", () => {
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "bad-model",
      display_name: "Bad Model",
      loadbalance_strategy_id: 9,
      context_window_tokens: "0",
      default_output_token_reserve: "",
      max_context_utilization: "0.90",
      preferred_context_utilization_threshold: "",
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Bad Model",
    }),
    "context_window_tokens_invalid",
  );
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "bad-model",
      display_name: "Bad Model",
      loadbalance_strategy_id: 9,
      context_window_tokens: "",
      default_output_token_reserve: "-1",
      max_context_utilization: "0.90",
      preferred_context_utilization_threshold: "",
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Bad Model",
    }),
    "default_output_token_reserve_invalid",
  );
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "bad-model",
      display_name: "Bad Model",
      loadbalance_strategy_id: 9,
      context_window_tokens: "",
      default_output_token_reserve: "4096",
      max_context_utilization: "1.5",
      preferred_context_utilization_threshold: "",
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Bad Model",
    }),
    "max_context_utilization_invalid",
  );
});

test("validation rejects invalid preferred threshold values and bands above max", () => {
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "bad-model",
      display_name: "Bad Model",
      loadbalance_strategy_id: 9,
      context_window_tokens: "",
      default_output_token_reserve: "4096",
      max_context_utilization: "0.90",
      preferred_context_utilization_threshold: "1.2",
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Bad Model",
    }),
    "preferred_context_utilization_threshold_invalid",
  );
  assert.equal(
    validateModelFormData({
      vendor_id: null,
      api_family: "openai",
      model_id: "bad-model",
      display_name: "Bad Model",
      loadbalance_strategy_id: 9,
      context_window_tokens: "",
      default_output_token_reserve: "4096",
      max_context_utilization: "0.70",
      preferred_context_utilization_threshold: "0.75",
      access_targets: [],
      is_enabled: false,
      last_auto_display_name: "Bad Model",
    }),
    "preferred_context_utilization_threshold_exceeds_max",
  );
});

test("enabled models require at least one enabled valid access target", () => {
  const enabledDraft = {
    vendor_id: null,
    api_family: "openai",
    model_id: "live-model",
    display_name: "Live Model",
    loadbalance_strategy_id: 9,
    context_window_tokens: "",
    default_output_token_reserve: "4096",
    max_context_utilization: "0.90",
    preferred_context_utilization_threshold: "",
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

test("changing api family clears incompatible access targets", () => {
  const formData = setApiFamilyOnForm(
    {
      vendor_id: 11,
      api_family: "openai",
      model_id: "targeted-model",
      display_name: "Targeted Model",
      loadbalance_strategy_id: 17,
      context_window_tokens: "",
      default_output_token_reserve: "4096",
      max_context_utilization: "0.90",
      preferred_context_utilization_threshold: "",
      access_targets: [{ target_type: "connection", connection_id: 88, position: 0, is_enabled: true }],
      is_enabled: false,
      last_auto_display_name: "Targeted Model",
    },
    "anthropic",
  );

  assert.equal(formData.api_family, "anthropic");
  assert.deepEqual(formData.access_targets, []);
});

test("payload shaping serializes capability strings to numeric fields", () => {
  const formData = {
    vendor_id: 11,
    api_family: "openai",
    model_id: "live-model",
    display_name: "  Live Model  ",
    loadbalance_strategy_id: 17,
    context_window_tokens: "",
    default_output_token_reserve: "4096",
    max_context_utilization: "0.90",
    preferred_context_utilization_threshold: "0.70",
    access_targets: [
      { target_type: "connection", connection_id: 77, position: 4, is_enabled: true },
      { target_type: "model", target_model_id: "target-model", position: 9, is_enabled: true },
      { target_type: "model", target_model_id: "target-model", position: 10, is_enabled: false },
    ],
    is_enabled: true,
    last_auto_display_name: "Live Model",
  };

  const expectedAccessTargets = [{ target_type: "model", target_model_id: "target-model", position: 0, is_enabled: true }];

  assert.deepEqual(toModelCreatePayload(formData), {
    vendor_id: 11,
    api_family: "openai",
    model_id: "live-model",
    display_name: "Live Model",
    is_enabled: true,
    loadbalance_strategy_id: 17,
    context_window_tokens: null,
    default_output_token_reserve: 4096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.7,
    access_targets: expectedAccessTargets,
  });

  assert.deepEqual(toModelUpdatePayload(formData), {
    vendor_id: 11,
    api_family: "openai",
    display_name: "  Live Model  ",
    model_id: "live-model",
    is_enabled: true,
    loadbalance_strategy_id: 17,
    context_window_tokens: null,
    default_output_token_reserve: 4096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.7,
    access_targets: expectedAccessTargets,
  });
});
