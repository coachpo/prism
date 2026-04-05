export type WebSocketLocationLike = Pick<Location, "protocol" | "host">;

export type WebSocketInitialConnectionState = "connecting" | "reconnecting";

export function createRealtimeWebSocketUrl(
  location: WebSocketLocationLike,
  overrideUrl?: string,
  apiBase?: string,
): string {
  if (overrideUrl) {
    return overrideUrl;
  }

  const rawApiBase = apiBase ?? import.meta.env.VITE_API_BASE;

  if (typeof rawApiBase === "string" && rawApiBase.trim().length > 0) {
    try {
      const apiUrl = new URL(rawApiBase.trim(), `${location.protocol}//${location.host}`);
      const protocol = apiUrl.protocol === "https:" ? "wss:" : "ws:";
      return `${protocol}//${apiUrl.host}/api/realtime/ws`;
    } catch {}
  }

  const protocol = location.protocol === "https:" ? "wss:" : "ws:";
  return `${protocol}//${location.host}/api/realtime/ws`;
}

export function getInitialConnectionState({
  hasConnectedOnce,
  reconnectAttempts,
}: {
  hasConnectedOnce: boolean;
  reconnectAttempts: number;
}): WebSocketInitialConnectionState {
  return hasConnectedOnce || reconnectAttempts > 0 ? "reconnecting" : "connecting";
}

export function calculateReconnectDelay(
  reconnectInterval: number,
  reconnectAttempts: number,
): number {
  return reconnectInterval * Math.min(reconnectAttempts, 5);
}
