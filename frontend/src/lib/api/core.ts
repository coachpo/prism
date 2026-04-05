import type { SessionResponse } from "../types";
import { getStaticMessages } from "@/i18n/staticMessages";

const rawApiBase = import.meta.env.VITE_API_BASE;
const API_BASE =
  typeof rawApiBase === "string" && rawApiBase.trim().length > 0
    ? rawApiBase.trim().replace(/\/+$/, "")
    : "";

let currentProfileId: number | null = null;

export function setApiProfileId(profileId: number | null) {
  currentProfileId = profileId;
}

export function getApiProfileId(): number | null {
  return currentProfileId;
}

export class ApiError extends Error {
  readonly status: number;
  readonly detail: unknown;

  constructor(message: string, status: number, detail: unknown) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.detail = detail;
  }
}

function extractErrorMessage(body: unknown): string {
  if (!body || typeof body !== "object") {
    return getStaticMessages().common.requestFailed;
  }
  const detail = (body as { detail?: unknown }).detail;
  if (typeof detail === "string" && detail.trim().length > 0) {
    return detail;
  }
  if (Array.isArray(detail) && detail.length > 0) {
    return detail.map((item) => JSON.stringify(item)).join(", ");
  }
  if (detail && typeof detail === "object") {
    const maybeMessage = (detail as { message?: unknown }).message;
    if (typeof maybeMessage === "string" && maybeMessage.trim().length > 0) {
      return maybeMessage;
    }
  }
  return getStaticMessages().common.requestFailed;
}

const AUTH_REFRESH_EXEMPT_PATHS = new Set([
  "/api/auth/status",
  "/api/auth/login",
  "/api/auth/logout",
  "/api/auth/refresh",
  "/api/auth/password-reset/request",
  "/api/auth/password-reset/confirm",
]);

function buildHeaders(path: string, init?: RequestInit): Record<string, string> {
  const headers: Record<string, string> = {
    "Content-Type": "application/json",
    ...(init?.headers as Record<string, string>),
  };

  if (path.startsWith("/api/") && currentProfileId !== null) {
    headers["X-Profile-Id"] = String(currentProfileId);
  }

  return headers;
}

let refreshPromise: Promise<boolean> | null = null;

async function refreshSession(): Promise<boolean> {
  if (refreshPromise) {
    return refreshPromise;
  }
  refreshPromise = (async () => {
    try {
      const res = await fetch(`${API_BASE}/api/auth/refresh`, {
        method: "POST",
        credentials: "include",
        headers: {
          "Content-Type": "application/json",
        },
      });

      if (!res.ok) {
        return false;
      }

      const body = (await res.json()) as SessionResponse;
      return body.authenticated;
    } catch {
      return false;
    } finally {
      refreshPromise = null;
    }
  })();
  return refreshPromise;
}

export async function request<T>(
  path: string,
  init?: RequestInit,
  options?: { allowAuthRefresh?: boolean }
): Promise<T> {
  const headers = buildHeaders(path, init);

  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    credentials: "include",
    headers,
  });

  const allowAuthRefresh = options?.allowAuthRefresh ?? true;
  if (
    res.status === 401 &&
    allowAuthRefresh &&
    path.startsWith("/api/") &&
    !AUTH_REFRESH_EXEMPT_PATHS.has(path) &&
    (await refreshSession())
  ) {
    return request<T>(path, init, { allowAuthRefresh: false });
  }

  if (!res.ok) {
    let body: unknown = null;
    try {
      body = await res.json();
    } catch {
      body = null;
    }
    throw new ApiError(extractErrorMessage(body), res.status, body);
  }

  if (res.status === 204 || res.status === 205) {
    return undefined as T;
  }

  const text = await res.text();
  if (text.length === 0) {
    return undefined as T;
  }

  return JSON.parse(text) as T;
}

export function buildQuery(
  params?: Record<string, string | number | boolean | null | undefined>
) {
  const qs = new URLSearchParams();
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== "") {
        qs.set(key, String(value));
      }
    });
  }
  return qs.toString();
}
