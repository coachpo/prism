import type {
  RealtimeChannel,
  RealtimeMessage,
  RealtimeSubscriptionScope,
} from "../websocket";

function buildScopedChannelMessage<TType extends string>(
  type: TType,
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
) {
  if (channel === "analytics") {
    return { type, channel, preset: scope?.preset } as const;
  }

  return { type, channel } as const;
}

export function buildSubscribeMessage(
  profileId: number,
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
) {
  if (channel === "analytics") {
    return {
      ...buildScopedChannelMessage("subscribe", channel, scope),
      profile_id: profileId,
    } as const;
  }

  return { type: "subscribe" as const, profile_id: profileId, channel };
}

export function buildUnsubscribeChannelMessage(
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
) {
  return buildScopedChannelMessage("unsubscribe_channel", channel, scope);
}

export function buildRefreshMessage(
  profileId: number,
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
) {
  if (channel === "analytics") {
    return {
      ...buildScopedChannelMessage("refresh", channel, scope),
      profile_id: profileId,
    } as const;
  }

  return { type: "refresh" as const, profile_id: profileId, channel };
}

export function buildUnsubscribeAllMessage() {
  return { type: "unsubscribe" as const };
}

export function buildPingMessage() {
  return { type: "ping" as const };
}

export function buildPongMessage() {
  return { type: "pong" as const };
}

export function parseRealtimeMessage(rawMessage: string): RealtimeMessage {
  return JSON.parse(rawMessage) as RealtimeMessage;
}

export function shouldReplyWithPong(message: RealtimeMessage): boolean {
  return message.type === "heartbeat";
}
