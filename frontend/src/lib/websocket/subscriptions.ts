import type { RealtimeChannel } from "../websocket";

export function incrementChannelRefCount(
  refCounts: ReadonlyMap<RealtimeChannel, number>,
  channel: RealtimeChannel,
) {
  const nextRefCounts = new Map(refCounts);
  const currentCount = nextRefCounts.get(channel) ?? 0;
  nextRefCounts.set(channel, currentCount + 1);

  return {
    nextRefCounts,
    shouldSubscribe: currentCount === 0,
  };
}

export function decrementChannelRefCount(
  refCounts: ReadonlyMap<RealtimeChannel, number>,
  channel: RealtimeChannel,
) {
  const nextRefCounts = new Map(refCounts);
  const currentCount = nextRefCounts.get(channel) ?? 0;

  if (currentCount === 0) {
    return {
      nextRefCounts,
      shouldUnsubscribe: false,
      hasSubscriptions: nextRefCounts.size > 0,
    };
  }

  if (currentCount === 1) {
    nextRefCounts.delete(channel);
    return {
      nextRefCounts,
      shouldUnsubscribe: true,
      hasSubscriptions: nextRefCounts.size > 0,
    };
  }

  nextRefCounts.set(channel, currentCount - 1);
  return {
    nextRefCounts,
    shouldUnsubscribe: false,
    hasSubscriptions: true,
  };
}
