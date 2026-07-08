import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { createTsModuleLoader } from "../helpers/loadTsModule.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const frontendDir = path.resolve(__dirname, "../..");

function loadProtocolModule() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });
  return load(path.join(frontendDir, "src/lib/websocket/protocol.ts"));
}

function loadSubscriptionsModule() {
  const { load } = createTsModuleLoader({ rootDir: frontendDir });
  return load(path.join(frontendDir, "src/lib/websocket/subscriptions.ts"));
}

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

function installFakeTimeouts() {
  const nativeSetTimeout = globalThis.setTimeout;
  const nativeClearTimeout = globalThis.clearTimeout;
  let nextId = 1;
  const timers = new Map();

  globalThis.setTimeout = (callback, delay, ...args) => {
    const timerId = nextId;
    nextId += 1;
    timers.set(timerId, { callback: () => callback(...args), delay });
    return timerId;
  };

  globalThis.clearTimeout = (timerId) => {
    timers.delete(timerId);
  };

  return {
    pendingDelays: () => Array.from(timers.values(), (timer) => timer.delay),
    runNext: () => {
      const [timerId, timer] = timers.entries().next().value ?? [];
      assert.ok(timer, "expected a pending timeout");
      timers.delete(timerId);
      timer.callback();
    },
    restore: () => {
      globalThis.setTimeout = nativeSetTimeout;
      globalThis.clearTimeout = nativeClearTimeout;
    },
  };
}

test("websocket protocol builders include analytics preset scope only for analytics", () => {
  const protocol = loadProtocolModule();

  assert.deepEqual(protocol.buildSubscribeMessage(7, "dashboard"), {
    type: "subscribe",
    profile_id: 7,
    channel: "dashboard",
  });
  assert.deepEqual(protocol.buildUnsubscribeChannelMessage("dashboard"), {
    type: "unsubscribe_channel",
    channel: "dashboard",
  });
  assert.deepEqual(protocol.buildSubscribeMessage(7, "analytics", { preset: "24h" }), {
    type: "subscribe",
    profile_id: 7,
    channel: "analytics",
    preset: "24h",
  });
  assert.deepEqual(protocol.buildUnsubscribeChannelMessage("analytics", { preset: "7d" }), {
    type: "unsubscribe_channel",
    channel: "analytics",
    preset: "7d",
  });
  assert.deepEqual(protocol.buildRefreshMessage(7, "analytics", { preset: "30d" }), {
    type: "refresh",
    profile_id: 7,
    channel: "analytics",
    preset: "30d",
  });
});

test("subscription ref-counts are keyed by normalized dashboard and analytics scopes", () => {
  const subscriptions = loadSubscriptionsModule();
  let refCounts = new Map();

  let result = subscriptions.incrementChannelRefCount(refCounts, "dashboard");
  assert.equal(result.key, "dashboard");
  assert.equal(result.shouldSubscribe, true);
  refCounts = result.nextRefCounts;

  result = subscriptions.incrementChannelRefCount(refCounts, "dashboard");
  assert.equal(result.shouldSubscribe, false);
  refCounts = result.nextRefCounts;

  result = subscriptions.incrementChannelRefCount(refCounts, "analytics", { preset: "24h" });
  assert.equal(result.key, "analytics:24h");
  assert.equal(result.shouldSubscribe, true);
  refCounts = result.nextRefCounts;

  result = subscriptions.incrementChannelRefCount(refCounts, "analytics", { preset: "7d" });
  assert.equal(result.key, "analytics:7d");
  assert.equal(result.shouldSubscribe, true);
  refCounts = result.nextRefCounts;

  let decrement = subscriptions.decrementChannelRefCount(refCounts, "analytics", { preset: "24h" });
  assert.equal(decrement.shouldUnsubscribe, true);
  refCounts = decrement.nextRefCounts;
  assert.equal(subscriptions.hasChannelSubscription(refCounts, "analytics", { preset: "7d" }), true);
  assert.equal(subscriptions.hasChannelSubscription(refCounts, "analytics", { preset: "24h" }), false);

  decrement = subscriptions.decrementChannelRefCount(refCounts, "dashboard");
  assert.equal(decrement.shouldUnsubscribe, false);
  assert.equal(decrement.hasSubscriptions, true);
  refCounts = decrement.nextRefCounts;

  decrement = subscriptions.decrementChannelRefCount(refCounts, "dashboard");
  assert.equal(decrement.shouldUnsubscribe, true);
  assert.equal(decrement.hasSubscriptions, true);
});

