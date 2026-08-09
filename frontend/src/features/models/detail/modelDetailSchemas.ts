import { z } from "zod"

export const MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET = "create-terminal-target"

const positiveInteger = z.string().regex(/^[1-9]\d*$/)

export const modelDetailSearchSchema = z
  .object({
    // One-shot query-driven create action: opens the Terminal Target dialog
    // with an optional preselected endpoint, then clears itself (replace).
    action: z.literal(MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET).optional(),
    // An endpoint is only meaningful for the create action and must be a
    // positive database ID. The action without an endpoint remains valid.
    endpoint_id: positiveInteger.optional(),
    // One-shot focus anchor: cleared (replace) after the target card focuses.
    focus_connection_id: z.string().regex(/^\d+$/).optional(),
  })
  .superRefine((value, context) => {
    if (value.endpoint_id != null && value.action == null) {
      context.addIssue({
        code: "custom",
        path: ["endpoint_id"],
        message: "endpoint_id requires action=create-terminal-target",
      })
    }
  })

export type AttachTerminalTargetSearch = {
  action: typeof MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET
  endpoint_id: number
}

export function parseAttachTerminalTargetSearch(search: {
  action?: string
  endpoint_id?: string
}): AttachTerminalTargetSearch | null {
  const parsed = modelDetailSearchSchema.safeParse(search)
  if (!parsed.success) return null
  const value = parsed.data
  if (value.action !== MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET || value.endpoint_id == null) return null
  return {
    action: MODEL_DETAIL_ACTION_CREATE_TERMINAL_TARGET,
    endpoint_id: Number.parseInt(value.endpoint_id, 10),
  }
}

export type ModelDetailSearch = z.input<typeof modelDetailSearchSchema>

// normalizeModelDetailSearch strips dead `tab` state and keeps only the
// supported one-shot parameters. Old `?tab=connections|events` URLs parse to
// an empty search and are rewritten to the canonical `/models/:id` route.
export function normalizeModelDetailSearch(value: unknown): ModelDetailSearch {
  return modelDetailSearchSchema.parse(value)
}
