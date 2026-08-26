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
  return load(path.join(frontendDir, "src/lib/api/modelExport.ts"));
}

function normalizeFetchInit(init) {
  if (!init) return {};
  const headers = init.headers ?? {};
  return {
    method: init.method,
    cache: init.cache,
    headers,
    body: typeof init.body === "string" ? JSON.parse(init.body) : init.body,
  };
}

test("model export client fetches source from the managed per-platform route", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init: normalizeFetchInit(init) });
    return {
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          platform: "pi",
          target_version: "0.84.3",
          models: [],
          source_digest: "d".repeat(64),
        }),
    };
  };
  try {
    const api = await loadApi();
    const response = await api.fetchModelExportSource("pi");
    assert.equal(requests.length, 1);
    assert.ok(requests[0].url.endsWith("/api/models/exports/pi/source"));
    assert.equal(requests[0].init.cache, "no-store");
    assert.equal(response.source_digest.length, 64);
    assert.equal(response.target_version, "0.84.3");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("model export render posts digest-guarded replay bodies without query-cache coupling", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init: normalizeFetchInit(init) });
    return {
      ok: true,
      status: 200,
      text: async () =>
        JSON.stringify({
          platform: "opencode",
          target_version: "1.18.23",
          content: "{}\n",
          content_sha256: "e".repeat(64),
          file_name: "opencode-prism.json",
          mime_type: "application/json;charset=utf-8",
          model_results: [],
        }),
    };
  };
  try {
    const api = await loadApi();
    const response = await api.renderModelExport(
      {
        expected_source_digest: "a".repeat(64),
        model_config_ids: [5, 3],
        base_url: "https://prism.example",
        provider_id: "prism-home",
        enhancements: { 3: { fields: { headers: { "x-trace": "ok" } } } },
        // Explicit empty remains distinct from include:false; the backend owns
        // whether the target file carries an empty credential field.
        credential: { include: true, api_key: "" },
        default_model_config_id: 3,
      },
      "opencode",
    );
    assert.equal(requests[0].init.method, "POST");
    assert.equal(requests[0].init.cache, "no-store");
    assert.ok(requests[0].url.endsWith("/api/models/exports/opencode/render"));
    assert.equal(requests[0].init.body.expected_source_digest, "a".repeat(64));
    assert.deepEqual(requests[0].init.body.model_config_ids, [5, 3]);
    assert.equal(requests[0].init.body.base_url, "https://prism.example");
    assert.equal(requests[0].init.body.provider_id, "prism-home");
    assert.deepEqual(requests[0].init.body.credential, {
      include: true,
      api_key: "",
    });
    assert.equal(requests[0].init.body.default_model_config_id, 3);
    assert.equal("enrichment_candidates" in requests[0].init.body, false);
    assert.equal("include_api_keys" in requests[0].init.body, false);
    assert.equal("api_key_overrides" in requests[0].init.body, false);
    assert.equal(response.file_name, "opencode-prism.json");
  } finally {
    globalThis.fetch = originalFetch;
  }
});
