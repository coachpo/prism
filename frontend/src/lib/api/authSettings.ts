import type {
  AuthSettings,
  AuthSettingsUpdate,
  AuthStatus,
  EmailVerificationConfirmRequest,
  EmailVerificationRequest,
  EmailVerificationResponse,
  LoginRequest,
  PasswordResetConfirmRequest,
  PasswordResetRequest,
  ProxyApiKey,
  ProxyApiKeyCreate,
  ProxyApiKeyCreateResponse,
  ProxyApiKeyRotateResponse,
  ProxyApiKeyUpdate,
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
  requestPasswordReset: (data: PasswordResetRequest) =>
    request<{ success: boolean }>("/api/auth/password-reset/request", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  confirmPasswordReset: (data: PasswordResetConfirmRequest) =>
    request<{ success: boolean }>("/api/auth/password-reset/confirm", {
      method: "POST",
      body: JSON.stringify(data),
    }),
};

export const settings = {
  auth: {
    get: () => request<AuthSettings>("/api/settings/auth"),
    update: (data: AuthSettingsUpdate) =>
      request<AuthSettings>("/api/settings/auth", {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    requestEmailVerification: (data: EmailVerificationRequest) =>
      request<EmailVerificationResponse>("/api/settings/auth/email-verification/request", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    confirmEmailVerification: (data: EmailVerificationConfirmRequest) =>
      request<EmailVerificationResponse>("/api/settings/auth/email-verification/confirm", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    proxyKeys: {
      list: () => request<ProxyApiKey[]>("/api/settings/auth/proxy-keys"),
      create: (data: ProxyApiKeyCreate) =>
        request<ProxyApiKeyCreateResponse>("/api/settings/auth/proxy-keys", {
          method: "POST",
          body: JSON.stringify(data),
        }),
      update: (id: number, data: ProxyApiKeyUpdate) =>
        request<ProxyApiKey>(`/api/settings/auth/proxy-keys/${id}`, {
          method: "PATCH",
          body: JSON.stringify(data),
        }),
      rotate: (id: number) =>
        request<ProxyApiKeyRotateResponse>(`/api/settings/auth/proxy-keys/${id}/rotate`, {
          method: "POST",
        }),
      delete: (id: number) =>
        request<{ deleted: boolean }>(`/api/settings/auth/proxy-keys/${id}`, {
          method: "DELETE",
        }),
    },
  },
};
