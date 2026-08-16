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
  normalizeOpenAIAcceptedFormatForForm,
  setApiFamilyOnForm,
  setOpenAIAcceptedFormatOnForm,
  toModelCreatePayload,
  toModelUpdatePayload,
  validateModelFormData,
} = load(path.join(frontendDir, "src/pages/models/modelFormState.ts"));

const baseEditModel = {
  id: 7,
  api_family: "openai",
  model_id: "native-model",
  display_name: "Native Model",
  openai_accepted_format: "responses_only",
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
};

test("new model defaults seed model CRUD fields only", () => {
  const formData = createNewModelFormData(42);

  assert.equal(DEFAULT_MODEL_FORM_DATA.is_enabled, false);
  assert.equal(DEFAULT_MODEL_FORM_DATA.openai_accepted_format, "dual_native");
  assert.equal(DEFAULT_MODEL_FORM_DATA.openai_image_operations, "");
  assert.deepEqual(Object.keys(DEFAULT_MODEL_FORM_DATA).sort(), [
    "api_family",
    "display_name",
    "is_enabled",
    "last_auto_display_name",
    "loadbalance_strategy_id",
    "model_id",
    "openai_accepted_format",
    "openai_image_operations",
  ]);
  assert.equal(formData.is_enabled, false);
  assert.equal(formData.openai_accepted_format, "dual_native");
  assert.equal(formData.openai_image_operations, "");
  assert.equal(formData.loadbalance_strategy_id, 42);
});

test("edit hydration seeds model form fields from list and detail payloads", () => {
  const fromList = createEditModelFormData(baseEditModel);
  const fromDetail = createEditModelFormData({
    ...baseEditModel,
    display_name: null,
    access_targets: [
      { target_type: "connection", connection_id: 88, position: 5, is_enabled: true, connection: null },
    ],
  });

  assert.equal(fromList.openai_accepted_format, "responses_only");
  assert.equal(fromDetail.display_name, "");
  assert.equal(fromDetail.openai_accepted_format, "responses_only");
});

// Hydration must preserve an absent text mode rather than defaulting it: a
// pure image model has no text format, and defaulting would silently re-author
// it as a text model on the next save.
test("edit hydration preserves an absent OpenAI accepted format", () => {
  const { openai_accepted_format: _format, ...imageOnlyModel } = baseEditModel;

  const hydrated = createEditModelFormData({ ...imageOnlyModel, openai_image_operations: "generations" });
  assert.equal(hydrated.openai_accepted_format, "");
  assert.equal(hydrated.openai_image_operations, "generations");
});

test("edit hydration seeds the image dimension and ignores unknown values", () => {
  assert.equal(createEditModelFormData(baseEditModel).openai_image_operations, "");
  assert.equal(
    createEditModelFormData({ ...baseEditModel, openai_image_operations: "generations_and_edits" }).openai_image_operations,
    "generations_and_edits",
  );
  assert.equal(
    createEditModelFormData({ ...baseEditModel, openai_image_operations: "legacy" }).openai_image_operations,
    "",
  );
});

test("accepted format normalization stays centralized in model form state", () => {
  assert.equal(normalizeOpenAIAcceptedFormatForForm("openai", "responses_only"), "responses_only");
  assert.equal(normalizeOpenAIAcceptedFormatForForm("openai", "chat_completions_only"), "chat_completions_only");
  assert.equal(normalizeOpenAIAcceptedFormatForForm("openai", "dual_native"), "dual_native");
  assert.equal(normalizeOpenAIAcceptedFormatForForm("openai", null), "dual_native");
  assert.equal(normalizeOpenAIAcceptedFormatForForm("openai", "legacy"), "dual_native");
  assert.equal(normalizeOpenAIAcceptedFormatForForm("anthropic", "dual_native"), "");

  const openAIForm = setOpenAIAcceptedFormatOnForm(createNewModelFormData(17), "responses_only");
  assert.equal(openAIForm.openai_accepted_format, "responses_only");

  const anthropicForm = setApiFamilyOnForm(openAIForm, "anthropic");
  assert.equal(anthropicForm.openai_accepted_format, "");
  assert.equal(setOpenAIAcceptedFormatOnForm(anthropicForm, "chat_completions_only").openai_accepted_format, "");
  assert.equal(setApiFamilyOnForm(anthropicForm, "openai").openai_accepted_format, "dual_native");
});

