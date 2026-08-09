import { z } from "zod"

export const MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET = "create-terminal-target"

export const modelDetailSearchSchema = z.object({
  // One-shot query-driven create action: opens the Terminal Target dialog with
  // an optional preselected endpoint, then clears itself (replace).
  action: z.literal(MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET).optional(),
  // Only legal together with action=create-terminal-target.
  endpoint_id: z.string().regex(/^\d+$/).optional(),
  // One-shot focus anchor: cleared (replace) after the target card focuses.
  focus_connection_id: z.string().regex(/^\d+$/).optional(),
})

export type ModelDetailSearch = z.input<typeof modelDetailSearchSchema>

// normalizeModelDetailSearch strips dead `tab` state and keeps only the
// supported one-shot parameters. Old `?tab=connections|events` URLs parse to
// an empty search and are rewritten to the canonical `/models/:id` route.
export function normalizeModelDetailSearch(value: unknown): ModelDetailSearch {
  return modelDetailSearchSchema.parse(value)
}
