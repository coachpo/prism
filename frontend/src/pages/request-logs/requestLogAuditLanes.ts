import { ApiError } from "@/lib/api/request";
import type { AuditLogDetail, AuditLogListItem } from "@/lib/types";
import type { RequestLogDetail } from "@/lib/types/request-logs";
import type { RequestAuditCaptureMode } from "./requestLogAuditState";

export type RequestLanePhase =
  | "idle"
  | "loading"
  | "ready"
  | "missing"
  | "disabled"
  | "invalid_timestamp"
  | "error";

export type ListLanePhase = "idle" | "loading" | "ready" | "empty" | "error";

export type DetailLanePhase =
  | "idle"
  | "loading"
  | "ready"
  | "missing_selection"
  | "error";

export interface RequestLaneState {
  phase: RequestLanePhase;
  request: RequestLogDetail | null;
  captureMode: RequestAuditCaptureMode | null;
  error: string | null;
}

export interface ListLaneState {
  phase: ListLanePhase;
  items: AuditLogListItem[];
  nextCursor: string | null;
  hasMore: boolean;
  error: string | null;
}

export interface DetailLaneState {
  phase: DetailLanePhase;
  detail: AuditLogDetail | null;
  selectedAuditId: number | null;
  missingAuditLabel: string | null;
  error: string | null;
}

export interface RequestLogAuditWindow {
  from: string;
  to: string;
}

export const IDLE_REQUEST: RequestLaneState = {
  phase: "idle",
  request: null,
  captureMode: null,
  error: null,
};

export const IDLE_LIST: ListLaneState = {
  phase: "idle",
  items: [],
  nextCursor: null,
  hasMore: false,
  error: null,
};

export const IDLE_DETAIL: DetailLaneState = {
  phase: "idle",
  detail: null,
  selectedAuditId: null,
  missingAuditLabel: null,
  error: null,
};

export function getRequestLogAuditErrorMessage(
  error: unknown,
  fallback: string,
): string {
  return error instanceof Error ? error.message : fallback;
}

export function isRequestLogAuditMissing(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}

export type { AuditLogDetail, AuditLogListItem };
