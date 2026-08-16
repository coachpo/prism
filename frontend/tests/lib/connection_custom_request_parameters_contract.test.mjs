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

function stubFetch(bodies) {
  const calls = [];
  const originalFetch = globalThis.fetch;
  globalThis.fetch = async (url, init) => {
    calls.push({ url: String(url), init });
    const body = bodies.length > 0 ? bodies.shift() : { id: 1, custom_request_parameters: null };
    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };
  return { calls, restore: () => (globalThis.fetch = originalFetch) };
}

test("connection create sends the configured custom request parameters object", async () => {
  const { calls, restore } = stubFetch([]);
  try {
    const { api } = loadApi();
    await api.models.connections.create(42, {
      api_family: "openai",
      endpoint_id: 7,
      custom_request_parameters: { provider: { only: ["deepinfra/turbo"], allow_fallbacks: false } },
    });
    assert.equal(calls.length, 1);
    const payload = JSON.parse(calls[0].init.body);
    assert.equal(calls[0].url, "/api/models/42/connections");
    assert.deepEqual(payload.custom_request_parameters, {
      provider: { only: ["deepinfra/turbo"], allow_fallbacks: false },
    });
  } finally {
    restore();
  }
});

test("connection create sends explicit null for unconfigured custom request parameters", async () => {
  const { calls, restore } = stubFetch([]);
  try {
    const { api } = loadApi();
    await api.models.connections.create(42, {
      api_family: "openai",
      endpoint_id: 7,
      custom_request_parameters: null,
    });
    const payload = JSON.parse(calls[0].init.body);
    assert.equal(payload.custom_request_parameters, null);
  } finally {
    restore();
  }
});

test("connection update sends the replaced object and explicit null clears", async () => {
  const { calls, restore } = stubFetch([]);
  try {
    const { api } = loadApi();
    await api.models.connections.update(42, 9, {
      custom_request_parameters: { provider: { only: ["google-vertex/us-east5"] } },
    });
    await api.models.connections.update(42, 9, { custom_request_parameters: null });
    const replacePayload = JSON.parse(calls[0].init.body);
    assert.equal(calls[0].url, "/api/models/42/connections/9");
    assert.equal(calls[0].init.method, "PATCH");
    assert.deepEqual(replacePayload.custom_request_parameters, { provider: { only: ["google-vertex/us-east5"] } });
    const clearPayload = JSON.parse(calls[1].init.body);
    assert.equal(clearPayload.custom_request_parameters, null);
  } finally {
    restore();
  }
});

test("connection payload keeps omission semantics available for untouched updates", async () => {
  const { calls, restore } = stubFetch([]);
  try {
    const { api } = loadApi();
    await api.models.connections.update(42, 9, { name: "rename only" });
    const payload = JSON.parse(calls[0].init.body);
    assert.equal(Object.hasOwn(payload, "custom_request_parameters"), false);
  } finally {
    restore();
  }
});
