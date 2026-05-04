import type { RealtimeChannel, RealtimeSubscriptionScope } from "../websocket";

export type RealtimeSubscriptionKey = "dashboard" | `analytics:${NonNullable<RealtimeSubscriptionScope["preset"]>}`;

export interface RealtimeSubscriptionRefCount {
  channel: RealtimeChannel;
  scope?: RealtimeSubscriptionScope;
  count: number;
}

export type RealtimeSubscriptionRefCounts = ReadonlyMap<
  RealtimeSubscriptionKey,
  RealtimeSubscriptionRefCount
>;

export function normalizeSubscriptionKey(
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
): RealtimeSubscriptionKey {
  if (channel === "dashboard") {
    return "dashboard";
  }

  if (!scope?.preset) {
    throw new Error("Analytics realtime subscriptions require a preset scope");
  }

  return `analytics:${scope.preset}`;
}

export function incrementChannelRefCount(
  refCounts: RealtimeSubscriptionRefCounts,
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
) {
  const key = normalizeSubscriptionKey(channel, scope);
  const nextRefCounts = new Map(refCounts);
  const currentEntry = nextRefCounts.get(key);
  const currentCount = currentEntry?.count ?? 0;

  nextRefCounts.set(key, {
    channel,
    scope,
    count: currentCount + 1,
  });

  return {
    key,
    nextRefCounts,
    shouldSubscribe: currentCount === 0,
  };
}

export function decrementChannelRefCount(
  refCounts: RealtimeSubscriptionRefCounts,
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
) {
  const key = normalizeSubscriptionKey(channel, scope);
  const nextRefCounts = new Map(refCounts);
  const currentEntry = nextRefCounts.get(key);
  const currentCount = currentEntry?.count ?? 0;

  if (currentCount === 0) {
    return {
      key,
      nextRefCounts,
      shouldUnsubscribe: false,
      hasSubscriptions: nextRefCounts.size > 0,
    };
  }

  if (currentCount === 1) {
    nextRefCounts.delete(key);
    return {
      key,
      nextRefCounts,
      shouldUnsubscribe: true,
      hasSubscriptions: nextRefCounts.size > 0,
    };
  }

  nextRefCounts.set(key, {
    channel,
    scope: currentEntry?.scope ?? scope,
    count: currentCount - 1,
  });
  return {
    key,
    nextRefCounts,
    shouldUnsubscribe: false,
    hasSubscriptions: true,
  };
}

export function hasChannelSubscription(
  refCounts: RealtimeSubscriptionRefCounts,
  channel: RealtimeChannel,
  scope?: RealtimeSubscriptionScope,
): boolean {
  const key = normalizeSubscriptionKey(channel, scope);
  return (refCounts.get(key)?.count ?? 0) > 0;
}
