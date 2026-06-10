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

test("friendly OpenAI probe controls resolve to the expected raw variant", () => {
  const { resolveOpenAIProbeVariant } = load(
    path.join(frontendDir, "src/pages/model-detail/connectionProbeBehavior.ts"),
  );

  assert.equal(
    resolveOpenAIProbeVariant({ probeApi: "chat_completions", reasoningMode: "disabled" }),
    "chat_completions_reasoning_none",
  );
});

test("raw OpenAI probe variants decompose into the expected friendly control state", () => {
  const { decomposeOpenAIProbeVariant } = load(
    path.join(frontendDir, "src/pages/model-detail/connectionProbeBehavior.ts"),
  );

  assert.deepEqual(decomposeOpenAIProbeVariant("responses_reasoning_none"), {
    probeApi: "responses",
    reasoningMode: "disabled",
  });
  assert.deepEqual(decomposeOpenAIProbeVariant(undefined), {
    probeApi: "responses",
    reasoningMode: "default",
  });
});

test("draft payload defaults OpenAI capability and probe behavior independently", () => {
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
      name: "Primary",
      is_active: true,
      custom_headers: null,
      pricing_template_id: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
      openai_text_capability: null,
      openai_probe_endpoint_variant: null,
    },
    headerRows: [],
    editingConnection: null,
    endpointSourceDefaultName: "OpenAI Primary",
  });

  assert.equal(result.errorMessage, null);
  assert.equal(result.payload.openai_text_capability, "responses_only");
  assert.equal(result.payload.openai_probe_endpoint_variant, "responses_minimal");
  assert.equal(result.payload.endpoint_id, 11);
});

test("draft payload omits OpenAI capability and probe behavior for non-OpenAI models", () => {
  const { buildConnectionDraftPayload } = load(
    path.join(frontendDir, "src/pages/model-detail/useModelDetailDataSupport.ts"),
  );

  const result = buildConnectionDraftPayload({
    apiFamily: "anthropic",
    createMode: "select",
    selectedEndpointId: "11",
    newEndpointForm: {
      name: "",
      base_url: "",
      api_key: "",
    },
    connectionForm: {
      name: "Anthropic",
      is_active: true,
      custom_headers: null,
      pricing_template_id: null,
      qps_limit: null,
      max_in_flight_non_stream: null,
      max_in_flight_stream: null,
      openai_probe_endpoint_variant: "chat_completions_minimal",
    },
    headerRows: [],
    editingConnection: null,
    endpointSourceDefaultName: "Anthropic Endpoint",
  });

  assert.equal(result.errorMessage, null);
  assert.ok(!Object.hasOwn(result.payload, "openai_text_capability"));
  assert.ok(!Object.hasOwn(result.payload, "openai_probe_endpoint_variant"));
  assert.equal(result.payload.endpoint_id, 11);
});

test("normalized connection headers collapse duplicate keys to the effective payload shape", () => {
  const { normalizeConnectionHeaders } = load(
    path.join(frontendDir, "src/pages/model-detail/useModelDetailDataSupport.ts"),
  );

  assert.deepEqual(
    normalizeConnectionHeaders([
      { id: "1", key: "x-test", value: "first" },
      { id: "2", key: "", value: "ignored" },
      { id: "3", key: "x-test", value: "second" },
      { id: "4", key: "x-trace", value: "trace" },
    ]),
    {
      "x-test": "second",
      "x-trace": "trace",
    },
  );
});
