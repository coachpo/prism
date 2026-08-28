import { request } from "./request";
import type {
  RoutingDiagnosticRoute,
  RoutingDiagnosticsResponse,
} from "../types/routing-diagnostics";

export type { RoutingDiagnosticRoute, RoutingDiagnosticsResponse };

/**
 * Every read takes an optional signal. An abandoned panel that only ignores
 * its answer still holds a management admission slot until the query finishes,
 * so navigating away has to actually cancel, not just stop listening.
 */
export const modelRoutingDiagnostics = {
  get: (modelConfigId: number, signal?: AbortSignal) =>
    request<RoutingDiagnosticsResponse>(
      `/api/models/${modelConfigId}/routing-diagnostics`,
      { signal },
    ),
};
