import { useEffect, useRef, useState } from "react";
import {
  getWebSocketClient,
  type ConnectionState,
  type RealtimeChannel,
  type RealtimeChannelPayloadMap,
  type RealtimeMessage,
  type RealtimeSubscriptionScope,
} from "@/lib/websocket";

type BufferedEvent<TData> = { type: "data"; payload: TData };

export const CHANNEL_PAYLOAD_EXTRACTORS: {
  [K in RealtimeChannel]: (
    message: RealtimeMessage
  ) => RealtimeChannelPayloadMap[K] | null;
} = {
  dashboard: (message) => {
    if (message.type === "dashboard.snapshot") {
      return {
        type: message.type,
        profile_id: message.profile_id,
        snapshot: message.snapshot,
      };
    }

    if (message.type === "dashboard.activity") {
      return {
        type: message.type,
        profile_id: message.profile_id,
        activity_watermark: message.activity_watermark,
        activity: message.activity,
      };
    }

    return null;
  },
  analytics: (message) =>
    message.type === "analytics.snapshot"
      ? {
          channel: message.channel,
          profile_id: message.profile_id,
          preset: message.preset,
          sequence: message.sequence,
          generated_at: message.generated_at,
          snapshot: message.snapshot,
          endpoint_model_statistics_by_endpoint_id:
            message.endpoint_model_statistics_by_endpoint_id,
        }
      : null,
};

export interface UseRealtimeDataOptions<
  TChannel extends RealtimeChannel = "dashboard",
> {
  profileId: number | null;
  channel?: TChannel;
  enabled?: boolean;
  scope?: RealtimeSubscriptionScope;
  onData?: (payload: RealtimeChannelPayloadMap[TChannel]) => void;
  onReconnect?: () => void;
}

export interface UseRealtimeDataReturn<TData> {
  isConnected: boolean;
  isSubscribed: boolean;
  isSyncing: boolean;
  connectionState: ConnectionState;
  lastMessage: RealtimeMessage | null;
  lastData: TData | null;
  markSyncComplete: () => void;
  refresh: () => void;
}

export function matchesRealtimeDataScope({
  channel,
  message,
  profileId,
  scope,
}: {
  channel: RealtimeChannel;
  message: RealtimeMessage;
  profileId: number | null;
  scope?: RealtimeSubscriptionScope;
}): boolean {
  if (channel === "dashboard") {
    return (
      (message.type === "dashboard.snapshot" ||
        message.type === "dashboard.activity") &&
      message.profile_id === profileId
    );
  }

  return (
    message.type === "analytics.snapshot" &&
    message.profile_id === profileId &&
    message.preset === scope?.preset
  );
}

export function useRealtimeData<TChannel extends RealtimeChannel = "dashboard">(
  options: UseRealtimeDataOptions<TChannel>
): UseRealtimeDataReturn<RealtimeChannelPayloadMap[TChannel]> {
  const {
    profileId,
    channel = "dashboard" as TChannel,
    enabled = true,
    scope,
    onData,
    onReconnect,
  } = options;
  const client = getWebSocketClient();
  const onDataRef = useRef(onData);
  const onReconnectRef = useRef(onReconnect);
  const isSyncingRef = useRef(false);
  const pendingEventsRef = useRef<
    BufferedEvent<RealtimeChannelPayloadMap[TChannel]>[]
  >([]);

  const [isConnected, setIsConnected] = useState(client.isConnected());
  const [isSubscribed, setIsSubscribed] = useState(
    client.hasChannelSubscription(channel, profileId, scope)
  );
  const [isSyncing, setIsSyncing] = useState(false);
  const [connectionState, setConnectionState] = useState(client.getConnectionState());
  const [lastMessage, setLastMessage] = useState<RealtimeMessage | null>(null);
  const [lastData, setLastData] = useState<
    RealtimeChannelPayloadMap[TChannel] | null
  >(null);

  useEffect(() => {
    onDataRef.current = onData;
  }, [onData]);

  useEffect(() => {
    onReconnectRef.current = onReconnect;
  }, [onReconnect]);

  function markSyncComplete() {
    isSyncingRef.current = false;
    setIsSyncing(false);

    const pendingEvents = pendingEventsRef.current;
    pendingEventsRef.current = [];

    for (const pendingEvent of pendingEvents) {
      onDataRef.current?.(pendingEvent.payload);
    }
  }

  function refresh() {
    client.refreshChannel(profileId, channel, scope);
  }

  useEffect(() => {
    if (!enabled) {
      return;
    }

    const handleMessage = (message: RealtimeMessage) => {
      setLastMessage(message);
      setIsConnected(client.isConnected());
      setConnectionState(client.getConnectionState());

      if (
        message.type === "subscribed" &&
        message.channel === channel &&
        message.profile_id === profileId &&
        (channel !== "analytics" || message.preset === scope?.preset)
      ) {
        setIsSubscribed(true);
        return;
      }

      if (
        message.type === "unsubscribed" &&
        (message.channel === undefined ||
          (message.channel === channel &&
            (channel !== "analytics" || message.preset === scope?.preset)))
      ) {
        setIsSubscribed(false);
        return;
      }

      if (message.type === "reconnected") {
        if (onReconnectRef.current) {
          isSyncingRef.current = true;
          setIsSyncing(true);
          onReconnectRef.current();
        }
        return;
      }

      const payload = CHANNEL_PAYLOAD_EXTRACTORS[channel](message);
      if (
        payload !== null &&
        matchesRealtimeDataScope({ channel, message, profileId, scope })
      ) {
        setLastData(payload);

        if (isSyncingRef.current) {
          pendingEventsRef.current.push({ type: "data", payload });
          return;
        }

        onDataRef.current?.(payload);
      }
    };

    const unsubscribeHandler = client.on(handleMessage);
    client.connect();

    const statusTimer = setInterval(() => {
      setIsConnected(client.isConnected());
      setConnectionState(client.getConnectionState());
      setIsSubscribed(client.hasChannelSubscription(channel, profileId, scope));
    }, 500);

    if (profileId !== null) {
      client.subscribeChannel(profileId, channel, scope);
    }

    return () => {
      clearInterval(statusTimer);
      unsubscribeHandler();
      isSyncingRef.current = false;
      pendingEventsRef.current = [];
      setIsSyncing(false);

      if (profileId !== null) {
        client.unsubscribeChannel(channel, scope);
      }
    };
  }, [channel, client, enabled, profileId, scope]);

  return {
    isConnected,
    isSubscribed: enabled ? isSubscribed : false,
    isSyncing,
    connectionState,
    lastMessage,
    lastData,
    markSyncComplete,
    refresh,
  };
}
