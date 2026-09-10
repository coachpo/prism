import type { BatchMaintenanceAction, BatchMaintenanceRequest, BatchMaintenanceResponse } from "../types";
import { request } from "./request";

function send(action: BatchMaintenanceAction, phase: "preview" | "apply", data: BatchMaintenanceRequest, signal?: AbortSignal) {
  return request<BatchMaintenanceResponse>(`/api/models/batch/${action}/${phase}`, {
    method: "POST", cache: "no-store", body: JSON.stringify(data), signal,
  });
}
export const batchMaintenance = {
  preview: (action: BatchMaintenanceAction, data: BatchMaintenanceRequest, signal?: AbortSignal) => send(action, "preview", data, signal),
  apply: (action: BatchMaintenanceAction, data: BatchMaintenanceRequest, signal?: AbortSignal) => send(action, "apply", data, signal),
};
