import { z } from "zod"

export const MODEL_DETAIL_TABS = ["connections"] as const

export const MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET = "create-terminal-target" as const

export type ModelDetailTab = (typeof MODEL_DETAIL_TABS)[number]

export const DEFAULT_MODEL_DETAIL_TAB: ModelDetailTab = "connections"

// Every key falls back instead of throwing: a stale link or a hand-edited id
// must degrade to the default view rather than replace the whole page with an
// error boundary. `MODEL_DETAIL_FALLBACK_SEARCH_KEYS` is what the page reports
// as ignored, so the fallback stays visible instead of silent.
export const modelDetailSearchSchema = z.object({
  tab: z.enum(MODEL_DETAIL_TABS).catch(DEFAULT_MODEL_DETAIL_TAB),
  metrics_scope: z.enum(["ingress", "final_execution"]).optional().catch(undefined),
  focus_connection_id: z.string().regex(/^\d+$/).optional().catch(undefined),
  // Endpoint-page handoff (MC-A6): preselect and lock this endpoint for a new
  // Terminal Target; only the validated endpoint_id is carried, never secrets.
  action: z.literal("create-terminal-target").optional().catch(undefined),
  endpoint_id: z.string().regex(/^\d+$/).optional().catch(undefined),
})

export const MODEL_DETAIL_FALLBACK_SEARCH_KEYS = [
  "metrics_scope",
  "focus_connection_id",
  "action",
  "endpoint_id",
] as const

export type ModelDetailSearch = z.input<typeof modelDetailSearchSchema>

export function normalizeModelDetailTab(value: unknown): ModelDetailTab {
  return modelDetailSearchSchema.parse({ tab: value }).tab
}

export function normalizeModelDetailSearch(value: unknown): ModelDetailSearch {
  return modelDetailSearchSchema.parse(value ?? {})
}
