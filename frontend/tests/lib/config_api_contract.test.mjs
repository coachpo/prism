import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function buildProfileBundle() {
  return {
    version: 3,
    bundle_kind: "profile_config",
    endpoints: [],
    pricing_templates: [],
    connections: [],
    loadbalance_strategies: [],
    models: [],
    secret_payload: {
      kind: "encrypted",
      cipher: "fernet-v1",
      key_id: "test-key",
      entries: [],
    },
  };
}

function loadConfigApi() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });

  return load(path.join(frontendDir, "src/lib/api.ts"));
}

function installFetchRecorder(requests) {
  const originalFetch = globalThis.fetch;

  globalThis.fetch = async (url, init) => {
    requests.push({ url: String(url), init });
    return {
      ok: true,
      status: 200,
      text: async () => "{}",
    };
  };

  return () => {
    globalThis.fetch = originalFetch;
  };
}

function assertHasProfileHeader(call, profileId) {
  assert.equal(
    call.init?.headers?.["X-Profile-Id"],
    String(profileId),
    `${call.url} should attach X-Profile-Id through api/core.ts`,
  );
}

test("api.config profile helpers attach X-Profile-Id and required confirmation headers", async () => {
  const requests = [];
  const restoreFetch = installFetchRecorder(requests);
  const { api, setApiProfileId } = loadConfigApi();
  const profileBundle = buildProfileBundle();

  try {
    setApiProfileId(42);

    await api.config.export();
    await api.config.exportWithSecrets();
    await api.config.previewImport(profileBundle);
    await api.config.import(profileBundle, "profile-preview-token");
  } finally {
    setApiProfileId(null);
    restoreFetch();
  }

  assert.deepEqual(requests, [
    {
      url: "/api/config/profile/export",
      init: {
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "42" },
      },
    },
    {
      url: "/api/config/profile/export/with-secrets",
      init: {
        method: "POST",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
          "X-Prism-Dangerous-Confirm": "profile-export",
          "X-Profile-Id": "42",
        },
      },
    },
    {
      url: "/api/config/profile/import/preview",
      init: {
        method: "POST",
        body: JSON.stringify(profileBundle),
        credentials: "include",
        headers: { "Content-Type": "application/json", "X-Profile-Id": "42" },
      },
    },
    {
      url: "/api/config/profile/import",
      init: {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "X-Prism-Preview-Token": "profile-preview-token",
          "X-Profile-Id": "42",
        },
        body: JSON.stringify(profileBundle),
        credentials: "include",
      },
    },
  ]);
  requests.forEach((request) => assertHasProfileHeader(request, 42));
});
