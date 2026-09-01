import { getStaticMessages } from "@/i18n/staticMessages";
import { ApiError } from "@/lib/api/request";

export const UPSTREAM_MODEL_ID_MAX_LENGTH = 200;

export type UpstreamModelIdFieldIssue = "required" | "too_long";

export function validateUpstreamModelIdField(
  value: string | null | undefined,
  required: boolean,
): UpstreamModelIdFieldIssue | null {
  const normalized = value?.trim() ?? "";
  if (required && normalized.length === 0) return "required";
  if ([...normalized].length > UPSTREAM_MODEL_ID_MAX_LENGTH) return "too_long";
  return null;
}

export function upstreamModelIdIssueFromError(
  error: unknown,
): UpstreamModelIdFieldIssue | null {
  if (!(error instanceof ApiError) || error.status !== 422) return null;
  const body = error.detail;
  if (!body || typeof body !== "object" || Array.isArray(body)) return null;

  const fields = body as Record<string, unknown>;
  if (fields.field !== "upstream_model_id") return null;
  return fields.reason === "too_long" ? "too_long" : "required";
}

export function upstreamModelIdIssueMessage(
  issue: UpstreamModelIdFieldIssue,
  mode: "create" | "edit",
): string {
  const copy = getStaticMessages().modelDetail;
  if (issue === "too_long") return copy.upstreamModelIdTooLong;
  return mode === "edit"
    ? copy.upstreamModelIdRequired
    : copy.upstreamModelIdBlank;
}