test("subscription helpers reject unscoped analytics subscriptions", () => {
  const subscriptions = loadSubscriptionsModule();

  assert.throws(
    () => subscriptions.incrementChannelRefCount(new Map(), "analytics"),
    /preset scope/,
  );
  assert.throws(
    () => subscriptions.hasChannelSubscription(new Map(), "analytics"),
    /preset scope/,
  );
});

test("analytics extractor preserves snapshot shape and ignores non-analytics messages", () => {
  const hookModule = loadHookModuleWithClient({});
  const snapshot = { generated_at: "2026-05-04T00:00:00Z" };
  const payload = hookModule.CHANNEL_PAYLOAD_EXTRACTORS.analytics({
    type: "analytics.snapshot",
    channel: "analytics",
    profile_id: 7,
    preset: "24h",
    sequence: 12,
    generated_at: "2026-05-04T00:00:00Z",
    snapshot,
    endpoint_model_statistics_by_endpoint_id: { "1": [] },
  });

  assert.deepEqual(payload, {
    channel: "analytics",
    profile_id: 7,
    preset: "24h",
    sequence: 12,
    generated_at: "2026-05-04T00:00:00Z",
    snapshot,
    endpoint_model_statistics_by_endpoint_id: { "1": [] },
  });
  assert.equal(
    hookModule.CHANNEL_PAYLOAD_EXTRACTORS.analytics({ type: "dashboard.snapshot" }),
    null,
  );
});

test("dashboard extractor accepts split snapshot and single activity payloads only", () => {
  const hookModule = loadHookModuleWithClient({});
  const snapshot = {
    generated_at: "2026-05-04T00:00:00Z",
    snapshot_revision: "01HVVYV9XG0000000000000000",
    source_watermark: {
      latest_usage_event_created_at: null,
      latest_usage_event_id: null,
    },
  };
  const activity = {
    request_log_id: 101,
    created_at: "2026-05-04T00:00:01Z",
    model_id: "gpt-4o",
    model_label: "GPT-4o",
    resolved_target_model_id: null,
    resolved_target_model_label: null,
    endpoint_id: null,
    endpoint_label: "Unknown Endpoint",
    status_code: 200,
    response_time_ms: 123,
    ttft_ms: null,
    completion_duration_ms: null,
    is_stream: false,
    stream_outcome: "not_streaming",
    total_tokens: null,
    total_cost_user_currency_micros: null,
    priced_flag: null,
    unpriced_reason: null,
    report_currency_symbol: null,
  };

  assert.deepEqual(
    hookModule.CHANNEL_PAYLOAD_EXTRACTORS.dashboard({
      type: "dashboard.snapshot",
      profile_id: 7,
      snapshot,
    }),
    {
      type: "dashboard.snapshot",
      profile_id: 7,
      snapshot,
    },
  );
  assert.deepEqual(
    hookModule.CHANNEL_PAYLOAD_EXTRACTORS.dashboard({
      type: "dashboard.activity",
      profile_id: 7,
      activity_watermark: {
        latest_request_log_created_at: "2026-05-04T00:00:01Z",
        latest_request_log_id: 101,
      },
      activity,
    }),
    {
      type: "dashboard.activity",
      profile_id: 7,
      activity_watermark: {
        latest_request_log_created_at: "2026-05-04T00:00:01Z",
        latest_request_log_id: 101,
      },
      activity,
    },
  );
  assert.equal(Array.isArray(activity), false);
  assert.equal(
    hookModule.CHANNEL_PAYLOAD_EXTRACTORS.dashboard({ type: "dashboard.legacy" }),
    null,
  );
});

