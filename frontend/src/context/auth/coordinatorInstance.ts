// Process-local auth session coordinator singleton (SPEC §12.1): constructed
// before the QueryClient, router and any protected client. The refresh probe
// lives here so the coordinator never depends on the API client module.

import { AuthSessionCoordinator } from "./sessionCoordinator";


export interface RawRefreshResult {
  status: number | null;
  body: unknown;
  networkError: boolean;
  timedOut: boolean;
  retryAfterSeconds?: number;
}

async function refreshProbe(): Promise<RawRefreshResult> {
  const rawApiBase = import.meta.env.VITE_API_BASE as string | undefined;
  const apiBase = typeof rawApiBase === "string" && rawApiBase.trim().length > 0 ? rawApiBase.trim().replace(/\/+$/, "") : "";
  try {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 15_000);
    let response: Response;
    try {
      response = await fetch(`${apiBase}/api/auth/refresh`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        signal: controller.signal,
      });
    } finally {
      window.clearTimeout(timeout);
    }
    let body: unknown = null;
    try {
      body = await response.json();
    } catch {
      body = null;
    }
    return {
      status: response.status,
      body,
      networkError: false,
      timedOut: false,
      retryAfterSeconds: parseRetryAfterSeconds(response.headers.get("Retry-After")),
    };
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") {
      return { status: null, body: null, networkError: false, timedOut: true };
    }
    return { status: null, body: null, networkError: true, timedOut: false };
  }
}

function parseRetryAfterSeconds(value: string | null): number | undefined {
  if (!value) {
    return undefined;
  }
  const trimmed = value.trim();
  if (/^\d+$/.test(trimmed)) {
    return Number(trimmed);
  }
  const parsed = new Date(trimmed);
  if (Number.isNaN(parsed.getTime())) {
    return undefined;
  }
  return Math.max(0, Math.round((parsed.getTime() - Date.now()) / 1000));
}

// The disabled-access probe (SPEC §5.4): a fixed, queryless GET /api/models
// that must pass through the same ordinary management auth middleware. A
// legal 2xx with a strict Models list payload (even an empty list) confirms
// open access; anything else exhausts the incident.
async function disabledAccessProbe(): Promise<"confirmed" | "unauthorized" | "unavailable"> {
  const rawApiBase = import.meta.env.VITE_API_BASE as string | undefined;
  const apiBase = typeof rawApiBase === "string" && rawApiBase.trim().length > 0 ? rawApiBase.trim().replace(/\/+$/, "") : "";
  try {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => controller.abort(), 10_000);
    let response: Response;
    try {
      response = await fetch(`${apiBase}/api/models`, {
        method: "GET",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        signal: controller.signal,
      });
    } finally {
      window.clearTimeout(timeout);
    }
    if (response.status === 401) {
      return "unauthorized";
    }
    if (!response.ok) {
      return "unavailable";
    }
    const body: unknown = await response.json();
    if (Array.isArray(body)) {
      return "confirmed";
    }
    return "unavailable";
  } catch {
    return "unavailable";
  }
}

// coordinatorInstance is the single process-local store. The React provider
// subscribes to it; the API client dispatches typed events into it.
export const authSessionCoordinator = new AuthSessionCoordinator({
  refresh: refreshProbe,
  disabledAccessProbe,
});
