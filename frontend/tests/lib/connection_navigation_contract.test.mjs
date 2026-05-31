import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");
const apiCalls = [];
const toasts = [];
const navigations = [];
let referenceResponse = {
  connection_id: 77,
  items: [
    { target_id: 12, model_config_id: 42, model_id: "demo", api_family: "openai", position: 0, is_enabled: true },
  ],
};
const apiMock = {
  api: {
    connections: {
      references: async (connectionId) => {
        apiCalls.push(["references", connectionId]);
        return referenceResponse;
      },
    },
  },
};

const toastMock = {
  toast: {
    error: (message) => toasts.push(message),
  },
};
const staticMessagesMock = {
  getStaticMessages: () => ({
    requestLogsDetail: {
      connectionNotFound: "Connection not found",
    },
  }),
};

const { load } = createTsModuleLoader({
  rootDir: frontendDir,
  mocks: {
    "@/lib/api": apiMock,
    "sonner": toastMock,
    "@/i18n/staticMessages": staticMessagesMock,
  },
});
const { createConnectionNavigator } = load(
  path.join(frontendDir, "src/pages/request-logs/connectionNavigation.ts"),
);

function resetHarness() {
  apiCalls.length = 0;
  toasts.length = 0;
  navigations.length = 0;
  referenceResponse = {
    connection_id: 77,
    items: [
      { target_id: 12, model_config_id: 42, model_id: "demo", api_family: "openai", position: 0, is_enabled: true },
    ],
  };
}
test("request-log connection navigation uses references instead of retired owner route", async () => {
  resetHarness();
  const navigate = (to) => navigations.push(to);
  const navigateToConnection = createConnectionNavigator({ navigate });

  await navigateToConnection(77);

  assert.deepEqual(apiCalls, [["references", 77]]);
  assert.deepEqual(navigations, ["/models/42?focus_connection_id=77"]);
  assert.deepEqual(toasts, []);
});

test("request-log connection navigation shows not-found when references have no owner", async () => {
  resetHarness();
  referenceResponse = { connection_id: 77, items: [] };
  const navigate = (to) => navigations.push(to);
  const navigateToConnection = createConnectionNavigator({ navigate });

  await navigateToConnection(77);

  assert.deepEqual(apiCalls, [["references", 77]]);
  assert.deepEqual(navigations, []);
  assert.deepEqual(toasts, ["Connection not found"]);
});
