import type { RealtimeChannel, RealtimeMessage } from "../websocket";

export function buildSubscribeMessage(profileId: number, channel: RealtimeChannel) {
  return { type: "subscribe" as const, profile_id: profileId, channel };
}

export function buildUnsubscribeChannelMessage(channel: RealtimeChannel) {
  return { type: "unsubscribe_channel" as const, channel };
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
