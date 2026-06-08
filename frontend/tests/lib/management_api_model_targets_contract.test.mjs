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
    const { api, setApiProfileId } = apiModule;
    setApiProfileId(42);

    await api.models.targets.movePosition(10, 20, 3);

    assert.deepEqual(requests, [
      {
        url: "/api/models/10/targets/20/position",
        init: {
          method: "PATCH",
          body: JSON.stringify({ to_index: 3 }),
          credentials: "include",
          headers: { "Content-Type": "application/json", "X-Profile-Id": "42" },
        },
      },
    ]);
  } finally {
    apiModule?.setApiProfileId(null);
    globalThis.fetch = originalFetch;
  }
});
