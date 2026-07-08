import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadHookModuleWithClient(client) {
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      react: {
        useEffect: () => undefined,
        useRef: (value) => ({ current: value }),
        useState: (value) => [typeof value === "function" ? value() : value, () => undefined],
      },
      "@/lib/websocket": {
        getWebSocketClient: () => client,
      },
    },
  });
  return load(path.join(frontendDir, "src/hooks/useRealtimeData.ts"));
}

function loadUsagePageDataModule() {
  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      react: {
        useCallback: (callback) => callback,
        useEffect: () => undefined,
        useMemo: (factory) => factory(),
        useRef: (value) => ({ current: value }),
        useState: (value) => [typeof value === "function" ? value() : value, () => undefined],
      },
      "@/lib/api": { api: { stats: {} } },
      "@/i18n/useLocale": { useLocale: () => ({ messages: {} }) },
      "@/lib/referenceData": { getSharedModels: async () => [] },
      "./useUsageStatisticsRealtimeData": {
        useUsageStatisticsRealtimeData: () => ({ lastMessage: null, refresh: () => undefined }),
      },
    },
  });

  return load(path.join(frontendDir, "src/pages/statistics/useUsageStatisticsPageData.ts"));
}
function loadClientModule(sentMessages) {
  const sockets = [];
  class MockWebSocket {
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;

    constructor(url) {
      this.url = url;
      this.readyState = MockWebSocket.CONNECTING;
      this.onopen = null;
      this.onclose = null;
      sockets.push(this);
    }

    open() {
      this.readyState = MockWebSocket.OPEN;
      this.onopen?.();
    }

    send(data) {
      sentMessages.push(JSON.parse(data));
    }
    close() {
      this.readyState = MockWebSocket.CLOSED;
      this.onclose?.();
    }
  }

  globalThis.WebSocket = MockWebSocket;
  globalThis.window = { location: { protocol: "http:", host: "example.test" } };

  const { load } = createTsModuleLoader({
    rootDir: frontendDir,
    mocks: {
      "@/lib/websocket/transport": {
        calculateReconnectDelay: (reconnectInterval, reconnectAttempts) =>
          reconnectInterval * Math.min(reconnectAttempts, 5),
        createRealtimeWebSocketUrl: (_location, overrideUrl) => overrideUrl ?? "ws://example.test/ws",
        getInitialConnectionState: ({ hasConnectedOnce, reconnectAttempts }) =>
          hasConnectedOnce || reconnectAttempts > 0 ? "reconnecting" : "connecting",
      },
    },
  });
  return {
    sockets,
    ...load(path.join(frontendDir, "src/lib/websocket.ts")),
  };
}

test("analytics realtime scope ignores stale inbound profile ids", () => {
  const hookModule = loadHookModuleWithClient({});
  const baseMessage = {
    type: "analytics.snapshot",
    channel: "analytics",
    profile_id: 1,
    preset: "24h",
    sequence: 5,
    generated_at: "2026-05-04T00:00:00Z",
    snapshot: {},
    endpoint_model_statistics_by_endpoint_id: {},
  };

  assert.equal(hookModule.matchesRealtimeDataScope({
    channel: "analytics",
    message: baseMessage,
    profileId: 1,
    scope: { preset: "24h" },
  }), true);
  assert.equal(hookModule.matchesRealtimeDataScope({
    channel: "analytics",
    message: { ...baseMessage, profile_id: 2 },
    profileId: 1,
    scope: { preset: "24h" },
  }), true);
  assert.equal(hookModule.matchesRealtimeDataScope({
    channel: "analytics",
    message: { ...baseMessage, preset: "7d" },
    profileId: 1,
    scope: { preset: "24h" },
  }), false);
  assert.equal(hookModule.matchesRealtimeDataScope({
    channel: "analytics",
    message: { ...baseMessage, type: "analytics.error" },
    profileId: 1,
    scope: { preset: "24h" },
  }), false);
});

test("analytics snapshot ordering rejects older sequence and generation only within same scope", () => {
  const { isStaleAnalyticsSnapshot } = loadUsagePageDataModule();
  const current = {
    generatedAtMs: Date.parse("2026-05-04T00:00:10Z"),
    preset: "24h",
    profileId: 1,
    sequence: 8,
  };

  assert.equal(isStaleAnalyticsSnapshot(current, {
    ...current,
    generatedAtMs: Date.parse("2026-05-04T00:00:11Z"),
    sequence: 7,
  }), true);
  assert.equal(isStaleAnalyticsSnapshot(current, {
    ...current,
    generatedAtMs: Date.parse("2026-05-04T00:00:09Z"),
    sequence: 8,
  }), true);
  assert.equal(isStaleAnalyticsSnapshot(current, {
    ...current,
    generatedAtMs: Date.parse("2026-05-04T00:00:09Z"),
    sequence: 9,
  }), false);
  assert.equal(isStaleAnalyticsSnapshot(current, {
    ...current,
    generatedAtMs: Date.parse("2026-05-04T00:00:00Z"),
    preset: "7d",
    sequence: 1,
  }), false);
  assert.equal(isStaleAnalyticsSnapshot(current, {
    ...current,
    generatedAtMs: Date.parse("2026-05-04T00:00:00Z"),
    profileId: 2,
    sequence: 1,
  }), false);
});

test("analytics page normalizes realtime snapshot profile ids to the Default profile", () => {
  const { normalizeAnalyticsSnapshotProfileId } = loadUsagePageDataModule();

  assert.equal(normalizeAnalyticsSnapshotProfileId(1, 7), 1);
  assert.equal(normalizeAnalyticsSnapshotProfileId(null, 7), 1);
  assert.equal(normalizeAnalyticsSnapshotProfileId(42, 7), 1);
});

test("analytics manual refresh sends websocket refresh only for active subscribed scope", () => {
  const sentMessages = [];
  const { WebSocketClient, sockets } = loadClientModule(sentMessages);
  const client = new WebSocketClient({ url: "ws://example.test/realtime" });

  client.connect();
  sockets[0].open();
  client.subscribeChannel(1, "analytics", { preset: "24h" });
  sentMessages.length = 0;

  client.refreshChannel(1, "analytics", { preset: "7d" });
  client.refreshChannel(1, "dashboard");
  client.refreshChannel(1, "analytics", { preset: "24h" });

  assert.deepEqual(sentMessages, [
    { type: "refresh", profile_id: 1, channel: "analytics", preset: "24h" },
  ]);
  client.disconnect();
});
