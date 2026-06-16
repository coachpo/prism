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
  getAccessTargetModelsForApiFamily,
  getEditModelConnectionOptions,
  getPromotionTargetModelsForApiFamily,
  setApiFamilyOnForm,
  toModelCreatePayload,
  toModelUpdatePayload,
  validateModelFormData,
} = load(path.join(frontendDir, "src/pages/models/modelFormState.ts"));

const baseEditModel = {
  id: 7,
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
  const formData = createNewModelFormData(42);

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

test("edit model connection options preserve terminal target display names", () => {
  const connectionOptions = getEditModelConnectionOptions({
    ...baseEditModel,
    access_targets: [
      {
        target_type: "connection",
        connection_id: 12,
        position: 1,
        is_enabled: true,
        connection: {
          id: 12,
          name: "Sub-CPA-B",
          endpoint: { name: "Endpoint fallback" },
        },
      },
      {
        target_type: "connection",
        connection_id: 13,
        position: 0,
        is_enabled: true,
        connection: {
          id: 13,
          name: null,
          endpoint: { name: "Free-CPA-A" },
        },
      },
    ],
  });

  assert.deepEqual(
    connectionOptions.map((connection) => ({
      id: connection.id,
      name: connection.name,
      endpointName: connection.endpoint?.name,
      priority: connection.priority,
    })),
    [
      { id: 13, name: null, endpointName: "Free-CPA-A", priority: 0 },
      { id: 12, name: "Sub-CPA-B", endpointName: "Endpoint fallback", priority: 1 },
    ],
  );
});

test("edit hydration normalizes flat ordered model access targets", () => {
  const formData = createEditModelFormData({
    ...baseEditModel,
    access_targets: [
      {
        id: 501,
        target_type: "model",
        target_model_id: "peer-model-a",
        connection_id: null,
        terminal_target_id: null,
        position: 8,
        is_enabled: true,
        target_model: { id: 91, profile_id: 7, api_family: "openai", model_id: "peer-model-a", display_name: "Peer A", loadbalance_strategy_id: 21, is_enabled: true },
        connection: null,
        terminal_target: null,
        created_at: "2026-04-20T00:00:00Z",
        updated_at: "2026-04-20T00:00:00Z",
      },
      {
        id: 502,
        target_type: "model",
        target_model_id: "peer-model-b",
        connection_id: null,
        terminal_target_id: null,
        position: 11,
        is_enabled: false,
        target_model: { id: 92, profile_id: 7, api_family: "openai", model_id: "peer-model-b", display_name: "Peer B", loadbalance_strategy_id: 21, is_enabled: true },
        connection: null,
        terminal_target: null,
        created_at: "2026-04-20T00:00:00Z",
        updated_at: "2026-04-20T00:00:00Z",
      },
    ],
  });

  assert.deepEqual(formData.access_targets, [
    {
      target_type: "model",
      target_model_id: "peer-model-a",
      position: 0,
      is_enabled: true,
    },
    {
      target_type: "model",
      target_model_id: "peer-model-b",
      position: 1,
      is_enabled: false,
    },
  ]);
});

test("disabled drafts validate with no access targets", () => {
  assert.equal(
    validateModelFormData({
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

test("changing api family clears incompatible access targets and promotion target", () => {
  const formData = setApiFamilyOnForm(
    {
      api_family: "openai",
      model_id: "targeted-model",
      display_name: "Targeted Model",
      loadbalance_strategy_id: 17,
      context_window_tokens: "",
      default_output_token_reserve: "4096",
      max_context_utilization: "0.90",
      preferred_context_utilization_threshold: "",
      context_overflow_promotion_target_id: "promoted-model",
      access_targets: [{ target_type: "connection", connection_id: 88, position: 0, is_enabled: true }],
      is_enabled: false,
      last_auto_display_name: "Targeted Model",
    },
    "anthropic",
  );

  assert.equal(formData.api_family, "anthropic");
  assert.equal(formData.context_overflow_promotion_target_id, "");
  assert.deepEqual(formData.access_targets, []);
});

test("promotion target filtering excludes obvious invalid local choices", () => {
  const models = [
    { id: 1, api_family: "openai", model_id: "current-model", is_enabled: true },
    { id: 2, api_family: "openai", model_id: "enabled-target", is_enabled: true },
    { id: 3, api_family: "openai", model_id: "disabled-target", is_enabled: false },
    { id: 4, api_family: "openai", model_id: "facade-target", is_enabled: true, facade_enabled: true },
    { id: 5, api_family: "openai", model_id: "legacy-facade-target", is_enabled: true, is_facade: true },
    { id: 6, api_family: "anthropic", model_id: "wrong-family", is_enabled: true },
  ];

  assert.deepEqual(
    getPromotionTargetModelsForApiFamily(models, "openai", " current-model ").map((model) => model.model_id),
    ["enabled-target"],
  );
  assert.deepEqual(
    getAccessTargetModelsForApiFamily(models, "openai", "current-model").map((model) => model.model_id),
    ["enabled-target"],
  );
});

test("payload shaping serializes capability strings to numeric fields", () => {
  const formData = {
    api_family: "openai",
    model_id: "live-model",
    display_name: "  Live Model  ",
    loadbalance_strategy_id: 17,
    context_window_tokens: "",
    default_output_token_reserve: "4096",
    max_context_utilization: "0.90",
    preferred_context_utilization_threshold: "0.70",
    context_overflow_promotion_target_id: "",
    access_targets: [
      { target_type: "connection", connection_id: 77, position: 4, is_enabled: true },
      { target_type: "model", target_model_id: "target-model", position: 9, is_enabled: true },
      { target_type: "model", target_model_id: "target-model", position: 10, is_enabled: false },
    ],
    is_enabled: true,
    last_auto_display_name: "Live Model",
  };

  const expectedAccessTargets = [{
    target_type: "model",
    target_model_id: "target-model",
    position: 0,
    is_enabled: true,
  }];

  assert.deepEqual(toModelCreatePayload(formData), {
    api_family: "openai",
    model_id: "live-model",
    display_name: "Live Model",
    is_enabled: true,
    loadbalance_strategy_id: 17,
    context_window_tokens: null,
    default_output_token_reserve: 4096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.7,
    context_overflow_promotion_target_id: null,
    access_targets: expectedAccessTargets,
  });

  assert.deepEqual(toModelUpdatePayload(formData), {
    api_family: "openai",
    display_name: "  Live Model  ",
    model_id: "live-model",
    is_enabled: true,
    loadbalance_strategy_id: 17,
    context_window_tokens: null,
    default_output_token_reserve: 4096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: 0.7,
    context_overflow_promotion_target_id: null,
    access_targets: expectedAccessTargets,
  });
});

test("payload shaping keeps promotion target separate from access target normalization", () => {
  const formData = {
    api_family: "openai",
    model_id: "live-model",
    display_name: "Live Model",
    loadbalance_strategy_id: 17,
    context_window_tokens: "",
    default_output_token_reserve: "4096",
    max_context_utilization: "0.90",
    preferred_context_utilization_threshold: "",
    context_overflow_promotion_target_id: " promoted-model ",
    access_targets: [
      { target_type: "connection", connection_id: 77, position: 0, is_enabled: true },
      { target_type: "model", target_model_id: "fallback-model", position: 1, is_enabled: true },
    ],
    is_enabled: true,
    last_auto_display_name: "Live Model",
  };

  assert.deepEqual(toModelUpdatePayload(formData), {
    api_family: "openai",
    display_name: "Live Model",
    model_id: "live-model",
    is_enabled: true,
    loadbalance_strategy_id: 17,
    context_window_tokens: null,
    default_output_token_reserve: 4096,
    max_context_utilization: 0.9,
    preferred_context_utilization_threshold: null,
    context_overflow_promotion_target_id: "promoted-model",
    access_targets: [
      {
        target_type: "model",
        target_model_id: "fallback-model",
        position: 0,
        is_enabled: true,
      },
    ],
  });
});