// Re-selecting the family a form already has must not re-add a text mode the
// author cleared, or an image-only model would be silently re-authored.
test("re-selecting openai preserves a deliberately cleared text mode", () => {
  const imageOnlyForm = {
    ...createNewModelFormData(17),
    openai_accepted_format: "",
    openai_image_operations: "generations_and_edits",
  };

  const reselected = setApiFamilyOnForm(imageOnlyForm, "openai");
  assert.equal(reselected.openai_accepted_format, "");
  assert.equal(reselected.openai_image_operations, "generations_and_edits");
});

test("switching a model away from openai clears both dimensions", () => {
  const dualForm = {
    ...createNewModelFormData(17),
    openai_accepted_format: "responses_only",
    openai_image_operations: "edits",
  };

  const anthropic = setApiFamilyOnForm(dualForm, "anthropic");
  assert.equal(anthropic.openai_accepted_format, "");
  assert.equal(anthropic.openai_image_operations, "");
});

test("edit model connection options preserve terminal target display names", () => {
  const connectionOptions = getEditModelConnectionOptions({
    ...baseEditModel,
    access_targets: [
      { target_type: "connection", connection_id: 12, position: 1, is_enabled: true, connection: { id: 12, name: "Sub-CPA-B", endpoint: { name: "Endpoint fallback" } } },
      { target_type: "connection", connection_id: 13, position: 0, is_enabled: true, connection: { id: 13, name: null, endpoint: { name: "Free-CPA-A" } } },
    ],
  });

  assert.deepEqual(
    connectionOptions.map((connection) => ({ id: connection.id, name: connection.name, endpointName: connection.endpoint?.name, priority: connection.priority })),
    [
      { id: 13, name: null, endpointName: "Free-CPA-A", priority: 0 },
      { id: 12, name: "Sub-CPA-B", endpointName: "Endpoint fallback", priority: 1 },
    ],
  );
});

test("edit hydration omits access targets from model CRUD form state", () => {
  const formData = createEditModelFormData({
    ...baseEditModel,
    access_targets: [
      { id: 501, target_type: "model", target_model_id: "peer-model-a", connection_id: null, terminal_target_id: null, position: 8, is_enabled: true, target_model: { id: 91, profile_id: 7, api_family: "openai", model_id: "peer-model-a", display_name: "Peer A", loadbalance_strategy_id: 21, is_enabled: true }, connection: null, terminal_target: null, created_at: "2026-04-20T00:00:00Z", updated_at: "2026-04-20T00:00:00Z" },
      { id: 502, target_type: "model", target_model_id: "peer-model-b", connection_id: null, terminal_target_id: null, position: 11, is_enabled: false, target_model: { id: 92, profile_id: 7, api_family: "openai", model_id: "peer-model-b", display_name: "Peer B", loadbalance_strategy_id: 21, is_enabled: true }, connection: null, terminal_target: null, created_at: "2026-04-20T00:00:00Z", updated_at: "2026-04-20T00:00:00Z" },
    ],
  });

  assert.equal(Object.hasOwn(formData, "access_targets"), false);
});

test("drafts validate without access target payloads", () => {
  assert.equal(validateModelFormData({ ...createNewModelFormData(9), model_id: "draft-model", display_name: "Draft Model" }), null);
});

test("validation rejects blank model id before other form work proceeds", () => {
  assert.equal(validateModelFormData({ ...createNewModelFormData(9), model_id: "   ", display_name: "Draft Model" }), "model_id_required");
});

test("enabled model state is validated by backend invariant, not model CRUD payloads", () => {
  const enabledDraft = { ...createNewModelFormData(9), model_id: "live-model", display_name: "Live Model", is_enabled: true };

  assert.equal(validateModelFormData(enabledDraft), null);
});

test("changing api family only normalizes OpenAI accepted format", () => {
  const formData = setApiFamilyOnForm(
    { ...createNewModelFormData(17), model_id: "targeted-model", display_name: "Targeted Model" },
    "anthropic",
  );

  assert.equal(formData.api_family, "anthropic");
  assert.equal(formData.openai_accepted_format, "");
  assert.equal(Object.hasOwn(formData, "access_targets"), false);
});

test("access target filtering excludes obvious invalid local choices", () => {
  const models = [
    { id: 1, api_family: "openai", model_id: "current-model", is_enabled: true },
    { id: 2, api_family: "openai", model_id: "enabled-target", is_enabled: true },
    { id: 3, api_family: "openai", model_id: "disabled-target", is_enabled: false },
    { id: 4, api_family: "openai", model_id: "second-enabled-target", is_enabled: true },
    { id: 5, api_family: "openai", model_id: "third-enabled-target", is_enabled: true },
    { id: 6, api_family: "anthropic", model_id: "wrong-family", is_enabled: true },
  ];

  assert.deepEqual(
    getAccessTargetModelsForApiFamily(models, "openai", "current-model").map((model) => model.model_id),
    ["enabled-target", "second-enabled-target", "third-enabled-target"],
  );
});

