import { request } from "./request";
import type { RouteExplanation } from "../types/route-explanation";
export const routeExplanation = {
  get: (modelId: number, operation: string, signal?: AbortSignal) =>
    request<RouteExplanation>(`/api/models/${modelId}/route-explanation?operation=${encodeURIComponent(operation)}`, { signal, cache: "no-store" }),
};
