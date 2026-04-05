import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  calculateReconnectDelay,
  createRealtimeWebSocketUrl,
  getInitialConnectionState,
} from "@/lib/websocket/transport";
import {
  buildPingMessage,
  buildPongMessage,
  buildSubscribeMessage,
  buildUnsubscribeAllMessage,
  buildUnsubscribeChannelMessage,
} from "@/lib/websocket/protocol";
import {
  decrementChannelRefCount,
  incrementChannelRefCount,
} from "@/lib/websocket/subscriptions";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readonly url: string;
  readyState = MockWebSocket.CONNECTING;
  onopen: ((event: Event) => void) | null = null;
  onmessage: ((event: MessageEvent) => void) | null = null;
  onerror: ((event: Event) => void) | null = null;
  onclose: ((event: CloseEvent) => void) | null = null;
  sent: Array<Record<string, unknown>> = [];

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
  }

  send(data: string) {
    this.sent.push(JSON.parse(data) as Record<string, unknown>);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.(new Event("close") as CloseEvent);
  }

  open() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.(new Event("open"));
  }

  serverClose() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.(new Event("close") as CloseEvent);
  }
}

const originalWebSocket = globalThis.WebSocket;

describe("websocket helpers", () => {
  beforeEach(() => {
    MockWebSocket.instances = [];
    vi.useFakeTimers();
    globalThis.WebSocket = MockWebSocket as unknown as typeof WebSocket;
  });

  afterEach(() => {
    globalThis.WebSocket = originalWebSocket;
    vi.useRealTimers();
  });

  it("caps reconnect delay growth after the fifth attempt", () => {
    expect(calculateReconnectDelay(3_000, 1)).toBe(3_000);
    expect(calculateReconnectDelay(3_000, 3)).toBe(9_000);
    expect(calculateReconnectDelay(3_000, 5)).toBe(15_000);
    expect(calculateReconnectDelay(3_000, 8)).toBe(15_000);
  });

  it("returns reconnecting only after a previous connection or reconnect attempt", () => {
    expect(getInitialConnectionState({ hasConnectedOnce: false, reconnectAttempts: 0 })).toBe(
      "connecting",
    );
    expect(getInitialConnectionState({ hasConnectedOnce: true, reconnectAttempts: 0 })).toBe(
      "reconnecting",
    );
    expect(getInitialConnectionState({ hasConnectedOnce: false, reconnectAttempts: 2 })).toBe(
      "reconnecting",
    );
  });

  it("derives the realtime websocket URL from the configured API base", () => {
    expect(
      createRealtimeWebSocketUrl(
        { protocol: "http:", host: "localhost:15173" },
        undefined,
        "http://localhost:18000",
      ),
    ).toBe("ws://localhost:18000/api/realtime/ws");

    expect(
      createRealtimeWebSocketUrl(
        { protocol: "https:", host: "prism.example.com" },
        undefined,
        "https://api.prism.example.com/",
      ),
    ).toBe("wss://api.prism.example.com/api/realtime/ws");
  });

  it("builds websocket protocol messages with the existing payload shapes", () => {
    expect(buildSubscribeMessage(7, "dashboard")).toEqual({
      type: "subscribe",
      profile_id: 7,
      channel: "dashboard",
    });
    expect(buildUnsubscribeChannelMessage("dashboard")).toEqual({
      type: "unsubscribe_channel",
      channel: "dashboard",
    });
    expect(buildUnsubscribeAllMessage()).toEqual({ type: "unsubscribe" });
    expect(buildPingMessage()).toEqual({ type: "ping" });
    expect(buildPongMessage()).toEqual({ type: "pong" });
  });

  it("tracks first subscribe and last unsubscribe for a channel", () => {
    const firstAdd = incrementChannelRefCount(new Map(), "dashboard");
    expect(firstAdd.shouldSubscribe).toBe(true);
    expect(firstAdd.nextRefCounts.get("dashboard")).toBe(1);

    const secondAdd = incrementChannelRefCount(firstAdd.nextRefCounts, "dashboard");
    expect(secondAdd.shouldSubscribe).toBe(false);
    expect(secondAdd.nextRefCounts.get("dashboard")).toBe(2);

    const firstRemove = decrementChannelRefCount(secondAdd.nextRefCounts, "dashboard");
    expect(firstRemove.shouldUnsubscribe).toBe(false);
    expect(firstRemove.nextRefCounts.get("dashboard")).toBe(1);
    expect(firstRemove.hasSubscriptions).toBe(true);

    const finalRemove = decrementChannelRefCount(firstRemove.nextRefCounts, "dashboard");
    expect(finalRemove.shouldUnsubscribe).toBe(true);
    expect(finalRemove.nextRefCounts.has("dashboard")).toBe(false);
    expect(finalRemove.hasSubscriptions).toBe(false);
  });

  it("reconnects after an unexpected close and resubscribes active channels", async () => {
    const { WebSocketClient } = await import("@/lib/websocket");

    const client = new WebSocketClient({
      url: "ws://example.test/api/realtime/ws",
      reconnectInterval: 100,
      maxReconnectAttempts: 2,
      heartbeatInterval: 1_000,
    });

    client.connect();

    expect(MockWebSocket.instances).toHaveLength(1);
    expect(client.getConnectionState()).toBe("connecting");

    const firstSocket = MockWebSocket.instances[0];
    firstSocket.open();
    client.subscribeChannel(7, "dashboard");

    expect(client.getConnectionState()).toBe("connected");
    expect(firstSocket.sent).toEqual([buildSubscribeMessage(7, "dashboard")]);

    firstSocket.serverClose();

    expect(client.getConnectionState()).toBe("reconnecting");

    vi.advanceTimersByTime(100);

    expect(MockWebSocket.instances).toHaveLength(2);

    const secondSocket = MockWebSocket.instances[1];
    secondSocket.open();

    expect(secondSocket.sent).toEqual([buildSubscribeMessage(7, "dashboard")]);

    client.disconnect();
  });
});
