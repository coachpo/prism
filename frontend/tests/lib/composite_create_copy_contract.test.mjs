import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadApi() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });
  return load(path.join(frontendDir, "src/lib/api.ts"));
}

test("composite model create sends the nested initial terminal target", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init, body: JSON.parse(init?.body ?? "{}") });
    return {
      ok: true,
      status: 201,
      text: async () => JSON.stringify({
        model: {
          id: 7,
          profile_id: 1,
          api_family: "openai",
          model_id: "composite-model",
          display_name: "Composite",
          openai_accepted_format: "dual_native",
          loadbalance_strategy_id: 11,
          loadbalance_strategy: null,
          access_targets: [{ id: 1, target_type: "connection", connection_id: 15 }],
          is_enabled: true,
          created_at: "2026-08-08T00:00:00Z",
          updated_at: "2026-08-08T00:00:00Z",
        },
        configuration_warnings: [],
      }),
    };
  };

  try {
    const { api } = loadApi();
    const response = await api.models.create({
      api_family: "openai",
      model_id: "composite-model",
      display_name: "Composite",
      loadbalance_strategy_id: 11,
      openai_accepted_format: "dual_native",
      is_enabled: true,
      initial_terminal_target: {
        endpoint_id: 3,
        openai_text_capability: "responses_only",
        custom_headers: { "X-Tenant": "prism" },
      },
    });

    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, "/api/models");
    assert.equal(requests[0].init.method, "POST");
    assert.deepEqual(requests[0].body.initial_terminal_target, {
      endpoint_id: 3,
      openai_text_capability: "responses_only",
      custom_headers: { "X-Tenant": "prism" },
    });
    assert.equal(response.model.id, 7);
    assert.deepEqual(response.configuration_warnings, []);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("configure-later omits the nested target and forces disabled", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init, body: JSON.parse(init?.body ?? "{}") });
    return {
      ok: true,
      status: 201,
      text: async () => JSON.stringify({ model: { id: 8, profile_id: 1, api_family: "openai", model_id: "later-model", display_name: "Later", openai_accepted_format: "dual_native", loadbalance_strategy_id: 11, loadbalance_strategy: null, access_targets: [], is_enabled: false, created_at: "2026-08-08T00:00:00Z", updated_at: "2026-08-08T00:00:00Z" }, configuration_warnings: [] }),
    };
  };

  try {
    const { api } = loadApi();
    await api.models.create({
      api_family: "openai",
      model_id: "later-model",
      display_name: "Later",
      loadbalance_strategy_id: 11,
      is_enabled: false,
    });

    assert.equal(requests[0].body.initial_terminal_target, undefined);
    assert.equal(requests[0].body.is_enabled, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("terminal target copy request keeps enable_copies default false semantics", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init, body: JSON.parse(init?.body ?? "{}") });
    return {
      ok: true,
      status: 201,
      text: async () => JSON.stringify({
        source_connection_id: 15,
        items: [{
          model_config_id: 8,
          connection_summary: { id: 25, name: "Primary", endpoint_id: 1, is_active: true, openai_text_capability: "dual_native", pricing_template: null, qps_limit: null, max_in_flight_non_stream: null, max_in_flight_stream: null, custom_header_count: 0 },
          access_target: { id: 52, target_type: "connection", connection_id: 25, terminal_target_id: 25, position: 0, is_enabled: false },
        }],
        configuration_warnings: [],
      }),
    };
  };

  try {
    const { api } = loadApi();
    const response = await api.models.connections.copies(7, 15, { destination_model_config_ids: [8] });

    assert.equal(requests.length, 1);
    assert.equal(requests[0].url, "/api/models/7/connections/15/copies");
    assert.deepEqual(requests[0].body, { destination_model_config_ids: [8] });
    assert.equal(response.items[0].access_target.is_enabled, false);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("connection mutation envelopes unwrap connection and never leak warnings into it", async () => {
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async () => ({
    ok: true,
    status: 200,
    text: async () => JSON.stringify({
      connection: { id: 15, profile_id: 1, api_family: "openai", endpoint_id: 1, is_active: true, priority: 0, name: "Primary", auth_type: null, custom_headers: null, openai_text_capability: "responses_only", pricing_template_id: null, qps_limit: null, max_in_flight_non_stream: null, max_in_flight_stream: null, pricing_template: null, created_at: "2026-08-08T00:00:00Z", updated_at: "2026-08-08T00:00:00Z", model_config_id: 7, endpoint: { id: 1, name: "E", base_url: "https://e.invalid", has_api_key: true, masked_api_key: "****", position: 0, created_at: "", updated_at: "" } },
      access_targets: [{ id: 1, target_type: "connection", connection_id: 15, terminal_target_id: 15, position: 0, is_enabled: true }],
      configuration_warnings: [{ code: "openai_target_partial_coverage", severity: "warning", message: "", path: "openai_text_capability", model_config_id: 7, access_target_id: 1, connection_id: 15, operation_names: [], details: null }],
    }),
  });

  try {
    const { api } = loadApi();
    const response = await api.models.connections.update(7, 15, { openai_text_capability: "responses_only" });
    assert.equal(response.connection.id, 15);
    assert.equal(response.configuration_warnings.length, 1);
    assert.equal(response.configuration_warnings[0].code, "openai_target_partial_coverage");
    assert.equal(response.access_targets[0].connection_id, 15);
    assert.equal(response.connection.openai_text_capability, "responses_only");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
