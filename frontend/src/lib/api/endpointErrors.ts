import type { ApiError } from "./request";
import type { Endpoint, EndpointReferenceItem, EndpointReferencePage, EndpointReferenceSummary } from "../types";

// Typed Endpoint error guards. These live at the typed API/domain boundary and
// never specialize ApiError globally. ApiError.status and the raw body are
// preserved; known codes map to localized UI, unknown errors use fallback text.

export type EndpointInUseError = ApiError & {
  detail: {
    code: "endpoint_in_use";
    message: string;
    endpoint_id: number;
    summary: EndpointReferenceSummary;
    reference_page: EndpointReferencePage;
    references_url: string;
  };
};

export type ConnectionNotOrphanedError = ApiError & {
  detail: {
    code: "connection_not_orphaned";
    message: string;
    item: EndpointReferenceItem;
  };
};

export type EndpointConfigChangedError = ApiError & {
  detail: {
    code: "endpoint_config_changed";
    message: string;
    endpoint: Endpoint;
  };
};

export type EndpointStaleError = ApiError & {
  detail: {
    code: "endpoint_stale";
    message: string;
    endpoint: Endpoint;
  };
};

export type ReferenceIntegrityError = ApiError & {
  detail: {
    code: "reference_integrity_error";
    message: string;
    endpoint_ids?: number[];
    affected_connection_ids?: number[];
  };
};

export type ReferenceSnapshotStaleError = ApiError & {
  detail: {
    code: "reference_snapshot_stale";
    message: string;
  };
};

export type ReferenceCursorError = ApiError & {
  detail: {
    code: "reference_cursor_invalid" | "reference_cursor_mismatch";
    message: string;
  };
};

export type EndpointFieldError = ApiError & {
  detail: {
    code: "validation_failed";
    fields?: Record<string, string>;
  };
};

function isApiError(error: unknown): error is ApiError {
  return (
    typeof error === "object" &&
    error !== null &&
    "status" in error &&
    "detail" in error &&
    typeof (error as ApiError).status === "number"
  );
}

function detailCode(error: ApiError): string | null {
  // ApiError.detail is the raw response body: {"detail": {code, ...}}.
  if (!error.detail || typeof error.detail !== "object") {
    return null;
  }
  const body = error.detail as { detail?: unknown };
  const detail = body.detail ?? body;
  if (!detail || typeof detail !== "object") {
    return null;
  }
  const code = (detail as { code?: unknown }).code;
  return typeof code === "string" ? code : null;
}

export function innerDetail<T>(error: ApiError): T | null {
  if (!error.detail || typeof error.detail !== "object") return null;
  const body = error.detail as { detail?: unknown };
  const detail = body.detail ?? body;
  if (!detail || typeof detail !== "object") return null;
  return detail as T;
}

export function isEndpointInUseError(error: unknown): error is EndpointInUseError {
  return isApiError(error) && detailCode(error) === "endpoint_in_use";
}

export function isConnectionNotOrphanedError(error: unknown): error is ConnectionNotOrphanedError {
  return isApiError(error) && detailCode(error) === "connection_not_orphaned";
}

export function isEndpointConfigChangedError(error: unknown): error is EndpointConfigChangedError {
  return isApiError(error) && detailCode(error) === "endpoint_config_changed";
}

export function isEndpointStaleError(error: unknown): error is EndpointStaleError {
  return isApiError(error) && error.status === 409 && detailCode(error) === "endpoint_stale";
}

export function isReferenceIntegrityError(error: unknown): error is ReferenceIntegrityError {
  return isApiError(error) && detailCode(error) === "reference_integrity_error";
}

export function isReferenceSnapshotStaleError(error: unknown): error is ReferenceSnapshotStaleError {
  return isApiError(error) && detailCode(error) === "reference_snapshot_stale";
}

export function isReferenceCursorError(error: unknown): error is ReferenceCursorError {
  return isApiError(error) && (detailCode(error) === "reference_cursor_invalid" || detailCode(error) === "reference_cursor_mismatch");
}

// extractEndpointFieldErrors returns the stable field code map from a typed 422.
export function extractEndpointFieldErrors(error: unknown): Record<string, string> | null {
  if (!isApiError(error) || detailCode(error) !== "validation_failed") {
    return null;
  }
  const inner = innerDetail<{ fields?: unknown }>(error);
  const fields = inner?.fields;
  if (!fields || typeof fields !== "object") {
    return null;
  }
  const result: Record<string, string> = {};
  for (const [key, value] of Object.entries(fields)) {
    if (typeof value === "string") {
      result[key] = value;
    }
  }
  return Object.keys(result).length > 0 ? result : null;
}
