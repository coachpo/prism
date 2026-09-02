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

test("model target reorder client uses the explicit position route contract", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  let apiModule;
  globalThis.fetch = async (url, init) => {
    requests.push({
        url: String(url),
        init: normalizeFetchInit(init),
      });
    return {
      ok: true,
      status: 200,
      text: async () => JSON.stringify({ access_targets: [], configuration_warnings: [] }),
    };
  };

  try {
    apiModule = loadApi();
    const { api } = apiModule;

    await api.models.targets.movePosition(10, 20, 3);

    assert.deepEqual(requests, [
      {
        url: "/api/models/10/targets/20/position",
        init: {
          method: "PATCH",
          body: JSON.stringify({ to_index: 3 }),
          credentials: "include",
          headers: { "Content-Type": "application/json", "X-Profile-Id": "1" },
        },
      },
    ]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("model target client rejects obsolete routing metadata from API responses", async () => {
  const originalFetch = globalThis.fetch;
  let apiModule;
  globalThis.fetch = async () => ({
    ok: true,
    status: 200,
    text: async () => JSON.stringify([
      {
        id: 1,
        profile_id: 42,
        api_family: "openai",
        model_id: "gpt-4o-mini",
        display_name: "GPT-4o mini",
        loadbalance_strategy_id: null,
        loadbalance_strategy: null,
        access_targets: [
          {
            id: 7,
            target_type: "model",
            target_model_id: "gpt-4o-terminal",
            connection_id: null,
            terminal_target_id: null,
            position: 0,
            is_enabled: true,
            target_model: null,
            connection: null,
            terminal_target: null,
            created_at: "2026-06-16T00:00:00Z",
            updated_at: "2026-06-16T00:00:00Z",
            weight: 1,
            target_priority: 0,
          },
        ],
        direct_request_enabled: true,
        incoming_model_target_count: 0,
        configuration_warnings: [],
        is_enabled: true,
        connection_count: 0,
        active_connection_count: 0,
        health_success_rate: null,
        health_total_requests: 0,
        created_at: "2026-06-16T00:00:00Z",
        updated_at: "2026-06-16T00:00:00Z",
      },
    ]),
  });

  try {
    apiModule = loadApi();
    const { api } = apiModule;

    await assert.rejects(api.models.list(), /access_targets\.weight/);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("model list preserves the backend routing summary and model delete envelope", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    if (String(url) === "/api/models/7") {
      return {
        ok: true,
        status: 200,
        text: async () => JSON.stringify({ deleted: true }),
      };
    }
    return {
      ok: true,
      status: 200,
      text: async () => JSON.stringify([{
        id: 7,
        profile_id: 1,
        api_family: "openai",
        model_id: "gpt-summary",
        display_name: null,
        openai_accepted_format: "dual_native",
        openai_image_operations: null,
        loadbalance_strategy_id: 3,
        loadbalance_strategy: null,
        access_targets: [],
        direct_request_enabled: true,
        incoming_model_target_count: 0,
        configuration_warnings: [],
        is_enabled: true,
        connection_count: 2,
        active_connection_count: 1,
        health_success_rate: null,
        health_total_requests: 0,
        routing_summary: {
          enabled_access_target_count: 2,
          total_access_target_count: 3,
          openai_mode: "dual_native",
          coverage: "partial",
          operation_groups: [{ group: "responses", status: "routable" }],
          single_truncated_access_target_ids: [12],
          warning_codes: ["single_strategy_truncates_targets"],
        },
        created_at: "2026-08-28T00:00:00Z",
        updated_at: "2026-08-28T00:00:00Z",
      }]),
    };
  };

  try {
    const { api } = loadApi();
    const items = await api.models.list();
    assert.equal(items[0].connection_count, 2);
    assert.equal(items[0].routing_summary.coverage, "partial");
    assert.deepEqual(items[0].routing_summary.single_truncated_access_target_ids, [12]);
    assert.deepEqual(await api.models.delete(7), { deleted: true });
    assert.equal(requests[1].init.method, "DELETE");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("routing diagnostics passes the caller AbortSignal to fetch", async () => {
  const originalFetch = globalThis.fetch;
  let observedSignal;
  globalThis.fetch = async (_url, init) => {
    observedSignal = init?.signal;
    return {
      ok: true,
      status: 200,
      text: async () => JSON.stringify({
        model_config_id: 7,
        openai_accepted_format: null,
        strategy: { id: 3, type: "single" },
        accepted_operations: [],
        stages: [],
        targets: [],
        operation_routes: [],
        operation_coverage: [],
        configuration_warnings: [],
      }),
    };
  };

  try {
    const { load } = createTsModuleLoader({ rootDir: frontendDir });
    const { modelRoutingDiagnostics } = load(
      path.join(frontendDir, "src/lib/api/model_routing.ts"),
    );
    const controller = new AbortController();
    await modelRoutingDiagnostics.get(7, controller.signal);
    assert.equal(observedSignal instanceof AbortSignal, true);
    controller.abort();
    assert.equal(observedSignal.aborted, true);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

// The auth session coordinator attaches a live epoch AbortSignal to every
// protected fetch; the route contract is the remaining init surface.
function normalizeFetchInit(init) {
  if (!init) return init;
  const { signal: _signal, ...rest } = init;
  return rest;
}
