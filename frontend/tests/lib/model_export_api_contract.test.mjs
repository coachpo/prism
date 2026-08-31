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

function loadPublicApi() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });
  return load(path.join(frontendDir, "src/lib/api.ts"));
}

function normalizeFetchInit(init) {
  if (!init) return {};
  const headers = init.headers ?? {};
  return {
    method: init.method,
    cache: init.cache,
    headers,
    body: typeof init.body === "string" ? JSON.parse(init.body) : init.body,
    signal: init.signal,
  };
}

function stubFetch(responseBody) {
  const originalFetch = globalThis.fetch;
  const requests = [];
  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init: normalizeFetchInit(init) });
    return {
      ok: true,
      status: 200,
      text: async () => JSON.stringify(responseBody),
    };
  };
  return {
    requests,
    restore: () => {
      globalThis.fetch = originalFetch;
    },
  };
}

test("model export client fetches source from the Pi static route", async () => {
  const { requests, restore } = stubFetch({
    target_version: "0.84.3",
    catalog: { status: "fresh", revision: "rev-1" },
    models: [],
    source_digest: "d".repeat(64),
  });
  try {
    const api = await loadApi();
    const response = await api.fetchModelExportSource();
    assert.equal(requests.length, 1);
    assert.ok(requests[0].url.endsWith("/api/models/exports/pi/source"));
    assert.equal(requests[0].init.cache, "no-store");
    assert.equal(response.source_digest.length, 64);
    assert.equal(response.target_version, "0.84.3");
  } finally {
    restore();
  }
});

test("public api exposes the complete modelExport namespace", async () => {
  const publicApi = await loadPublicApi();
  assert.equal(typeof publicApi.api.modelExport.fetchModelPi, "function");
  assert.equal(typeof publicApi.api.modelExport.searchModelPiCatalog, "function");
  assert.equal(typeof publicApi.api.modelExport.unbindModelPi, "function");
});

test("single-model Pi read uses the literal no-store management route", async () => {
  const { requests, restore } = stubFetch({
    model: {
      model_config_id: 7,
      model_id: "codex/gpt-x",
      api_family: "openai",
      pi_api: "openai-responses",
    },
    catalog: { status: "unavailable" },
    candidate_status: "catalog_unavailable",
    candidates: [],
    binding_status: "unbound",
    binding_renderable: false,
    binding: { bound: false, source: null, override: null, effective: null },
  });
  try {
    const api = await loadApi();
    const response = await api.fetchModelPi(7);
    assert.ok(requests[0].url.endsWith("/api/models/7/pi"));
    assert.equal(requests[0].init.method, undefined);
    assert.equal(requests[0].init.cache, "no-store");
    assert.equal(response.model.model_id, "codex/gpt-x");
    assert.equal(response.catalog.status, "unavailable");
  } finally {
    restore();
  }
});

test("model export render posts digest-guarded Pi bodies with binding-coordinate selections", async () => {
  const { requests, restore } = stubFetch({
    target_version: "0.84.3",
    content: "{}\n",
    content_sha256: "e".repeat(64),
    file_name: "prism-pi-models.json",
    mime_type: "application/json;charset=utf-8",
    model_results: [],
    source_digest: "e".repeat(64),
  });
  try {
    const api = await loadApi();
    const response = await api.renderModelExport({
      expected_source_digest: "a".repeat(64),
      model_config_ids: [5, 3],
      base_url: "https://prism.example",
      provider_id: "prism-home",
      credential: { include: true, api_key: "proxy-key" },
      selections: {
        3: {
          provider_id: "openai",
          model_id: "gpt-x",
          api: "openai-responses",
        },
        5: {
          provider_id: "anthropic",
          model_id: "claude-x",
          api: "anthropic-messages",
        },
      },
    });
    assert.equal(requests[0].init.method, "POST");
    assert.equal(requests[0].init.cache, "no-store");
    assert.ok(requests[0].url.endsWith("/api/models/exports/pi/render"));
    assert.equal(requests[0].init.body.expected_source_digest, "a".repeat(64));
    assert.deepEqual(requests[0].init.body.model_config_ids, [5, 3]);
    assert.equal(requests[0].init.body.base_url, "https://prism.example");
    assert.equal(requests[0].init.body.provider_id, "prism-home");
    assert.deepEqual(requests[0].init.body.credential, {
      include: true,
      api_key: "proxy-key",
    });
    assert.deepEqual(requests[0].init.body.selections[3], {
      provider_id: "openai",
      model_id: "gpt-x",
      api: "openai-responses",
    });
    assert.deepEqual(requests[0].init.body.selections[5], {
      provider_id: "anthropic",
      model_id: "claude-x",
      api: "anthropic-messages",
    });
    assert.equal(response.file_name, "prism-pi-models.json");
  } finally {
    restore();
  }
});