test("OpenAI access target candidates keep all same-family enabled options", () => {
  const models = [
    { id: 1, api_family: "openai", model_id: "current-model", is_enabled: true, openai_accepted_format: "dual_native" },
    { id: 2, api_family: "openai", model_id: "dual-peer", is_enabled: true, openai_accepted_format: "dual_native" },
    { id: 3, api_family: "openai", model_id: "chat-peer", is_enabled: true, openai_accepted_format: "chat_completions_only" },
    { id: 4, api_family: "openai", model_id: "responses-peer", is_enabled: true, openai_accepted_format: "responses_only" },
    { id: 5, api_family: "openai", model_id: "mode-less-peer", is_enabled: true, openai_accepted_format: null },
  ];

  assert.deepEqual(
    getAccessTargetModelsForApiFamily(models, "openai", "current-model", "dual_native").map((model) => model.model_id),
    ["dual-peer", "chat-peer", "responses-peer", "mode-less-peer"],
  );
});

test("non-OpenAI access target candidates ignore the mode argument", () => {
  const models = [
    { id: 1, api_family: "anthropic", model_id: "current-model", is_enabled: true },
    { id: 2, api_family: "anthropic", model_id: "claude-peer", is_enabled: true },
    { id: 3, api_family: "openai", model_id: "openai-peer", is_enabled: true, openai_accepted_format: "dual_native" },
  ];

  assert.deepEqual(
    getAccessTargetModelsForApiFamily(models, "anthropic", "current-model", "dual_native").map((model) => model.model_id),
    ["claude-peer"],
  );
});

test("payload shaping preserves model CRUD fields only", () => {
  const formData = { ...createNewModelFormData(17), model_id: "live-model", display_name: "  Live Model  ", is_enabled: true };

  assert.deepEqual(toModelCreatePayload(formData), {
    api_family: "openai",
    model_id: "live-model",
    display_name: "Live Model",
    is_enabled: true,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    loadbalance_strategy_id: 17,
  });
  assert.deepEqual(toModelUpdatePayload(formData), {
    api_family: "openai",
    display_name: "  Live Model  ",
    model_id: "live-model",
    is_enabled: true,
    openai_accepted_format: "dual_native",
    openai_image_operations: null,
    loadbalance_strategy_id: 17,
  });
});

test("non-OpenAI model payloads omit accepted format", () => {
  const payload = toModelCreatePayload({ ...setApiFamilyOnForm(createNewModelFormData(17), "anthropic"), model_id: "claude-model", display_name: "Claude Model" });

  assert.equal(payload.api_family, "anthropic");
  assert.equal(Object.hasOwn(payload, "openai_accepted_format"), false);
});

// The two dimensions are independent, so a blank text format is valid exactly
// when the image dimension carries the model.
test("OpenAI payload shaping allows a blank accepted format only alongside images", () => {
  const imageOnly = toModelCreatePayload({
    ...createNewModelFormData(17),
    model_id: "gpt-image-2",
    display_name: "GPT Image",
    openai_accepted_format: "",
    openai_image_operations: "generations_and_edits",
  });

  assert.equal(imageOnly.api_family, "openai");
  assert.equal(imageOnly.openai_accepted_format, null);
  assert.equal(imageOnly.openai_image_operations, "generations_and_edits");

  assert.throws(
    () => toModelCreatePayload({
      ...createNewModelFormData(17),
      model_id: "legacy-openai",
      display_name: "Legacy OpenAI",
      openai_accepted_format: "",
    }),
    /at least one OpenAI capability dimension is required/,
  );
});

test("OpenAI models must declare at least one capability dimension", () => {
  const neither = { ...createNewModelFormData(17), model_id: "no-capability", openai_accepted_format: "", openai_image_operations: "" };
  assert.equal(validateModelFormData(neither), "openai_capability_required");

  const imageOnly = { ...neither, openai_image_operations: "generations" };
  assert.equal(validateModelFormData(imageOnly), null);

  const textOnly = { ...neither, openai_accepted_format: "responses_only" };
  assert.equal(validateModelFormData(textOnly), null);

  assert.equal(
    validateModelFormData({ ...neither, openai_image_operations: "legacy" }),
    "openai_image_operations_invalid",
  );
});
