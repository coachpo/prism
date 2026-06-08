import type {
  DashboardRealtimeUpdatePayload,
  UsageModelStatistic,
  UsageSnapshotPreset,
  UsageSnapshotResponse,
} from "@/lib/types";
import {
  buildPingMessage,
  buildPongMessage,
  buildRefreshMessage,
  buildSubscribeMessage,
  buildUnsubscribeAllMessage,
  buildUnsubscribeChannelMessage,
  parseRealtimeMessage,
  shouldReplyWithPong,
} from "@/lib/websocket/protocol";
import {
  decrementChannelRefCount,
  hasChannelSubscription,
  incrementChannelRefCount,
  type RealtimeSubscriptionRefCounts,
} from "@/lib/websocket/subscriptions";
import {
  calculateReconnectDelay,
  createRealtimeWebSocketUrl,
  getInitialConnectionState,
} from "@/lib/websocket/transport";

export type RealtimeChannel = "dashboard" | "analytics";

export type RealtimeSubscriptionScope = { preset?: UsageSnapshotPreset };

export interface AnalyticsRealtimeSnapshotPayload {
  channel: "analytics";
  profile_id: number;
  preset: UsageSnapshotPreset;
  sequence: number;
  generated_at: string;
  snapshot: UsageSnapshotResponse;
  endpoint_model_statistics_by_endpoint_id: Record<string, UsageModelStatistic[]>;
}

export interface AnalyticsRealtimeErrorPayload {
  channel: "analytics";
  profile_id?: number;
  preset?: UsageSnapshotPreset;
  code: string;
  message: string;
}

export interface RealtimeChannelPayloadMap {
  dashboard: DashboardRealtimeUpdatePayload;
  analytics: AnalyticsRealtimeSnapshotPayload;
}

export type RealtimeMessage =
  | { type: "authenticated"; username: string }
  | { type: "unauthenticated"; message: string }
  | { type: "heartbeat" }
  | { type: "subscribed"; profile_id: number; channel: RealtimeChannel; preset?: UsageSnapshotPreset }
  | { type: "unsubscribed"; channel?: RealtimeChannel; preset?: UsageSnapshotPreset }
  | ({ type: "dashboard.update" } & DashboardRealtimeUpdatePayload)
  | ({ type: "analytics.snapshot" } & AnalyticsRealtimeSnapshotPayload)
  | ({ type: "analytics.error" } & AnalyticsRealtimeErrorPayload)
  | { type: "reconnected" }
  | { type: "error"; message: string }
  | { type: "pong" };

export type ConnectionState =
  | "connecting"
  | "connected"
  | "reconnecting"
  | "disconnected";

export type RealtimeEventHandler = (message: RealtimeMessage) => void;

export interface WebSocketClientOptions {
  url?: string;
  reconnectInterval?: number;
  maxReconnectAttempts?: number;
  heartbeatInterval?: number;
}

const IDLE_CLOSE_GRACE_MS = 15_000;

function shouldScheduleReconnect({
  isIntentionallyClosed,
  reconnectAttempts,
  maxReconnectAttempts,
}: {
  isIntentionallyClosed: boolean;
  reconnectAttempts: number;
  maxReconnectAttempts: number;
}) {
  return !isIntentionallyClosed && reconnectAttempts < maxReconnectAttempts;
}

function sendProfileSubscriptions({
  profileId,
  channelRefCounts,
  send,
}: {
  profileId: number | null;
  channelRefCounts: RealtimeSubscriptionRefCounts;
  send: (data: Record<string, unknown>) => void;
}) {
  if (profileId === null) {
    return;
  }

  for (const { channel, scope } of channelRefCounts.values()) {
    send(buildSubscribeMessage(profileId, channel, scope));
  }
}

function sendProfileSwitchMessages({
  previousProfileId,
  nextProfileId,
  channelRefCounts,
  send,
}: {
  previousProfileId: number | null;
  nextProfileId: number;
  channelRefCounts: RealtimeSubscriptionRefCounts;
  send: (data: Record<string, unknown>) => void;
}) {
  if (previousProfileId === null || previousProfileId === nextProfileId) {
    return;
  }

  for (const { channel, scope } of channelRefCounts.values()) {
    send(buildUnsubscribeChannelMessage(channel, scope));
  }

  sendProfileSubscriptions({
    profileId: nextProfileId,
    channelRefCounts,
    send,
  });
}

