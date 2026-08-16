// observe_return: a compact, validated return-state token that restores the
// source routing-health event context when navigating back from Requests.
// It is built only from the validated event fragment state (never from user
// input or external origins) and strictly validated on the Requests side.
import type { EventsQueryContextPreset } from "@/lib/api/observability";

export interface ObserveReturnPayload {
  v: 1;
  event_id: string;
  preset: EventsQueryContextPreset;
  from_time?: string;
  to_time?: string;
  event_type?: string;
  event_failure_kind?: string;
  event_admission_reason?: string;
  event_model_id?: string;
  event_endpoint_id?: string;
  event_terminal_target_id?: string;
  event_sort_order?: "desc" | "asc";
  event_cursor?: string;
}

const ALLOWED_KEYS = new Set([
  "v",
  "event_id",
  "preset",
  "from_time",
  "to_time",
  "event_type",
  "event_failure_kind",
  "event_admission_reason",
  "event_model_id",
  "event_endpoint_id",
  "event_terminal_target_id",
  "event_sort_order",
  "event_cursor",
]);

const MAX_OBSERVE_RETURN_BYTES = 2048;

function base64UrlEncode(value: string): string {
  return btoa(unescape(encodeURIComponent(value)))
    .replace(/\+/g, "-")
    .replace(/\//g, "_")
    .replace(/=+$/g, "");
}

function base64UrlDecode(value: string): string {
  const padded = value.replace(/-/g, "+").replace(/_/g, "/");
  return decodeURIComponent(escape(atob(padded)));
}

export function encodeObserveReturn(payload: ObserveReturnPayload): string {
  return base64UrlEncode(JSON.stringify(payload));
}

/** Strict validation: only allowlisted keys, bounded size, correct types. */
export function decodeObserveReturn(raw: string | undefined | null): ObserveReturnPayload | null {
  if (!raw || raw.length > MAX_OBSERVE_RETURN_BYTES) {
    return null;
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(base64UrlDecode(raw));
  } catch {
    return null;
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    return null;
  }
  const record = parsed as Record<string, unknown>;
  for (const key of Object.keys(record)) {
    if (!ALLOWED_KEYS.has(key)) {
      return null;
    }
  }
  if (record.v !== 1 || typeof record.event_id !== "string" || record.event_id === "") {
    return null;
  }
  if (typeof record.preset !== "string") {
    return null;
  }
  for (const optionalKey of [
    "from_time",
    "to_time",
    "event_type",
    "event_failure_kind",
    "event_admission_reason",
    "event_model_id",
    "event_endpoint_id",
    "event_terminal_target_id",
    "event_cursor",
  ]) {
    if (record[optionalKey] !== undefined && typeof record[optionalKey] !== "string") {
      return null;
    }
  }
  if (record.event_sort_order !== undefined && record.event_sort_order !== "desc" && record.event_sort_order !== "asc") {
    return null;
  }
  return record as unknown as ObserveReturnPayload;
}

/** Restores the routing-health search object from a validated return payload. */
export function observeReturnToSearch(payload: ObserveReturnPayload): Record<string, unknown> {
  const search: Record<string, unknown> = { tab: "events", preset: payload.preset };
  if (payload.from_time && payload.to_time) {
    search.preset = "custom";
    search.from_time = payload.from_time;
    search.to_time = payload.to_time;
  }
  const keys: Array<[keyof ObserveReturnPayload, string]> = [
    ["event_type", "event_type"],
    ["event_failure_kind", "event_failure_kind"],
    ["event_admission_reason", "event_admission_reason"],
    ["event_model_id", "event_model_id"],
    ["event_endpoint_id", "event_endpoint_id"],
    ["event_terminal_target_id", "event_terminal_target_id"],
    ["event_sort_order", "event_sort_order"],
    ["event_cursor", "event_cursor"],
    ["event_id", "event_id"],
  ];
  for (const [sourceKey, searchKey] of keys) {
    const value = payload[sourceKey];
    if (value !== undefined && value !== "") {
      search[searchKey] = value;
    }
  }
  return search;
}