test("hook analytics scope filter requires matching profile and preset", () => {
  const hookModule = loadHookModuleWithClient({});
  const baseMessage = {
    type: "analytics.snapshot",
    channel: "analytics",
    profile_id: 7,
    preset: "24h",
    sequence: 12,
    generated_at: "2026-05-04T00:00:00Z",
    snapshot: {},
    endpoint_model_statistics_by_endpoint_id: {},
  };

  assert.equal(
    hookModule.matchesRealtimeDataScope({
      channel: "analytics",
      message: baseMessage,
      profileId: 7,
      scope: { preset: "24h" },
    }),
    true,
  );
  assert.equal(
    hookModule.matchesRealtimeDataScope({
      channel: "analytics",
      message: { ...baseMessage, profile_id: 8 },
      profileId: 7,
      scope: { preset: "24h" },
    }),
    false,
  );
  assert.equal(
    hookModule.matchesRealtimeDataScope({
      channel: "analytics",
      message: { ...baseMessage, preset: "7d" },
      profileId: 7,
      scope: { preset: "24h" },
    }),
    false,
  );
  assert.equal(
    hookModule.matchesRealtimeDataScope({
      channel: "dashboard",
      message: { type: "dashboard.snapshot", profile_id: 7, snapshot: {} },
      profileId: 7,
    }),
    true,
  );
  assert.equal(
    hookModule.matchesRealtimeDataScope({
      channel: "dashboard",
      message: { type: "dashboard.snapshot", profile_id: 8, snapshot: {} },
      profileId: 7,
    }),
    false,
  );
  assert.equal(
    hookModule.matchesRealtimeDataScope({
      channel: "dashboard",
      message: { type: "dashboard.legacy" },
      profileId: 7,
    }),
    false,
  );
});

test("websocket client sends one analytics subscribe per preset ref-count", () => {
  const sentMessages = [];
  const { WebSocketClient, sockets } = loadClientModule(sentMessages);
  const client = new WebSocketClient({ url: "ws://example.test/realtime" });

  client.connect();
  sockets[0].open();
  client.subscribeChannel(1, "analytics", { preset: "24h" });
  client.subscribeChannel(1, "analytics", { preset: "24h" });
  client.subscribeChannel(1, "analytics", { preset: "7d" });

  assert.deepEqual(sentMessages, [
    { type: "subscribe", profile_id: 1, channel: "analytics", preset: "24h" },
    { type: "subscribe", profile_id: 1, channel: "analytics", preset: "7d" },
  ]);
  assert.equal(client.hasChannelSubscription("analytics", 1, { preset: "24h" }), true);
  assert.equal(client.hasChannelSubscription("analytics", 1, { preset: "7d" }), true);

  client.unsubscribeChannel("analytics", { preset: "24h" });
  assert.equal(sentMessages.length, 2);
  assert.equal(client.hasChannelSubscription("analytics", 1, { preset: "24h" }), true);

  client.unsubscribeChannel("analytics", { preset: "24h" });
  assert.deepEqual(sentMessages.at(-1), {
    type: "unsubscribe_channel",
    channel: "analytics",
    preset: "24h",
  });
  assert.equal(client.hasChannelSubscription("analytics", 1, { preset: "24h" }), false);
  assert.equal(client.hasChannelSubscription("analytics", 1, { preset: "7d" }), true);
  client.disconnect();
});

test("websocket client refresh is analytics scoped and dashboard refresh is a no-op", () => {
  const sentMessages = [];
  const { WebSocketClient, sockets } = loadClientModule(sentMessages);
  const client = new WebSocketClient({ url: "ws://example.test/realtime" });

  client.connect();
  sockets[0].open();
  client.subscribeChannel(1, "dashboard");
  client.subscribeChannel(1, "analytics", { preset: "30d" });
  sentMessages.length = 0;

  client.refreshChannel(1, "dashboard");
  client.refreshChannel(1, "analytics", { preset: "30d" });
  client.refreshChannel(1, "analytics", { preset: "7d" });

  assert.deepEqual(sentMessages, [
    { type: "refresh", profile_id: 1, channel: "analytics", preset: "30d" },
  ]);
  client.disconnect();
});