export class WebSocketClient {
  private ws: WebSocket | null = null;
  private readonly url: string;
  private readonly reconnectInterval: number;
  private readonly maxReconnectAttempts: number;
  private readonly heartbeatInterval: number;
  private reconnectAttempts = 0;
  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private idleCloseTimer: ReturnType<typeof setTimeout> | null = null;
  private readonly handlers: Set<RealtimeEventHandler> = new Set();
  private isIntentionallyClosed = false;
  private currentProfileId: number | null = null;
  private channelRefCounts: RealtimeSubscriptionRefCounts = new Map();
  private connectionState: ConnectionState = "disconnected";
  private hasConnectedOnce = false;
  private shouldEmitReconnect = false;

  constructor(options: WebSocketClientOptions = {}) {
    this.url = createRealtimeWebSocketUrl(window.location, options.url);
    this.reconnectInterval = options.reconnectInterval || 3000;
    this.maxReconnectAttempts = options.maxReconnectAttempts || 10;
    this.heartbeatInterval = options.heartbeatInterval || 30000;
  }

  connect(): void {
    if (
      this.ws?.readyState === WebSocket.OPEN ||
      this.ws?.readyState === WebSocket.CONNECTING
    ) {
      return;
    }

    this.isIntentionallyClosed = false;
    this.setConnectionState(
      getInitialConnectionState({
        hasConnectedOnce: this.hasConnectedOnce,
        reconnectAttempts: this.reconnectAttempts,
      })
    );

    try {
      const ws = new WebSocket(this.url);
      this.ws = ws;

      ws.onopen = () => {
        if (this.ws !== ws) {
          return;
        }

        const isReconnect = this.shouldEmitReconnect;
        this.shouldEmitReconnect = false;
        this.hasConnectedOnce = true;
        this.reconnectAttempts = 0;
        this.setConnectionState("connected");
        this.startHeartbeat();
        this.resubscribeAll();

        if (isReconnect) {
          this.emit({ type: "reconnected" });
        }
      };

      ws.onmessage = (event) => {
        if (this.ws !== ws) {
          return;
        }

        try {
          const message = parseRealtimeMessage(event.data);
          this.handleMessage(message);
        } catch (error) {
          console.error("[WebSocket] Failed to parse message:", error);
        }
      };

      ws.onerror = (error) => {
        if (this.ws !== ws) {
          return;
        }

        console.error("[WebSocket] Error:", error);
      };

      ws.onclose = () => {
        if (this.ws !== ws) {
          return;
        }

        this.stopHeartbeat();
        this.ws = null;

        if (
          shouldScheduleReconnect({
            isIntentionallyClosed: this.isIntentionallyClosed,
            reconnectAttempts: this.reconnectAttempts,
            maxReconnectAttempts: this.maxReconnectAttempts,
          })
        ) {
          this.shouldEmitReconnect = this.hasConnectedOnce;
          this.setConnectionState("reconnecting");
          this.scheduleReconnect();
          return;
        }

        this.setConnectionState("disconnected");
      };
    } catch (error) {
      console.error("[WebSocket] Connection failed:", error);
      if (
        shouldScheduleReconnect({
          isIntentionallyClosed: this.isIntentionallyClosed,
          reconnectAttempts: this.reconnectAttempts,
          maxReconnectAttempts: this.maxReconnectAttempts,
        })
      ) {
        this.shouldEmitReconnect = this.hasConnectedOnce;
        this.setConnectionState("reconnecting");
        this.scheduleReconnect();
      } else {
        this.setConnectionState("disconnected");
      }
    }
  }

  disconnect(): void {
    this.isIntentionallyClosed = true;
    this.currentProfileId = null;
    this.channelRefCounts = new Map();
    this.cancelIdleClose();

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }

    this.stopHeartbeat();
    this.setConnectionState("disconnected");

