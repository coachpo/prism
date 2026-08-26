import type {
  EventsQueryContextResponse,
  GlobalCurrentStateResponse,
  LoadbalanceAdmissionReason,
  LoadbalanceCurrentStateResetResponse,
  LoadbalanceCurrentStateValue,
  LoadbalanceEventDetail,
  LoadbalanceEventListResponse,
  LoadbalanceEventType,
  LoadbalanceFailureKind,
  LoadbalanceIncidentListResponse,
} from "../types";
import { buildQuery, request } from "./request";

export type EventsQueryContextPreset = "1h" | "6h" | "24h" | "7d" | "30d" | "all" | "custom";

export interface EventsQueryContextParams {
  requested_preset: EventsQueryContextPreset;
  custom_from_time?: string;
  custom_to_time?: string;
}

export interface ListEventsParams {
  query_context: string;
  model_id?: string;
  event_type?: LoadbalanceEventType[];
  failure_kind?: LoadbalanceFailureKind[];
  admission_reason?: LoadbalanceAdmissionReason[];
  endpoint_id?: number;
  terminal_target_id?: number;
  sort_order?: "desc" | "asc";
  limit?: number;
  cursor?: string;
}

export interface ListCurrentStateParams {
  model_id?: string;
  state?: Array<LoadbalanceCurrentStateValue | "unobserved">;
  endpoint_id?: number;
  terminal_target_id?: number;
  limit?: number;
  cursor?: string;
}

export const loadbalance = {
  issueEventsQueryContext: (params: EventsQueryContextParams) =>
    request<EventsQueryContextResponse>("/api/loadbalance/events/query-context", {
      method: "POST",
      body: JSON.stringify(params),
    }),
  listCurrentState: (params: ListCurrentStateParams = {}, signal?: AbortSignal) => {
    const query = buildQuery(params as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<GlobalCurrentStateResponse>(
      `/api/loadbalance/current-state${query ? `?${query}` : ""}`,
      { signal }
    );
  },
  resetCurrentState: (terminalTargetId: number) =>
    request<LoadbalanceCurrentStateResetResponse>(
      `/api/loadbalance/current-state/${terminalTargetId}/reset`,
      { method: "POST" }
    ),
  listEvents: (params: ListEventsParams) => {
    const query = buildQuery(params as unknown as Record<string, string | number | boolean | null | undefined> | undefined);
    return request<LoadbalanceEventListResponse>(`/api/loadbalance/events${query ? `?${query}` : ""}`);
  },
  listIncidents: (params?: { limit?: number; since_hours?: number }) => {
    const query = buildQuery(params);
    return request<LoadbalanceIncidentListResponse>(`/api/loadbalance/incidents${query ? `?${query}` : ""}`);
  },
  getEvent: (eventId: string, queryContext: string) =>
    request<LoadbalanceEventDetail>(`/api/loadbalance/events/${eventId}?query_context=${encodeURIComponent(queryContext)}`),
};
