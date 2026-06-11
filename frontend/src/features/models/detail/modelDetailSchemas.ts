import { z } from "zod"

export const MODEL_DETAIL_TABS = ["connections", "events"] as const

export type ModelDetailTab = (typeof MODEL_DETAIL_TABS)[number]

export const DEFAULT_MODEL_DETAIL_TAB: ModelDetailTab = "connections"

export const modelDetailSearchSchema = z.object({
  tab: z.enum(MODEL_DETAIL_TABS).catch(DEFAULT_MODEL_DETAIL_TAB),
  focus_connection_id: z.string().regex(/^\d+$/).optional(),
})

export type ModelDetailSearch = z.input<typeof modelDetailSearchSchema>

export function normalizeModelDetailTab(value: unknown): ModelDetailTab {
  return modelDetailSearchSchema.parse({ tab: value }).tab
}