test("websocket client freezes subscriptions to Default profile id 1", () => {
  const sentMessages = [];
  const { WebSocketClient, sockets } = loadClientModule(sentMessages);
  const client = new WebSocketClient({ url: "ws://example.test/realtime", reconnectInterval: 1 });

  client.connect();
  sockets[0].open();
  client.subscribeChannel(7, "dashboard");
  client.subscribeChannel(9, "analytics", { preset: "24h" });

  assert.deepEqual(sentMessages, [
    { type: "subscribe", profile_id: 1, channel: "dashboard" },
    { type: "subscribe", profile_id: 1, channel: "analytics", preset: "24h" },
  ]);
  assert.equal(client.hasChannelSubscription("dashboard", 7), true);
  assert.equal(client.hasChannelSubscription("dashboard", 9), true);

  sentMessages.length = 0;
  sockets[0].close();
  client.connect();
  sockets.at(-1).open();

  assert.deepEqual(sentMessages, [
    { type: "subscribe", profile_id: 1, channel: "dashboard" },
    { type: "subscribe", profile_id: 1, channel: "analytics", preset: "24h" },
  ]);
  client.disconnect();
});


test("websocket client closes idle socket after last subscription grace without reconnect", () => {
  const timers = installFakeTimeouts();

  try {
    const sentMessages = [];
    const { WebSocketClient, sockets } = loadClientModule(sentMessages);
    const client = new WebSocketClient({ url: "ws://example.test/realtime", reconnectInterval: 1 });

    client.connect();
    sockets[0].open();
    client.subscribeChannel(1, "dashboard");
    sentMessages.length = 0;

    client.unsubscribeChannel("dashboard");

    assert.deepEqual(sentMessages, [{ type: "unsubscribe_channel", channel: "dashboard" }]);
    assert.equal(sockets[0].readyState, WebSocket.OPEN);
    assert.equal(client.getConnectionState(), "connected");
    assert.deepEqual(timers.pendingDelays(), [15_000]);

    timers.runNext();
    assert.equal(sockets[0].readyState, WebSocket.CLOSED);
    assert.equal(client.getConnectionState(), "disconnected");
    assert.equal(sockets.length, 1);
    assert.deepEqual(timers.pendingDelays(), []);
  } finally {
    timers.restore();
  }
});

test("websocket client cancels pending idle close on new subscription", () => {
  const timers = installFakeTimeouts();

  try {
    const sentMessages = [];
    const { WebSocketClient, sockets } = loadClientModule(sentMessages);
    const client = new WebSocketClient({ url: "ws://example.test/realtime" });

    client.connect();
    sockets[0].open();
    client.subscribeChannel(1, "dashboard");
    sentMessages.length = 0;

    client.unsubscribeChannel("dashboard");
    assert.deepEqual(timers.pendingDelays(), [15_000]);

    client.connect();
    assert.deepEqual(timers.pendingDelays(), [15_000]);

    client.subscribeChannel(1, "dashboard");

    assert.deepEqual(timers.pendingDelays(), []);
    assert.equal(sockets[0].readyState, WebSocket.OPEN);
    assert.deepEqual(sentMessages, [
      { type: "unsubscribe_channel", channel: "dashboard" },
      { type: "subscribe", profile_id: 1, channel: "dashboard" },
    ]);
    client.disconnect();
  } finally {
    timers.restore();
  }
});

test("websocket client cancels pending idle close on refresh request", () => {
  const timers = installFakeTimeouts();

  try {
    const sentMessages = [];
    const { WebSocketClient, sockets } = loadClientModule(sentMessages);
    const refreshClient = new WebSocketClient({ url: "ws://example.test/realtime" });

    refreshClient.connect();
    sockets[0].open();
    refreshClient.subscribeChannel(1, "analytics", { preset: "24h" });
    refreshClient.unsubscribeChannel("analytics", { preset: "24h" });
    assert.deepEqual(timers.pendingDelays(), [15_000]);

    refreshClient.refreshChannel(1, "analytics", { preset: "24h" });

    assert.deepEqual(timers.pendingDelays(), []);
    assert.equal(sockets[0].readyState, WebSocket.OPEN);
    refreshClient.disconnect();
    assert.ok(sentMessages.some((message) => message.type === "unsubscribe_channel"));
  } finally {
    timers.restore();
  }
});
