import type {
  AuthSettings,
  AuthSettingsUpdate,
  AuthStatus,
  LoginRequest,
  ProxyApiKeyCreateResponse,
  ProxyApiKeyDeleteResponse,
  ProxyApiKeyListResponse,
  ProxyApiKeyRotateResponse,
  ProxyApiKeyUpdate,
  ProxyApiKeyUpdateResponse,
  SessionResponse,
} from "../types";
import { request } from "./core";

export const auth = {
  status: () => request<AuthStatus>("/api/auth/status"),
  publicBootstrap: () => request<SessionResponse>("/api/auth/public-bootstrap"),
  login: (data: LoginRequest) =>
    request<SessionResponse>("/api/auth/login", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  logout: () => request<SessionResponse>("/api/auth/logout", { method: "POST" }),
  refresh: () => request<SessionResponse>("/api/auth/refresh", { method: "POST" }),
  session: () => request<SessionResponse>("/api/auth/session"),
};

export const settings = {
  auth: {
    get: () => request<AuthSettings>("/api/settings/auth"),
    update: (data: AuthSettingsUpdate) =>
      request<AuthSettings>("/api/settings/auth", {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    proxyKeys: {
      list: () => request<ProxyApiKeyListResponse>("/api/settings/auth/proxy-keys"),
      create: (data: { name: string; notes?: string | null; expires_at?: string | null }) =>
        // no-store: the create response carries the one-time raw key and must
        // never be cached by a reverse proxy, service worker or browser.
        request<ProxyApiKeyCreateResponse>("/api/settings/auth/proxy-keys", {
          method: "POST",
          body: JSON.stringify(data),
          cache: "no-store",
        }),
      update: (id: number, data: ProxyApiKeyUpdate) =>
        request<ProxyApiKeyUpdateResponse>(`/api/settings/auth/proxy-keys/${id}`, {
          method: "PATCH",
          body: JSON.stringify(data),
        }),
      rotate: (id: number) =>
        request<ProxyApiKeyRotateResponse>(`/api/settings/auth/proxy-keys/${id}/rotate`, {
          method: "POST",
          cache: "no-store",
        }),
      delete: (id: number) =>
        request<ProxyApiKeyDeleteResponse>(`/api/settings/auth/proxy-keys/${id}`, {
          method: "DELETE",
        }),
    },
  },
};
