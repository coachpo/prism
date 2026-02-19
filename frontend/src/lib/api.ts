import type {
  Provider,
  ModelConfig,
  ModelConfigListItem,
  ModelConfigCreate,
  ModelConfigUpdate,
  Endpoint,
  EndpointCreate,
  EndpointUpdate,
  HealthCheckResponse,
} from "./types";

const API_BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8000";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  if (!res.ok) {
    const body = await res.json().catch(() => ({ detail: res.statusText }));
    throw new Error(body.detail ?? `HTTP ${res.status}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

// --- Providers ---
export const api = {
  providers: {
    list: () => request<Provider[]>("/api/providers"),
    get: (id: number) => request<Provider>(`/api/providers/${id}`),
  },

  models: {
    list: () => request<ModelConfigListItem[]>("/api/models"),
    get: (id: number) => request<ModelConfig>(`/api/models/${id}`),
    create: (data: ModelConfigCreate) =>
      request<ModelConfig>("/api/models", {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: number, data: ModelConfigUpdate) =>
      request<ModelConfig>(`/api/models/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: number) =>
      request<void>(`/api/models/${id}`, { method: "DELETE" }),
  },

  endpoints: {
    list: (modelConfigId: number) =>
      request<Endpoint[]>(`/api/models/${modelConfigId}/endpoints`),
    create: (modelConfigId: number, data: EndpointCreate) =>
      request<Endpoint>(`/api/models/${modelConfigId}/endpoints`, {
        method: "POST",
        body: JSON.stringify(data),
      }),
    update: (id: number, data: EndpointUpdate) =>
      request<Endpoint>(`/api/endpoints/${id}`, {
        method: "PUT",
        body: JSON.stringify(data),
      }),
    delete: (id: number) =>
      request<void>(`/api/endpoints/${id}`, { method: "DELETE" }),
    healthCheck: (id: number) =>
      request<HealthCheckResponse>(`/api/endpoints/${id}/health-check`, {
        method: "POST",
      }),
  },
};