test("model export bind carries an explicit cross-directory coordinate", async () => {
  const { requests, restore } = stubFetch({
    bound: true,
    bind_source: "manual",
    provider_id: "openai",
    catalog_model_id: "gpt-x",
    api: "openai-responses",
    prism_model_id_at_bind: "codex/gpt-x",
    catalog_revision: "sha256-" + "b".repeat(64),
    source: null,
    override: null,
    effective: null,
  });
  try {
    const api = await loadApi();
    const response = await api.bindModelPi(7, {
      provider_id: "openai",
      catalog_model_id: "gpt-x",
      expected_catalog_revision: "sha256-" + "b".repeat(64),
      expected_prism_model_id: "codex/gpt-x",
      expected_pi_api: "openai-responses",
    });
    assert.equal(requests[0].init.method, "POST");
    assert.ok(requests[0].url.endsWith("/api/models/7/pi/bind"));
    assert.equal(requests[0].init.cache, "no-store");
    assert.deepEqual(requests[0].init.body, {
      provider_id: "openai",
      catalog_model_id: "gpt-x",
      expected_catalog_revision: "sha256-" + "b".repeat(64),
      expected_prism_model_id: "codex/gpt-x",
      expected_pi_api: "openai-responses",
    });
    assert.equal(response.catalog_model_id, "gpt-x");
    assert.equal(response.prism_model_id_at_bind, "codex/gpt-x");
  } finally {
    restore();
  }
});

test("directory search is a no-store backend POST that never selects", async () => {
  const { requests, restore } = stubFetch({
    query: "GPT-X",
    api: "openai-responses",
    limit: 20,
    offset: 20,
    total: 1,
    returned: 1,
    truncated: false,
    selected: false,
    catalog: { status: "fresh", revision: "sha256-" + "c".repeat(64) },
    fetched_at: "2026-08-30T00:00:00Z",
    checked_at: "2026-08-30T00:01:00Z",
    export_identity: {
      model_config_id: 7,
      model_id: "codex/gpt-x",
      api: "openai-responses",
      provider_id_source: "operator_input",
    },
    results: [
      {
        provider_id: "openai",
        model_id: "gpt-x",
        api: "openai-responses",
        dropped_fields: ["headers"],
      },
    ],
  });
  try {
    const api = await loadApi();
    const controller = new AbortController();
    const response = await api.searchModelPiCatalog(7, {
      model_id_query: "GPT-X",
      limit: 20,
      offset: 20,
    }, controller.signal);
    assert.equal(requests[0].init.method, "POST");
    assert.equal(requests[0].init.cache, "no-store");
    assert.ok(requests[0].url.endsWith("/api/models/7/pi/search"));
    assert.ok(!/pi\.dev/i.test(requests[0].url));
    assert.deepEqual(requests[0].init.body, {
      model_id_query: "GPT-X",
      limit: 20,
      offset: 20,
    });
    assert.ok(requests[0].init.signal instanceof AbortSignal);
    assert.equal(requests[0].init.body.signal, undefined);
    assert.equal(response.offset, 20);
    assert.equal(response.checked_at, "2026-08-30T00:01:00Z");
    assert.equal(response.selected, false);
    assert.equal(response.results.length, 1);
    assert.equal(response.export_identity.model_id, "codex/gpt-x");
    assert.equal(response.export_identity.provider_id_source, "operator_input");
  } finally {
    restore();
  }
});

