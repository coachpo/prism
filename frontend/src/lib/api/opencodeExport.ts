import type {
  OpenCodeExportRenderRequest,
  OpenCodeExportRenderResponse,
  OpenCodeExportSourceResponse,
} from "@/lib/types";
import { request } from "./request";

export function fetchOpenCodeExportSource(
  signal?: AbortSignal,
): Promise<OpenCodeExportSourceResponse> {
  return request("/api/models/exports/opencode/source", {
    cache: "no-store",
    signal,
  });
}

/** Key-bearing output stays in the active export session, outside query caches. */
export function renderOpenCodeExport(
  body: OpenCodeExportRenderRequest,
  signal?: AbortSignal,
): Promise<OpenCodeExportRenderResponse> {
  return request("/api/models/exports/opencode/render", {
    method: "POST",
    cache: "no-store",
    signal,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}
