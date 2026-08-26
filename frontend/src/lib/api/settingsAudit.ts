import type {
  AuditSettingsResponse,
  AuditSettingsUpdate,
  AuditStorageSummary,
} from "../types";
import { request } from "./request";

export const settingsAudit = {
  get: () => request<AuditSettingsResponse>("/api/settings/audit"),
  update: (data: AuditSettingsUpdate) =>
    request<{ operation_id: string; replayed: boolean; settings: AuditSettingsResponse }>("/api/settings/audit", {
      method: "PUT",
      body: JSON.stringify(data),
    }),
  storageSummary: () => request<AuditStorageSummary>("/api/settings/audit/storage-summary"),
};
