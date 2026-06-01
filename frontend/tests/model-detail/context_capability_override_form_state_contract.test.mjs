import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

const staticMessagesStub = {
  getStaticMessages: () => ({
    modelDetailData: {
      selectEndpoint: "selectEndpoint",
      fillEndpointFields: "fillEndpointFields",
    },
  }),
};

const modelFormStateStub = {
  normalizeProxyTargets: (value) => value,
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "@/i18n/staticMessages": staticMessagesStub,
    "../models/modelFormState": modelFormStateStub,
  },
});

test("connection capability drafts build mixed inherit and override payloads", () => {
  const { buildConnectionDraftPayload } = load(
    path.join(frontendDir, "src/pages/model-detail/useModelDetailDataSupport.ts"),
  );

  const result = buildConnectionDraftPayload({
    apiFamily: "openai",
    createMode: "select",
    selectedEndpointId: "11",
    newEndpointForm: {
      name: "",
      base_url: "",
      api_key: "",
    },
    connectionForm: {
      api_family: "openai",
      name: "Primary",
      is_active: true,
      custom_headers: null,
      pricing_template_id: 7,
      qps_limit: 12,
      max_in_flight_non_stream: 4,
      max_in_flight_stream: 2,
      openai_probe_endpoint_variant: "responses_reasoning_none",
      context_capability_drafts: {
        context_window_tokens: { mode: "inherit", value: "16384" },
        default_output_token_reserve: { mode: "override", value: "8192" },
        max_context_utilization: { mode: "override", value: "0.75" },
      },
    },
    headerRows: [{ id: "header-1", key: "x-test", value: "enabled" }],
    editingConnection: null,
    endpointSourceDefaultName: "OpenAI Primary",
  });

  assert.equal(result.errorMessage, null);
  assert.equal(result.payload.endpoint_id, 11);
  assert.equal(result.payload.context_window_tokens, null);
  assert.equal(result.payload.default_output_token_reserve, 8192);
  assert.equal(result.payload.max_context_utilization, 0.75);
  assert.equal(result.payload.openai_probe_endpoint_variant, "responses_reasoning_none");
  assert.deepEqual(result.payload.custom_headers, { "x-test": "enabled" });
  assert.equal(result.payload.qps_limit, 12);
  assert.equal(result.payload.max_in_flight_non_stream, 4);
  assert.equal(result.payload.max_in_flight_stream, 2);
});

test("edit hydration preserves same-as-owner explicit override intent from raw metadata", () => {
  const { createEditConnectionForm } = load(
    path.join(frontendDir, "src/pages/model-detail/useModelDetailDialogState.ts"),
  );

  const hydratedForm = createEditConnectionForm(
    {
      id: 33,
      profile_id: 9,
      model_config_id: 14,
      api_family: "openai",
      endpoint_id: 11,
      endpoint: undefined,
      is_active: true,
      priority: 0,
      name: "Owner override preserved",
      auth_type: null,
      custom_headers: null,
      openai_probe_endpoint_variant: "responses_minimal",
      context_window_tokens: 16384,
      default_output_token_reserve: 4096,
      max_context_utilization: 0.9,
      context_capability_overrides: {
        context_window_tokens: 16384,
        default_output_token_reserve: null,
        max_context_utilization: 0.9,
      },
      pricing_template_id: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
      pricing_template: null,
      health_status: "healthy",
      health_detail: null,
      last_health_check: null,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-01T00:00:00Z",
    },
    {
      apiFamily: "openai",
      ownerCapabilityDefaults: {
        context_window_tokens: 16384,
        default_output_token_reserve: 4096,
        max_context_utilization: 0.9,
      },
    },
  );

  assert.deepEqual(hydratedForm.context_capability_drafts.context_window_tokens, {
    mode: "override",
    value: "16384",
  });
  assert.deepEqual(hydratedForm.context_capability_drafts.default_output_token_reserve, {
    mode: "inherit",
    value: "4096",
  });
  assert.deepEqual(hydratedForm.context_capability_drafts.max_context_utilization, {
    mode: "override",
    value: "0.9",
  });
});

test("switching an override draft back to inherit emits explicit null instead of stale numeric data", () => {
  const { createEditConnectionForm } = load(
    path.join(frontendDir, "src/pages/model-detail/useModelDetailDialogState.ts"),
  );
  const { buildConnectionDraftPayload } = load(
    path.join(frontendDir, "src/pages/model-detail/useModelDetailDataSupport.ts"),
  );

  const connectionForm = createEditConnectionForm(
    {
      id: 77,
      profile_id: 9,
      model_config_id: 14,
      api_family: "openai",
      endpoint_id: 11,
      endpoint: undefined,
      is_active: true,
      priority: 1,
      name: "Reverted override",
      auth_type: null,
      custom_headers: null,
      openai_probe_endpoint_variant: "responses_minimal",
      context_window_tokens: 32768,
      default_output_token_reserve: 2048,
      max_context_utilization: 0.8,
      context_capability_overrides: {
        context_window_tokens: 32768,
        default_output_token_reserve: 2048,
        max_context_utilization: 0.8,
      },
      pricing_template_id: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
      pricing_template: null,
      health_status: "healthy",
      health_detail: null,
      last_health_check: null,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-01T00:00:00Z",
    },
    {
      apiFamily: "openai",
      ownerCapabilityDefaults: {
        context_window_tokens: 16384,
        default_output_token_reserve: 4096,
        max_context_utilization: 0.9,
      },
    },
  );

  connectionForm.context_capability_drafts.context_window_tokens = {
    mode: "inherit",
    value: "32768",
  };

  const result = buildConnectionDraftPayload({
    apiFamily: "openai",
    createMode: "select",
    selectedEndpointId: "11",
    newEndpointForm: {
      name: "",
      base_url: "",
      api_key: "",
    },
    connectionForm,
    headerRows: [],
    editingConnection: {
      id: 77,
      profile_id: 9,
      model_config_id: 14,
      api_family: "openai",
      endpoint_id: 11,
      endpoint: undefined,
      is_active: true,
      priority: 1,
      name: "Reverted override",
      auth_type: null,
      custom_headers: null,
      openai_probe_endpoint_variant: "responses_minimal",
      context_window_tokens: 32768,
      default_output_token_reserve: 2048,
      max_context_utilization: 0.8,
      context_capability_overrides: {
        context_window_tokens: 32768,
        default_output_token_reserve: 2048,
        max_context_utilization: 0.8,
      },
      pricing_template_id: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
      pricing_template: null,
      health_status: "healthy",
      health_detail: null,
      last_health_check: null,
      created_at: "2026-06-01T00:00:00Z",
      updated_at: "2026-06-01T00:00:00Z",
    },
    endpointSourceDefaultName: "OpenAI Primary",
  });

  assert.equal(result.errorMessage, null);
  assert.equal(result.payload.context_window_tokens, null);
  assert.equal(result.payload.default_output_token_reserve, 2048);
  assert.equal(result.payload.max_context_utilization, 0.8);
});
