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
    requests.push({ url: String(url), init });
    return {
      ok: true,
      status: 200,
      text: async () => "[]",
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
