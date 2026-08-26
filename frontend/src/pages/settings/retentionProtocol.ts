import { ApiError } from "@/lib/api";
import type { RetentionPreflightResponse } from "@/lib/types";

export function newRetentionOperationId(prefix: string) {
  return typeof crypto !== "undefined" &&
    typeof crypto.randomUUID === "function"
    ? crypto.randomUUID()
    : `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export function retentionPreflightFactsComplete(preflight: RetentionPreflightResponse | null) {
  return Boolean(
    preflight &&
      preflight.affected_domains.length > 0 &&
      preflight.affected_domains.every(
        (domain) => domain.impact.semantic_facts_complete,
      ),
  );
}

export function isStaleRetentionPreflightError(error: unknown) {
  return error instanceof ApiError && error.code === "retention_preflight_stale";
}
