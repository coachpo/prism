export const AUTH_STATE_BROADCAST_KEY = "prism.authStateVersion";

export function broadcastAuthStateChange() {
  if (typeof window === "undefined") {
    return;
  }

  window.localStorage.setItem(AUTH_STATE_BROADCAST_KEY, String(Date.now()));
}
