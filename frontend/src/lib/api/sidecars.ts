import type {
  SidecarAuthModelsResponse,
  SidecarAuthMutationFieldsInput,
  SidecarAuthMutationResponse,
  SidecarAuthSnapshot,
  SidecarAuthSnapshotListResponse,
  SidecarInstance,
  SidecarListResponse,
  SidecarProviderSnapshotListResponse,
  SidecarSyncResponse,
  SidecarTestConnectionResponse,
} from "../types";
import { request } from "./core";

type SidecarInstanceWrite = {
  name: string;
  base_url: string;
  enabled?: boolean;
  environment_label?: string | null;
  sync_interval_seconds?: number;
  request_timeout_seconds?: number;
  allow_private_network?: boolean;
  allow_insecure_http?: boolean;
  skip_tls_verify?: boolean;
};

type SidecarInstanceCreateInput = SidecarInstanceWrite & {
  management_password: string;
};

type SidecarInstanceUpdateInput = Partial<SidecarInstanceWrite> & {
  management_password?: string | null;
};

type SidecarAuthMutationStatusInput = {
  disabled: boolean;
  force_live?: boolean;
};

type SidecarAuthDeleteInput = {
  confirm_name: string;
};

export const sidecars = {
  list: () => request<SidecarListResponse>("/api/sidecars"),
  get: (id: number) => request<SidecarInstance>(`/api/sidecars/${id}`),
  create: (data: SidecarInstanceCreateInput) =>
    request<SidecarInstance>("/api/sidecars", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  update: (id: number, data: SidecarInstanceUpdateInput) =>
    request<SidecarInstance>(`/api/sidecars/${id}`, {
      method: "PATCH",
      body: JSON.stringify(data),
    }),
  delete: (id: number) => request<void>(`/api/sidecars/${id}`, { method: "DELETE" }),
  testConnection: (id: number) =>
    request<SidecarTestConnectionResponse>(`/api/sidecars/${id}/test-connection`, {
      method: "POST",
    }),
  sync: (id: number) =>
    request<SidecarSyncResponse>(`/api/sidecars/${id}/sync`, {
      method: "POST",
    }),
  authSnapshots: (sidecarId: number) =>
    request<SidecarAuthSnapshotListResponse>(`/api/sidecars/${sidecarId}/auth-snapshots`),
  authSnapshot: (sidecarId: number, snapshotId: number | string) =>
    request<SidecarAuthSnapshot>(`/api/sidecars/${sidecarId}/auth-snapshots/${encodeURIComponent(String(snapshotId))}`),
  providerSnapshots: (sidecarId: number) =>
    request<SidecarProviderSnapshotListResponse>(`/api/sidecars/${sidecarId}/provider-snapshots`),
  authFileModels: (sidecarId: number, authFileName: string) => {
    const query = new URLSearchParams({ name: authFileName });
    return request<SidecarAuthModelsResponse>(`/api/sidecars/${sidecarId}/auth-files/models?${query.toString()}`);
  },
  deleteAuthFile: (sidecarId: number, authId: string, data: SidecarAuthDeleteInput) =>
    request<SidecarAuthMutationResponse>(
      `/api/sidecars/${sidecarId}/auth-files/${encodeURIComponent(authId)}`,
      {
        method: "DELETE",
        body: JSON.stringify(data),
      },
    ),
  updateAuthFileStatus: (sidecarId: number, authId: string, data: SidecarAuthMutationStatusInput) => {
    const { force_live: forceLive, ...payload } = data;
    const query = forceLive ? "?force_live=true" : "";
    return request<SidecarAuthMutationResponse>(
      `/api/sidecars/${sidecarId}/auth-files/${encodeURIComponent(authId)}/status${query}`,
      {
        method: "PATCH",
        body: JSON.stringify(payload),
      },
    );
  },
  updateAuthFileFields: (sidecarId: number, authId: string, data: SidecarAuthMutationFieldsInput) => {
    const { force_live: forceLive, ...payload } = data;
    const query = forceLive ? "?force_live=true" : "";
    return request<SidecarAuthMutationResponse>(
      `/api/sidecars/${sidecarId}/auth-files/${encodeURIComponent(authId)}/fields${query}`,
      {
        method: "PATCH",
        body: JSON.stringify(payload),
      },
    );
  },
};
