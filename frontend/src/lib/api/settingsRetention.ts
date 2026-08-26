import type {
  CancelRetentionJobResponse,
  CreateManualRetentionJobRequest,
  GlobalRetentionJobDetail,
  GlobalRetentionJobList,
  GlobalRetentionJobSummary,
  ManualCleanupPreflightRequest,
  PolicyChangePreflightRequest,
  RetentionPreflightResponse,
  RetentionSettingsResponse,
  RetentionSettingsUpdate,
} from "../types";
import { request } from "./request";

export const settingsRetention = {
  get: () => request<RetentionSettingsResponse>("/api/settings/log-retention"),
  update: (data: RetentionSettingsUpdate) =>
    request<{
      settings: RetentionSettingsResponse;
      changes: Array<Record<string, unknown>>;
      scheduled_work: Array<Record<string, unknown>>;
      operation_id: string;
      replayed: boolean;
    }>("/api/settings/log-retention", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  preflight: (data: PolicyChangePreflightRequest | ManualCleanupPreflightRequest) =>
    request<RetentionPreflightResponse>("/api/maintenance/log-retention/preflights", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  createJob: (data: CreateManualRetentionJobRequest) =>
    request<{ operation_id: string; replayed: boolean; job: GlobalRetentionJobSummary }>("/api/maintenance/log-retention/jobs", {
      method: "POST",
      body: JSON.stringify(data),
    }),
  jobs: {
    list: (params?: { origin?: "manual" | "automatic"; state?: string[]; cursor?: string }) => {
      const query = new URLSearchParams({ scope: "global", type: "log_retention" });
      if (params?.origin) query.set("origin", params.origin);
      if (params?.state?.length) query.set("state", params.state.join(","));
      if (params?.cursor) query.set("cursor", params.cursor);
      return request<GlobalRetentionJobList>(`/api/management/jobs?${query.toString()}`);
    },
    get: (id: string) =>
      request<GlobalRetentionJobDetail>(`/api/management/jobs/${encodeURIComponent(id)}?scope=global&type=log_retention`),
    checkpoints: (id: string, params?: { limit?: number; cursor?: string }) => {
      const query = new URLSearchParams({ scope: "global", type: "log_retention" });
      if (params?.limit) query.set("limit", String(params.limit));
      if (params?.cursor) query.set("cursor", params.cursor);
      return request<GlobalRetentionJobDetail["checkpoints"]>(`/api/management/jobs/${encodeURIComponent(id)}/checkpoints?${query.toString()}`);
    },
    partitions: (id: string, params?: { limit?: number; cursor?: string }) => {
      const query = new URLSearchParams({ scope: "global", type: "log_retention" });
      if (params?.limit) query.set("limit", String(params.limit));
      if (params?.cursor) query.set("cursor", params.cursor);
      return request<GlobalRetentionJobDetail["partitions"]>(`/api/management/jobs/${encodeURIComponent(id)}/partitions?${query.toString()}`);
    },
    cancel: (id: string, operationId: string) =>
      request<CancelRetentionJobResponse>(
        `/api/management/jobs/${encodeURIComponent(id)}/cancel?scope=global&type=log_retention`,
        { method: "POST", body: JSON.stringify({ operation_id: operationId }) },
      ),
  },
};