    if (this.ws) {
      this.ws.close();
      this.ws = null;
    }
  }

  subscribeChannel(
    profileId: number,
    channel: RealtimeChannel,
    scope?: RealtimeSubscriptionScope,
  ): void {
    this.cancelIdleClose();

    if (this.currentProfileId !== null && this.currentProfileId !== profileId) {
      this.setProfile(profileId);
    } else {
      this.currentProfileId = profileId;
    }

    const { nextRefCounts, shouldSubscribe } = incrementChannelRefCount(
      this.channelRefCounts,
      channel,
      scope,
    );
    this.channelRefCounts = nextRefCounts;

    if (shouldSubscribe && this.ws?.readyState === WebSocket.OPEN) {
      this.send(buildSubscribeMessage(profileId, channel, scope));
    }
  }

  unsubscribeChannel(
    channel: RealtimeChannel,
    scope?: RealtimeSubscriptionScope,
  ): void {
    const { hasSubscriptions, nextRefCounts, shouldUnsubscribe } = decrementChannelRefCount(
      this.channelRefCounts,
      channel,
      scope,
    );
    this.channelRefCounts = nextRefCounts;

    if (shouldUnsubscribe && this.ws?.readyState === WebSocket.OPEN) {
      this.send(buildUnsubscribeChannelMessage(channel, scope));
    }

    if (!hasSubscriptions) {
      this.currentProfileId = null;
      this.scheduleIdleClose();
    }
  }

  unsubscribe(): void {
    this.channelRefCounts = new Map();
    this.currentProfileId = null;

    if (this.ws?.readyState === WebSocket.OPEN) {
      this.send(buildUnsubscribeAllMessage());
    }

    this.scheduleIdleClose();
  }

  setProfile(profileId: number): void {
    this.cancelIdleClose();

    if (profileId === this.currentProfileId) {
      return;
    }

    const previousProfileId = this.currentProfileId;
    this.currentProfileId = profileId;

    if (this.ws?.readyState !== WebSocket.OPEN || previousProfileId === null) {
      return;
    }

    sendProfileSwitchMessages({
      previousProfileId,
      nextProfileId: profileId,
      channelRefCounts: this.channelRefCounts,
      send: this.send.bind(this),
    });
  }

  on(handler: RealtimeEventHandler): () => void {
    this.handlers.add(handler);
    return () => this.handlers.delete(handler);
  }

  isConnected(): boolean {
    return this.ws?.readyState === WebSocket.OPEN;
  }

  getConnectionState(): ConnectionState {
    return this.connectionState;
  }

  hasChannelSubscription(
    channel: RealtimeChannel,
    profileId: number | null,
    scope?: RealtimeSubscriptionScope,
  ): boolean {
    if (profileId === null || this.currentProfileId !== profileId) {
      return false;
    }

    return hasChannelSubscription(this.channelRefCounts, channel, scope);
  }

  refreshChannel(
    profileId: number | null,
    channel: RealtimeChannel,
    scope?: RealtimeSubscriptionScope,
  ): void {
    this.cancelIdleClose();

    if (
      channel !== "analytics" ||
      profileId === null ||
      this.currentProfileId !== profileId ||
      !hasChannelSubscription(this.channelRefCounts, channel, scope)
    ) {
      return;
    }

    this.send(buildRefreshMessage(profileId, channel, scope));
  }

  private send(data: Record<string, unknown>): void {
    if (this.ws?.readyState === WebSocket.OPEN) {
      this.ws.send(JSON.stringify(data));
    }
  }

  private handleMessage(message: RealtimeMessage): void {
    if (shouldReplyWithPong(message)) {
      this.send(buildPongMessage());
    }

    this.emit(message);
  }

  private emit(message: RealtimeMessage): void {
    this.handlers.forEach((handler) => {
      try {
        handler(message);
      } catch (error) {
        console.error("[WebSocket] Handler error:", error);
      }
    });
  }

  private resubscribeAll(): void {
    if (this.ws?.readyState !== WebSocket.OPEN) {
      return;
    }

    sendProfileSubscriptions({
      profileId: this.currentProfileId,
      channelRefCounts: this.channelRefCounts,
      send: this.send.bind(this),
    });
  }

  private scheduleIdleClose(): void {
    if (this.idleCloseTimer || this.channelRefCounts.size > 0) {
      return;
    }

    this.idleCloseTimer = setTimeout(() => {
      this.idleCloseTimer = null;
      this.closeIdleConnection();
    }, IDLE_CLOSE_GRACE_MS);
  }

  private cancelIdleClose(): void {
    if (!this.idleCloseTimer) {
      return;
    }

    clearTimeout(this.idleCloseTimer);
    this.idleCloseTimer = null;
  }

  private closeIdleConnection(): void {
    if (this.channelRefCounts.size > 0) {
      return;
    }

    this.isIntentionallyClosed = true;
    this.shouldEmitReconnect = false;

    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }

    if (this.ws) {
      this.ws.close();
      return;
    }

    this.stopHeartbeat();
    this.setConnectionState("disconnected");
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimer) {
      return;
    }

    this.reconnectAttempts += 1;
    const delay = calculateReconnectDelay(
      this.reconnectInterval,
      this.reconnectAttempts
    );

    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      this.connect();
    }, delay);
  }

  private startHeartbeat(): void {
    this.stopHeartbeat();
    this.heartbeatTimer = setInterval(() => {
      this.send(buildPingMessage());
    }, this.heartbeatInterval);
  }

  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  private setConnectionState(nextState: ConnectionState): void {
    this.connectionState = nextState;
  }
}

let globalClient: WebSocketClient | null = null;

export function getWebSocketClient(): WebSocketClient {
  if (!globalClient) {
    globalClient = new WebSocketClient();
  }
  return globalClient;
}
