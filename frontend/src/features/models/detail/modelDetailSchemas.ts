import { z } from "zod"

export const MODEL_DETAIL_TABS = ["connections"] as const

export const MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET = "create-terminal-target" as const

export type ModelDetailTab = (typeof MODEL_DETAIL_TABS)[number]

export const DEFAULT_MODEL_DETAIL_TAB: ModelDetailTab = "connections"

export const modelDetailSearchSchema = z.object({
  tab: z.enum(MODEL_DETAIL_TABS).catch(DEFAULT_MODEL_DETAIL_TAB),
  focus_connection_id: z.string().regex(/^\d+$/).optional(),
  // Endpoint-page handoff (MC-A6): preselect and lock this endpoint for a new
  // Terminal Target; only the validated endpoint_id is carried, never secrets.
  action: z.literal("create-terminal-target").optional(),
  endpoint_id: z.string().regex(/^\d+$/).optional(),
})

export type ModelDetailSearch = z.input<typeof modelDetailSearchSchema>

export function normalizeModelDetailTab(value: unknown): ModelDetailTab {
  return modelDetailSearchSchema.parse({ tab: value }).tab
}

export function normalizeModelDetailSearch(value: unknown): ModelDetailSearch {
  return modelDetailSearchSchema.parse(value ?? {})
}