test("model export refresh preview and commit hit their own routes", async () => {
  const preview = stubFetch({
    bound: true,
    provider_id: "openai",
    catalog_model_id: "gpt-x",
    api: "openai-responses",
    changed: true,
    changes: [
      {
        field: "context_window",
        current: "128000",
        next: "256000",
        kind: "changed",
      },
    ],
    catalog_revision: "sha256-" + "b".repeat(64),
    binding_updated_at: "2026-01-01T00:00:00Z",
    fetched_at: "2026-01-01T00:00:00Z",
  });
  try {
    const api = await loadApi();
    const previewResponse = await api.refreshModelPiPreview(7);
    assert.equal(preview.requests[0].init.method, "POST");
    assert.ok(
      preview.requests[0].url.endsWith("/api/models/7/pi/refresh/preview"),
    );
    assert.equal(previewResponse.changed, true);
  } finally {
    preview.restore();
  }

  const commit = stubFetch({
    bound: true,
    provider_id: "openai",
    catalog_model_id: "gpt-x",
    api: "openai-responses",
    catalog_revision: "sha256-" + "b".repeat(64),
    source: null,
    override: null,
    effective: null,
  });
  try {
    const api = await loadApi();
    await api.refreshModelPiCommit(7, {
      expected_provider_id: "openai",
      expected_catalog_model_id: "gpt-x",
      expected_api: "openai-responses",
      expected_binding_updated_at: "2026-01-01T00:00:00Z",
      expected_catalog_revision: "sha256-" + "b".repeat(64),
    });
    assert.equal(commit.requests[0].init.method, "POST");
    assert.ok(
      commit.requests[0].url.endsWith("/api/models/7/pi/refresh/commit"),
    );
    assert.equal(
      commit.requests[0].init.body.expected_catalog_revision,
      "sha256-" + "b".repeat(64),
    );
    assert.deepEqual(commit.requests[0].init.body, {
      expected_provider_id: "openai",
      expected_catalog_model_id: "gpt-x",
      expected_api: "openai-responses",
      expected_binding_updated_at: "2026-01-01T00:00:00Z",
      expected_catalog_revision: "sha256-" + "b".repeat(64),
    });
  } finally {
    commit.restore();
  }
});

test("model export override write and clear hit PUT/DELETE on the same route", async () => {
  const write = stubFetch({
    bound: true,
    provider_id: "openai",
    catalog_model_id: "gpt-x",
    api: "openai-responses",
    source: null,
    override: {
      name: "Renamed",
      reasoning: null,
      input: null,
      context_window: null,
      max_tokens: null,
      thinking_level_map: null,
      compat: null,
    },
    effective: null,
  });
  try {
    const api = await loadApi();
    await api.putModelPiOverride(7, { name: "Renamed" });
    assert.equal(write.requests[0].init.method, "PUT");
    assert.ok(write.requests[0].url.endsWith("/api/models/7/pi/override"));
    assert.equal(write.requests[0].init.body.name, "Renamed");
  } finally {
    write.restore();
  }

  const clear = stubFetch({
    bound: true,
    provider_id: "openai",
    catalog_model_id: "gpt-x",
    api: "openai-responses",
    source: null,
    override: null,
    effective: null,
  });
  try {
    const api = await loadApi();
    await api.clearModelPiOverride(7);
    assert.equal(clear.requests[0].init.method, "DELETE");
    assert.ok(clear.requests[0].url.endsWith("/api/models/7/pi/override"));
  } finally {
    clear.restore();
  }
});

test("model export unbind sends DELETE to the per-model pi route", async () => {
  const { requests, restore } = stubFetch({
    bound: false,
    source: null,
    override: null,
    effective: null,
  });
  try {
    const api = await loadApi();
    const response = await api.unbindModelPi(7);
    assert.equal(requests[0].init.method, "DELETE");
    assert.ok(requests[0].url.endsWith("/api/models/7/pi"));
    assert.equal(response.bound, false);
  } finally {
    restore();
  }
});
